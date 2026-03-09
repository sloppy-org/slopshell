package web

import (
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

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
	r.Get("/api/projects/{project_id}/meeting-items", a.handleProjectMeetingItemsGet)
	r.Post("/api/projects/{project_id}/meeting-items", a.handleProjectMeetingItemsCreate)
	r.Get("/api/projects/{project_id}/items", a.handleProjectItemsList)
	r.Get("/api/projects/{project_id}/workspaces", a.handleProjectWorkspacesList)
	r.Post("/api/ink/submit", a.handleInkSubmit)
	r.Post("/api/review/submit", a.handleReviewSubmit)
	r.Post("/api/bugs/report", a.handleBugReportCreate)
	r.Get("/api/workspaces", a.handleWorkspaceList)
	r.Get("/api/watches", a.handleWorkspaceWatchList)
	r.Post("/api/workspaces", a.handleWorkspaceCreate)
	r.Get("/api/workspaces/{workspace_id}", a.handleWorkspaceGet)
	r.Put("/api/workspaces/{workspace_id}", a.handleWorkspaceUpdate)
	r.Put("/api/workspaces/{workspace_id}/project", a.handleWorkspaceProjectUpdate)
	r.Get("/api/workspaces/{workspace_id}/watch", a.handleWorkspaceWatchGet)
	r.Post("/api/workspaces/{workspace_id}/watch", a.handleWorkspaceWatchStart)
	r.Delete("/api/workspaces/{workspace_id}/watch", a.handleWorkspaceWatchStop)
	r.Delete("/api/workspaces/{workspace_id}", a.handleWorkspaceDelete)
	r.Get("/api/time-entries", a.handleTimeEntryList)
	r.Get("/api/time-entries/summary", a.handleTimeEntrySummary)
	r.Post("/api/time-entries/stamp-in", a.handleTimeEntryStampIn)
	r.Post("/api/time-entries/stamp-out", a.handleTimeEntryStampOut)
	r.Get("/api/spheres/{sphere}/accounts", a.handleExternalAccountList)
	r.Post("/api/spheres/{sphere}/accounts", a.handleExternalAccountCreate)
	r.Delete("/api/spheres/{sphere}/accounts/{account_id}", a.handleExternalAccountDelete)
	r.Get("/api/external-accounts", a.handleExternalAccountList)
	r.Post("/api/external-accounts", a.handleExternalAccountCreate)
	r.Put("/api/external-accounts/{account_id}", a.handleExternalAccountUpdate)
	r.Delete("/api/external-accounts/{account_id}", a.handleExternalAccountDelete)
	r.Get("/api/container-mappings", a.handleContainerMappingList)
	r.Post("/api/container-mappings", a.handleContainerMappingCreate)
	r.Delete("/api/container-mappings/{mapping_id}", a.handleContainerMappingDelete)
	r.Get("/api/actors", a.handleActorList)
	r.Post("/api/actors", a.handleActorCreate)
	r.Get("/api/actors/{actor_id}", a.handleActorGet)
	r.Delete("/api/actors/{actor_id}", a.handleActorDelete)
	r.Get("/api/artifacts", a.handleArtifactList)
	r.Post("/api/artifacts", a.handleArtifactCreate)
	r.Get("/api/artifacts/{artifact_id}", a.handleArtifactGet)
	r.Post("/api/artifacts/{artifact_id}/extract-figures", a.handleArtifactFigureExtract)
	r.Get("/api/artifacts/{artifact_id}/items", a.handleArtifactItemList)
	r.Delete("/api/artifacts/{artifact_id}", a.handleArtifactDelete)
	r.Get("/api/batches", a.handleBatchList)
	r.Get("/api/batches/{batch_id}", a.handleBatchGet)
	r.Get("/api/batches/{batch_id}/artifact", a.handleBatchArtifact)
	r.Get("/api/items", a.handleItemList)
	r.Post("/api/items", a.handleItemCreate)
	r.Get("/api/items/inbox", a.handleItemInbox)
	r.Get("/api/items/waiting", a.handleItemWaiting)
	r.Get("/api/items/someday", a.handleItemSomeday)
	r.Get("/api/items/done", a.handleItemDone)
	r.Get("/api/items/counts", a.handleItemCounts)
	r.Post("/api/items/sync/github", a.handleGitHubIssueSync)
	r.Post("/api/items/sync/github/reviews", a.handleGitHubPRReviewSync)
	r.Get("/api/items/{item_id}/artifacts", a.handleItemArtifactList)
	r.Post("/api/items/{item_id}/artifacts", a.handleItemArtifactLink)
	r.Delete("/api/items/{item_id}/artifacts/{artifact_id}", a.handleItemArtifactUnlink)
	r.Put("/api/items/{item_id}/assign", a.handleItemAssign)
	r.Put("/api/items/{item_id}/unassign", a.handleItemUnassign)
	r.Put("/api/items/{item_id}/complete", a.handleItemComplete)
	r.Put("/api/items/{item_id}/workspace", a.handleItemWorkspaceUpdate)
	r.Put("/api/items/{item_id}/project", a.handleItemProjectUpdate)
	r.Put("/api/items/{item_id}/state", a.handleItemStateUpdate)
	r.Post("/api/items/{item_id}/triage", a.handleItemTriage)
	r.Get("/api/items/{item_id}/print", a.handleItemPrint)
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
	if a.hasAuth(r) {
		page = strings.Replace(page, `<div id="view-login" class="view">`, `<div id="view-login" class="view" style="display:none">`, 1)
		page = strings.Replace(page, `<div id="view-main" class="view" style="display:none">`, `<div id="view-main" class="view">`, 1)
	}
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
