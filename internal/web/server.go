package web

import (
	"bytes"
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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/serve"
	"github.com/krystophny/tabura/internal/store"
)

const (
	DefaultHost                 = "127.0.0.1"
	DefaultPort                 = 8420
	DefaultAppServerURL         = "ws://127.0.0.1:8787"
	SessionCookie               = "tabura_session"
	cookieMaxAgeSec             = 60 * 60 * 24 * 365
	DaemonPort                  = 9420
	LocalSessionID              = "local"
	DefaultSparkReasoningEffort = "low"
	SparkModel                  = modelprofile.ModelSpark
	mcpToolsCallTimeout         = 45 * time.Second
	appStateDefaultChatModelKey = "default_chat_model"
)

//go:embed static/* static/vendor/*
var staticFiles embed.FS

type App struct {
	dataDir                       string
	localProjectDir               string
	localMCPURL                   string
	appServerURL                  string
	appServerModel                string
	appServerSparkReasoningEffort string
	intentClassifierURL           string
	intentLLMURL                  string
	ttsURL                        string
	devRuntime                    bool

	store *store.Store

	appServerClient *appserver.Client

	upgrader websocket.Upgrader

	mu                 sync.Mutex
	canvasWS           map[string]map[*websocket.Conn]struct{}
	chatWS             map[string]map[*chatWSConn]struct{}
	chatTurnCancel     map[string]map[string]context.CancelFunc
	chatTurnQueue      map[string]int
	chatTurnOutputMode map[string][]string
	chatTurnWorker     map[string]bool
	chatAppSessions    map[string]*appserver.Session
	remoteCanvasWS     map[string]*websocket.Conn
	tunnelPorts        map[string]int
	relayCancel        map[string]context.CancelFunc
	localServe         *serve.App
	localServeCancel   context.CancelFunc
	projectServes      map[string]*serve.App
	projectServeStop   map[string]context.CancelFunc
	ghCommandRunner ghCommandRunner

	bootID    string
	startedAt string
}

const DefaultModel = modelprofile.ModelSpark

func New(dataDir, localProjectDir, localMCPURL, appServerURL, model, ttsURL, sparkReasoningEffort string, devRuntime bool) (*App, error) {
	s, err := store.New(filepath.Join(dataDir, "tabura.db"))
	if err != nil {
		return nil, err
	}
	appServerURL = strings.TrimSpace(appServerURL)
	var appServerClient *appserver.Client
	if appServerURL != "" {
		appServerClient, err = appserver.NewClient(appServerURL)
		if err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	resolvedModel := strings.TrimSpace(model)
	if resolvedModel == "" {
		resolvedModel = strings.TrimSpace(os.Getenv("TABURA_APP_SERVER_MODEL"))
	}
	if resolvedModel == "" {
		resolvedModel = persistedDefaultChatModel(s)
	}
	if resolvedModel == "" {
		resolvedModel = DefaultModel
	}
	resolvedModel = enforceSparkModel(resolvedModel)
	if strings.TrimSpace(sparkReasoningEffort) == "" {
		sparkReasoningEffort = strings.TrimSpace(os.Getenv("TABURA_APP_SERVER_SPARK_REASONING_EFFORT"))
	}
	resolvedSparkReasoningEffort := resolveSparkReasoningEffort(strings.TrimSpace(sparkReasoningEffort))
	resolvedTTSURL := strings.TrimSpace(ttsURL)
	if resolvedTTSURL == "" {
		resolvedTTSURL = strings.TrimSpace(os.Getenv("TABURA_TTS_URL"))
	}
	resolvedIntentClassifierURL := strings.TrimSpace(os.Getenv("TABURA_INTENT_CLASSIFIER_URL"))
	if strings.EqualFold(resolvedIntentClassifierURL, "off") {
		resolvedIntentClassifierURL = ""
	}
	if resolvedIntentClassifierURL == "" {
		resolvedIntentClassifierURL = DefaultIntentClassifierURL
	}
	resolvedIntentLLMURL := strings.TrimSpace(os.Getenv("TABURA_INTENT_LLM_URL"))
	if strings.EqualFold(resolvedIntentLLMURL, "off") {
		resolvedIntentLLMURL = ""
	}
	if resolvedIntentLLMURL == "" {
		resolvedIntentLLMURL = DefaultIntentLLMURL
	}
	if err := s.SetAppState(appStateDefaultChatModelKey, modelprofile.AliasSpark); err != nil {
		_ = s.Close()
		return nil, err
	}
	app := &App{
		dataDir:                       dataDir,
		localProjectDir:               localProjectDir,
		localMCPURL:                   localMCPURL,
		appServerURL:                  appServerURL,
		appServerModel:                resolvedModel,
		appServerSparkReasoningEffort: resolvedSparkReasoningEffort,
		intentClassifierURL:           resolvedIntentClassifierURL,
		intentLLMURL:                  resolvedIntentLLMURL,
		ttsURL:                        resolvedTTSURL,
		devRuntime:                    devRuntime,
		store:                         s,
		appServerClient:               appServerClient,
		upgrader:                      websocket.Upgrader{CheckOrigin: checkWSOrigin},
		canvasWS:                      map[string]map[*websocket.Conn]struct{}{},
		chatWS:                        map[string]map[*chatWSConn]struct{}{},
		chatTurnCancel:                map[string]map[string]context.CancelFunc{},
		chatTurnQueue:                 map[string]int{},
		chatTurnOutputMode:            map[string][]string{},
		chatTurnWorker:                map[string]bool{},
		chatAppSessions:               map[string]*appserver.Session{},
		remoteCanvasWS:                map[string]*websocket.Conn{},
		tunnelPorts:                   map[string]int{},
		relayCancel:                   map[string]context.CancelFunc{},
		projectServes:                 map[string]*serve.App{},
		projectServeStop:              map[string]context.CancelFunc{},
		ghCommandRunner: runGitHubCLI,
		bootID:                        strconv.FormatInt(time.Now().UnixNano(), 16),
		startedAt:                     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := app.ensureDefaultProjectRecord(); err != nil {
		_ = s.Close()
		return nil, err
	}
	if _, err := app.ensureHubProject(); err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := app.ensurePromptContractFresh(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return app, nil
}

func persistedDefaultChatModel(s *store.Store) string {
	if s == nil {
		return ""
	}
	modelValue, err := s.AppState(appStateDefaultChatModelKey)
	if err != nil || strings.TrimSpace(modelValue) == "" {
		return ""
	}
	return modelprofile.ResolveModel(modelValue, "")
}

func randomToken() string {
	return strconv.FormatInt(time.Now().UnixNano(), 16) + "-" + strconv.FormatInt(time.Now().Unix()%99991, 16)
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *App) setAuthCookieForRequest(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieMaxAgeSec,
	})
}

func (a *App) clearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
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
	r.Post("/api/login", a.handleLogin)
	r.Post("/api/logout", a.handleLogout)

	// runtime
	r.Get("/api/runtime", a.handleRuntime)
	r.Get("/api/projects", a.handleProjectsList)
	r.Post("/api/projects", a.handleProjectCreate)
	r.Post("/api/projects/{project_id}/activate", a.handleProjectActivate)
	r.Post("/api/projects/{project_id}/chat-model", a.handleProjectChatModelUpdate)
	r.Get("/api/projects/{project_id}/context", a.handleProjectContext)
	r.Get("/api/projects/{project_id}/files", a.handleProjectFilesList)
	r.Post("/api/chat/sessions", a.handleChatSessionCreate)
	r.Get("/api/chat/sessions/{session_id}/history", a.handleChatSessionHistory)
	r.Get("/api/chat/sessions/{session_id}/activity", a.handleChatSessionActivity)
	r.Post("/api/chat/sessions/{session_id}/messages", a.handleChatSessionMessage)
	r.Post("/api/chat/sessions/{session_id}/commands", a.handleChatSessionCommand)
	r.Post("/api/chat/sessions/{session_id}/cancel", a.handleChatSessionCancel)
	r.Post("/api/chat/sessions/{session_id}/cancel-delegates", a.handleChatSessionCancelDelegates)
	r.Get("/api/hotword/status", a.handleHotwordStatus)

	// canvas/file proxy
	r.Get("/api/canvas/{session_id}/snapshot", a.handleCanvasSnapshot)
	r.Get("/api/files/{session_id}/*", a.handleFilesProxy)

	// ws
	r.Get("/ws/chat/{session_id}", a.handleChatWS)
	r.Get("/ws/canvas/{session_id}", a.handleCanvasWS)

	// static
	r.Get("/", a.serveIndex)
	r.Get("/canvas", a.serveCanvas)
	if a.devRuntime {
		diskDir := filepath.Join(a.localProjectDir, "internal", "web", "static")
		r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(diskDir))))
	} else {
		r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSubFS()))))
	}
	return securityHeaders(r)
}

func staticSubFS() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("embedded static/ directory missing: " + err.Error())
	}
	return sub
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'wasm-unsafe-eval'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; "+
				"connect-src 'self' ws: wss:; "+
				"frame-ancestors 'none'; "+
				"base-uri 'none'; "+
				"form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (a *App) serveIndex(w http.ResponseWriter, r *http.Request) {
	var data []byte
	var err error
	if a.devRuntime {
		data, err = os.ReadFile(filepath.Join(a.localProjectDir, "internal", "web", "static", "index.html"))
	} else {
		data, err = staticFiles.ReadFile("static/index.html")
	}
	if err != nil {
		http.Error(w, "web client not found", http.StatusNotFound)
		return
	}
	page := string(data)
	boot := strings.TrimSpace(a.bootID)
	if boot != "" {
		styleTag := `href="/static/style.css"`
		styleTagVer := fmt.Sprintf(`href="/static/style.css?v=%s"`, url.QueryEscape(boot))
		scriptTag := `src="/static/app.js"`
		scriptTagVer := fmt.Sprintf(`src="/static/app.js?v=%s"`, url.QueryEscape(boot))
		page = strings.Replace(page, styleTag, styleTagVer, 1)
		page = strings.Replace(page, scriptTag, scriptTagVer, 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func (a *App) serveCanvas(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/?desktop=1", http.StatusTemporaryRedirect)
}

func decodeJSON(r *http.Request, out interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 16*1024*1024)).Decode(out)
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func (a *App) handleSetupCheck(w http.ResponseWriter, r *http.Request) {
	hasPassword := a.store.HasAdminPassword()
	res := map[string]interface{}{
		"has_password":  hasPassword,
		"authenticated": a.hasAuth(r),
	}
	if a.localProjectDir != "" {
		res["local_session"] = LocalSessionID
	}
	writeJSON(w, res)
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
	a.setAuthCookieForRequest(w, r, token)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = a.store.DeleteAuthSession(c.Value)
	}
	a.clearAuthCookie(w)
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sparkReasoningEffort := ""
	if isSparkModel(a.appServerModel) {
		sparkReasoningEffort = a.appServerSparkReasoningEffort
	}
	writeJSON(w, map[string]interface{}{
		"boot_id":                     a.bootID,
		"started_at":                  a.startedAt,
		"version":                     "0.1.4",
		"dev_mode":                    a.devRuntime,
		"local_mcp_url":               a.localMCPURL,
		"app_server_url":              a.appServerURL,
		"app_server_model":            a.appServerModel,
		"app_server_reasoning_effort": sparkReasoningEffort,
		"intent_classifier_url":       a.intentClassifierURL,
		"intent_llm_url":              a.intentLLMURL,
		"available_models":            modelprofile.SupportedModels(),
		"available_reasoning_efforts": modelprofile.AvailableReasoningEffortsByAlias(),
		"tts_enabled":                 a.ttsURL != "",
	})
}

func enforceSparkModel(rawModel string) string {
	if isSparkModel(strings.TrimSpace(rawModel)) {
		return strings.TrimSpace(rawModel)
	}
	return DefaultModel
}

func resolveSparkReasoningEffort(raw string) string {
	clean := strings.ToLower(strings.TrimSpace(raw))
	switch clean {
	case modelprofile.ReasoningLow, modelprofile.ReasoningMedium, modelprofile.ReasoningHigh, modelprofile.ReasoningExtraHigh:
		return clean
	default:
		return DefaultSparkReasoningEffort
	}
}

func isSparkModel(model string) bool {
	return modelprofile.AliasForModel(model) == modelprofile.AliasSpark
}

func appServerReasoningParamsForModel(model, effort string) map[string]interface{} {
	if !isSparkModel(model) {
		return nil
	}
	effort = resolveSparkReasoningEffort(strings.TrimSpace(effort))
	if strings.TrimSpace(effort) == "" {
		return nil
	}
	return map[string]interface{}{
		"model_reasoning_effort": effort,
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
	host := u.Hostname()
	if host == "" {
		return true
	}
	requestHost := r.Host
	if i := strings.LastIndex(requestHost, ":"); i >= 0 {
		requestHost = requestHost[:i]
	}
	if strings.EqualFold(host, requestHost) {
		return true
	}
	return isLoopbackHost(host)
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
	a.mu.Lock()
	port, ok := a.tunnelPorts[sid]
	a.mu.Unlock()
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
	a.mu.Lock()
	_, alreadyReady := a.tunnelPorts[LocalSessionID]
	a.mu.Unlock()
	if alreadyReady {
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
		a.mu.Lock()
		a.tunnelPorts[LocalSessionID] = port
		a.mu.Unlock()
		a.startCanvasRelay(LocalSessionID, port)
		return nil
	}

	app := serve.NewApp(a.localProjectDir)
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.localServe = app
	a.localServeCancel = cancel
	a.mu.Unlock()
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
	return errors.New("local tabura MCP listener did not become healthy in time")
}

func (a *App) Start(host string, port int) error {
	return a.start(host, port, "", "")
}

func (a *App) StartTLS(host string, port int, certFile, keyFile string) error {
	return a.start(host, port, strings.TrimSpace(certFile), strings.TrimSpace(keyFile))
}

// ListenTLS starts an additional HTTPS listener without triggering local serve
// startup (the caller is expected to also call Start for the primary HTTP
// listener which handles that).
func (a *App) ListenTLS(host string, port int, certFile, keyFile string) error {
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           a.Router(),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Println("tabura server HTTPS listener listening on:")
	for _, u := range serve.ListenURLsWithScheme(host, port, "https") {
		fmt.Printf("  %s\n", u)
	}
	err := srv.ListenAndServeTLS(certFile, keyFile)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *App) start(host string, port int, certFile, keyFile string) error {
	if err := a.startLocalServe(); err != nil {
		return err
	}
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", host, port), Handler: a.Router(), ReadHeaderTimeout: 15 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	scheme := "http"
	if certFile != "" && keyFile != "" {
		scheme = "https"
	}
	fmt.Println("tabura server web listener listening on:")
	for _, u := range serve.ListenURLsWithScheme(host, port, scheme) {
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
	var err error
	if certFile != "" && keyFile != "" {
		err = srv.ListenAndServeTLS(certFile, keyFile)
	} else {
		err = srv.ListenAndServe()
	}
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *App) Shutdown(ctx context.Context) error {
	projectStops := map[string]context.CancelFunc{}
	projectApps := map[string]*serve.App{}
	a.mu.Lock()
	for _, cancel := range a.relayCancel {
		cancel()
	}
	for _, runs := range a.chatTurnCancel {
		for _, cancel := range runs {
			cancel()
		}
	}
	for _, ws := range a.remoteCanvasWS {
		_ = ws.Close()
	}
	for _, set := range a.chatWS {
		for conn := range set {
			_ = conn.conn.Close()
		}
	}
	for sid, cancel := range a.projectServeStop {
		projectStops[sid] = cancel
	}
	for sid, app := range a.projectServes {
		projectApps[sid] = app
	}
	a.mu.Unlock()

	for _, cancel := range projectStops {
		cancel()
	}
	for _, app := range projectApps {
		if app != nil {
			_ = app.Stop(ctx)
		}
	}
	if a.localServe != nil {
		_ = a.localServe.Stop(ctx)
	}
	if a.localServeCancel != nil {
		a.localServeCancel()
	}
	return a.store.Close()
}
