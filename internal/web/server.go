package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabula/internal/pty"
	"github.com/krystophny/tabula/internal/serve"
	"github.com/krystophny/tabula/internal/store"
)

const (
	DefaultHost           = "127.0.0.1"
	DefaultPort           = 8420
	SessionCookie         = "tabula_session"
	cookieMaxAgeSec       = 60 * 60 * 24 * 365
	DaemonPort            = 9420
	LocalSessionID        = "local"
	defaultProducerMCPURL = "http://127.0.0.1:8090/mcp"
)

//go:embed static/* static/vendor/*
var staticFiles embed.FS

type App struct {
	dataDir         string
	localProjectDir string
	localMCPURL     string
	ptydURL         string
	devRuntime      bool

	store *store.Store

	upgrader websocket.Upgrader

	mu               sync.Mutex
	terminalWS       map[string]*websocket.Conn
	canvasWS         map[string]map[*websocket.Conn]struct{}
	remoteCanvasWS   map[string]*websocket.Conn
	tunnelPorts      map[string]int
	relayCancel      map[string]context.CancelFunc
	localServe       *serve.App
	localServeCancel context.CancelFunc

	bootID    string
	startedAt string
}

func New(dataDir, localProjectDir, localMCPURL, ptydURL string, devRuntime bool) (*App, error) {
	s, err := store.New(pathJoin(dataDir, "tabula.db"))
	if err != nil {
		return nil, err
	}
	return &App{
		dataDir:         dataDir,
		localProjectDir: localProjectDir,
		localMCPURL:     localMCPURL,
		ptydURL:         ptydURL,
		devRuntime:      devRuntime,
		store:           s,
		upgrader:        websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		terminalWS:      map[string]*websocket.Conn{},
		canvasWS:        map[string]map[*websocket.Conn]struct{}{},
		remoteCanvasWS:  map[string]*websocket.Conn{},
		tunnelPorts:     map[string]int{},
		relayCancel:     map[string]context.CancelFunc{},
		bootID:          strconv.FormatInt(time.Now().UnixNano(), 16),
		startedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

func pathJoin(parts ...string) string {
	return strings.Join(parts, "/")
}

func randomToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 16) + "-" + strconv.FormatInt(time.Now().Unix()%99991, 16)
}

func (a *App) setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   cookieMaxAgeSec,
	})
}

func (a *App) clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func (a *App) hasAuth(r *http.Request) bool {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return false
	}
	return a.store.HasAuthSession(c.Value)
}

func (a *App) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if !a.hasAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	// auth/setup
	r.Get("/api/setup", a.handleSetupCheck)
	r.Post("/api/setup", a.handleSetupPassword)
	r.Post("/api/login", a.handleLogin)
	r.Post("/api/logout", a.handleLogout)

	// hosts
	r.Get("/api/hosts", a.handleHostsList)
	r.Post("/api/hosts", a.handleHostsCreate)
	r.Get("/api/hosts/{id}", a.handleHostsGet)
	r.Put("/api/hosts/{id}", a.handleHostsUpdate)
	r.Delete("/api/hosts/{id}", a.handleHostsDelete)

	// sessions/runtime
	r.Post("/api/connect", a.handleConnect)
	r.Post("/api/disconnect", a.handleDisconnect)
	r.Get("/api/sessions", a.handleSessions)
	r.Get("/api/runtime", a.handleRuntime)
	r.Post("/api/daemon/start", a.handleDaemonStart)

	// canvas/file proxy
	r.Get("/api/canvas/{session_id}/snapshot", a.handleCanvasSnapshot)
	r.Get("/api/files/{session_id}/*", a.handleFilesProxy)
	r.Post("/api/mail/action-capabilities", a.handleMailActionCapabilities)
	r.Post("/api/mail/action", a.handleMailAction)

	// ws
	r.Get("/ws/terminal/{session_id}", a.handleTerminalWS)
	r.Get("/ws/canvas/{session_id}", a.handleCanvasWS)

	// static
	r.Get("/", a.serveIndex)
	r.Get("/canvas", a.serveCanvas)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSubFS()))))
	return securityHeaders(r)
}

func staticSubFS() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return staticFiles
	}
	return sub
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *App) serveIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "web client not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (a *App) serveCanvas(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/?desktop=1", http.StatusTemporaryRedirect)
}

func decodeJSON(r *http.Request, out interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func (a *App) handleSetupCheck(w http.ResponseWriter, r *http.Request) {
	res := map[string]interface{}{
		"has_password":  a.store.HasAdminPassword(),
		"authenticated": a.hasAuth(r),
	}
	if a.localProjectDir != "" {
		res["local_session"] = LocalSessionID
	}
	writeJSON(w, res)
}

func (a *App) handleSetupPassword(w http.ResponseWriter, r *http.Request) {
	if a.store.HasAdminPassword() {
		http.Error(w, "admin password already set", http.StatusConflict)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.store.SetAdminPassword(req.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token := randomToken()
	_ = a.store.AddAuthSession(token)
	a.setAuthCookie(w, token)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if !a.store.VerifyAdminPassword(req.Password) {
		time.Sleep(1 * time.Second)
		http.Error(w, "invalid password", http.StatusUnauthorized)
		return
	}
	token := randomToken()
	_ = a.store.AddAuthSession(token)
	a.setAuthCookie(w, token)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = a.store.DeleteAuthSession(c.Value)
	}
	a.clearAuthCookie(w)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handleHostsList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	hosts, err := a.store.ListHosts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, hosts)
}

func (a *App) handleHostsCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req store.HostConfig
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	h, err := a.store.AddHost(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, h)
}

func parseID(r *http.Request) (int, error) {
	idStr := chi.URLParam(r, "id")
	return strconv.Atoi(idStr)
}

func (a *App) handleHostsGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	id, err := parseID(r)
	if err != nil {
		http.Error(w, "invalid host id", http.StatusBadRequest)
		return
	}
	h, err := a.store.GetHost(id)
	if err != nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}
	writeJSON(w, h)
}

func (a *App) handleHostsUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	id, err := parseID(r)
	if err != nil {
		http.Error(w, "invalid host id", http.StatusBadRequest)
		return
	}
	var updates map[string]interface{}
	if err := decodeJSON(r, &updates); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	h, err := a.store.UpdateHost(id, updates)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, h)
}

func (a *App) handleHostsDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	id, err := parseID(r)
	if err != nil {
		http.Error(w, "invalid host id", http.StatusBadRequest)
		return
	}
	_ = a.store.DeleteHost(id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleConnect(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	http.Error(w, "SSH remote sessions are not yet implemented in Go port", http.StatusNotImplemented)
}

func (a *App) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	if ws := a.terminalWS[req.SessionID]; ws != nil {
		_ = ws.Close()
		delete(a.terminalWS, req.SessionID)
	}
	if set := a.canvasWS[req.SessionID]; set != nil {
		for ws := range set {
			_ = ws.Close()
		}
		delete(a.canvasWS, req.SessionID)
	}
	if cancel := a.relayCancel[req.SessionID]; cancel != nil {
		cancel()
		delete(a.relayCancel, req.SessionID)
	}
	if rc := a.remoteCanvasWS[req.SessionID]; rc != nil {
		_ = rc.Close()
		delete(a.remoteCanvasWS, req.SessionID)
	}
	delete(a.tunnelPorts, req.SessionID)
	a.mu.Unlock()
	_ = a.store.DeleteRemoteSession(req.SessionID)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	res := map[string]interface{}{"sessions": []string{}}
	if a.localProjectDir != "" {
		mcpURL := a.localMCPURL
		if mcpURL == "" {
			mcpURL = fmt.Sprintf("http://127.0.0.1:%d/mcp", DaemonPort)
		}
		res["local_session"] = map[string]interface{}{"session_id": LocalSessionID, "project_dir": a.localProjectDir, "mcp_url": mcpURL}
	}
	writeJSON(w, res)
}

func (a *App) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	writeJSON(w, map[string]interface{}{
		"boot_id":       a.bootID,
		"started_at":    a.startedAt,
		"version":       "0.3.0",
		"dev_mode":      a.devRuntime,
		"ptyd_url":      emptyToNil(a.ptydURL),
		"local_mcp_url": emptyToNil(a.localMCPURL),
	})
}

func emptyToNil(v string) interface{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

func (a *App) handleDaemonStart(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		SessionID string `json:"session_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	port, ok := a.tunnelPorts[req.SessionID]
	a.mu.Unlock()
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"tunnel_port": port, "status": "running"})
}

func (a *App) createTerminalTransport(sessionID string) (pty.Transport, error) {
	if sessionID != LocalSessionID || a.localProjectDir == "" {
		return nil, errors.New("session not found")
	}
	if a.ptydURL != "" {
		return pty.OpenPtyd(a.ptydURL, sessionID, a.localProjectDir, 120, 40)
	}
	return pty.OpenLocal(a.localProjectDir)
}

func (a *App) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sid := chi.URLParam(r, "session_id")
	ws, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	tr, err := a.createTerminalTransport(sid)
	if err != nil {
		_ = ws.Close()
		return
	}
	a.mu.Lock()
	a.terminalWS[sid] = ws
	a.mu.Unlock()
	defer func() {
		_ = tr.Close()
		_ = ws.Close()
		a.mu.Lock()
		if a.terminalWS[sid] == ws {
			delete(a.terminalWS, sid)
		}
		a.mu.Unlock()
	}()

	done := make(chan struct{})
	go func() {
		_ = tr.ReadLoop(func(data []byte) error {
			return ws.WriteMessage(websocket.BinaryMessage, data)
		})
		close(done)
	}()

	for {
		select {
		case <-done:
			return
		default:
		}
		mt, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.BinaryMessage:
			_ = tr.Write(msg)
		case websocket.TextMessage:
			var payload map[string]interface{}
			if json.Unmarshal(msg, &payload) == nil {
				if typ, _ := payload["type"].(string); typ == "resize" {
					cols := intFromAny(payload["cols"], 120)
					rows := intFromAny(payload["rows"], 40)
					_ = tr.Resize(cols, rows)
					continue
				}
			}
			_ = tr.Write(msg)
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

func (a *App) handleCanvasWS(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sid := chi.URLParam(r, "session_id")
	ws, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.mu.Lock()
	if a.canvasWS[sid] == nil {
		a.canvasWS[sid] = map[*websocket.Conn]struct{}{}
	}
	a.canvasWS[sid][ws] = struct{}{}
	remote := a.remoteCanvasWS[sid]
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		if set := a.canvasWS[sid]; set != nil {
			delete(set, ws)
		}
		a.mu.Unlock()
		_ = ws.Close()
	}()

	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if remote != nil {
			_ = remote.WriteMessage(websocket.TextMessage, msg)
		}
	}
}

func (a *App) handleCanvasSnapshot(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sid := chi.URLParam(r, "session_id")
	a.mu.Lock()
	port, ok := a.tunnelPorts[sid]
	a.mu.Unlock()
	if !ok {
		http.Error(w, "no active tunnel for session", http.StatusNotFound)
		return
	}
	status, err := a.mcpToolsCall(port, "canvas_status", map[string]interface{}{"session_id": sid})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	event, _ := status["active_artifact"].(map[string]interface{})
	writeJSON(w, map[string]interface{}{"status": status, "event": event})
}

func (a *App) handleMailActionCapabilities(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		Provider       string `json:"provider"`
		ProducerMCPURL string `json:"producer_mcp_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	mcpURL, err := normalizeProducerMCPURL(req.ProducerMCPURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := mcpToolsCallURL(mcpURL, "email_action_capabilities", map[string]interface{}{"provider": req.Provider})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, resp)
}

func (a *App) handleMailAction(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		Provider       string `json:"provider"`
		Action         string `json:"action"`
		MessageID      string `json:"message_id"`
		UntilAt        string `json:"until_at"`
		ProducerMCPURL string `json:"producer_mcp_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Action) == "" || strings.TrimSpace(req.MessageID) == "" {
		http.Error(w, "action and message_id are required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.UntilAt) != "" {
		if _, err := time.Parse(time.RFC3339, req.UntilAt); err != nil {
			http.Error(w, "until_at must be RFC3339", http.StatusBadRequest)
			return
		}
	}
	mcpURL, err := normalizeProducerMCPURL(req.ProducerMCPURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	args := map[string]interface{}{
		"provider":   req.Provider,
		"action":     req.Action,
		"message_id": req.MessageID,
	}
	if strings.TrimSpace(req.UntilAt) != "" {
		args["until_at"] = req.UntilAt
	}
	resp, err := mcpToolsCallURL(mcpURL, "email_action", args)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, resp)
}

func (a *App) mcpToolsCall(port int, name string, arguments map[string]interface{}) (map[string]interface{}, error) {
	return mcpToolsCallURL(fmt.Sprintf("http://127.0.0.1:%d/mcp", port), name, arguments)
}

func mcpToolsCallURL(mcpURL, name string, arguments map[string]interface{}) (map[string]interface{}, error) {
	payload := map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]interface{}{"name": name, "arguments": arguments}}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(mcpURL, "application/json", strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("MCP call failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if e, ok := out["error"].(map[string]interface{}); ok {
		return nil, fmt.Errorf("MCP error: %v", e["message"])
	}
	result, _ := out["result"].(map[string]interface{})
	sc, _ := result["structuredContent"].(map[string]interface{})
	if sc == nil {
		return nil, errors.New("MCP call failed: missing structuredContent")
	}
	return sc, nil
}

func normalizeProducerMCPURL(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		candidate = defaultProducerMCPURL
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid producer_mcp_url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("producer_mcp_url must use http or https")
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("producer_mcp_url must include host")
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("producer_mcp_url host must be loopback")
	}
	if strings.TrimSpace(u.Path) == "" || u.Path == "/" {
		u.Path = "/mcp"
	}
	if u.Path != "/mcp" {
		return "", fmt.Errorf("producer_mcp_url path must be /mcp")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("producer_mcp_url must not include query or fragment")
	}
	return u.String(), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *App) handleFilesProxy(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sid := chi.URLParam(r, "session_id")
	filePath := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	if strings.Contains(filePath, "..") || strings.ContainsRune(filePath, '\x00') {
		http.Error(w, "invalid path", http.StatusForbidden)
		return
	}
	a.mu.Lock()
	port, ok := a.tunnelPorts[sid]
	a.mu.Unlock()
	if !ok {
		http.Error(w, "no active tunnel for session", http.StatusNotFound)
		return
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/files/%s", port, filePath)
	resp, err := http.Get(url)
	if err != nil {
		http.Error(w, "file fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		if strings.EqualFold(k, "Content-Type") {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func extractPort(raw string) (int, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return 0, err
	}
	p := u.Port()
	if p == "" {
		if u.Scheme == "https" {
			return 443, nil
		}
		if u.Scheme == "http" {
			return 80, nil
		}
		return 0, errors.New("cannot infer port")
	}
	return strconv.Atoi(p)
}

func (a *App) startCanvasRelay(sessionID string, port int) {
	a.mu.Lock()
	if cancel := a.relayCancel[sessionID]; cancel != nil {
		cancel()
		delete(a.relayCancel, sessionID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.relayCancel[sessionID] = cancel
	a.mu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			delete(a.relayCancel, sessionID)
			if rc := a.remoteCanvasWS[sessionID]; rc != nil {
				_ = rc.Close()
				delete(a.remoteCanvasWS, sessionID)
			}
			a.mu.Unlock()
		}()

		wsURL := fmt.Sprintf("ws://127.0.0.1:%d/ws/canvas", port)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			return
		}
		a.mu.Lock()
		a.remoteCanvasWS[sessionID] = conn
		a.mu.Unlock()

		for {
			select {
			case <-ctx.Done():
				_ = conn.Close()
				return
			default:
			}
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if mt != websocket.TextMessage {
				continue
			}
			a.mu.Lock()
			clients := make([]*websocket.Conn, 0)
			for ws := range a.canvasWS[sessionID] {
				clients = append(clients, ws)
			}
			a.mu.Unlock()
			for _, ws := range clients {
				_ = ws.WriteMessage(websocket.TextMessage, msg)
			}
		}
	}()
}

func (a *App) startLocalServe() error {
	if a.localProjectDir == "" {
		return nil
	}
	if a.localMCPURL != "" {
		port, err := extractPort(a.localMCPURL)
		if err != nil {
			return err
		}
		a.mu.Lock()
		a.tunnelPorts[LocalSessionID] = port
		a.mu.Unlock()
		a.startCanvasRelay(LocalSessionID, port)
		return nil
	}

	app := serve.NewApp(a.localProjectDir, true)
	a.localServe = app
	ctx, cancel := context.WithCancel(context.Background())
	a.localServeCancel = cancel
	go func() {
		_ = app.Start("127.0.0.1", DaemonPort)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return errors.New("local serve canceled")
		default:
		}
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", DaemonPort))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				a.mu.Lock()
				a.tunnelPorts[LocalSessionID] = DaemonPort
				a.mu.Unlock()
				a.startCanvasRelay(LocalSessionID, DaemonPort)
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("local tabula serve did not become healthy in time")
}

func (a *App) Start(host string, port int) error {
	if err := a.startLocalServe(); err != nil {
		return err
	}
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", host, port), Handler: a.Router(), ReadHeaderTimeout: 15 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	fmt.Println("tabula web listening on:")
	for _, u := range serve.ListenURLs(host, port) {
		fmt.Printf("  %s\n", u)
	}
	if a.localProjectDir != "" {
		mcpURL := a.localMCPURL
		if mcpURL == "" {
			mcpURL = fmt.Sprintf("http://127.0.0.1:%d/mcp", DaemonPort)
		}
		fmt.Printf("  local project: %s\n", a.localProjectDir)
		fmt.Printf("  local MCP:     %s\n", mcpURL)
	}
	if a.ptydURL != "" {
		fmt.Printf("  local PTYD:    %s\n", a.ptydURL)
	}
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *App) Shutdown(ctx context.Context) error {
	a.mu.Lock()
	for _, cancel := range a.relayCancel {
		cancel()
	}
	for _, ws := range a.remoteCanvasWS {
		_ = ws.Close()
	}
	a.mu.Unlock()
	if a.localServe != nil {
		_ = a.localServe.Stop(ctx)
	}
	if a.localServeCancel != nil {
		a.localServeCancel()
	}
	return a.store.Close()
}
