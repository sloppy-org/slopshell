package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
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
	"github.com/krystophny/tabura/internal/extensions"
	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/plugins"
	"github.com/krystophny/tabura/internal/serve"
	"github.com/krystophny/tabura/internal/store"
)

const (
	DefaultHost                  = "127.0.0.1"
	DefaultPort                  = 8420
	DefaultAppServerURL          = "ws://127.0.0.1:8787"
	DefaultSTTURL                = "http://127.0.0.1:8427"
	DefaultSTTAllowedLanguages   = "en,de"
	DefaultSTTFallbackLanguage   = "en"
	DefaultSTTPreVADThresholdDB  = -58.0
	DefaultSTTPreVADMinSpeechMS  = 120
	SessionCookie                = "tabura_session"
	cookieMaxAgeSec              = 60 * 60 * 24 * 365
	DaemonPort                   = 9420
	LocalSessionID               = "local"
	DefaultSparkReasoningEffort  = "low"
	SparkModel                   = modelprofile.ModelSpark
	mcpToolsCallTimeout          = 45 * time.Second
	appStateDefaultChatModelKey  = "default_chat_model"
	appStateYoloModeKey          = "safety.yolo_mode"
	appStateDisclaimerAckKey     = "safety.disclaimer_ack.version"
	appStateDisclaimerAckAtKey   = "safety.disclaimer_ack.timestamp"
	appStateSilentModeKey        = "runtime.silent_mode"
	appStateInputModeKey         = "runtime.input_mode"
	appStateInputModeExplicitKey = "runtime.input_mode.explicit"
	appStateStartupBehaviorKey   = "runtime.startup_behavior"
	disclaimerVersionCurrent     = "2026-03-03-v1"
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
	intentLLMModel                string
	intentLLMProfile              string
	intentLLMProfileOptions       []string
	sttURL                        string
	sttAllowedLanguagesDefault    []string
	sttFallbackLanguageDefault    string
	sttInitialPromptDefault       string
	sttPreVADEnabledDefault       bool
	sttPreVADThresholdDBDefault   float64
	sttPreVADMinSpeechMSDefault   int
	ttsURL                        string
	pluginsDir                    string
	extensionsDir                 string
	pluginManager                 *plugins.Manager
	extensionHost                 *extensions.Host
	hookProviders                 []plugins.HookProvider
	devRuntime                    bool

	store *store.Store

	appServerClient *appserver.Client

	upgrader websocket.Upgrader

	mu               sync.Mutex
	confirmMu        sync.Mutex
	workerWG         sync.WaitGroup
	hub              *wsHub
	turns            *chatTurnTracker
	companionTurns   *companionPendingTurnTracker
	companionRuntime *companionRuntimeTracker
	chatInputModes   *chatInputModeTracker
	projectAttention *projectAttentionTracker
	tunnels          *tunnelRegistry
	chatAppSessions  map[string]*appserver.Session
	pendingDanger    map[string]*pendingDangerousAction
	ghCommandRunner  ghCommandRunner

	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	bootID         string
	startedAt      string
}

const DefaultModel = modelprofile.ModelSpark

func New(dataDir, localProjectDir, localMCPURL, appServerURL, model, ttsURL, sparkReasoningEffort string, devRuntime bool) (*App, error) {
	s, err := store.New(filepath.Join(dataDir, "tabura.db"))
	if err != nil {
		return nil, err
	}
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
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
	} else if resolvedIntentClassifierURL == "" {
		resolvedIntentClassifierURL = DefaultIntentClassifierURL
	}
	resolvedIntentLLMURL := strings.TrimSpace(os.Getenv("TABURA_INTENT_LLM_URL"))
	if strings.EqualFold(resolvedIntentLLMURL, "off") {
		resolvedIntentLLMURL = ""
	} else if resolvedIntentLLMURL == "" {
		resolvedIntentLLMURL = DefaultIntentLLMURL
	}
	resolvedIntentLLMModel := strings.TrimSpace(os.Getenv("TABURA_INTENT_LLM_MODEL"))
	if strings.EqualFold(resolvedIntentLLMModel, "off") {
		resolvedIntentLLMModel = ""
	} else if resolvedIntentLLMModel == "" {
		resolvedIntentLLMModel = DefaultIntentLLMModel
	}
	resolvedIntentLLMProfile := resolveIntentLLMProfile(os.Getenv("TABURA_INTENT_LLM_PROFILE"))
	resolvedIntentLLMProfileOptions := parseIntentLLMProfileOptions(os.Getenv("TABURA_INTENT_LLM_PROFILE_OPTIONS"))
	if len(resolvedIntentLLMProfileOptions) == 0 {
		resolvedIntentLLMProfileOptions = parseIntentLLMProfileOptions(DefaultIntentLLMProfileOptions)
	}
	resolvedIntentLLMProfileOptions = ensureIntentLLMProfileOption(resolvedIntentLLMProfileOptions, resolvedIntentLLMProfile)
	resolvedSTTURL := strings.TrimSpace(os.Getenv("TABURA_STT_URL"))
	if strings.EqualFold(resolvedSTTURL, "off") {
		resolvedSTTURL = ""
	} else if resolvedSTTURL == "" {
		resolvedSTTURL = DefaultSTTURL
	}
	resolvedSTTAllowedLanguages := parseLanguageListEnv(strings.TrimSpace(os.Getenv("TABURA_STT_ALLOWED_LANGUAGES")))
	if len(resolvedSTTAllowedLanguages) == 0 {
		resolvedSTTAllowedLanguages = parseLanguageListEnv(strings.TrimSpace(os.Getenv("TABURA_STT_LANGUAGE")))
	}
	if len(resolvedSTTAllowedLanguages) == 0 {
		resolvedSTTAllowedLanguages = parseLanguageListEnv(DefaultSTTAllowedLanguages)
	}
	resolvedSTTFallbackLanguage := normalizeLanguageCodeEnv(strings.TrimSpace(os.Getenv("TABURA_STT_FALLBACK_LANGUAGE")))
	if resolvedSTTFallbackLanguage == "" {
		if len(resolvedSTTAllowedLanguages) > 0 {
			resolvedSTTFallbackLanguage = resolvedSTTAllowedLanguages[0]
		} else {
			resolvedSTTFallbackLanguage = DefaultSTTFallbackLanguage
		}
	}
	resolvedSTTInitialPrompt := strings.TrimSpace(os.Getenv("TABURA_STT_PROMPT"))
	resolvedSTTPreVADEnabled := parseEnvBoolDefault("TABURA_STT_PREVAD_ENABLED", true)
	resolvedSTTPreVADThresholdDB := parseEnvFloatDefault("TABURA_STT_PREVAD_THRESHOLD_DB", DefaultSTTPreVADThresholdDB)
	resolvedSTTPreVADMinSpeechMS := parseEnvIntDefault("TABURA_STT_PREVAD_MIN_SPEECH_MS", DefaultSTTPreVADMinSpeechMS)
	if err := s.SetAppState(appStateDefaultChatModelKey, modelprofile.AliasSpark); err != nil {
		_ = s.Close()
		return nil, err
	}
	resolvedPluginsDir := strings.TrimSpace(os.Getenv("TABURA_PLUGINS_DIR"))
	if strings.EqualFold(resolvedPluginsDir, "off") {
		resolvedPluginsDir = ""
	} else if resolvedPluginsDir == "" {
		resolvedPluginsDir = filepath.Join(dataDir, "plugins")
	}
	resolvedExtensionsDir := strings.TrimSpace(os.Getenv("TABURA_EXTENSIONS_DIR"))
	if strings.EqualFold(resolvedExtensionsDir, "off") {
		resolvedExtensionsDir = ""
	} else if resolvedExtensionsDir == "" {
		resolvedExtensionsDir = filepath.Join(dataDir, "extensions")
	}
	pluginManager, err := plugins.New(plugins.Options{
		Dir: resolvedPluginsDir,
		Logf: func(format string, args ...interface{}) {
			log.Printf("plugins: "+format, args...)
		},
	})
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	extensionHost, err := extensions.New(extensions.Options{
		Dir:            resolvedExtensionsDir,
		RuntimeVersion: "0.1.8",
		Logf: func(format string, args ...interface{}) {
			log.Printf("extensions: "+format, args...)
		},
	})
	if err != nil {
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
		intentLLMModel:                resolvedIntentLLMModel,
		intentLLMProfile:              resolvedIntentLLMProfile,
		intentLLMProfileOptions:       resolvedIntentLLMProfileOptions,
		sttURL:                        resolvedSTTURL,
		sttAllowedLanguagesDefault:    resolvedSTTAllowedLanguages,
		sttFallbackLanguageDefault:    resolvedSTTFallbackLanguage,
		sttInitialPromptDefault:       resolvedSTTInitialPrompt,
		sttPreVADEnabledDefault:       resolvedSTTPreVADEnabled,
		sttPreVADThresholdDBDefault:   resolvedSTTPreVADThresholdDB,
		sttPreVADMinSpeechMSDefault:   resolvedSTTPreVADMinSpeechMS,
		ttsURL:                        resolvedTTSURL,
		pluginsDir:                    resolvedPluginsDir,
		extensionsDir:                 resolvedExtensionsDir,
		pluginManager:                 pluginManager,
		extensionHost:                 extensionHost,
		hookProviders:                 buildHookProviders(extensionHost, pluginManager),
		devRuntime:                    devRuntime,
		store:                         s,
		appServerClient:               appServerClient,
		upgrader:                      websocket.Upgrader{CheckOrigin: checkWSOrigin},
		hub:                           newWSHub(),
		turns:                         newChatTurnTracker(),
		companionTurns:                newCompanionPendingTurnTracker(),
		companionRuntime:              newCompanionRuntimeTracker(),
		chatInputModes:                newChatInputModeTracker(),
		projectAttention:              newProjectAttentionTracker(),
		tunnels:                       newTunnelRegistry(),
		chatAppSessions:               map[string]*appserver.Session{},
		pendingDanger:                 map[string]*pendingDangerousAction{},
		ghCommandRunner:               runGitHubCLI,
		shutdownCtx:                   shutdownCtx,
		shutdownCancel:                shutdownCancel,
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
	app.startItemResurfacer()
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
	r.Patch("/api/runtime/preferences", a.handleRuntimePreferencesUpdate)
	r.Post("/api/runtime/yolo", a.handleRuntimeYoloModeUpdate)
	r.Post("/api/runtime/disclaimer-ack", a.handleRuntimeDisclaimerAck)
	r.Get("/api/plugins", a.handlePlugins)
	r.Post("/api/plugins/meeting-partner/decide", a.handleMeetingPartnerDecide)
	r.Get("/api/extensions", a.handleExtensions)
	r.Post("/api/extensions/commands/{command_id}", a.handleExtensionCommandExecute)
	r.Get("/api/projects", a.handleProjectsList)
	r.Get("/api/projects/activity", a.handleProjectsActivity)
	r.Post("/api/projects", a.handleProjectCreate)
	r.Post("/api/projects/{project_id}/activate", a.handleProjectActivate)
	r.Post("/api/projects/{project_id}/persist", a.handleTemporaryProjectPersist)
	r.Post("/api/projects/{project_id}/discard", a.handleTemporaryProjectDiscard)
	r.Post("/api/projects/{project_id}/chat-model", a.handleProjectChatModelUpdate)
	r.Get("/api/projects/{project_id}/context", a.handleProjectContext)
	r.Get("/api/projects/{project_id}/files", a.handleProjectFilesList)
	r.Get("/api/projects/{project_id}/welcome", a.handleProjectWelcome)
	r.Get("/api/projects/{project_id}/companion/config", a.handleProjectCompanionConfigGet)
	r.Put("/api/projects/{project_id}/companion/config", a.handleProjectCompanionConfigPut)
	r.Get("/api/projects/{project_id}/companion/state", a.handleProjectCompanionState)
	r.Get("/api/projects/{project_id}/transcript", a.handleProjectCompanionTranscript)
	r.Get("/api/projects/{project_id}/summary", a.handleProjectCompanionSummary)
	r.Get("/api/projects/{project_id}/references", a.handleProjectCompanionReferences)
	r.Post("/api/ink/submit", a.handleInkSubmit)
	r.Post("/api/review/submit", a.handleReviewSubmit)
	r.Get("/api/workspaces", a.handleWorkspaceList)
	r.Post("/api/workspaces", a.handleWorkspaceCreate)
	r.Get("/api/workspaces/{workspace_id}", a.handleWorkspaceGet)
	r.Delete("/api/workspaces/{workspace_id}", a.handleWorkspaceDelete)
	r.Get("/api/external-accounts", a.handleExternalAccountList)
	r.Post("/api/external-accounts", a.handleExternalAccountCreate)
	r.Put("/api/external-accounts/{account_id}", a.handleExternalAccountUpdate)
	r.Delete("/api/external-accounts/{account_id}", a.handleExternalAccountDelete)
	r.Get("/api/actors", a.handleActorList)
	r.Post("/api/actors", a.handleActorCreate)
	r.Get("/api/actors/{actor_id}", a.handleActorGet)
	r.Delete("/api/actors/{actor_id}", a.handleActorDelete)
	r.Get("/api/artifacts", a.handleArtifactList)
	r.Post("/api/artifacts", a.handleArtifactCreate)
	r.Get("/api/artifacts/{artifact_id}", a.handleArtifactGet)
	r.Delete("/api/artifacts/{artifact_id}", a.handleArtifactDelete)
	r.Get("/api/items", a.handleItemList)
	r.Post("/api/items", a.handleItemCreate)
	r.Post("/api/items/sync/github", a.handleGitHubIssueSync)
	r.Put("/api/items/{item_id}/assign", a.handleItemAssign)
	r.Put("/api/items/{item_id}/unassign", a.handleItemUnassign)
	r.Put("/api/items/{item_id}/complete", a.handleItemComplete)
	r.Put("/api/items/{item_id}/state", a.handleItemStateUpdate)
	r.Post("/api/items/{item_id}/triage", a.handleItemTriage)
	r.Get("/api/items/{item_id}", a.handleItemGet)
	r.Put("/api/items/{item_id}", a.handleItemUpdate)
	r.Delete("/api/items/{item_id}", a.handleItemDelete)
	r.Post("/api/chat/sessions", a.handleChatSessionCreate)
	r.Get("/api/chat/sessions/{session_id}/history", a.handleChatSessionHistory)
	r.Get("/api/chat/sessions/{session_id}/activity", a.handleChatSessionActivity)
	r.Post("/api/chat/sessions/{session_id}/messages", a.handleChatSessionMessage)
	r.Post("/api/chat/sessions/{session_id}/commands", a.handleChatSessionCommand)
	r.Post("/api/chat/sessions/{session_id}/cancel", a.handleChatSessionCancel)
	r.Get("/api/hotword/status", a.handleHotwordStatus)
	r.Post("/api/stt/transcribe", a.handleSTTTranscribe)
	r.Get("/api/stt/config", a.handleSTTConfigGet)
	r.Put("/api/stt/config", a.handleSTTConfigPut)
	r.Get("/api/stt/replacements", a.handleSTTReplacementsGet)
	r.Put("/api/stt/replacements", a.handleSTTReplacementsPut)

	// participant (meeting notes)
	r.Get("/api/participant/config", a.handleParticipantConfigGet)
	r.Put("/api/participant/config", a.handleParticipantConfigPut)
	r.Get("/api/participant/status", a.handleParticipantStatus)
	r.Get("/api/participant/sessions", a.handleParticipantSessionsList)
	r.Get("/api/participant/sessions/{id}/transcript", a.handleParticipantTranscript)
	r.Get("/api/participant/sessions/{id}/search", a.handleParticipantSearch)
	r.Get("/api/participant/sessions/{id}/export", a.handleParticipantExport)

	// canvas/file proxy
	r.Get("/api/canvas/{session_id}/snapshot", a.handleCanvasSnapshot)
	r.Get("/api/files/{session_id}/*", a.handleFilesProxy)

	// ws
	r.Get("/ws/chat/{session_id}", a.handleChatWS)
	r.Get("/ws/canvas/{session_id}", a.handleCanvasWS)

	// static
	r.Get("/", a.serveIndex)
	r.Get("/canvas", a.serveCanvas)
	r.Get("/capture", a.serveCapture)
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

func publicBasePath(r *http.Request) string {
	if r == nil {
		return "/"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Prefix")); forwarded != "" {
		return normalizeBasePath(forwarded)
	}
	return normalizeBasePath(r.URL.Path)
}

func normalizeBasePath(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" || clean == "/" {
		return "/"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	clean = strings.TrimRight(clean, "/")
	if clean == "" {
		return "/"
	}
	lastSlash := strings.LastIndex(clean, "/")
	lastDot := strings.LastIndex(clean, ".")
	if lastSlash >= 0 && lastDot > lastSlash {
		clean = clean[:lastSlash]
		if clean == "" {
			return "/"
		}
	}
	return clean + "/"
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self' 'wasm-unsafe-eval' https://cdn.jsdelivr.net; "+
				"style-src 'self' 'unsafe-inline'; "+
				"font-src 'self'; "+
				"worker-src 'self' blob:; "+
				"img-src 'self' data:; "+
				"connect-src 'self' ws: wss: https://cdn.jsdelivr.net; "+
				"frame-ancestors 'none'; "+
				"base-uri 'none'; "+
				"form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=()")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Embedder-Policy", "credentialless")
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
	baseHref := html.EscapeString(publicBasePath(r))
	page = strings.Replace(page, "<head>", fmt.Sprintf("<head>\n  <base href=\"%s\">", baseHref), 1)
	boot := strings.TrimSpace(a.bootID)
	if boot != "" {
		styleTag := `href="./static/style.css"`
		styleTagVer := fmt.Sprintf(`href="./static/style.css?v=%s"`, url.QueryEscape(boot))
		scriptTag := `src="./static/app.js"`
		scriptTagVer := fmt.Sprintf(`src="./static/app.js?v=%s"`, url.QueryEscape(boot))
		page = strings.Replace(page, styleTag, styleTagVer, 1)
		page = strings.Replace(page, scriptTag, scriptTagVer, 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func (a *App) serveCanvas(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Location", "./?desktop=1")
	w.WriteHeader(http.StatusTemporaryRedirect)
}

func (a *App) serveCapture(w http.ResponseWriter, r *http.Request) {
	var data []byte
	var err error
	if a.devRuntime {
		data, err = os.ReadFile(filepath.Join(a.localProjectDir, "internal", "web", "static", "capture.html"))
	} else {
		data, err = staticFiles.ReadFile("static/capture.html")
	}
	if err != nil {
		http.Error(w, "capture client not found", http.StatusNotFound)
		return
	}
	page := string(data)
	boot := strings.TrimSpace(a.bootID)
	if boot != "" {
		styleTag := `href="./static/capture.css"`
		styleTagVer := fmt.Sprintf(`href="./static/capture.css?v=%s"`, url.QueryEscape(boot))
		scriptTag := `src="./static/capture.js"`
		scriptTagVer := fmt.Sprintf(`src="./static/capture.js?v=%s"`, url.QueryEscape(boot))
		page = strings.Replace(page, styleTag, styleTagVer, 1)
		page = strings.Replace(page, scriptTag, scriptTagVer, 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
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
		"version":                     "0.1.8",
		"dev_mode":                    a.devRuntime,
		"local_mcp_url":               a.localMCPURL,
		"app_server_url":              a.appServerURL,
		"app_server_model":            a.appServerModel,
		"app_server_reasoning_effort": sparkReasoningEffort,
		"intent_classifier_url":       a.intentClassifierURL,
		"intent_llm_url":              a.intentLLMURL,
		"intent_llm_model":            a.localIntentLLMModel(),
		"intent_llm_profile":          a.intentLLMProfile,
		"available_intent_llm_profiles": append(
			[]string(nil),
			a.intentLLMProfileOptions...,
		),
		"available_models":            modelprofile.SupportedModels(),
		"available_reasoning_efforts": modelprofile.AvailableReasoningEffortsByAlias(),
		"stt_url":                     a.sttURL,
		"tts_enabled":                 a.ttsURL != "",
		"silent_mode":                 a.silentModeEnabled(),
		"input_mode":                  a.runtimeInputMode(),
		"startup_behavior":            a.runtimeStartupBehavior(),
		"safety_yolo_mode":            a.yoloModeEnabled(),
		"disclaimer_version":          disclaimerVersionCurrent,
		"disclaimer_ack_required":     a.disclaimerAckRequired(),
		"disclaimer_ack_version":      a.disclaimerAckVersion(),
		"plugins_dir":                 a.pluginsDir,
		"plugins_loaded":              a.loadedPluginCount(),
		"extensions_dir":              a.extensionsDir,
		"extensions_loaded":           a.loadedExtensionCount(),
	})
}

func (a *App) handlePlugins(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	list := []plugins.PluginInfo{}
	if a.pluginManager != nil {
		list = a.pluginManager.List()
	}
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"dir":     a.pluginsDir,
		"count":   len(list),
		"plugins": list,
	})
}

func (a *App) loadedPluginCount() int {
	if a == nil || a.pluginManager == nil {
		return 0
	}
	return a.pluginManager.Count()
}

func (a *App) loadedExtensionCount() int {
	if a == nil || a.extensionHost == nil {
		return 0
	}
	return a.extensionHost.Count()
}

func (a *App) handleMeetingPartnerDecide(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		SessionID  string                 `json:"session_id"`
		ProjectKey string                 `json:"project_key"`
		Text       string                 `json:"text"`
		Metadata   map[string]interface{} `json:"metadata"`
	}
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	hookReq := plugins.HookRequest{
		Hook:       plugins.HookMeetingPartnerDecide,
		SessionID:  strings.TrimSpace(req.SessionID),
		ProjectKey: strings.TrimSpace(req.ProjectKey),
		Text:       strings.TrimSpace(req.Text),
		Metadata:   req.Metadata,
	}
	decision, matched := plugins.MeetingPartnerDecision{}, false
	for _, provider := range a.hookProviders {
		decision, matched = provider.DecideMeetingPartner(r.Context(), hookReq)
		if matched {
			break
		}
	}
	if !matched {
		decision = plugins.MeetingPartnerDecision{Decision: "noop"}
	}
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"matched":  matched,
		"decision": decision,
	})
}

func (a *App) handleExtensions(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	list := []extensions.ExtensionInfo{}
	if a.extensionHost != nil {
		list = a.extensionHost.List()
	}
	writeJSON(w, map[string]interface{}{
		"ok":         true,
		"dir":        a.extensionsDir,
		"count":      len(list),
		"extensions": list,
	})
}

func (a *App) handleExtensionCommandExecute(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	if a.extensionHost == nil {
		http.Error(w, "extensions are disabled", http.StatusNotFound)
		return
	}
	commandID := strings.TrimSpace(chi.URLParam(r, "command_id"))
	if commandID == "" {
		http.Error(w, "missing command_id", http.StatusBadRequest)
		return
	}
	var req extensions.CommandRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
	}
	req.CommandID = commandID
	result, err := a.extensionHost.ExecuteCommand(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":     true,
		"result": result,
	})
}

func buildHookProviders(ext *extensions.Host, mgr *plugins.Manager) []plugins.HookProvider {
	var providers []plugins.HookProvider
	if ext != nil {
		providers = append(providers, ext)
	}
	if mgr != nil {
		providers = append(providers, mgr)
	}
	return providers
}

func enforceSparkModel(rawModel string) string {
	if isSparkModel(strings.TrimSpace(rawModel)) {
		return strings.TrimSpace(rawModel)
	}
	return DefaultModel
}

func resolveSparkReasoningEffort(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return DefaultSparkReasoningEffort
	}
	return modelprofile.NormalizeReasoningEffort(modelprofile.AliasSpark, clean)
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
	return map[string]interface{}{"effort": effort}
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
	if a.shutdownCancel != nil {
		a.shutdownCancel()
	}
	a.turns.cancelAll()
	a.hub.closeAllChat()
	waitErr := a.waitForAssistantWorkers(ctx)
	a.closeAllAppSessions()
	a.tunnels.shutdown(ctx)
	storeErr := a.store.Close()
	if waitErr != nil {
		return waitErr
	}
	return storeErr
}
