package ptyd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/pty"
	"github.com/krystophny/tabura/internal/serve"
)

const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 9333
)

type session struct {
	id      string
	cwd     string
	created time.Time
	pty     pty.Transport
	clients map[*websocket.Conn]struct{}
	mu      sync.Mutex
}

type App struct {
	dataDir  string
	sessions map[string]*session
	mu       sync.Mutex
	upgrader websocket.Upgrader
	httpSrv  *http.Server
}

func New(dataDir string) *App {
	return &App{
		dataDir:  dataDir,
		sessions: map[string]*session{},
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
	}
}

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	r.Post("/api/pty/open", a.handleOpen)
	r.Post("/api/pty/close", a.handleClose)
	r.Get("/api/pty/list", a.handleList)
	r.Get("/api/health", a.handleHealth)
	r.Get("/ws/pty/{session_id}", a.handleWS)
	return r
}

func (a *App) handleOpen(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		Cols      int    `json:"cols"`
		Rows      int    `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" || req.CWD == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	s, ok := a.sessions[req.SessionID]
	a.mu.Unlock()
	if ok {
		if req.Cols > 0 && req.Rows > 0 {
			_ = s.pty.Resize(req.Cols, req.Rows)
		}
		writeJSON(w, map[string]interface{}{"session_id": s.id, "created": false, "created_at": s.created.Unix()})
		return
	}
	p, err := pty.OpenLocal(req.CWD)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Cols > 0 && req.Rows > 0 {
		_ = p.Resize(req.Cols, req.Rows)
	}
	s = &session{id: req.SessionID, cwd: req.CWD, created: time.Now(), pty: p, clients: map[*websocket.Conn]struct{}{}}
	a.mu.Lock()
	a.sessions[req.SessionID] = s
	a.mu.Unlock()
	go a.readLoop(s)
	writeJSON(w, map[string]interface{}{"session_id": s.id, "created": true, "created_at": s.created.Unix()})
}

func (a *App) readLoop(s *session) {
	_ = s.pty.ReadLoop(func(data []byte) error {
		s.mu.Lock()
		clients := make([]*websocket.Conn, 0, len(s.clients))
		for ws := range s.clients {
			clients = append(clients, ws)
		}
		s.mu.Unlock()
		for _, ws := range clients {
			_ = ws.WriteMessage(websocket.BinaryMessage, data)
		}
		return nil
	})
	a.mu.Lock()
	delete(a.sessions, s.id)
	a.mu.Unlock()
	s.mu.Lock()
	for ws := range s.clients {
		_ = ws.Close()
	}
	s.clients = map[*websocket.Conn]struct{}{}
	s.mu.Unlock()
	_ = s.pty.Close()
}

func (a *App) handleClose(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.SessionID == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	s, ok := a.sessions[req.SessionID]
	if ok {
		delete(a.sessions, req.SessionID)
	}
	a.mu.Unlock()
	if !ok {
		writeJSON(w, map[string]interface{}{"closed": false, "reason": "not_found"})
		return
	}
	_ = s.pty.Close()
	s.mu.Lock()
	for ws := range s.clients {
		_ = ws.Close()
	}
	s.clients = map[*websocket.Conn]struct{}{}
	s.mu.Unlock()
	writeJSON(w, map[string]interface{}{"closed": true, "session_id": req.SessionID})
}

func (a *App) handleList(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []map[string]interface{}{}
	for _, s := range a.sessions {
		s.mu.Lock()
		clients := len(s.clients)
		s.mu.Unlock()
		out = append(out, map[string]interface{}{"session_id": s.id, "cwd": s.cwd, "clients": clients, "created_at": s.created.Unix()})
	}
	writeJSON(w, map[string]interface{}{"sessions": out})
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	count := len(a.sessions)
	a.mu.Unlock()
	writeJSON(w, map[string]interface{}{"status": "ok", "sessions": count})
}

func (a *App) handleWS(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "session_id")
	a.mu.Lock()
	s, ok := a.sessions[sid]
	a.mu.Unlock()
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	ws, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.clients[ws] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ws)
		s.mu.Unlock()
		_ = ws.Close()
	}()
	for {
		mt, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.BinaryMessage:
			_ = s.pty.Write(msg)
		case websocket.TextMessage:
			var p map[string]interface{}
			if json.Unmarshal(msg, &p) == nil {
				if typ, _ := p["type"].(string); typ == "resize" {
					cols := intFromAny(p["cols"], 120)
					rows := intFromAny(p["rows"], 40)
					_ = s.pty.Resize(cols, rows)
					continue
				}
			}
			_ = s.pty.Write(msg)
		}
	}
}

func intFromAny(v interface{}, d int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	default:
		return d
	}
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (a *App) Start(host string, port int) error {
	a.httpSrv = &http.Server{Addr: fmt.Sprintf("%s:%d", host, port), Handler: a.Router(), ReadHeaderTimeout: 15 * time.Second}
	fmt.Println("tabura ptyd listening on:")
	for _, u := range serve.ListenURLs(host, port) {
		fmt.Printf("  %s\n", u)
	}
	fmt.Printf("  data dir: %s\n", a.dataDir)
	fmt.Printf("  boot id:  %d\n", time.Now().UnixNano())
	err := a.httpSrv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *App) Stop(ctx context.Context) error {
	if a.httpSrv == nil {
		return nil
	}
	return a.httpSrv.Shutdown(ctx)
}
