package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/serve"
)

func (a *App) handleCanvasWS(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sid := chi.URLParam(r, "session_id")
	ws, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.hub.registerCanvas(sid, ws)
	remote := a.tunnels.getRemoteCanvas(sid)

	defer func() {
		a.hub.unregisterCanvas(sid, ws)
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
	port, ok := a.tunnels.getPort(sid)
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

func (a *App) mcpToolsCall(port int, name string, arguments map[string]interface{}) (map[string]interface{}, error) {
	return mcpToolsCallURL(fmt.Sprintf("http://127.0.0.1:%d/mcp", port), name, arguments)
}

func mcpToolsCallURL(mcpURL, name string, arguments map[string]interface{}) (map[string]interface{}, error) {
	payload := map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]interface{}{"name": name, "arguments": arguments}}
	b, _ := json.Marshal(payload)
	ctx, cancel := context.WithTimeout(context.Background(), mcpToolsCallTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
			return nil, fmt.Errorf("MCP call timed out after %s", mcpToolsCallTimeout)
		}
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
	if isErr, _ := result["isError"].(bool); isErr {
		return nil, fmt.Errorf("MCP tool %q failed: %s", name, mcpResultErrorText(result))
	}
	sc, _ := result["structuredContent"].(map[string]interface{})
	if sc == nil {
		return nil, errors.New("MCP call failed: missing structuredContent")
	}
	return sc, nil
}

func mcpResultErrorText(result map[string]interface{}) string {
	if result == nil {
		return "unknown error"
	}
	content, _ := result["content"].([]interface{})
	parts := make([]string, 0, len(content))
	for _, item := range content {
		entry, _ := item.(map[string]interface{})
		if entry == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(entry["text"]))
		if text == "" || text == "<nil>" {
			continue
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return "unknown error"
	}
	return strings.Join(parts, " | ")
}

func checkWSOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.TrimSpace(u.Hostname()) == "" {
		return false
	}
	requestScheme := "http"
	if isHTTPS(r) {
		requestScheme = "https"
	}
	if !strings.EqualFold(strings.TrimSpace(u.Scheme), requestScheme) {
		return false
	}
	originHost, originPort := hostPortForScheme(u.Host, u.Scheme)
	requestHost, requestPort := hostPortForScheme(r.Host, requestScheme)
	if originHost == "" || requestHost == "" {
		return false
	}
	return strings.EqualFold(originHost, requestHost) && originPort == requestPort
}

func hostPortForScheme(rawHost, scheme string) (string, string) {
	ref := strings.TrimSpace(rawHost)
	if ref == "" {
		return "", ""
	}
	parsed, err := url.Parse("//" + ref)
	if err != nil {
		return "", ""
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", ""
	}
	port := strings.TrimSpace(parsed.Port())
	if port == "" {
		if strings.EqualFold(strings.TrimSpace(scheme), "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	return host, port
}

func (a *App) handleFilesProxy(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	// Files are rendered inside same-origin canvas panes (image/PDF), so this
	// route must allow same-origin embedding instead of the global DENY policy.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; "+
			"script-src 'self' 'wasm-unsafe-eval'; "+
			"style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; "+
			"connect-src 'self' ws: wss:; "+
			"frame-ancestors 'self'; "+
			"base-uri 'none'; "+
			"form-action 'self'")
	sid := chi.URLParam(r, "session_id")
	rawPath := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	filePath, err := url.PathUnescape(rawPath)
	if err != nil {
		http.Error(w, "invalid path encoding", http.StatusBadRequest)
		return
	}
	filePath = strings.TrimPrefix(filePath, "/")
	if filePath == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	if strings.Contains(filePath, "..") || strings.ContainsRune(filePath, '\x00') {
		http.Error(w, "invalid path", http.StatusForbidden)
		return
	}
	port, ok := a.tunnels.getPort(sid)
	if !ok {
		http.Error(w, "no active tunnel for session", http.StatusNotFound)
		return
	}
	upstreamURL := (&url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", port),
		Path:   "/files/" + filePath,
	}).String()
	resp, err := http.Get(upstreamURL)
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
	ctx := a.tunnels.replaceRelayCancel(sessionID)

	go func() {
		defer func() {
			a.tunnels.deleteRelayCancel(sessionID)
			a.tunnels.deleteRemoteCanvas(sessionID)
		}()

		wsURL := fmt.Sprintf("ws://127.0.0.1:%d/ws/canvas", port)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			return
		}
		a.tunnels.setRemoteCanvas(sessionID, conn)

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
			for _, ws := range a.hub.canvasClients(sessionID) {
				_ = ws.WriteMessage(websocket.TextMessage, msg)
			}
		}
	}()
}

func (a *App) startLocalServe() error {
	if a.tunnels.hasPort(LocalSessionID) {
		return nil
	}
	if a.localProjectDir == "" {
		return nil
	}
	if a.localMCPURL != "" {
		port, err := extractPort(a.localMCPURL)
		if err != nil {
			return err
		}
		a.tunnels.setPort(LocalSessionID, port)
		a.startCanvasRelay(LocalSessionID, port)
		return nil
	}

	app := serve.NewApp(a.localProjectDir)
	ctx, cancel := context.WithCancel(context.Background())
	a.tunnels.setLocalServe(app, cancel)
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
				a.tunnels.setPort(LocalSessionID, DaemonPort)
				a.startCanvasRelay(LocalSessionID, DaemonPort)
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("local tabura MCP listener did not become healthy in time")
}
