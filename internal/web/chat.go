package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

const assistantTurnTimeout = 2 * time.Hour

const (
	turnOutputModeVoice    = "voice"
	turnOutputModeSilent   = "silent"
	promptContractStateKey = "chat_prompt_contract_sha256"
)

const defaultVoiceHistoryPrompt = `You are Tabura, an AI assistant.
Chat text is always spoken via TTS.

## Response Format

Write naturally for speech. Avoid raw paths, URLs, or code in prose.
Use [lang:de] at the start of your answer when responding in German. Default is English.

Voice mode is chat-only:
- Do not emit :::file blocks.
- Do not emit :::canvas blocks.
- Do not render chat output on canvas.
- Keep spoken responses concise.

When user asks to show/open an existing file, do NOT paste that file body into chat. Use canvas_artifact_show with a brief spoken confirmation.

Line references: when the user mentions [Line N of "file"], apply changes at that location.
`

const defaultVoiceTurnPrompt = `Voice mode is chat-only:
- Reply with spoken chat text only.
- Do not emit :::file blocks.
- Do not emit :::canvas blocks.
- Do not render chat output on canvas.

When user asks to show/open an existing file, do NOT paste file body into chat; use canvas_artifact_show and keep chat text brief.

`

type chatMessageRequest struct {
	Text       string `json:"text"`
	OutputMode string `json:"output_mode"`
}

type chatCommandRequest struct {
	Command string `json:"command"`
}

func (a *App) handleChatSessionCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		ProjectKey string `json:"project_key"`
		ProjectID  string `json:"project_id"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
	}
	projectKey, err := a.resolveProjectKey(req.ProjectID, req.ProjectKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var resolvedProject store.Project
	projectResolved := false
	if strings.TrimSpace(req.ProjectID) != "" {
		if resolvedProject, err = a.store.GetProject(strings.TrimSpace(req.ProjectID)); err == nil {
			projectResolved = true
		} else if !isNoRows(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if !projectResolved {
		if resolvedProject, err = a.store.GetProjectByProjectKey(projectKey); err == nil {
			projectResolved = true
		} else if !isNoRows(err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	session, err := a.store.GetOrCreateChatSession(projectKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	canvasSessionID := LocalSessionID
	projectID := ""
	if projectResolved {
		projectID = resolvedProject.ID
		canvasSessionID = a.canvasSessionIDForProject(resolvedProject)
		if err := a.store.SetActiveProjectID(resolvedProject.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = a.store.TouchProject(resolvedProject.ID)
		if err := a.ensureProjectCanvasReady(resolvedProject); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok":                true,
		"session_id":        session.ID,
		"project_key":       session.ProjectKey,
		"project_id":        projectID,
		"mode":              session.Mode,
		"canvas_session_id": canvasSessionID,
	})
}

func (a *App) handleChatSessionHistory(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	messages, err := a.store.ListChatMessages(sessionID, 1000)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"session":  session,
		"messages": messages,
	})
}

func (a *App) handleChatSessionActivity(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	activeTurns := a.activeChatTurnCount(sessionID)
	queuedTurns := a.queuedChatTurnCount(sessionID)
	delegateActive := a.delegateActiveJobsForProject(session.ProjectKey)
	writeJSON(w, map[string]interface{}{
		"ok":              true,
		"active_turns":    activeTurns,
		"queued_turns":    queuedTurns,
		"delegate_active": delegateActive,
		"is_working":      activeTurns > 0 || queuedTurns > 0 || delegateActive > 0,
	})
}

func (a *App) handleChatSessionCancelDelegates(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	canceled := a.cancelDelegatedJobsForProject(session.ProjectKey)
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"canceled": canceled,
	})
}

func (a *App) handleChatSessionCommand(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	var req chatCommandRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	result, err := a.executeChatCommand(sessionID, req.Command)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":     true,
		"kind":   "command",
		"result": result,
	})
}

func (a *App) handleChatSessionCancel(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	activeCanceled, queuedCanceled := a.cancelChatWork(sessionID)
	delegateCanceled := a.cancelDelegatedJobsForProject(session.ProjectKey)
	writeJSON(w, map[string]interface{}{
		"ok":                true,
		"canceled":          activeCanceled + queuedCanceled + delegateCanceled,
		"active_canceled":   activeCanceled,
		"queued_canceled":   queuedCanceled,
		"delegate_canceled": delegateCanceled,
	})
}

func (a *App) handleChatSessionMessage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	if _, err := a.store.GetChatSession(sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	var req chatMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	text := strings.TrimSpace(req.Text)
	outputMode := normalizeTurnOutputMode(req.OutputMode)
	if text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	commandText := ""
	if strings.HasPrefix(text, "/") {
		commandText = text
	}
	if commandText != "" {
		result, err := a.executeChatCommand(sessionID, commandText)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]interface{}{
			"ok":     true,
			"kind":   "command",
			"result": result,
		})
		return
	}
	storedUser, err := a.store.AddChatMessage(sessionID, "user", text, text, "text")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.broadcastChatEvent(sessionID, map[string]interface{}{
		"type":    "message_accepted",
		"role":    "user",
		"content": text,
		"id":      storedUser.ID,
	})
	queuedTurns := a.enqueueAssistantTurn(sessionID, outputMode)
	writeJSON(w, map[string]interface{}{
		"ok":         true,
		"kind":       "turn_queued",
		"message_id": storedUser.ID,
		"queued":     queuedTurns,
	})
}

func (a *App) executeChatCommand(sessionID, raw string) (map[string]interface{}, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("command is required")
	}
	if strings.HasPrefix(trimmed, "/") {
		trimmed = strings.TrimPrefix(trimmed, "/")
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return nil, errors.New("command is required")
	}
	name := strings.ToLower(parts[0])
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		return nil, err
	}

	switch name {
	case "plan":
		targetMode := session.Mode
		arg := ""
		if len(parts) > 1 {
			arg = strings.ToLower(parts[1])
		}
		switch arg {
		case "", "toggle":
			if targetMode == "plan" {
				targetMode = "chat"
			} else {
				targetMode = "plan"
			}
		case "on":
			targetMode = "plan"
		case "off":
			targetMode = "chat"
		default:
			return nil, errors.New("usage: /plan [on|off]")
		}
		updated, err := a.store.UpdateChatSessionMode(sessionID, targetMode)
		if err != nil {
			return nil, err
		}
		message := fmt.Sprintf("Plan mode %s.", map[bool]string{true: "enabled", false: "disabled"}[updated.Mode == "plan"])
		_, _ = a.store.AddChatMessage(sessionID, "system", message, message, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":    "mode_changed",
			"mode":    updated.Mode,
			"message": message,
		})
		return map[string]interface{}{
			"name":    "plan",
			"mode":    updated.Mode,
			"message": message,
		}, nil
	case "stop", "cancel":
		activeCanceled, queuedCanceled := a.cancelChatWork(sessionID)
		delegateCanceled := a.cancelDelegatedJobsForProject(session.ProjectKey)
		canceled := activeCanceled + queuedCanceled + delegateCanceled
		message := "No assistant turn is currently running."
		if canceled > 0 {
			message = "Stopping assistant work and clearing queued prompts."
		}
		return map[string]interface{}{
			"name":              name,
			"canceled":          canceled,
			"active_canceled":   activeCanceled,
			"queued_canceled":   queuedCanceled,
			"delegate_canceled": delegateCanceled,
			"message":           message,
		}, nil
	case "status":
		message, err := a.fetchCodexStatusMessage(session.ProjectKey)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"name":    "status",
			"message": message,
		}, nil
	case "pr":
		selector := ""
		if len(parts) > 1 {
			selector = strings.TrimSpace(strings.Join(parts[1:], " "))
		}
		review, err := a.loadGitHubPRReview(session.ProjectKey, selector)
		if err != nil {
			return nil, err
		}
		canvasSessionID := strings.TrimSpace(a.resolveCanvasSessionID(session.ProjectKey))
		if canvasSessionID == "" {
			return nil, errors.New("canvas session is not available")
		}
		artifactPath := filepath.ToSlash(filepath.Join(".tabura", "artifacts", "pr", fmt.Sprintf("pr-%d.diff", review.View.Number)))
		if !a.writeCanvasFileBlock(session.ProjectKey, canvasSessionID, fileBlock{
			Path:    artifactPath,
			Content: review.Diff,
		}) {
			return nil, errors.New("failed to publish PR diff to canvas")
		}
		title := strings.TrimSpace(review.View.Title)
		if title == "" {
			title = fmt.Sprintf("PR #%d", review.View.Number)
		}
		baseRef := strings.TrimSpace(review.View.BaseRefName)
		headRef := strings.TrimSpace(review.View.HeadRefName)
		if baseRef == "" || headRef == "" {
			return map[string]interface{}{
				"name":          "pr",
				"pr_number":     review.View.Number,
				"pr_title":      title,
				"pr_url":        strings.TrimSpace(review.View.URL),
				"files_changed": review.FileCount,
				"message":       fmt.Sprintf("Loaded %s (%d files).", title, review.FileCount),
			}, nil
		}
		return map[string]interface{}{
			"name":          "pr",
			"pr_number":     review.View.Number,
			"pr_title":      title,
			"pr_url":        strings.TrimSpace(review.View.URL),
			"base_ref":      baseRef,
			"head_ref":      headRef,
			"files_changed": review.FileCount,
			"message":       fmt.Sprintf("Loaded PR #%d: %s (%s -> %s, %d files).", review.View.Number, title, headRef, baseRef, review.FileCount),
		}, nil
	case "clear", "clearall", "reset":
		report, err := a.clearAllAgentsAndContexts(session.ID)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"name":              name,
			"message":           "All agents and contexts cleared.",
			"active_canceled":   report.ActiveCanceled,
			"queued_canceled":   report.QueuedCanceled,
			"delegate_canceled": report.DelegateCanceled,
			"sessions_closed":   report.SessionsClosed,
			"tmp_files_cleared": report.TempFilesCleared,
		}, nil
	case "compact":
		// Close the current app-server session, forcing a fresh thread on
		// the next turn. The new thread gets only recent local history as
		// initial context, which is equivalent to a context compaction.
		a.closeAppSession(sessionID)
		if err := a.store.ResetChatSessionThread(sessionID); err != nil {
			return nil, err
		}
		message := "Context compacted. Next message starts a fresh app-server thread."
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":    "chat_compacted",
			"message": message,
		})
		return map[string]interface{}{
			"name":    "compact",
			"message": message,
		}, nil
	default:
		return nil, fmt.Errorf("unknown command: /%s", name)
	}
}

func (a *App) fetchCodexStatusMessage(projectKey string) (string, error) {
	if a.appServerClient == nil {
		return "", errors.New("app-server is not configured")
	}
	cwd := strings.TrimSpace(a.cwdForProjectKey(projectKey))
	if cwd == "" {
		cwd = "."
	}
	profile := a.appServerModelProfileForProjectKey(projectKey)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resp, err := a.appServerClient.SendPrompt(ctx, appserver.PromptRequest{
		CWD:          cwd,
		Prompt:       "/status",
		Model:        profile.Model,
		ThreadParams: profile.ThreadParams,
		TurnParams:   profile.TurnParams,
		Timeout:      45 * time.Second,
	})
	if err != nil {
		return "", fmt.Errorf("status command failed: %s", normalizeAssistantError(err))
	}
	message := strings.TrimSpace(resp.Message)
	if message == "" {
		return "", errors.New("status command returned an empty response")
	}
	return message, nil
}

func (a *App) registerActiveChatTurn(sessionID, runID string, cancel context.CancelFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chatTurnCancel[sessionID] == nil {
		a.chatTurnCancel[sessionID] = map[string]context.CancelFunc{}
	}
	a.chatTurnCancel[sessionID][runID] = cancel
}

func (a *App) unregisterActiveChatTurn(sessionID, runID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	runs := a.chatTurnCancel[sessionID]
	if runs == nil {
		return
	}
	delete(runs, runID)
	if len(runs) == 0 {
		delete(a.chatTurnCancel, sessionID)
	}
}

func (a *App) cancelActiveChatTurns(sessionID string) int {
	a.mu.Lock()
	runs := a.chatTurnCancel[sessionID]
	if len(runs) == 0 {
		a.mu.Unlock()
		return 0
	}
	cancels := make([]context.CancelFunc, 0, len(runs))
	for _, cancel := range runs {
		cancels = append(cancels, cancel)
	}
	delete(a.chatTurnCancel, sessionID)
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels)
}

func (a *App) clearQueuedChatTurns(sessionID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	queued := a.chatTurnQueue[sessionID]
	delete(a.chatTurnQueue, sessionID)
	delete(a.chatTurnOutputMode, sessionID)
	return queued
}

func (a *App) cancelChatWork(sessionID string) (int, int) {
	activeCanceled := a.cancelActiveChatTurns(sessionID)
	queuedCanceled := a.clearQueuedChatTurns(sessionID)
	if queuedCanceled > 0 {
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":  "turn_queue_cleared",
			"count": queuedCanceled,
		})
	}
	return activeCanceled, queuedCanceled
}

type clearAllReport struct {
	ActiveCanceled   int
	QueuedCanceled   int
	DelegateCanceled int
	SessionsClosed   int
	TempFilesCleared int
}

func (a *App) clearCanvasForProject(projectKey string) {
	canvasSessionID := strings.TrimSpace(a.resolveCanvasSessionID(projectKey))
	if canvasSessionID == "" {
		return
	}
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return
	}
	_, _ = a.mcpToolsCall(port, "canvas_clear", map[string]interface{}{
		"session_id": canvasSessionID,
		"reason":     "context reset",
	})
}

func (a *App) clearProjectTempCanvasFiles(projectKey string) int {
	cwd := strings.TrimSpace(a.cwdForProjectKey(projectKey))
	if cwd == "" {
		return 0
	}
	tmpDir := filepath.Join(cwd, ".tabura", "artifacts", "tmp")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return 0
	}
	cleared := 0
	for _, entry := range entries {
		target := filepath.Join(tmpDir, entry.Name())
		if err := os.RemoveAll(target); err == nil {
			cleared++
		}
	}
	return cleared
}

func (a *App) clearAllAgentsAndContexts(currentSessionID string) (clearAllReport, error) {
	report := clearAllReport{}
	sessions, err := a.store.ListChatSessions()
	if err != nil {
		return report, err
	}
	for _, session := range sessions {
		activeCanceled, queuedCanceled := a.cancelChatWork(session.ID)
		report.ActiveCanceled += activeCanceled
		report.QueuedCanceled += queuedCanceled
		report.DelegateCanceled += a.cancelDelegatedJobsForProject(session.ProjectKey)
		report.TempFilesCleared += a.clearProjectTempCanvasFiles(session.ProjectKey)
		a.clearCanvasForProject(session.ProjectKey)
		a.broadcastChatEvent(session.ID, map[string]interface{}{
			"type": "chat_cleared",
		})
	}
	closed := 0
	a.mu.Lock()
	appSessions := a.chatAppSessions
	a.chatAppSessions = map[string]*appserver.Session{}
	a.mu.Unlock()
	for _, s := range appSessions {
		if s == nil {
			continue
		}
		_ = s.Close()
		closed++
	}
	report.SessionsClosed = closed
	if err := a.store.ClearAllChatMessages(); err != nil {
		return report, err
	}
	if err := a.store.ClearAllChatEvents(); err != nil {
		return report, err
	}
	if err := a.store.ResetAllChatSessionThreads(); err != nil {
		return report, err
	}
	if strings.TrimSpace(currentSessionID) != "" {
		a.closeAppSession(currentSessionID)
	}
	return report, nil
}

func (a *App) delegateActiveJobsForProject(projectKey string) int {
	cwd := strings.TrimSpace(a.cwdForProjectKey(projectKey))
	if cwd == "" {
		return 0
	}
	canvasSessionID := strings.TrimSpace(a.resolveCanvasSessionID(projectKey))
	if canvasSessionID == "" {
		return 0
	}
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return 0
	}
	status, err := a.mcpToolsCall(port, "delegate_to_model_active_count", map[string]interface{}{"cwd_prefix": cwd})
	if err != nil {
		log.Printf("delegate activity probe failed for project=%q cwd=%q: %v", projectKey, cwd, err)
		return 0
	}
	return intFromAny(status["active"], 0)
}

func (a *App) cancelDelegatedJobsForProject(projectKey string) int {
	cwd := strings.TrimSpace(a.cwdForProjectKey(projectKey))
	if cwd == "" {
		return 0
	}
	canvasSessionID := strings.TrimSpace(a.resolveCanvasSessionID(projectKey))
	if canvasSessionID == "" {
		return 0
	}
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return 0
	}
	status, err := a.mcpToolsCall(port, "delegate_to_model_cancel_all", map[string]interface{}{"cwd_prefix": cwd})
	if err != nil {
		log.Printf("delegate cancel-all failed for project=%q cwd=%q: %v", projectKey, cwd, err)
		return 0
	}
	return intFromAny(status["canceled"], 0)
}

func (a *App) activeChatTurnCount(sessionID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.chatTurnCancel[sessionID])
}

func (a *App) queuedChatTurnCount(sessionID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.chatTurnQueue[sessionID]
}

func (a *App) enqueueAssistantTurn(sessionID, outputMode string) int {
	mode := normalizeTurnOutputMode(outputMode)
	a.mu.Lock()
	a.chatTurnOutputMode[sessionID] = append(a.chatTurnOutputMode[sessionID], mode)
	a.chatTurnQueue[sessionID] = a.chatTurnQueue[sessionID] + 1
	queued := a.chatTurnQueue[sessionID]
	workerRunning := a.chatTurnWorker[sessionID]
	if !workerRunning {
		a.chatTurnWorker[sessionID] = true
	}
	a.mu.Unlock()
	if !workerRunning {
		go a.runAssistantTurnQueue(sessionID)
	}
	return queued
}

func (a *App) dequeueAssistantTurn(sessionID string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	queued := a.chatTurnQueue[sessionID]
	if queued <= 0 {
		return "", false
	}
	modes := a.chatTurnOutputMode[sessionID]
	mode := turnOutputModeVoice
	if len(modes) > 0 {
		mode = normalizeTurnOutputMode(modes[0])
		modes = modes[1:]
		if len(modes) == 0 {
			delete(a.chatTurnOutputMode, sessionID)
		} else {
			a.chatTurnOutputMode[sessionID] = modes
		}
	}
	queued--
	if queued <= 0 {
		delete(a.chatTurnQueue, sessionID)
		delete(a.chatTurnOutputMode, sessionID)
		return mode, true
	}
	a.chatTurnQueue[sessionID] = queued
	return mode, true
}

func (a *App) markAssistantWorkerIdleIfQueueEmpty(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.chatTurnQueue[sessionID] > 0 {
		return false
	}
	delete(a.chatTurnWorker, sessionID)
	return true
}

func (a *App) runAssistantTurnQueue(sessionID string) {
	for {
		outputMode, ok := a.dequeueAssistantTurn(sessionID)
		if !ok {
			if a.markAssistantWorkerIdleIfQueueEmpty(sessionID) {
				return
			}
			continue
		}
		a.runAssistantTurn(sessionID, outputMode)
	}
}

func (a *App) getOrCreateAppSession(sessionID string, cwd string, profile appServerModelProfile) (*appserver.Session, bool, error) {
	a.mu.Lock()
	s := a.chatAppSessions[sessionID]
	a.mu.Unlock()
	if s != nil && s.IsOpen() {
		return s, true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Try to resume the previous thread if one was stored.
	var existingThreadID string
	if sess, err := a.store.GetChatSession(sessionID); err == nil {
		existingThreadID = strings.TrimSpace(sess.AppThreadID)
	}
	var newSess *appserver.Session
	var resumed bool
	if existingThreadID != "" {
		rs, ok, err := a.appServerClient.ResumeSessionWithParams(ctx, cwd, profile.Model, profile.ThreadParams, existingThreadID)
		if err != nil {
			return nil, false, err
		}
		newSess = rs
		resumed = ok
	} else {
		rs, err := a.appServerClient.OpenSessionWithParams(ctx, cwd, profile.Model, profile.ThreadParams)
		if err != nil {
			return nil, false, err
		}
		newSess = rs
	}
	a.mu.Lock()
	if old := a.chatAppSessions[sessionID]; old != nil {
		_ = old.Close()
	}
	a.chatAppSessions[sessionID] = newSess
	a.mu.Unlock()
	return newSess, resumed, nil
}

func (a *App) closeAppSession(sessionID string) {
	a.mu.Lock()
	s := a.chatAppSessions[sessionID]
	delete(a.chatAppSessions, sessionID)
	a.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
}

func (a *App) runAssistantTurn(sessionID string, outputMode string) {
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
		return
	}
	messages, err := a.store.ListChatMessages(sessionID, 200)
	if err != nil {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
		return
	}
	if project, projectErr := a.store.GetProjectByProjectKey(session.ProjectKey); projectErr == nil && isHubProject(project) {
		a.runHubTurn(sessionID, session, messages, outputMode)
		return
	}
	if a.appServerClient == nil {
		errText := "app-server is not configured"
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}

	cwd := a.cwdForProjectKey(session.ProjectKey)
	profile := a.appServerModelProfileForProjectKey(session.ProjectKey)
	appSess, resumed, sessErr := a.getOrCreateAppSession(sessionID, cwd, profile)
	if sessErr != nil {
		a.runAssistantTurnLegacy(sessionID, session, messages, outputMode, profile)
		return
	}

	canvasCtx := a.resolveCanvasContext(session.ProjectKey)
	var prompt string
	if resumed {
		prompt = buildTurnPromptForMode(messages, canvasCtx, outputMode, profile.Alias)
	} else {
		prompt = buildPromptFromHistoryForMode(session.Mode, messages, canvasCtx, outputMode, profile.Alias)
		_ = a.store.UpdateChatSessionThread(sessionID, appSess.ThreadID())
	}
	if strings.TrimSpace(prompt) == "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), assistantTurnTimeout)
	runID := randomToken()
	a.registerActiveChatTurn(sessionID, runID, cancel)
	defer func() {
		cancel()
		a.unregisterActiveChatTurn(sessionID, runID)
	}()

	go a.watchCanvasFile(ctx, session.ProjectKey)

	latestMessage := ""
	latestTurnID := ""
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	persistedAssistantPlain := ""
	persistedAssistantFormat := ""
	persistWriteFailed := false

	persistAssistantSnapshot := func(text string, renderOnCanvas bool, autoCanvas bool) {
		candidateMarkdown, candidatePlain, candidateFormat := assistantSnapshotContent(text, renderOnCanvas, autoCanvas)
		if candidateMarkdown == "" && candidatePlain == "" {
			return
		}
		if persistedAssistantID == 0 {
			storedAssistant, storeErr := a.store.AddChatMessage(sessionID, "assistant", candidateMarkdown, candidatePlain, candidateFormat)
			if storeErr != nil {
				if !persistWriteFailed {
					persistWriteFailed = true
					a.broadcastChatEvent(sessionID, map[string]interface{}{
						"type":  "error",
						"error": storeErr.Error(),
					})
				}
				return
			}
			persistedAssistantID = storedAssistant.ID
			persistedAssistantText = candidateMarkdown
			persistedAssistantPlain = candidatePlain
			persistedAssistantFormat = candidateFormat
			return
		}
		if candidateMarkdown == persistedAssistantText &&
			candidatePlain == persistedAssistantPlain &&
			candidateFormat == persistedAssistantFormat {
			return
		}
		if storeErr := a.store.UpdateChatMessageContent(persistedAssistantID, candidateMarkdown, candidatePlain, candidateFormat); storeErr != nil {
			if !persistWriteFailed {
				persistWriteFailed = true
				a.broadcastChatEvent(sessionID, map[string]interface{}{
					"type":  "error",
					"error": storeErr.Error(),
				})
			}
			return
		}
		persistedAssistantText = candidateMarkdown
		persistedAssistantPlain = candidatePlain
		persistedAssistantFormat = candidateFormat
	}

	appResp, err := appSess.SendTurnWithParams(ctx, prompt, "", profile.TurnParams, func(ev appserver.StreamEvent) {
		payload := map[string]interface{}{
			"type":        ev.Type,
			"thread_id":   ev.ThreadID,
			"turn_id":     ev.TurnID,
			"output_mode": outputMode,
		}
		shouldBroadcast := true
		switch ev.Type {
		case "thread_started":
			// Thread ID already stored on session open.
		case "turn_started":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
		case "assistant_message":
			latestMessage = ev.Message
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(ev.Message, outputMode)
			persistAssistantSnapshot(ev.Message, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = ev.Message
			payload["delta"] = ev.Delta
			if renderPlan.RenderOnCanvas {
				payload["render_on_canvas"] = true
			}
			if renderPlan.AutoCanvas {
				payload["auto_canvas"] = true
			}
		case "turn_completed":
			if strings.TrimSpace(ev.Message) != "" {
				latestMessage = ev.Message
			}
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(latestMessage, outputMode)
			persistAssistantSnapshot(latestMessage, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = latestMessage
			if renderPlan.RenderOnCanvas {
				payload["render_on_canvas"] = true
			}
			if renderPlan.AutoCanvas {
				payload["auto_canvas"] = true
			}
		case "item_completed":
			payload["item_type"] = ev.Message
			if ev.Detail != "" {
				payload["detail"] = ev.Detail
			}
		case "context_usage":
			payload["context_used"] = ev.ContextUsed
			payload["context_max"] = ev.ContextMax
		case "context_compact":
			// pass through to frontend
		case "error":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			shouldBroadcast = false
		}
		if shouldBroadcast {
			a.broadcastChatEvent(sessionID, payload)
		}
	})
	if err != nil {
		a.closeAppSession(sessionID)
		if errors.Is(err, context.Canceled) {
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":    "turn_cancelled",
				"turn_id": latestTurnID,
			})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			errText := "assistant request timed out"
			_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
			payload := map[string]interface{}{
				"type":  "error",
				"error": errText,
			}
			if strings.TrimSpace(latestTurnID) != "" {
				payload["turn_id"] = latestTurnID
			}
			a.broadcastChatEvent(sessionID, payload)
			return
		}
		errText := normalizeAssistantError(err)
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		payload := map[string]interface{}{
			"type":  "error",
			"error": errText,
		}
		if strings.TrimSpace(latestTurnID) != "" {
			payload["turn_id"] = latestTurnID
		}
		a.broadcastChatEvent(sessionID, payload)
		return
	}

	assistantText := strings.TrimSpace(appResp.Message)
	if assistantText == "" {
		assistantText = strings.TrimSpace(latestMessage)
	}
	if assistantText == "" {
		assistantText = "(assistant returned no content)"
	}

	assistantText = a.finalizeAssistantResponse(sessionID, session.ProjectKey, assistantText,
		&persistedAssistantID, &persistedAssistantText, appResp.TurnID, latestTurnID, appResp.ThreadID, outputMode)
	_ = assistantText
}

// runAssistantTurnLegacy is the single-shot fallback when persistent session
// fails to connect. Each call creates a new WS + thread.
func (a *App) runAssistantTurnLegacy(sessionID string, session store.ChatSession, messages []store.ChatMessage, outputMode string, profile appServerModelProfile) {
	canvasCtx := a.resolveCanvasContext(session.ProjectKey)
	prompt := buildPromptFromHistoryForMode(session.Mode, messages, canvasCtx, outputMode, profile.Alias)
	if strings.TrimSpace(prompt) == "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), assistantTurnTimeout)
	runID := randomToken()
	a.registerActiveChatTurn(sessionID, runID, cancel)
	defer func() {
		cancel()
		a.unregisterActiveChatTurn(sessionID, runID)
	}()

	go a.watchCanvasFile(ctx, session.ProjectKey)

	latestMessage := ""
	latestTurnID := ""
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	persistedAssistantPlain := ""
	persistedAssistantFormat := ""
	persistWriteFailed := false
	persistAssistantSnapshot := func(text string, renderOnCanvas bool, autoCanvas bool) {
		candidateMarkdown, candidatePlain, candidateFormat := assistantSnapshotContent(text, renderOnCanvas, autoCanvas)
		if candidateMarkdown == "" && candidatePlain == "" {
			return
		}
		if persistedAssistantID == 0 {
			storedAssistant, storeErr := a.store.AddChatMessage(sessionID, "assistant", candidateMarkdown, candidatePlain, candidateFormat)
			if storeErr != nil {
				if !persistWriteFailed {
					persistWriteFailed = true
					a.broadcastChatEvent(sessionID, map[string]interface{}{
						"type":  "error",
						"error": storeErr.Error(),
					})
				}
				return
			}
			persistedAssistantID = storedAssistant.ID
			persistedAssistantText = candidateMarkdown
			persistedAssistantPlain = candidatePlain
			persistedAssistantFormat = candidateFormat
			return
		}
		if candidateMarkdown == persistedAssistantText &&
			candidatePlain == persistedAssistantPlain &&
			candidateFormat == persistedAssistantFormat {
			return
		}
		if storeErr := a.store.UpdateChatMessageContent(persistedAssistantID, candidateMarkdown, candidatePlain, candidateFormat); storeErr != nil {
			if !persistWriteFailed {
				persistWriteFailed = true
				a.broadcastChatEvent(sessionID, map[string]interface{}{
					"type":  "error",
					"error": storeErr.Error(),
				})
			}
			return
		}
		persistedAssistantText = candidateMarkdown
		persistedAssistantPlain = candidatePlain
		persistedAssistantFormat = candidateFormat
	}

	appResp, err := a.appServerClient.SendPromptStream(ctx, appserver.PromptRequest{
		CWD:          a.cwdForProjectKey(session.ProjectKey),
		Prompt:       prompt,
		Model:        profile.Model,
		ThreadParams: profile.ThreadParams,
		TurnParams:   profile.TurnParams,
		Timeout:      assistantTurnTimeout,
	}, func(ev appserver.StreamEvent) {
		payload := map[string]interface{}{
			"type":        ev.Type,
			"thread_id":   ev.ThreadID,
			"turn_id":     ev.TurnID,
			"output_mode": outputMode,
		}
		shouldBroadcast := true
		switch ev.Type {
		case "thread_started":
			if strings.TrimSpace(ev.ThreadID) != "" {
				_ = a.store.UpdateChatSessionThread(sessionID, ev.ThreadID)
			}
		case "turn_started":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
		case "assistant_message":
			latestMessage = ev.Message
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(ev.Message, outputMode)
			persistAssistantSnapshot(ev.Message, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = ev.Message
			payload["delta"] = ev.Delta
			if renderPlan.RenderOnCanvas {
				payload["render_on_canvas"] = true
			}
			if renderPlan.AutoCanvas {
				payload["auto_canvas"] = true
			}
		case "turn_completed":
			if strings.TrimSpace(ev.Message) != "" {
				latestMessage = ev.Message
			}
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(latestMessage, outputMode)
			persistAssistantSnapshot(latestMessage, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = latestMessage
			if renderPlan.RenderOnCanvas {
				payload["render_on_canvas"] = true
			}
			if renderPlan.AutoCanvas {
				payload["auto_canvas"] = true
			}
		case "error":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			shouldBroadcast = false
		}
		if shouldBroadcast {
			a.broadcastChatEvent(sessionID, payload)
		}
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":    "turn_cancelled",
				"turn_id": latestTurnID,
			})
			return
		}
		errText := normalizeAssistantError(err)
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		payload := map[string]interface{}{
			"type":  "error",
			"error": errText,
		}
		if strings.TrimSpace(latestTurnID) != "" {
			payload["turn_id"] = latestTurnID
		}
		a.broadcastChatEvent(sessionID, payload)
		return
	}

	assistantText := strings.TrimSpace(appResp.Message)
	if assistantText == "" {
		assistantText = strings.TrimSpace(latestMessage)
	}
	if assistantText == "" {
		assistantText = "(assistant returned no content)"
	}

	assistantText = a.finalizeAssistantResponse(sessionID, session.ProjectKey, assistantText,
		&persistedAssistantID, &persistedAssistantText, appResp.TurnID, latestTurnID, appResp.ThreadID, outputMode)
	_ = assistantText
}

// finalizeAssistantResponse handles post-processing shared by both turn paths:
// voice mode stays chat-only, while silent mode mirrors assistant text to canvas,
// then persists final content and broadcasts assistant_output.
func (a *App) finalizeAssistantResponse(
	sessionID, projectKey, text string,
	persistedID *int64, persistedText *string,
	turnID, fallbackTurnID, threadID string,
	outputMode string,
) string {
	outputMode = normalizeTurnOutputMode(outputMode)
	canvasSessionID := a.resolveCanvasSessionID(projectKey)
	autoCanvas := false
	renderOnCanvas := false
	if isVoiceOutputMode(outputMode) {
		// Voice mode is chat-only; drop file blocks instead of mirroring chat output to canvas.
		_, cleaned := parseFileBlocks(text)
		text = cleaned
	} else {
		canvasCtx := a.resolveCanvasContext(projectKey)
		content := strings.TrimSpace(text)
		if content != "" && canvasSessionID != "" {
			block := fileBlock{
				Path:    "",
				Content: content,
			}
			if canOverwriteSilentAutoCanvasArtifact(canvasCtx) {
				block.Path = canvasCtx.ArtifactTitle
			}
			autoCanvas = a.writeCanvasFileBlock(projectKey, canvasSessionID, block)
			if !autoCanvas && strings.TrimSpace(block.Path) != "" {
				// If the current artifact path is stale or outside the active project
				// root, fall back to a fresh scratch artifact for this response.
				block.Path = ""
				autoCanvas = a.writeCanvasFileBlock(projectKey, canvasSessionID, block)
			}
		}
		renderOnCanvas = autoCanvas
	}
	text = stripLangTags(text)
	chatMarkdown, chatPlain, renderFormat := assistantFinalChatContent(text, renderOnCanvas, autoCanvas)

	a.refreshCanvasFromDisk(projectKey)

	if *persistedID == 0 {
		stored, err := a.store.AddChatMessage(sessionID, "assistant", chatMarkdown, chatPlain, renderFormat)
		if err != nil {
			a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
			return chatMarkdown
		}
		*persistedID = stored.ID
		*persistedText = chatMarkdown
	} else {
		if err := a.store.UpdateChatMessageContent(*persistedID, chatMarkdown, chatPlain, renderFormat); err != nil {
			a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
			return chatMarkdown
		}
		*persistedText = chatMarkdown
	}
	tid := strings.TrimSpace(turnID)
	if tid == "" {
		tid = fallbackTurnID
	}
	payload := map[string]interface{}{
		"type":             "assistant_output",
		"role":             "assistant",
		"id":               *persistedID,
		"turn_id":          tid,
		"thread_id":        threadID,
		"output_mode":      outputMode,
		"message":          chatMarkdown,
		"render_on_canvas": renderOnCanvas,
	}
	if autoCanvas {
		payload["auto_canvas"] = true
	}
	a.broadcastChatEvent(sessionID, payload)
	return chatMarkdown
}

func assistantFinalChatContent(text string, _ bool, _ bool) (string, string, string) {
	trimmed := strings.TrimSpace(text)
	companion := strings.TrimSpace(stripCanvasFileMarkers(trimmed))
	return companion, companion, "markdown"
}

func assistantMessageUsesCanvasBlocks(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, ":::file{")
}

type assistantRenderDecision struct {
	RenderOnCanvas bool
	AutoCanvas     bool
}

var assistantParagraphSplitRe = regexp.MustCompile(`\n\s*\n+`)

func assistantCompanionText(text string) string {
	candidate := strings.TrimSpace(text)
	if candidate == "" {
		return ""
	}
	if _, cleaned := parseFileBlocks(candidate); cleaned != "" {
		candidate = cleaned
	}
	candidate = stripLangTags(candidate)
	candidate = stripCanvasFileMarkers(candidate)
	return strings.TrimSpace(candidate)
}

func assistantParagraphCount(text string) int {
	cleaned := assistantCompanionText(text)
	if cleaned == "" {
		return 0
	}
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\n")
	parts := assistantParagraphSplitRe.Split(cleaned, -1)
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func assistantNeedsAutoCanvas(text string) bool {
	return assistantParagraphCount(text) > 1
}

func assistantRenderPlan(text string) assistantRenderDecision {
	return assistantRenderPlanForMode(text, turnOutputModeVoice)
}

func assistantRenderPlanForMode(text string, outputMode string) assistantRenderDecision {
	_ = text
	_ = outputMode
	// Chat output no longer requests canvas rendering in either mode.
	return assistantRenderDecision{RenderOnCanvas: false, AutoCanvas: false}
}

func assistantSnapshotContent(text string, renderOnCanvas bool, _ bool) (string, string, string) {
	candidate := stripLangTags(strings.TrimSpace(text))
	if candidate == "" {
		return "", "", "markdown"
	}
	chat := assistantCompanionText(candidate)
	if chat == "" {
		if renderOnCanvas {
			return "", "", "text"
		}
		return "", "", "markdown"
	}
	return chat, chat, "markdown"
}

func normalizeTurnOutputMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", turnOutputModeVoice:
		return turnOutputModeVoice
	case turnOutputModeSilent:
		return turnOutputModeSilent
	}
	return turnOutputModeVoice
}

func isVoiceOutputMode(mode string) bool {
	return normalizeTurnOutputMode(mode) == turnOutputModeVoice
}

func (a *App) cwdForProjectKey(projectKey string) string {
	key := strings.TrimSpace(projectKey)
	if key != "" {
		if project, err := a.store.GetProjectByProjectKey(key); err == nil {
			root := strings.TrimSpace(project.RootPath)
			if root != "" {
				return root
			}
		}
		return key
	}
	if strings.TrimSpace(a.localProjectDir) != "" {
		return strings.TrimSpace(a.localProjectDir)
	}
	return "."
}

func normalizeAssistantError(err error) string {
	if err == nil {
		return "assistant request failed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "assistant request timed out"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "assistant request timed out"
	}
	errText := strings.TrimSpace(err.Error())
	if errText == "" {
		return "assistant request failed"
	}
	if strings.Contains(strings.ToLower(errText), "i/o timeout") {
		return "assistant request timed out"
	}
	return errText
}

func (a *App) resolveCanvasSessionID(projectKey string) string {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return ""
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return ""
	}
	return a.canvasSessionIDForProject(project)
}

func (a *App) resolveCanvasContext(projectKey string) *canvasContext {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return nil
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return nil
	}
	sid := a.canvasSessionIDForProject(project)
	a.mu.Lock()
	port, ok := a.tunnelPorts[sid]
	a.mu.Unlock()
	if !ok {
		return nil
	}
	status, err := a.mcpToolsCall(port, "canvas_status", map[string]interface{}{"session_id": sid})
	if err != nil {
		return nil
	}
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		return nil
	}
	title := strings.TrimSpace(fmt.Sprint(active["title"]))
	if title == "<nil>" {
		title = ""
	}
	kind := strings.TrimSpace(fmt.Sprint(active["kind"]))
	if kind == "<nil>" {
		kind = ""
	}
	return &canvasContext{HasArtifact: true, ArtifactTitle: title, ArtifactKind: kind}
}

type canvasContext struct {
	HasArtifact   bool
	ArtifactTitle string
	ArtifactKind  string
}

func buildPromptFromHistory(mode string, messages []store.ChatMessage, canvas *canvasContext) string {
	return buildPromptFromHistoryForMode(mode, messages, canvas, turnOutputModeVoice, "")
}

func buildPromptFromHistoryForMode(mode string, messages []store.ChatMessage, canvas *canvasContext, outputMode string, modelAlias string) string {
	isVoiceMode := isVoiceOutputMode(outputMode)
	const maxHistory = 80
	if len(messages) > maxHistory {
		messages = messages[len(messages)-maxHistory:]
	}
	var b strings.Builder

	promptTemplate := loadModePromptTemplate(outputMode, defaultVoiceHistoryPrompt, "")
	if isVoiceMode {
		b.WriteString(promptTemplate)
	}
	if !isVoiceMode {
		silentPrompt := loadModePromptTemplate(outputMode, "", "")
		if silentPrompt != "" {
			b.WriteString(silentPrompt)
		}
	}

	appendDelegationSection(&b)
	if hints := modelprofile.ModelSystemHints(modelAlias); hints != "" {
		b.WriteString(hints)
		b.WriteString("\n")
	}

	if isVoiceMode && canvas != nil && canvas.HasArtifact {
		b.WriteString("## Current Artifact\n")
		fmt.Fprintf(&b, "- Active artifact tab: %q (kind: %s)\n\n", canvas.ArtifactTitle, canvas.ArtifactKind)
	}

	if strings.EqualFold(strings.TrimSpace(mode), "plan") {
		b.WriteString("You are in plan mode. Focus on analysis, design, and specification before implementation.\n\n")
	}

	b.WriteString("Conversation transcript:\n")
	for _, msg := range messages {
		content := strings.TrimSpace(msg.ContentPlain)
		if content == "" {
			content = strings.TrimSpace(msg.ContentMarkdown)
		}
		if content == "" {
			continue
		}
		role := strings.ToUpper(strings.TrimSpace(msg.Role))
		if role == "" {
			role = "USER"
		}
		b.WriteString(role)
		b.WriteString(":\n")
		if role == "USER" {
			content = applyDelegationHints(content)
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	if isVoiceMode {
		b.WriteString("Reply as ASSISTANT.")
	}
	return b.String()
}

// buildTurnPrompt constructs a prompt for a resumed thread: only the latest
// user message plus optional canvas context update.
func buildTurnPrompt(messages []store.ChatMessage, canvas *canvasContext) string {
	return buildTurnPromptForMode(messages, canvas, turnOutputModeVoice, "")
}

func buildTurnPromptForMode(messages []store.ChatMessage, canvas *canvasContext, outputMode string, modelAlias string) string {
	isVoiceMode := isVoiceOutputMode(outputMode)
	var lastUserMsg string
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			lastUserMsg = strings.TrimSpace(messages[i].ContentPlain)
			if lastUserMsg == "" {
				lastUserMsg = strings.TrimSpace(messages[i].ContentMarkdown)
			}
			break
		}
	}
	if lastUserMsg == "" {
		return ""
	}
	var b strings.Builder
	if isVoiceMode {
		b.WriteString(loadModePromptTemplate(outputMode, defaultVoiceTurnPrompt, ""))
		if canvas != nil && canvas.HasArtifact {
			fmt.Fprintf(&b, "[Active artifact tab: %q (kind: %s)]\n\n", canvas.ArtifactTitle, canvas.ArtifactKind)
		}
	} else {
		appendDelegationSection(&b)
	}
	if hints := modelprofile.ModelSystemHints(modelAlias); hints != "" {
		b.WriteString(hints)
		b.WriteString("\n")
	}
	b.WriteString(applyDelegationHints(lastUserMsg))
	return b.String()
}

func appendDelegationSection(b *strings.Builder) {
	b.WriteString("## Delegation\n")
	b.WriteString("Use `delegate_to_model` for tasks that benefit from another model.\n")
	b.WriteString("- 'let codex do this' / 'ask codex' -> model='codex'. 'ask gpt' / 'use the big model' -> model='gpt'.\n")
	b.WriteString("- Auto-delegate complex multi-file coding or deep analysis to 'codex'.\n")
	b.WriteString("- Provide 'context' and 'system_prompt' when delegating.\n")
	b.WriteString("- Do NOT delegate simple conversational replies.\n")
	b.WriteString("- Delegates have full filesystem access and edit files directly on disk.\n")
	b.WriteString("- Do NOT parse or apply patches/diffs from the delegate response.\n")
	b.WriteString("- `delegate_to_model` starts an async job and returns `job_id` immediately.\n")
	b.WriteString("- Use `delegate_to_model_status` with `job_id` and `after_seq` to fetch incremental progress.\n")
	b.WriteString("- Summarize progress updates for the user periodically while polling status.\n")
	b.WriteString("- Use `delegate_to_model_cancel` if the user asks to stop.\n")
	b.WriteString("- Final status includes `files_changed` and final `message`; relay that summary to the user.\n\n")
}

func loadModePromptTemplate(outputMode, defaultPrompt, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	switch strings.TrimSpace(outputMode) {
	case turnOutputModeVoice:
		// Voice mode always uses the built-in Go prompt template.
		return normalizePromptTemplate(firstNonEmptyPrompt(defaultPrompt, fallback))
	case turnOutputModeSilent:
		// Silent mode always uses the built-in Go prompt template.
		return normalizePromptTemplate(firstNonEmptyPrompt(defaultPrompt, fallback))
	default:
		return normalizePromptTemplate(fallback)
	}
}

func firstNonEmptyPrompt(primary, fallback string) string {
	primary = strings.TrimSpace(primary)
	if primary != "" {
		return primary
	}
	return strings.TrimSpace(fallback)
}

func normalizePromptTemplate(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	return prompt + "\n\n"
}

func currentPromptContractDigest() string {
	historyPrompt := buildPromptFromHistory("chat", nil, nil)
	turnPrompt := buildTurnPrompt([]store.ChatMessage{{
		Role:         "user",
		ContentPlain: "prompt-contract-sentinel",
	}}, nil)
	sum := sha256.Sum256([]byte(historyPrompt + "\n---\n" + turnPrompt))
	return hex.EncodeToString(sum[:])
}

func (a *App) ensurePromptContractFresh() error {
	currentDigest := strings.TrimSpace(currentPromptContractDigest())
	if currentDigest == "" {
		return nil
	}
	storedDigest, err := a.store.AppState(promptContractStateKey)
	if err != nil {
		return err
	}
	storedDigest = strings.TrimSpace(storedDigest)
	if storedDigest == "" {
		return a.store.SetAppState(promptContractStateKey, currentDigest)
	}
	if storedDigest == currentDigest {
		return nil
	}
	if _, err := a.clearAllAgentsAndContexts(""); err != nil {
		return err
	}
	return a.store.SetAppState(promptContractStateKey, currentDigest)
}

type delegationHint struct {
	Detected bool
	Model    string
	Task     string
}

var delegationPatterns = regexp.MustCompile(
	`(?i)^(?:let |ask |use )(codex|gpt|spark|the big model)\b[,: ]*(.*)`,
)

func detectDelegationHint(text string) delegationHint {
	m := delegationPatterns.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return delegationHint{}
	}
	model := strings.ToLower(m[1])
	if model == "the big model" {
		model = "gpt"
	}
	task := strings.TrimSpace(m[2])
	if task == "" {
		task = text
	}
	return delegationHint{Detected: true, Model: model, Task: task}
}

func applyDelegationHints(text string) string {
	hint := detectDelegationHint(text)
	if !hint.Detected {
		return text
	}
	return fmt.Sprintf("[Delegation hint: user wants model=%q] %s", hint.Model, text)
}

func (a *App) handleChatWS(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	if _, err := a.store.GetChatSession(sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	ws, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := newChatWSConn(ws)
	a.mu.Lock()
	if a.chatWS[sessionID] == nil {
		a.chatWS[sessionID] = map[*chatWSConn]struct{}{}
	}
	a.chatWS[sessionID][conn] = struct{}{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if set := a.chatWS[sessionID]; set != nil {
			delete(set, conn)
		}
		a.mu.Unlock()
		_ = ws.Close()
	}()

	if session, err := a.store.GetChatSession(sessionID); err == nil {
		_ = conn.writeJSON(map[string]interface{}{
			"type": "mode_changed",
			"mode": session.Mode,
		})
	}
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.BinaryMessage:
			handleSTTBinaryChunk(conn, data)
		case websocket.TextMessage:
			handleChatWSTextMessage(a, conn, sessionID, data)
		}
	}
}

func (a *App) broadcastChatEvent(sessionID string, payload map[string]interface{}) {
	payload["session_id"] = sessionID
	encoded, _ := json.Marshal(payload)
	turnID, _ := payload["turn_id"].(string)
	eventType, _ := payload["type"].(string)
	_ = a.store.AddChatEvent(sessionID, turnID, eventType, string(encoded))

	a.mu.Lock()
	clients := make([]*chatWSConn, 0)
	for conn := range a.chatWS[sessionID] {
		clients = append(clients, conn)
	}
	a.mu.Unlock()
	for _, conn := range clients {
		_ = conn.writeText(encoded)
	}
}
