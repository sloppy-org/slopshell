package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabula/internal/appserver"
	"github.com/krystophny/tabula/internal/store"
)

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
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
	}
	projectKey := strings.TrimSpace(req.ProjectKey)
	if projectKey == "" {
		projectKey = strings.TrimSpace(a.localProjectDir)
	}
	session, err := a.store.GetOrCreateChatSession(projectKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":                true,
		"session_id":        session.ID,
		"project_key":       session.ProjectKey,
		"mode":              session.Mode,
		"canvas_session_id": LocalSessionID,
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
	go a.runAssistantTurn(sessionID)
	writeJSON(w, map[string]interface{}{
		"ok":         true,
		"kind":       "turn_queued",
		"message_id": storedUser.ID,
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
	default:
		return nil, fmt.Errorf("unknown command: /%s", name)
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
	prompt := buildPromptFromHistory(session.Mode, messages)
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	latestMessage := ""
	latestTurnID := ""
	appResp, err := a.appServerClient.SendPromptStream(ctx, appserver.PromptRequest{
		CWD:     a.localProjectDir,
		Prompt:  prompt,
		Model:   a.appServerModel,
		Timeout: 3 * time.Minute,
	}, func(ev appserver.StreamEvent) {
		payload := map[string]interface{}{
			"type":      ev.Type,
			"thread_id": ev.ThreadID,
			"turn_id":   ev.TurnID,
		}
		switch ev.Type {
		case "thread_started":
			if strings.TrimSpace(ev.ThreadID) != "" {
				_ = a.store.UpdateChatSessionThread(sessionID, ev.ThreadID)
			}
		case "assistant_message":
			latestMessage = ev.Message
			latestTurnID = ev.TurnID
			payload["message"] = ev.Message
			payload["delta"] = ev.Delta
		case "turn_completed":
			if strings.TrimSpace(ev.Message) != "" {
				latestMessage = ev.Message
			}
			latestTurnID = ev.TurnID
			payload["message"] = latestMessage
		case "error":
			payload["error"] = ev.Error
		}
		a.broadcastChatEvent(sessionID, payload)
	})
	if err != nil {
		errText := strings.TrimSpace(err.Error())
		if errText == "" {
			errText = "assistant request failed"
		}
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":  "error",
			"error": errText,
		})
		return
	}

	assistantText := strings.TrimSpace(appResp.Message)
	if assistantText == "" {
		assistantText = strings.TrimSpace(latestMessage)
	}
	if assistantText == "" {
		assistantText = "(assistant returned no content)"
	}
	storedAssistant, storeErr := a.store.AddChatMessage(sessionID, "assistant", assistantText, assistantText, "markdown")
	if storeErr != nil {
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":  "error",
			"error": storeErr.Error(),
		})
		return
	}
	turnID := strings.TrimSpace(appResp.TurnID)
	if turnID == "" {
		turnID = latestTurnID
	}
	a.broadcastChatEvent(sessionID, map[string]interface{}{
		"type":      "message_persisted",
		"role":      "assistant",
		"id":        storedAssistant.ID,
		"turn_id":   turnID,
		"thread_id": appResp.ThreadID,
		"message":   assistantText,
	})
}

func buildPromptFromHistory(mode string, messages []store.ChatMessage) string {
	const maxHistory = 80
	if len(messages) > maxHistory {
		messages = messages[len(messages)-maxHistory:]
	}
	var b strings.Builder
	if strings.EqualFold(strings.TrimSpace(mode), "plan") {
		b.WriteString("You are in plan mode. Focus on analysis, design, and specification before implementation.\\n\\n")
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
	a.mu.Lock()
	if a.chatWS[sessionID] == nil {
		a.chatWS[sessionID] = map[*websocket.Conn]struct{}{}
	}
	a.chatWS[sessionID][ws] = struct{}{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		if set := a.chatWS[sessionID]; set != nil {
			delete(set, ws)
		}
		a.mu.Unlock()
		_ = ws.Close()
	}()

	if session, err := a.store.GetChatSession(sessionID); err == nil {
		_ = ws.WriteJSON(map[string]interface{}{
			"type": "mode_changed",
			"mode": session.Mode,
		})
	}
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			return
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
	clients := make([]*websocket.Conn, 0)
	for ws := range a.chatWS[sessionID] {
		clients = append(clients, ws)
	}
	a.mu.Unlock()
	for _, ws := range clients {
		_ = ws.WriteMessage(websocket.TextMessage, encoded)
	}
}
