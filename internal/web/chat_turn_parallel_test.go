package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/cerebras"
)

type mockParallelAppServerState struct {
	mu          sync.Mutex
	turnStarts  int
	writeErrors int
}

func (s *mockParallelAppServerState) recordTurnStart() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnStarts++
}

func (s *mockParallelAppServerState) recordWriteError() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeErrors++
}

func (s *mockParallelAppServerState) snapshot() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turnStarts, s.writeErrors
}

func setupMockParallelAppServer(t *testing.T, delay time.Duration, finalMessage string) (*httptest.Server, *mockParallelAppServerState) {
	t.Helper()
	state := &mockParallelAppServerState{}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode app-server message: %v", err)
			}
			method := strings.TrimSpace(strFromAny(msg["method"]))
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{
					"id":     msg["id"],
					"result": map[string]interface{}{"userAgent": "parallel-test"},
				})
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-parallel"},
					},
				})
			case "turn/start":
				state.recordTurnStart()
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-parallel"},
					},
				})
				time.Sleep(delay)
				if err := conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "agentMessage",
							"text": finalMessage,
						},
					},
				}); err != nil {
					state.recordWriteError()
				}
				if err := conn.WriteJSON(map[string]interface{}{
					"method": "turn/completed",
					"params": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-parallel", "status": "completed"},
					},
				}); err != nil {
					state.recordWriteError()
				}
				return
			}
		}
	}))
	return server, state
}

func setupMockCerebrasServer(t *testing.T, status int, delay time.Duration, content string) (*httptest.Server, *int) {
	t.Helper()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			t.Fatalf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer token-cerebras" {
			t.Fatalf("Authorization = %q, want Bearer token-cerebras", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if got := strings.TrimSpace(strFromAny(payload["reasoning_effort"])); got != cerebras.DefaultReasoningEffort {
			t.Fatalf("reasoning_effort = %q, want %q", got, cerebras.DefaultReasoningEffort)
		}
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"content": content,
						},
					},
				},
				"usage": map[string]any{
					"total_tokens": 64,
				},
			})
			return
		}
		_, _ = w.Write([]byte("upstream error"))
	}))
	return server, &requests
}

func newParallelTestApp(t *testing.T, appServerURL string) *App {
	t.Helper()
	app, err := New(t.TempDir(), "", "", appServerURL, "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})
	return app
}

func collectWSJSONTypesUntil(t *testing.T, clientConn *websocket.Conn, timeout time.Duration, terminalType string) []map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var out []map[string]interface{}
	for time.Now().Before(deadline) {
		if err := clientConn.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		mt, data, err := clientConn.ReadMessage()
		if err != nil {
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				continue
			}
			t.Fatalf("ReadMessage: %v", err)
		}
		if mt != websocket.TextMessage {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}
		out = append(out, payload)
		if strings.TrimSpace(strFromAny(payload["type"])) == terminalType {
			return out
		}
	}
	t.Fatalf("timed out waiting for websocket message type %q", terminalType)
	return nil
}

func wsTypes(payloads []map[string]interface{}) []string {
	out := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		out = append(out, strings.TrimSpace(strFromAny(payload["type"])))
	}
	return out
}

func countAssistantMessages(t *testing.T, app *App, sessionID string) int {
	t.Helper()
	messages, err := app.store.ListChatMessages(sessionID, 20)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	count := 0
	for _, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.Role), "assistant") {
			count++
		}
	}
	return count
}

func TestFinalParallelCommandTextPrefersLocalFallbackOverAck(t *testing.T) {
	evaluation := localTurnEvaluation{
		handled:  true,
		text:     "Focused on Focused.",
		payloads: []map[string]interface{}{{"type": "focus_workspace"}},
		ack:      "Switching to Focused.",
	}
	if got := finalParallelCommandText(evaluation, evaluation.ack); got != evaluation.text {
		t.Fatalf("finalParallelCommandText() = %q, want %q", got, evaluation.text)
	}
}

func TestCommandParallelTurnProvisionalTextPrefersExplicitAck(t *testing.T) {
	evaluation := localTurnEvaluation{
		handled:  true,
		text:     "Focused on Focused.",
		payloads: []map[string]interface{}{{"type": "focus_workspace"}},
		ack:      "Switching to Focused.",
	}
	if got := commandParallelTurnProvisionalText("focus on focused", evaluation); got != evaluation.ack {
		t.Fatalf("commandParallelTurnProvisionalText() = %q, want %q", got, evaluation.ack)
	}
}

func TestRunAssistantTurnParallelLocalHighConfidenceClaimsTurn(t *testing.T) {
	appServer, serverState := setupMockParallelAppServer(t, 200*time.Millisecond, "Spark fallback.")
	defer appServer.Close()
	wsURL := "ws" + strings.TrimPrefix(appServer.URL, "http")

	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"kind":"local_answer","text":"You are in the active workspace.","confidence":"high"}`)
	defer llm.Close()

	app := newParallelTestApp(t, wsURL)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "what workspace am I in?", "what workspace am I in?", "text"); err != nil {
		t.Fatalf("AddChatMessage(user): %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "You are in the active workspace." {
		t.Fatalf("assistant message = %q, want local high-confidence reply", got)
	}
	if got := countAssistantMessages(t, app, session.ID); got != 1 {
		t.Fatalf("assistant message count = %d, want 1", got)
	}

	payloads := collectWSJSONTypesUntil(t, clientConn, 2*time.Second, "assistant_output")
	types := strings.Join(wsTypes(payloads), ",")
	if !strings.Contains(types, "turn_claimed") || !strings.Contains(types, "turn_committed") {
		t.Fatalf("websocket types = %s, want turn_claimed and turn_committed", types)
	}

	starts, _ := serverState.snapshot()
	if starts != 1 {
		t.Fatalf("spark turn starts = %d, want 1", starts)
	}
}

func TestRunAssistantTurnParallelPrefersSparkForMediumConfidenceLocalAnswer(t *testing.T) {
	appServer, _ := setupMockParallelAppServer(t, 25*time.Millisecond, "Spark wins the turn.")
	defer appServer.Close()
	wsURL := "ws" + strings.TrimPrefix(appServer.URL, "http")

	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"kind":"local_answer","text":"Possible short answer.","confidence":"medium"}`)
	defer llm.Close()

	app := newParallelTestApp(t, wsURL)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "how does routing work?", "how does routing work?", "text"); err != nil {
		t.Fatalf("AddChatMessage(user): %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Spark wins the turn." {
		t.Fatalf("assistant message = %q, want Spark final reply", got)
	}
	if got := countAssistantMessages(t, app, session.ID); got != 1 {
		t.Fatalf("assistant message count = %d, want 1", got)
	}

	messages, err := app.store.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	assistant := messages[len(messages)-1]
	if assistant.Provider != assistantProviderOpenAI {
		t.Fatalf("provider = %q, want %q", assistant.Provider, assistantProviderOpenAI)
	}
}

func TestRunAssistantTurnParallelCommandEmitsProvisionalThenSparkFinal(t *testing.T) {
	appServer, _ := setupMockParallelAppServer(t, 75*time.Millisecond, "Focused on Focused.")
	defer appServer.Close()
	wsURL := "ws" + strings.TrimPrefix(appServer.URL, "http")

	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"action":"focus_workspace","workspace":"Focused"}`)
	defer llm.Close()

	app := newParallelTestApp(t, wsURL)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	focus, err := app.store.CreateWorkspace("Focused", t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace(Focused): %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "focus on focused", "focus on focused", "text"); err != nil {
		t.Fatalf("AddChatMessage(user): %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	focusedID, err := app.store.FocusedWorkspaceID()
	if err != nil {
		t.Fatalf("FocusedWorkspaceID(): %v", err)
	}
	if focusedID != focus.ID {
		t.Fatalf("focused workspace id = %d, want %d", focusedID, focus.ID)
	}
	if got := latestAssistantMessage(t, app, session.ID); got != "Focused on Focused." {
		t.Fatalf("assistant message = %q, want Spark narration", got)
	}
	if got := countAssistantMessages(t, app, session.ID); got != 1 {
		t.Fatalf("assistant message count = %d, want 1", got)
	}

	payloads := collectWSJSONTypesUntil(t, clientConn, 2*time.Second, "assistant_output")
	types := strings.Join(wsTypes(payloads), ",")
	if !strings.Contains(types, "system_action") || !strings.Contains(types, "turn_provisional") || !strings.Contains(types, "assistant_message") {
		t.Fatalf("websocket types = %s, want system_action, turn_provisional, assistant_message", types)
	}
}

func TestRunAssistantTurnParallelSlowSparkEmitsAcknowledgment(t *testing.T) {
	appServer, _ := setupMockParallelAppServer(t, 650*time.Millisecond, "Spark caught up.")
	defer appServer.Close()
	wsURL := "ws" + strings.TrimPrefix(appServer.URL, "http")

	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"kind":"dialogue","ack":"Checking."}`)
	defer llm.Close()

	app := newParallelTestApp(t, wsURL)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "what changed?", "what changed?", "text"); err != nil {
		t.Fatalf("AddChatMessage(user): %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Spark caught up." {
		t.Fatalf("assistant message = %q, want Spark final reply", got)
	}

	payloads := collectWSJSONTypesUntil(t, clientConn, 2*time.Second, "assistant_output")
	var provisionalText string
	for _, payload := range payloads {
		if strings.TrimSpace(strFromAny(payload["type"])) != "assistant_message" {
			continue
		}
		provisionalText = strings.TrimSpace(strFromAny(payload["message"]))
	}
	if provisionalText != "Checking." {
		t.Fatalf("provisional assistant_message = %q, want %q", provisionalText, "Checking.")
	}
	if got := countAssistantMessages(t, app, session.ID); got != 1 {
		t.Fatalf("assistant message count = %d, want 1", got)
	}
}

func TestRunAssistantTurnParallelFastSparkSkipsAcknowledgment(t *testing.T) {
	appServer, _ := setupMockParallelAppServer(t, 25*time.Millisecond, "Spark wins quickly.")
	defer appServer.Close()
	wsURL := "ws" + strings.TrimPrefix(appServer.URL, "http")

	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"kind":"dialogue","ack":"Checking."}`)
	defer llm.Close()

	app := newParallelTestApp(t, wsURL)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "what changed?", "what changed?", "text"); err != nil {
		t.Fatalf("AddChatMessage(user): %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Spark wins quickly." {
		t.Fatalf("assistant message = %q, want Spark final reply", got)
	}

	payloads := collectWSJSONTypesUntil(t, clientConn, 2*time.Second, "assistant_output")
	for _, payload := range payloads {
		if strings.TrimSpace(strFromAny(payload["type"])) != "assistant_message" {
			continue
		}
		t.Fatalf("unexpected provisional assistant_message payload: %#v", payload)
	}
}

func TestRunAssistantTurnParallelCerebrasClaimsTurn(t *testing.T) {
	appServer, _ := setupMockParallelAppServer(t, 250*time.Millisecond, "Spark fallback.")
	defer appServer.Close()
	wsURL := "ws" + strings.TrimPrefix(appServer.URL, "http")

	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"kind":"local_answer","text":"Possible short answer.","confidence":"medium"}`)
	defer llm.Close()

	cerebrasServer, requests := setupMockCerebrasServer(t, http.StatusOK, 20*time.Millisecond, `{"text":"Cerebras wins the turn.","confidence":"high"}`)
	defer cerebrasServer.Close()

	app := newParallelTestApp(t, wsURL)
	app.intentLLMURL = llm.URL
	app.cerebrasClient = cerebras.NewClient(cerebrasServer.URL, "token-cerebras", cerebras.DefaultModel, cerebras.DefaultReasoningEffort)
	app.cerebrasClient.HTTPClient = cerebrasServer.Client()

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "explain the routing policy", "explain the routing policy", "text"); err != nil {
		t.Fatalf("AddChatMessage(user): %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Cerebras wins the turn." {
		t.Fatalf("assistant message = %q, want Cerebras final reply", got)
	}
	messages, err := app.store.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	assistant := messages[len(messages)-1]
	if assistant.Provider != assistantProviderCerebras {
		t.Fatalf("provider = %q, want %q", assistant.Provider, assistantProviderCerebras)
	}
	if assistant.ProviderModel != cerebras.DefaultModel {
		t.Fatalf("provider_model = %q, want %q", assistant.ProviderModel, cerebras.DefaultModel)
	}
	if *requests != 1 {
		t.Fatalf("cerebras requests = %d, want 1", *requests)
	}
}

func TestRunAssistantTurnParallelQuotaExhaustedDisablesCerebrasForRestOfDay(t *testing.T) {
	appServer, _ := setupMockParallelAppServer(t, 25*time.Millisecond, "Spark handles the turn.")
	defer appServer.Close()
	wsURL := "ws" + strings.TrimPrefix(appServer.URL, "http")

	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"kind":"local_answer","text":"Possible short answer.","confidence":"medium"}`)
	defer llm.Close()

	cerebrasServer, requests := setupMockCerebrasServer(t, http.StatusTooManyRequests, 0, "")
	defer cerebrasServer.Close()

	app := newParallelTestApp(t, wsURL)
	app.intentLLMURL = llm.URL
	app.cerebrasClient = cerebras.NewClient(cerebrasServer.URL, "token-cerebras", cerebras.DefaultModel, cerebras.DefaultReasoningEffort)
	app.cerebrasClient.HTTPClient = cerebrasServer.Client()

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "first turn", "first turn", "text"); err != nil {
		t.Fatalf("AddChatMessage(user): %v", err)
	}
	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})
	if app.cerebrasClient.IsAvailable() {
		t.Fatal("cerebrasClient.IsAvailable() = true, want false after 429")
	}
	app.closeAppSession(session.ID)

	if _, err := app.store.AddChatMessage(session.ID, "user", "second turn", "second turn", "text"); err != nil {
		t.Fatalf("AddChatMessage(user): %v", err)
	}
	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Spark handles the turn." {
		t.Fatalf("assistant message = %q, want Spark final reply", got)
	}
	if *requests != 1 {
		t.Fatalf("cerebras requests = %d, want 1 after quota disable", *requests)
	}
	messages, err := app.store.ListChatMessages(session.ID, 20)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	for _, message := range messages {
		if strings.EqualFold(strings.TrimSpace(message.Role), "system") && strings.Contains(strings.ToLower(message.ContentPlain), "cerebras") {
			t.Fatalf("unexpected user-visible Cerebras error: %q", message.ContentPlain)
		}
	}
}
