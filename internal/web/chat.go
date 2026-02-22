package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/store"
)

const assistantTurnTimeout = 20 * time.Minute

type chatMessageRequest struct {
	Text string `json:"text"`
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
	if _, err := a.store.GetChatSession(sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	activeTurns := a.activeChatTurnCount(sessionID)
	queuedTurns := a.queuedChatTurnCount(sessionID)
	writeJSON(w, map[string]interface{}{
		"ok":           true,
		"active_turns": activeTurns,
		"queued_turns": queuedTurns,
		"is_working":   activeTurns > 0 || queuedTurns > 0,
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
	if text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	if strings.HasPrefix(text, "/") {
		result, err := a.executeChatCommand(sessionID, text)
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
	queuedTurns := a.enqueueAssistantTurn(sessionID)
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
	case "canvas", "review":
		message := "Open canvas requested."
		_, _ = a.store.AddChatMessage(sessionID, "system", message, message, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":    "action",
			"action":  "open_canvas",
			"message": message,
		})
		return map[string]interface{}{
			"name":    name,
			"action":  "open_canvas",
			"message": message,
		}, nil
	case "chat":
		message := "Open chat requested."
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":    "action",
			"action":  "open_chat",
			"message": message,
		})
		return map[string]interface{}{
			"name":    name,
			"action":  "open_chat",
			"message": message,
		}, nil
	case "commit":
		message := "Commit requested."
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":    "action",
			"action":  "commit_canvas",
			"message": message,
		})
		return map[string]interface{}{
			"name":    name,
			"action":  "commit_canvas",
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
	default:
		return nil, fmt.Errorf("unknown command: /%s", name)
	}
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

func (a *App) enqueueAssistantTurn(sessionID string) int {
	a.mu.Lock()
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

func (a *App) dequeueAssistantTurn(sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	queued := a.chatTurnQueue[sessionID]
	if queued <= 0 {
		return false
	}
	queued--
	if queued <= 0 {
		delete(a.chatTurnQueue, sessionID)
		return true
	}
	a.chatTurnQueue[sessionID] = queued
	return true
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
		if !a.dequeueAssistantTurn(sessionID) {
			if a.markAssistantWorkerIdleIfQueueEmpty(sessionID) {
				return
			}
			continue
		}
		a.runAssistantTurn(sessionID)
	}
}

func (a *App) runAssistantTurn(sessionID string) {
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
	canvasCtx := a.resolveCanvasContext(session.ProjectKey)
	prompt := buildPromptFromHistory(session.Mode, messages, canvasCtx)
	if strings.TrimSpace(prompt) == "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
		return
	}
	if a.appServerClient == nil {
		errText := "app-server is not configured"
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), assistantTurnTimeout)
	runID := randomToken()
	a.registerActiveChatTurn(sessionID, runID, cancel)
	defer func() {
		cancel()
		a.unregisterActiveChatTurn(sessionID, runID)
	}()

	latestMessage := ""
	latestTurnID := ""
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	persistWriteFailed := false
	persistAssistantSnapshot := func(text string) {
		candidate := strings.TrimSpace(text)
		if candidate == "" {
			return
		}
		if persistedAssistantID == 0 {
			storedAssistant, storeErr := a.store.AddChatMessage(sessionID, "assistant", candidate, candidate, "markdown")
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
			persistedAssistantText = candidate
			return
		}
		if candidate == persistedAssistantText {
			return
		}
		if storeErr := a.store.UpdateChatMessageContent(persistedAssistantID, candidate, candidate, "markdown"); storeErr != nil {
			if !persistWriteFailed {
				persistWriteFailed = true
				a.broadcastChatEvent(sessionID, map[string]interface{}{
					"type":  "error",
					"error": storeErr.Error(),
				})
			}
			return
		}
		persistedAssistantText = candidate
	}

	appResp, err := a.appServerClient.SendPromptStream(ctx, appserver.PromptRequest{
		CWD:     a.cwdForProjectKey(session.ProjectKey),
		Prompt:  prompt,
		Model:   a.appServerModel,
		Timeout: assistantTurnTimeout,
	}, func(ev appserver.StreamEvent) {
		payload := map[string]interface{}{
			"type":      ev.Type,
			"thread_id": ev.ThreadID,
			"turn_id":   ev.TurnID,
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
			persistAssistantSnapshot(ev.Message)
			payload["message"] = ev.Message
			payload["delta"] = ev.Delta
		case "turn_completed":
			if strings.TrimSpace(ev.Message) != "" {
				latestMessage = ev.Message
			}
			latestTurnID = ev.TurnID
			persistAssistantSnapshot(latestMessage)
			payload["message"] = latestMessage
		case "error":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			// Stream-level errors are normalized and emitted by the final error path
			// below so the UI receives one clean terminal error event.
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

	// Process canvas action markers: push content to canvas and strip markers from chat text.
	if actions, cleaned := parseCanvasActions(assistantText); len(actions) > 0 {
		canvasSessionID := a.resolveCanvasSessionID(session.ProjectKey)
		if canvasSessionID != "" {
			a.executeCanvasActions(canvasSessionID, actions)
		}
		assistantText = cleaned
	}

	if persistedAssistantID == 0 {
		storedAssistant, storeErr := a.store.AddChatMessage(sessionID, "assistant", assistantText, assistantText, "markdown")
		if storeErr != nil {
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":  "error",
				"error": storeErr.Error(),
			})
			return
		}
		persistedAssistantID = storedAssistant.ID
		persistedAssistantText = assistantText
	} else if assistantText != persistedAssistantText {
		if storeErr := a.store.UpdateChatMessageContent(persistedAssistantID, assistantText, assistantText, "markdown"); storeErr != nil {
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":  "error",
				"error": storeErr.Error(),
			})
			return
		}
	}
	turnID := strings.TrimSpace(appResp.TurnID)
	if turnID == "" {
		turnID = latestTurnID
	}
	a.broadcastChatEvent(sessionID, map[string]interface{}{
		"type":      "message_persisted",
		"role":      "assistant",
		"id":        persistedAssistantID,
		"turn_id":   turnID,
		"thread_id": appResp.ThreadID,
		"message":   assistantText,
	})
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
	const maxHistory = 80
	if len(messages) > maxHistory {
		messages = messages[len(messages)-maxHistory:]
	}
	var b strings.Builder

	b.WriteString("You are Tabura, an AI assistant with access to a canvas for showing artifacts.\n\n")
	b.WriteString("## Available Actions\n")
	b.WriteString("When appropriate, include these action markers in your response:\n\n")
	b.WriteString("- To show text/markdown on canvas: wrap content in :::canvas_show{title=\"Title\"}...:::\n")
	b.WriteString("- To show code on canvas: wrap in :::canvas_show{title=\"file.go\" kind=\"code\"}...:::\n")
	b.WriteString("- When asked to apply review comments, output the full revised document wrapped in canvas_show markers.\n\n")
	b.WriteString("## Guidelines\n")
	b.WriteString("- Use canvas_show for any substantial content (documents, code, analysis) so the user can review it visually.\n")
	b.WriteString("- For short answers or conversational replies, respond normally without canvas markers.\n")
	b.WriteString("- When applying review comments from a commit, output the complete revised artifact.\n\n")

	if canvas != nil && canvas.HasArtifact {
		b.WriteString("## Current Canvas State\n")
		fmt.Fprintf(&b, "- Active artifact: %q (kind: %s)\n\n", canvas.ArtifactTitle, canvas.ArtifactKind)
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
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	b.WriteString("Reply as ASSISTANT.")
	return b.String()
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
	if payload == nil {
		return
	}
	payload["session_id"] = sessionID
	encoded, _ := json.Marshal(payload)
	turnID := strings.TrimSpace(fmt.Sprint(payload["turn_id"]))
	eventType := strings.TrimSpace(fmt.Sprint(payload["type"]))
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

// injectChatMessage stores a message in the chat session, broadcasts it to
// WebSocket clients, and optionally enqueues an assistant turn so the LLM
// processes it. Returns the stored message ID.
func (a *App) injectChatMessage(chatSessionID, role, text string, triggerAssistant bool) (int64, error) {
	stored, err := a.store.AddChatMessage(chatSessionID, role, text, text, "text")
	if err != nil {
		return 0, err
	}
	a.broadcastChatEvent(chatSessionID, map[string]interface{}{
		"type":    "message_accepted",
		"role":    role,
		"content": text,
		"id":      stored.ID,
	})
	if triggerAssistant {
		a.enqueueAssistantTurn(chatSessionID)
	}
	return stored.ID, nil
}
