package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/plugins"
)

const (
	turnOutputModeVoice    = "voice"
	turnOutputModeSilent   = "silent"
	promptContractStateKey = "chat_prompt_contract_sha256"
)

const defaultVoiceHistoryPrompt = `You are Tabura, an AI assistant.
Chat text is always spoken via TTS.

## Response Format

Use exactly one response shape:

1. Spoken chat must be one paragraph max.
   If your response needs more than one paragraph, write that long content to a temp file and show it on canvas.
2. Canvas content must appear only inside :::file blocks with a file path.
   For temporary canvas files, create/remove paths via temp_file_create and temp_file_remove tools.
   Do not use :::canvas blocks.

If output needs more than one paragraph, put it in a temp file with temp_file_create and render on canvas.
Do not return metadata-only chat.

Write naturally for speech. Avoid raw paths, URLs, or code in prose.
Use [lang:de] at the start of your answer when responding in German. Default is English.

Voice mode is chat-only:
- Do not emit :::file blocks.
- Do not emit :::canvas blocks.
- Do not render chat output on canvas.
- Keep spoken responses concise.

When user asks to show/open an existing file, do NOT paste that file body into chat. Use canvas_artifact_show with a brief spoken confirmation.

Line references: when the user mentions [Line N of "file"], apply changes at that location.

When you need the user to tap or click a location, emit exactly [[request_position:Tap where you want it]] on its own line.

## PR review fast path:
When asked to review a PR, open PR view via gh CLI, read the diff, and respond with analysis.
Publish exactly one file block at path .tabura/artifacts/pr/pr-<number>.diff with the patch content.
`

const defaultVoiceTurnPrompt = `Voice mode is chat-only:
- Reply with spoken chat text only.
- Do not emit :::file blocks.
- Do not emit :::canvas blocks.
- Do not render chat output on canvas.

When user asks to show/open an existing file, do NOT paste file body into chat; use canvas_artifact_show and keep chat text brief.

When you need the user to tap or click a location, emit exactly [[request_position:Tap where you want it]] on its own line.

`

type chatMessageRequest struct {
	Text        string             `json:"text"`
	OutputMode  string             `json:"output_mode"`
	CaptureMode string             `json:"capture_mode,omitempty"`
	Cursor      *chatCursorContext `json:"cursor,omitempty"`
	LocalOnly   bool               `json:"local_only,omitempty"`
}

type chatCommandRequest struct {
	Command string `json:"command"`
}

func (a *App) applyPluginHook(ctx context.Context, req plugins.HookRequest) plugins.HookResult {
	if a == nil {
		return plugins.HookResult{Text: req.Text}
	}
	text := req.Text
	for _, provider := range a.hookProviders {
		result := provider.Apply(ctx, req)
		if result.Blocked {
			return result
		}
		text = result.Text
		req.Text = text
	}
	return plugins.HookResult{Text: text}
}

func (a *App) applyPreAssistantPromptHook(ctx context.Context, sessionID, projectKey, outputMode, mode, prompt string) (string, error) {
	result := a.applyPluginHook(ctx, plugins.HookRequest{
		Hook:       plugins.HookChatPreAssistantPrompt,
		SessionID:  sessionID,
		ProjectKey: projectKey,
		OutputMode: outputMode,
		Text:       prompt,
		Metadata: map[string]interface{}{
			"mode": mode,
		},
	})
	if result.Blocked {
		reason := strings.TrimSpace(result.Reason)
		if reason == "" {
			reason = "assistant prompt blocked by plugin"
		}
		return "", errors.New(reason)
	}
	return strings.TrimSpace(result.Text), nil
}

func (a *App) handleChatSessionCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req struct {
		WorkspaceID *int64 `json:"workspace_id"`
		ProjectKey  string `json:"project_key"`
		ProjectID   string `json:"project_id"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
	}
	workspace, resolvedProject, err := a.resolveChatSessionTarget(req.ProjectID, req.ProjectKey, req.WorkspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	session, err := a.store.GetOrCreateChatSessionForWorkspace(workspace.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	canvasSessionID := LocalSessionID
	projectID := ""
	if resolvedProject != nil {
		projectID = resolvedProject.ID
		canvasSessionID = a.canvasSessionIDForProject(*resolvedProject)
		if err := a.store.SetActiveProjectID(resolvedProject.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = a.markProjectSeen(*resolvedProject)
		if err := a.ensureProjectCanvasReady(*resolvedProject); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok":                true,
		"session_id":        session.ID,
		"workspace_id":      session.WorkspaceID,
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
	if _, err := a.store.GetChatSession(sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	state := a.projectRunStateForSession(sessionID)
	writeJSON(w, map[string]interface{}{
		"ok":             true,
		"active_turns":   state.ActiveTurns,
		"queued_turns":   state.QueuedTurns,
		"is_working":     state.IsWorking,
		"status":         state.Status,
		"active_turn_id": state.ActiveTurnID,
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
	if _, err := a.store.GetChatSession(sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	activeCanceled, queuedCanceled := a.cancelChatWork(sessionID)
	writeJSON(w, map[string]interface{}{
		"ok":              true,
		"canceled":        activeCanceled + queuedCanceled,
		"active_canceled": activeCanceled,
		"queued_canceled": queuedCanceled,
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
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
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
	pluginResult := a.applyPluginHook(r.Context(), plugins.HookRequest{
		Hook:       plugins.HookChatPreUserMessage,
		SessionID:  sessionID,
		ProjectKey: session.ProjectKey,
		OutputMode: outputMode,
		Text:       text,
		Metadata: map[string]interface{}{
			"local_only": req.LocalOnly,
		},
	})
	if pluginResult.Blocked {
		reason := strings.TrimSpace(pluginResult.Reason)
		if reason == "" {
			reason = "message blocked by plugin"
		}
		http.Error(w, reason, http.StatusBadRequest)
		return
	}
	text = strings.TrimSpace(pluginResult.Text)
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
	queuedTurns := a.enqueueAssistantTurn(sessionID, outputMode, chatTurnOptions{
		localOnly:   req.LocalOnly,
		messageID:   storedUser.ID,
		captureMode: req.CaptureMode,
		cursor:      req.Cursor,
	})
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
		canceled := activeCanceled + queuedCanceled
		message := "No assistant turn is currently running."
		if canceled > 0 {
			message = "Stopping assistant work and clearing queued prompts."
		}
		return map[string]interface{}{
			"name":            name,
			"canceled":        canceled,
			"active_canceled": activeCanceled,
			"queued_canceled": queuedCanceled,
			"message":         message,
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
			"sessions_closed":   report.SessionsClosed,
			"tmp_files_cleared": report.TempFilesCleared,
		}, nil
	case "compact":
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
	a.hub.registerChat(sessionID, conn)
	defer func() {
		if participantSessionID, ok := releaseParticipantSession(a, conn); ok {
			log.Printf("participant session stopped on websocket disconnect: %s", participantSessionID)
		}
		a.hub.unregisterChat(sessionID, conn)
		_ = ws.Close()
	}()

	if session, err := a.store.GetChatSession(sessionID); err == nil {
		_ = conn.writeJSON(map[string]interface{}{
			"type": "mode_changed",
			"mode": session.Mode,
		})
	}
	_ = conn.writeJSON(map[string]interface{}{
		"type":   "live_policy_changed",
		"policy": a.LivePolicy().String(),
	})
	if states, err := a.allWorkspaceBusyStates(); err == nil {
		_ = conn.writeJSON(map[string]interface{}{
			"type":   "workspace_busy_changed",
			"states": states,
		})
	}
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		switch mt {
		case websocket.BinaryMessage:
			conn.participantMu.Lock()
			isParticipant := conn.participantActive
			conn.participantMu.Unlock()
			if isParticipant {
				handleParticipantBinaryChunk(a, conn, data)
			} else {
				handleSTTBinaryChunk(conn, data)
			}
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

	for _, conn := range a.hub.chatClients(sessionID) {
		_ = conn.writeText(encoded)
	}
}
