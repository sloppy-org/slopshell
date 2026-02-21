package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabula/internal/appserver"
	"github.com/krystophny/tabula/internal/serve"
	"github.com/krystophny/tabula/internal/store"
)

const (
	DefaultHost           = "127.0.0.1"
	DefaultPort           = 8420
	DefaultAppServerURL   = "ws://127.0.0.1:8787"
	SessionCookie         = "tabula_session"
	cookieMaxAgeSec       = 60 * 60 * 24 * 365
	DaemonPort            = 9420
	LocalSessionID        = "local"
	defaultProducerMCPURL = "http://127.0.0.1:8090/mcp"
	maxMailSTTAudioBytes  = 10 * 1024 * 1024
)

//go:embed static/* static/vendor/*
var staticFiles embed.FS

type App struct {
	dataDir         string
	localProjectDir string
	localMCPURL     string
	appServerURL    string
	appServerModel  string
	devRuntime      bool
	noAuth          bool

	store *store.Store

	appServerClient *appserver.Client

	upgrader websocket.Upgrader

	mu               sync.Mutex
	canvasWS         map[string]map[*websocket.Conn]struct{}
	chatWS           map[string]map[*websocket.Conn]struct{}
	remoteCanvasWS   map[string]*websocket.Conn
	tunnelPorts      map[string]int
	relayCancel      map[string]context.CancelFunc
	localServe       *serve.App
	localServeCancel context.CancelFunc

	bootID    string
	startedAt string
}

func New(dataDir, localProjectDir, localMCPURL, appServerURL string, devRuntime bool) (*App, error) {
	s, err := store.New(pathJoin(dataDir, "tabula.db"))
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
	noAuth := !envTruthy("TABULA_REQUIRE_AUTH")
	if envTruthy("TABULA_NO_AUTH") {
		noAuth = true
	}
	return &App{
		dataDir:         dataDir,
		localProjectDir: localProjectDir,
		localMCPURL:     localMCPURL,
		appServerURL:    appServerURL,
		appServerModel:  strings.TrimSpace(os.Getenv("TABULA_APP_SERVER_MODEL")),
		devRuntime:      devRuntime,
		noAuth:          noAuth,
		store:           s,
		appServerClient: appServerClient,
		upgrader:        websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		canvasWS:        map[string]map[*websocket.Conn]struct{}{},
		chatWS:          map[string]map[*websocket.Conn]struct{}{},
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
	if a.noAuth {
		return true
	}
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

	// runtime
	r.Get("/api/runtime", a.handleRuntime)
	r.Post("/api/chat/sessions", a.handleChatSessionCreate)
	r.Get("/api/chat/sessions/{session_id}/history", a.handleChatSessionHistory)
	r.Post("/api/chat/sessions/{session_id}/messages", a.handleChatSessionMessage)
	r.Post("/api/chat/sessions/{session_id}/commands", a.handleChatSessionCommand)

	// canvas/file proxy
	r.Get("/api/canvas/{session_id}/snapshot", a.handleCanvasSnapshot)
	r.Post("/api/canvas/{session_id}/commit", a.handleCanvasCommit)
	r.Get("/api/files/{session_id}/*", a.handleFilesProxy)
	r.Post("/api/mail/action-capabilities", a.handleMailActionCapabilities)
	r.Post("/api/mail/read", a.handleMailRead)
	r.Post("/api/mail/mark-read", a.handleMailMarkRead)
	r.Post("/api/mail/action", a.handleMailAction)
	r.Post("/api/mail/draft-reply", a.handleMailDraftReply)
	r.Post("/api/mail/draft-intent", a.handleMailDraftIntent)
	r.Post("/api/mail/stt", a.handleMailSTT)
	r.Post("/api/stt/push-to-prompt", a.handlePushToPromptSTT)

	// ws
	r.Get("/ws/chat/{session_id}", a.handleChatWS)
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
	return json.NewDecoder(r.Body).Decode(out)
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func (a *App) handleSetupCheck(w http.ResponseWriter, r *http.Request) {
	hasPassword := a.store.HasAdminPassword()
	if a.noAuth {
		hasPassword = false
	}
	res := map[string]interface{}{
		"has_password":  hasPassword,
		"authenticated": a.hasAuth(r),
	}
	if a.localProjectDir != "" {
		res["local_session"] = LocalSessionID
	}
	writeJSON(w, res)
}

func (a *App) handleSetupPassword(w http.ResponseWriter, r *http.Request) {
	if a.noAuth {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
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
	if a.noAuth {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
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
	if a.noAuth {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = a.store.DeleteAuthSession(c.Value)
	}
	a.clearAuthCookie(w)
	writeJSON(w, map[string]bool{"ok": true})
}

func envTruthy(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (a *App) handleRuntime(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	writeJSON(w, map[string]interface{}{
		"boot_id":          a.bootID,
		"started_at":       a.startedAt,
		"version":          "0.0.5",
		"dev_mode":         a.devRuntime,
		"local_mcp_url":    emptyToNil(a.localMCPURL),
		"app_server_url":   emptyToNil(a.appServerURL),
		"app_server_model": emptyToNil(a.appServerModel),
	})
}

func emptyToNil(v string) interface{} {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
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

func (a *App) handleCanvasCommit(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sid := chi.URLParam(r, "session_id")
	if strings.TrimSpace(sid) == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}

	var req struct {
		ArtifactID   string `json:"artifact_id"`
		IncludeDraft *bool  `json:"include_draft"`
	}
	includeDraft := true
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed reading request body", http.StatusBadRequest)
		return
	}
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.IncludeDraft != nil {
			includeDraft = *req.IncludeDraft
		}
	}

	a.mu.Lock()
	port, ok := a.tunnelPorts[sid]
	a.mu.Unlock()
	if !ok {
		http.Error(w, "no active tunnel for session", http.StatusNotFound)
		return
	}

	resp, err := a.mcpToolsCall(port, "canvas_commit", map[string]interface{}{
		"session_id":    sid,
		"artifact_id":   req.ArtifactID,
		"include_draft": includeDraft,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if a.appServerClient != nil {
		aiSummary, aiErr := a.rewriteCommittedArtifactWithAppServer(r.Context(), sid, port, strings.TrimSpace(req.ArtifactID))
		if aiErr != nil {
			aiSummary = map[string]interface{}{
				"enabled": false,
				"applied": false,
				"error":   aiErr.Error(),
			}
		}
		resp["ai_review"] = aiSummary
	}
	writeJSON(w, resp)
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

func (a *App) handleMailRead(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		Provider       string `json:"provider"`
		MessageID      string `json:"message_id"`
		Format         string `json:"format"`
		ProducerMCPURL string `json:"producer_mcp_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.MessageID) == "" {
		http.Error(w, "message_id is required", http.StatusBadRequest)
		return
	}
	mcpURL, err := normalizeProducerMCPURL(req.ProducerMCPURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	format := strings.TrimSpace(req.Format)
	if format == "" {
		format = "full"
	}
	resp, err := mcpToolsCallURL(mcpURL, "email_read", map[string]interface{}{
		"provider":   req.Provider,
		"message_id": req.MessageID,
		"format":     format,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, resp)
}

func (a *App) handleMailMarkRead(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		Provider       string `json:"provider"`
		MessageID      string `json:"message_id"`
		ProducerMCPURL string `json:"producer_mcp_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.MessageID) == "" {
		http.Error(w, "message_id is required", http.StatusBadRequest)
		return
	}
	mcpURL, err := normalizeProducerMCPURL(req.ProducerMCPURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := mcpToolsCallURL(mcpURL, "email_mark_read", map[string]interface{}{
		"provider":    req.Provider,
		"message_ids": []string{req.MessageID},
	})
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

func (a *App) handleMailDraftReply(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("TABULA_DRAFT_REPLY_DISABLED")), "1") {
		http.Error(w, "draft reply is disabled", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Provider       string `json:"provider"`
		MessageID      string `json:"message_id"`
		Subject        string `json:"subject"`
		Sender         string `json:"sender"`
		SelectionText  string `json:"selection_text"`
		ProducerMCPURL string `json:"producer_mcp_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.MessageID) == "" {
		http.Error(w, "message_id is required", http.StatusBadRequest)
		return
	}
	mcpURL, err := normalizeProducerMCPURL(req.ProducerMCPURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if draft, ok := tryProducerDraftReply(mcpURL, req.Provider, req.MessageID, req.Subject, req.Sender, req.SelectionText); ok {
		writeJSON(w, map[string]interface{}{
			"draft_text": draft,
			"source":     "llm",
		})
		return
	}

	messagePreview := ""
	if readResp, err := mcpToolsCallURL(mcpURL, "email_read", map[string]interface{}{
		"provider":   req.Provider,
		"message_id": req.MessageID,
		"format":     "full",
	}); err == nil {
		if message, _ := readResp["message"].(map[string]interface{}); message != nil {
			messagePreview = strings.TrimSpace(firstNonEmpty(
				fmt.Sprint(message["snippet"]),
				fmt.Sprint(message["body"]),
				fmt.Sprint(message["plain"]),
			))
		}
	}
	draft := composeFallbackDraftReply(req.Sender, req.Subject, req.SelectionText, messagePreview)
	writeJSON(w, map[string]interface{}{
		"draft_text": draft,
		"source":     "fallback",
	})
}

func (a *App) handleMailDraftIntent(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		Transcript string `json:"transcript"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	transcript := strings.TrimSpace(req.Transcript)
	if transcript == "" {
		http.Error(w, "transcript is required", http.StatusBadRequest)
		return
	}
	intent := classifyDraftReplyIntent(transcript)
	writeJSON(w, map[string]interface{}{
		"intent":           intent.Intent,
		"reason":           intent.Reason,
		"fallback_applied": intent.FallbackApplied,
		"fallback_policy":  intent.FallbackPolicy,
	})
}

func (a *App) handleMailSTT(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		ProducerMCPURL string `json:"producer_mcp_url"`
		VoxTypeMCPURL  string `json:"voxtype_mcp_url"`
		MimeType       string `json:"mime_type"`
		AudioBase64    string `json:"audio_base64"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	decodedAudio := strings.TrimSpace(req.AudioBase64)
	if decodedAudio == "" {
		http.Error(w, "audio_base64 is required", http.StatusBadRequest)
		return
	}
	audioData, err := base64.StdEncoding.DecodeString(decodedAudio)
	if err != nil {
		http.Error(w, "audio_base64 must be valid base64", http.StatusBadRequest)
		return
	}
	if len(audioData) == 0 {
		http.Error(w, "audio payload is empty", http.StatusBadRequest)
		return
	}
	if len(audioData) > maxMailSTTAudioBytes {
		http.Error(w, "audio payload exceeds max size", http.StatusBadRequest)
		return
	}

	mimeType := normalizeSTTMimeType(req.MimeType)
	if !isAllowedSTTMimeType(mimeType) {
		http.Error(w, "mime_type must be audio/* or application/octet-stream", http.StatusBadRequest)
		return
	}

	sessionID := "mail-" + randomToken()
	startReq := pushToPromptRequest{
		Action:         sttActionStart,
		SessionID:      sessionID,
		MimeType:       mimeType,
		VoxTypeMCPURL:  req.VoxTypeMCPURL,
		ProducerMCPURL: req.ProducerMCPURL,
	}
	if _, err := dispatchPushToPromptVoxTypeMCP(startReq); err != nil {
		var he *httpErr
		if errors.As(err, &he) {
			http.Error(w, he.Message, he.Status)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	appendReq := pushToPromptRequest{
		Action:         sttActionAppend,
		SessionID:      sessionID,
		Seq:            0,
		AudioBase64:    decodedAudio,
		VoxTypeMCPURL:  req.VoxTypeMCPURL,
		ProducerMCPURL: req.ProducerMCPURL,
	}
	if _, err := dispatchPushToPromptVoxTypeMCP(appendReq); err != nil {
		_, _ = dispatchPushToPromptVoxTypeMCP(pushToPromptRequest{
			Action:        sttActionCancel,
			SessionID:     sessionID,
			VoxTypeMCPURL: req.VoxTypeMCPURL,
		})
		var he *httpErr
		if errors.As(err, &he) {
			http.Error(w, he.Message, he.Status)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	stopReq := pushToPromptRequest{
		Action:         sttActionStop,
		SessionID:      sessionID,
		VoxTypeMCPURL:  req.VoxTypeMCPURL,
		ProducerMCPURL: req.ProducerMCPURL,
	}
	result, err := dispatchPushToPromptVoxTypeMCP(stopReq)
	if err != nil {
		var he *httpErr
		if errors.As(err, &he) {
			http.Error(w, he.Message, he.Status)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func tryProducerDraftReply(mcpURL, provider, messageID, subject, sender, selectionText string) (string, bool) {
	resp, err := mcpToolsCallURL(mcpURL, "draft_reply", map[string]interface{}{
		"provider":       provider,
		"message_id":     messageID,
		"subject":        subject,
		"sender":         sender,
		"selection_text": selectionText,
	})
	if err != nil {
		return "", false
	}
	for _, key := range []string{"draft_text", "draft", "text"} {
		if text := strings.TrimSpace(fmt.Sprint(resp[key])); text != "" && text != "<nil>" {
			return text, true
		}
	}
	if nested, _ := resp["reply"].(map[string]interface{}); nested != nil {
		for _, key := range []string{"draft_text", "draft", "text"} {
			if text := strings.TrimSpace(fmt.Sprint(nested[key])); text != "" && text != "<nil>" {
				return text, true
			}
		}
	}
	return "", false
}

func composeFallbackDraftReply(sender, subject, selectionText, preview string) string {
	senderName := formatSenderName(sender)
	if senderName == "" {
		senderName = "there"
	}
	subjectLine := strings.TrimSpace(subject)
	if subjectLine == "" {
		subjectLine = "your message"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Hi %s,\n\n", senderName)
	fmt.Fprintf(&b, "Thanks for your email regarding \"%s\".\n", subjectLine)
	if strings.TrimSpace(selectionText) != "" {
		b.WriteString("\nI noted this point:\n")
		fmt.Fprintf(&b, "\"%s\"\n", strings.TrimSpace(selectionText))
	}
	if strings.TrimSpace(preview) != "" {
		b.WriteString("\nI reviewed your note and will follow up with a concrete next step shortly.\n")
	} else {
		b.WriteString("\nI will follow up with a concrete next step shortly.\n")
	}
	b.WriteString("\nBest,\n")
	b.WriteString("Your Name")
	return b.String()
}

func formatSenderName(sender string) string {
	sender = strings.TrimSpace(sender)
	if sender == "" {
		return ""
	}
	if i := strings.Index(sender, "<"); i > 0 {
		return strings.Trim(strings.TrimSpace(sender[:i]), "\"")
	}
	if at := strings.Index(sender, "@"); at > 0 {
		return strings.TrimSpace(sender[:at])
	}
	return sender
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && v != "<nil>" {
			return v
		}
	}
	return ""
}

func (a *App) mcpToolsCall(port int, name string, arguments map[string]interface{}) (map[string]interface{}, error) {
	return mcpToolsCallURL(fmt.Sprintf("http://127.0.0.1:%d/mcp", port), name, arguments)
}

type reviewComment struct {
	Comment    string
	MarkType   string
	TargetKind string
	LineStart  int
	LineEnd    int
	Page       int
	UpdatedAt  string
}

func (a *App) rewriteCommittedArtifactWithAppServer(ctx context.Context, sessionID string, tunnelPort int, requestedArtifactID string) (map[string]interface{}, error) {
	status, err := a.mcpToolsCall(tunnelPort, "canvas_status", map[string]interface{}{"session_id": sessionID})
	if err != nil {
		return nil, err
	}
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		return map[string]interface{}{
			"enabled": true,
			"applied": false,
			"reason":  "no_active_artifact",
		}, nil
	}
	kind := strings.TrimSpace(fmt.Sprint(active["kind"]))
	if kind != "text_artifact" && kind != "pdf_artifact" {
		return map[string]interface{}{
			"enabled": true,
			"applied": false,
			"reason":  "active_artifact_unsupported",
			"kind":    kind,
		}, nil
	}

	artifactID := strings.TrimSpace(fmt.Sprint(active["event_id"]))
	if artifactID == "" || artifactID == "<nil>" {
		artifactID = strings.TrimSpace(fmt.Sprint(status["active_artifact_id"]))
	}
	if artifactID == "" || artifactID == "<nil>" {
		return map[string]interface{}{
			"enabled": true,
			"applied": false,
			"reason":  "missing_artifact_id",
		}, nil
	}
	if strings.TrimSpace(requestedArtifactID) != "" && strings.TrimSpace(requestedArtifactID) != artifactID {
		return map[string]interface{}{
			"enabled":              true,
			"applied":              false,
			"reason":               "active_artifact_changed",
			"requested_artifact":   requestedArtifactID,
			"active_artifact_id":   artifactID,
			"active_artifact_kind": kind,
		}, nil
	}

	originalText := fmt.Sprint(active["text"])
	if kind == "text_artifact" && (strings.TrimSpace(originalText) == "" || originalText == "<nil>") {
		return map[string]interface{}{
			"enabled":     true,
			"applied":     false,
			"reason":      "active_text_missing",
			"artifact_id": artifactID,
		}, nil
	}
	if originalText == "<nil>" {
		originalText = ""
	}
	artifactPath := strings.TrimSpace(fmt.Sprint(active["path"]))
	if artifactPath == "<nil>" {
		artifactPath = ""
	}
	artifactPage := intFromAny(active["page"], 0)
	title := strings.TrimSpace(fmt.Sprint(active["title"]))
	if title == "<nil>" {
		title = ""
	}

	marksResp, err := a.mcpToolsCall(tunnelPort, "canvas_marks_list", map[string]interface{}{
		"session_id":  sessionID,
		"artifact_id": artifactID,
		"intent":      "persistent",
		"limit":       250,
	})
	if err != nil {
		return nil, err
	}
	rawMarks, _ := marksResp["marks"].([]interface{})
	comments := make([]reviewComment, 0, len(rawMarks))
	for _, rawMark := range rawMarks {
		mark, _ := rawMark.(map[string]interface{})
		if mark == nil {
			continue
		}
		comment := strings.TrimSpace(fmt.Sprint(mark["comment"]))
		if comment == "" || comment == "<nil>" {
			continue
		}
		target, _ := mark["target"].(map[string]interface{})
		lineStart := intFromAny(target["line_start"], 0)
		lineEnd := intFromAny(target["line_end"], lineStart)
		if lineEnd < lineStart {
			lineEnd = lineStart
		}
		page := intFromAny(target["page"], 0)
		comments = append(comments, reviewComment{
			Comment:    comment,
			MarkType:   strings.TrimSpace(fmt.Sprint(mark["type"])),
			TargetKind: strings.TrimSpace(fmt.Sprint(mark["target_kind"])),
			LineStart:  lineStart,
			LineEnd:    lineEnd,
			Page:       page,
			UpdatedAt:  strings.TrimSpace(fmt.Sprint(mark["updated_at"])),
		})
	}
	if len(comments) == 0 {
		return map[string]interface{}{
			"enabled":       true,
			"applied":       false,
			"reason":        "no_persistent_comments",
			"artifact_id":   artifactID,
			"marks_total":   len(rawMarks),
			"comments_used": 0,
		}, nil
	}
	sort.Slice(comments, func(i, j int) bool {
		if comments[i].UpdatedAt == comments[j].UpdatedAt {
			return comments[i].Comment < comments[j].Comment
		}
		return comments[i].UpdatedAt < comments[j].UpdatedAt
	})

	prompt := buildArtifactRewritePrompt(kind, title, originalText, artifactPath, artifactPage, comments)
	rewriteCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cwd := strings.TrimSpace(a.localProjectDir)
	if cwd == "" {
		cwd = "."
	}
	appResp, err := a.appServerClient.SendPrompt(rewriteCtx, appserver.PromptRequest{
		CWD:    cwd,
		Model:  a.appServerModel,
		Prompt: prompt,
	})
	if err != nil {
		return nil, err
	}

	rewrittenText := sanitizeAppServerTextResponse(appResp.Message)
	if strings.TrimSpace(rewrittenText) == "" {
		return nil, errors.New("app-server returned empty rewrite result")
	}
	if kind == "text_artifact" && normalizeLineEndings(strings.TrimSpace(rewrittenText)) == normalizeLineEndings(strings.TrimSpace(originalText)) {
		return map[string]interface{}{
			"enabled":       true,
			"applied":       false,
			"reason":        "unchanged",
			"artifact_id":   artifactID,
			"comments_used": len(comments),
			"thread_id":     appResp.ThreadID,
			"turn_id":       appResp.TurnID,
		}, nil
	}

	outputTitle := title
	if kind == "pdf_artifact" {
		if strings.TrimSpace(outputTitle) == "" {
			outputTitle = "PDF Review Notes"
		} else {
			outputTitle = outputTitle + " (AI Review Notes)"
		}
	}
	showResp, err := a.mcpToolsCall(tunnelPort, "canvas_artifact_show", map[string]interface{}{
		"session_id":       sessionID,
		"kind":             "text",
		"title":            outputTitle,
		"markdown_or_text": rewrittenText,
		"reason":           "commit_review_rewrite",
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"enabled":            true,
		"applied":            true,
		"source":             "codex_app_server",
		"artifact_kind":      kind,
		"input_artifact_id":  artifactID,
		"output_artifact_id": showResp["artifact_id"],
		"comments_used":      len(comments),
		"thread_id":          appResp.ThreadID,
		"turn_id":            appResp.TurnID,
	}, nil
}

func buildArtifactRewritePrompt(kind, title, text, path string, page int, comments []reviewComment) string {
	var b strings.Builder
	b.WriteString("You are revising a user artifact from review comments.\n")
	b.WriteString("Apply all comments faithfully and return only the rewritten output.\n")
	b.WriteString("No preface. No explanations. No code fences.\n")
	switch kind {
	case "text_artifact":
		b.WriteString("Artifact type: text/markdown.\n")
		b.WriteString("Preserve document structure and style unless comments request changes.\n")
		b.WriteString("Return the full revised document text.\n")
	case "pdf_artifact":
		b.WriteString("Artifact type: PDF.\n")
		b.WriteString("You cannot edit binary PDF content directly in this response.\n")
		b.WriteString("Return a Markdown review-note document with proposed revisions covering all comments.\n")
		b.WriteString("Use this format: title, summary, and numbered proposed edits.\n")
	}
	if strings.TrimSpace(title) != "" {
		fmt.Fprintf(&b, "Artifact title: %s\n", strings.TrimSpace(title))
	}
	if kind == "pdf_artifact" {
		if strings.TrimSpace(path) != "" {
			fmt.Fprintf(&b, "PDF path: %s\n", strings.TrimSpace(path))
		}
		if page > 0 {
			fmt.Fprintf(&b, "Current PDF page: %d\n", page)
		}
	}
	b.WriteString("\nReview comments (chronological):\n")
	for i, c := range comments {
		scope := make([]string, 0, 4)
		if strings.TrimSpace(c.MarkType) != "" && c.MarkType != "<nil>" {
			scope = append(scope, strings.TrimSpace(c.MarkType))
		}
		if strings.TrimSpace(c.TargetKind) != "" && c.TargetKind != "<nil>" {
			scope = append(scope, strings.TrimSpace(c.TargetKind))
		}
		if c.LineStart > 0 && c.LineEnd > 0 {
			if c.LineStart == c.LineEnd {
				scope = append(scope, fmt.Sprintf("line %d", c.LineStart))
			} else {
				scope = append(scope, fmt.Sprintf("lines %d-%d", c.LineStart, c.LineEnd))
			}
		}
		if c.Page > 0 {
			scope = append(scope, fmt.Sprintf("page %d", c.Page))
		}
		prefix := fmt.Sprintf("%d.", i+1)
		if len(scope) > 0 {
			prefix += " [" + strings.Join(scope, ", ") + "]"
		}
		fmt.Fprintf(&b, "%s %s\n", prefix, c.Comment)
	}
	if kind == "text_artifact" {
		b.WriteString("\nOriginal document text:\n")
		b.WriteString("<<<TEXT\n")
		b.WriteString(text)
		b.WriteString("\nTEXT\n")
		b.WriteString(">>>")
	}
	return b.String()
}

func sanitizeAppServerTextResponse(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") && strings.TrimSpace(lines[len(lines)-1]) == "```" {
			text = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	return strings.TrimSpace(text)
}

func normalizeLineEndings(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
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

func normalizeSTTMimeType(raw string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "audio/webm"
	}
	if i := strings.Index(candidate, ";"); i >= 0 {
		candidate = strings.TrimSpace(candidate[:i])
	}
	if candidate == "" {
		return "audio/webm"
	}
	return strings.ToLower(candidate)
}

func isAllowedSTTMimeType(mimeType string) bool {
	if mimeType == "application/octet-stream" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "audio/")
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
	for _, set := range a.chatWS {
		for ws := range set {
			_ = ws.Close()
		}
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
