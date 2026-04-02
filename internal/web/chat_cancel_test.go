package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func setupMockAppServerStatusServer(t *testing.T, statusMessage string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode message: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{
					"id":     msg["id"],
					"result": map[string]interface{}{"userAgent": "test-client"},
				})
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-status"},
					},
				})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-status"},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "agentMessage",
							"text": statusMessage,
						},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "turn/completed",
					"params": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-status", "status": "completed"},
					},
				})
				return
			}
		}
	}))
}

func TestExecuteChatCommandStopCancelsWork(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	projectRoot := t.TempDir()
	project, err := app.store.CreateEnrichedWorkspace(
		"stop-unit",
		"stop-unit",
		projectRoot,
		"local",
		"",
		"canvas-stop-unit",
		false,
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	cancelCalled := make(chan struct{}, 1)
	app.registerActiveChatTurn(session.ID, "run-1", func() {
		select {
		case cancelCalled <- struct{}{}:
		default:
		}
	})
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 1
	app.turns.mu.Unlock()

	result, err := app.executeChatCommand(session.ID, "/stop")
	if err != nil {
		t.Fatalf("execute /stop: %v", err)
	}

	select {
	case <-cancelCalled:
	default:
		t.Fatal("expected active chat cancel callback to run")
	}

	if got := intFromAny(result["active_canceled"], -1); got != 1 {
		t.Fatalf("active_canceled = %d, want 1", got)
	}
	if got := intFromAny(result["queued_canceled"], -1); got != 1 {
		t.Fatalf("queued_canceled = %d, want 1", got)
	}
	if got := intFromAny(result["canceled"], -1); got != 2 {
		t.Fatalf("canceled = %d, want 2", got)
	}
	if name := result["name"]; name != "stop" {
		t.Fatalf("expected name=stop, got %v", name)
	}
}

func TestExecuteChatCommandStatusUsesAppServerStatusOutput(t *testing.T) {
	statusMessage := "Weekly usage: 123k / 1M tokens."
	wsServer := setupMockAppServerStatusServer(t, statusMessage)
	defer wsServer.Close()
	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")

	app, err := New(t.TempDir(), "", "", wsURL, "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	projectRoot := t.TempDir()
	project, err := app.store.CreateEnrichedWorkspace(
		"status-project",
		"status-project",
		projectRoot,
		"local",
		"",
		"canvas-status-project",
		false,
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	result, err := app.executeChatCommand(session.ID, "/status")
	if err != nil {
		t.Fatalf("execute /status: %v", err)
	}
	if got := strings.TrimSpace(result["message"].(string)); got != statusMessage {
		t.Fatalf("status message mismatch: got %q want %q", got, statusMessage)
	}
	if got := strings.TrimSpace(result["name"].(string)); got != "status" {
		t.Fatalf("status name mismatch: got %q", got)
	}
}

func TestHandleChatSessionCommandStatusUsesAppServerStatusOutput(t *testing.T) {
	statusMessage := "Weekly usage: 222k / 1M tokens."
	wsServer := setupMockAppServerStatusServer(t, statusMessage)
	defer wsServer.Close()
	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")

	app, err := New(t.TempDir(), "", "", wsURL, "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	projectRoot := t.TempDir()
	project, err := app.store.CreateEnrichedWorkspace(
		"status-api-project",
		"status-api-project",
		projectRoot,
		"local",
		"",
		"canvas-status-api",
		false,
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions/"+session.ID+"/commands", map[string]any{
		"command": "/status",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	resultRaw, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response: %v", payload)
	}
	if got := strings.TrimSpace(resultRaw["message"].(string)); got != statusMessage {
		t.Fatalf("status message mismatch: got %q want %q", got, statusMessage)
	}
	if got := strings.TrimSpace(resultRaw["name"].(string)); got != "status" {
		t.Fatalf("status name mismatch: got %q", got)
	}
}

func TestHandleChatSessionCommandStopCancelsWork(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	projectRoot := t.TempDir()
	project, err := app.store.CreateEnrichedWorkspace(
		"stop-api",
		"stop-api",
		projectRoot,
		"local",
		"",
		"canvas-stop-api",
		false,
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 3
	app.turns.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions/"+session.ID+"/commands", map[string]any{
		"command": "/stop",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	resultRaw, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response: %v", payload)
	}
	if got := intFromAny(resultRaw["active_canceled"], -1); got != 0 {
		t.Fatalf("active_canceled = %d, want 0", got)
	}
	if got := intFromAny(resultRaw["queued_canceled"], -1); got != 3 {
		t.Fatalf("queued_canceled = %d, want 3", got)
	}
	if got := intFromAny(resultRaw["canceled"], -1); got != 3 {
		t.Fatalf("canceled = %d, want 3", got)
	}
}

func TestHandleChatSessionCancelEndpointStopsWork(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	projectRoot := t.TempDir()
	project, err := app.store.CreateEnrichedWorkspace(
		"cancel-endpoint",
		"cancel-endpoint",
		projectRoot,
		"local",
		"",
		"canvas-stop-endpoint",
		false,
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	cancelCalled := make(chan struct{}, 1)
	app.registerActiveChatTurn(session.ID, "run-1", func() {
		select {
		case cancelCalled <- struct{}{}:
		default:
		}
	})
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 2
	app.turns.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions/"+session.ID+"/cancel", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := intFromAny(payload["active_canceled"], -1); got != 1 {
		t.Fatalf("active_canceled = %d, want 1", got)
	}
	if got := intFromAny(payload["queued_canceled"], -1); got != 2 {
		t.Fatalf("queued_canceled = %d, want 2", got)
	}
	if got := intFromAny(payload["canceled"], -1); got != 3 {
		t.Fatalf("canceled = %d, want 3", got)
	}
	select {
	case <-cancelCalled:
	default:
		t.Fatal("expected active chat turn cancel callback to run")
	}
}

func TestHandleChatSessionCancelStopsActiveTurn(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	cancelCalled := make(chan struct{}, 1)
	app.registerActiveChatTurn(session.ID, "run-1", func() {
		select {
		case cancelCalled <- struct{}{}:
		default:
		}
	})

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions/"+session.ID+"/cancel", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	select {
	case <-cancelCalled:
	default:
		t.Fatalf("expected active chat turn cancel func to be invoked")
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := intFromAny(payload["canceled"], -1); got != 1 {
		t.Fatalf("expected canceled=1, got %v", payload["canceled"])
	}
	if got := intFromAny(payload["queued_canceled"], -1); got != 0 {
		t.Fatalf("expected queued_canceled=0, got %v", payload["queued_canceled"])
	}
}

func TestExecuteChatCommandStopWaitsForTurnShutdown(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	done := make(chan struct{}, 1)
	app.registerActiveChatTurn(session.ID, "run-stop-wait", func() {
		go func() {
			time.Sleep(80 * time.Millisecond)
			app.unregisterActiveChatTurn(session.ID, "run-stop-wait")
			done <- struct{}{}
		}()
	})

	started := time.Now()
	if _, err := app.executeChatCommand(session.ID, "/stop"); err != nil {
		t.Fatalf("execute /stop: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 60*time.Millisecond {
		t.Fatalf("/stop returned too early: %v", elapsed)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected /stop to wait for turn shutdown")
	}
	if got := app.activeChatTurnCount(session.ID); got != 0 {
		t.Fatalf("expected active chat turns to be drained, got %d", got)
	}
}

func TestHandleChatSessionCancelWaitsForTurnShutdown(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	done := make(chan struct{}, 1)
	app.registerActiveChatTurn(session.ID, "run-endpoint-wait", func() {
		go func() {
			time.Sleep(80 * time.Millisecond)
			app.unregisterActiveChatTurn(session.ID, "run-endpoint-wait")
			done <- struct{}{}
		}()
	})

	started := time.Now()
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions/"+session.ID+"/cancel", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if elapsed := time.Since(started); elapsed < 60*time.Millisecond {
		t.Fatalf("/cancel returned too early: %v", elapsed)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected /cancel to wait for turn shutdown")
	}
	if got := app.activeChatTurnCount(session.ID); got != 0 {
		t.Fatalf("expected active chat turns to be drained, got %d", got)
	}
}

func TestHandleChatSessionActivityReportsActiveTurns(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	firstCanceled := make(chan struct{}, 1)
	app.registerActiveChatTurn(session.ID, "run-1", func() {
		select {
		case firstCanceled <- struct{}{}:
		default:
		}
	})
	app.registerActiveChatTurn(session.ID, "run-2", func() {})
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 3
	app.turns.mu.Unlock()

	select {
	case <-firstCanceled:
	default:
		t.Fatal("expected replaced active turn to be canceled")
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/chat/sessions/"+session.ID+"/activity", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := intFromAny(payload["active_turns"], -1); got != 1 {
		t.Fatalf("expected active_turns=1, got %v", payload["active_turns"])
	}
	if got := intFromAny(payload["queued_turns"], -1); got != 3 {
		t.Fatalf("expected queued_turns=3, got %v", payload["queued_turns"])
	}
	if got := strFromAny(payload["active_turn_id"]); got != "run-2" {
		t.Fatalf("expected active_turn_id=run-2, got %q", got)
	}
	if got := strFromAny(payload["status"]); got != "running" {
		t.Fatalf("expected status=running, got %q", got)
	}
}

func TestHandleChatSessionCancelClearsQueuedTurns(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 2
	app.turns.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions/"+session.ID+"/cancel", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := intFromAny(payload["canceled"], -1); got != 2 {
		t.Fatalf("expected canceled=2, got %v", payload["canceled"])
	}
	if got := intFromAny(payload["queued_canceled"], -1); got != 2 {
		t.Fatalf("expected queued_canceled=2, got %v", payload["queued_canceled"])
	}
}

func TestExecuteChatCommandClearAllResetsChatContext(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	projectOne, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	s1, err := app.store.GetOrCreateChatSession(projectOne.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session 1: %v", err)
	}
	projectTwoRoot := filepath.Join(t.TempDir(), "clear-all-project-2")
	projectTwo, err := app.store.CreateEnrichedWorkspace("Clear All Two", "clear-all-project-2", projectTwoRoot, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject(project 2) error: %v", err)
	}
	s2, err := app.store.GetOrCreateChatSession(projectTwo.WorkspacePath)
	if err != nil {
		t.Fatalf("create chat session 2: %v", err)
	}

	if _, err := app.store.AddChatMessage(s1.ID, "user", "u1", "u1", "markdown"); err != nil {
		t.Fatalf("add message s1: %v", err)
	}
	if _, err := app.store.AddChatMessage(s2.ID, "assistant", "a2", "a2", "markdown"); err != nil {
		t.Fatalf("add message s2: %v", err)
	}
	if err := app.store.UpdateChatSessionThread(s1.ID, "thread-1"); err != nil {
		t.Fatalf("set thread s1: %v", err)
	}
	if err := app.store.UpdateChatSessionThread(s2.ID, "thread-2"); err != nil {
		t.Fatalf("set thread s2: %v", err)
	}

	canceled := make(chan struct{}, 1)
	app.registerActiveChatTurn(s1.ID, "run-1", func() { canceled <- struct{}{} })
	app.turns.mu.Lock()
	app.turns.queue[s2.ID] = 1
	app.turns.mu.Unlock()

	result, err := app.executeChatCommand(s1.ID, "/clear")
	if err != nil {
		t.Fatalf("execute /clear: %v", err)
	}
	if got := intFromAny(result["active_canceled"], -1); got != 1 {
		t.Fatalf("active_canceled = %d, want 1", got)
	}
	if got := intFromAny(result["queued_canceled"], -1); got != 1 {
		t.Fatalf("queued_canceled = %d, want 1", got)
	}

	select {
	case <-canceled:
	default:
		t.Fatal("expected active turn cancel callback to run")
	}

	msgs1, err := app.store.ListChatMessages(s1.ID, 1000)
	if err != nil {
		t.Fatalf("list messages s1: %v", err)
	}
	if len(msgs1) != 0 {
		t.Fatalf("expected s1 messages cleared, got %d", len(msgs1))
	}
	msgs2, err := app.store.ListChatMessages(s2.ID, 1000)
	if err != nil {
		t.Fatalf("list messages s2: %v", err)
	}
	if len(msgs2) != 0 {
		t.Fatalf("expected s2 messages cleared, got %d", len(msgs2))
	}

	got1, err := app.store.GetChatSession(s1.ID)
	if err != nil {
		t.Fatalf("get session s1: %v", err)
	}
	got2, err := app.store.GetChatSession(s2.ID)
	if err != nil {
		t.Fatalf("get session s2: %v", err)
	}
	if got1.AppThreadID != "" || got2.AppThreadID != "" {
		t.Fatalf("expected app thread ids to be reset, got s1=%q s2=%q", got1.AppThreadID, got2.AppThreadID)
	}
}
