package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func setupMockDelegateCancelServer(t *testing.T, canceled int, seen *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if seen != nil {
			*seen += 1
		}
		if result, ok := payload["params"].(map[string]any); ok {
			if args, ok := result["arguments"].(map[string]any); ok {
				if _, ok := args["cwd_prefix"]; !ok {
					http.Error(w, "missing cwd_prefix", http.StatusBadRequest)
					return
				}
			}
		}
		out := map[string]any{
			"result": map[string]any{
				"structuredContent": map[string]any{
					"canceled": float64(canceled),
				},
			},
		}
		if err := json.NewEncoder(w).Encode(out); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
}

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

func TestExecuteChatCommandStopCancelsDelegatedWork(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	projectRoot := t.TempDir()
	project, err := app.store.CreateProject(
		"delegate-stop-unit",
		"delegate-stop-unit",
		projectRoot,
		"local",
		"",
		"canvas-stop-unit",
		false,
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
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

	delegateCancelCalls := 0
	server := setupMockDelegateCancelServer(t, 7, &delegateCancelCalls)
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse mock url: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse mock port: %v", err)
	}
	app.tunnels.setPort(project.CanvasSessionID, port)

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
	if got := intFromAny(result["delegate_canceled"], -1); got != 7 {
		t.Fatalf("delegate_canceled = %d, want 7", got)
	}
	if got := intFromAny(result["canceled"], -1); got != 9 {
		t.Fatalf("canceled = %d, want 9", got)
	}
	if name := result["name"]; name != "stop" {
		t.Fatalf("expected name=stop, got %v", name)
	}
	if delegateCancelCalls != 1 {
		t.Fatalf("expected 1 delegate cancel RPC call, got %d", delegateCancelCalls)
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
	project, err := app.store.CreateProject(
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
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
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
	project, err := app.store.CreateProject(
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
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
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

func TestHandleChatSessionCommandStopCancelsDelegatedWork(t *testing.T) {
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
	project, err := app.store.CreateProject(
		"delegate-stop-api",
		"delegate-stop-api",
		projectRoot,
		"local",
		"",
		"canvas-stop-api",
		false,
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 3
	app.turns.mu.Unlock()

	delegateCancelCalls := 0
	server := setupMockDelegateCancelServer(t, 5, &delegateCancelCalls)
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse mock url: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse mock port: %v", err)
	}
	app.tunnels.setPort(project.CanvasSessionID, port)

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
	if got := intFromAny(resultRaw["delegate_canceled"], -1); got != 5 {
		t.Fatalf("delegate_canceled = %d, want 5", got)
	}
	if got := intFromAny(resultRaw["canceled"], -1); got != 8 {
		t.Fatalf("canceled = %d, want 8", got)
	}
	if delegateCancelCalls != 1 {
		t.Fatalf("expected 1 delegate cancel RPC call, got %d", delegateCancelCalls)
	}
}

func TestHandleChatSessionCancelEndpointStopsDelegatedWork(t *testing.T) {
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
	project, err := app.store.CreateProject(
		"delegate-cancel-endpoint",
		"delegate-cancel-endpoint",
		projectRoot,
		"local",
		"",
		"canvas-stop-endpoint",
		false,
	)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
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

	delegateCancelCalls := 0
	server := setupMockDelegateCancelServer(t, 4, &delegateCancelCalls)
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse mock url: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse mock port: %v", err)
	}
	app.tunnels.setPort(project.CanvasSessionID, port)

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
	if got := intFromAny(payload["delegate_canceled"], -1); got != 4 {
		t.Fatalf("delegate_canceled = %d, want 4", got)
	}
	if got := intFromAny(payload["canceled"], -1); got != 7 {
		t.Fatalf("canceled = %d, want 7", got)
	}
	select {
	case <-cancelCalled:
	default:
		t.Fatal("expected active chat turn cancel callback to run")
	}
	if delegateCancelCalls != 1 {
		t.Fatalf("expected 1 delegate cancel RPC call, got %d", delegateCancelCalls)
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

	session, err := app.store.GetOrCreateChatSession("cancel-test-project")
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
	if got := intFromAny(payload["delegate_canceled"], -1); got != 0 {
		t.Fatalf("expected delegate_canceled=0, got %v", payload["delegate_canceled"])
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

	session, err := app.store.GetOrCreateChatSession("activity-test-project")
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	app.registerActiveChatTurn(session.ID, "run-1", func() {})
	app.registerActiveChatTurn(session.ID, "run-2", func() {})
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 3
	app.turns.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/chat/sessions/"+session.ID+"/activity", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := intFromAny(payload["active_turns"], -1); got != 2 {
		t.Fatalf("expected active_turns=2, got %v", payload["active_turns"])
	}
	if got := intFromAny(payload["queued_turns"], -1); got != 3 {
		t.Fatalf("expected queued_turns=3, got %v", payload["queued_turns"])
	}
	if got := intFromAny(payload["delegate_active"], -1); got != 0 {
		t.Fatalf("expected delegate_active=0, got %v", payload["delegate_active"])
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

	session, err := app.store.GetOrCreateChatSession("cancel-queued-project")
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
	if got := intFromAny(payload["delegate_canceled"], -1); got != 0 {
		t.Fatalf("expected delegate_canceled=0, got %v", payload["delegate_canceled"])
	}
}

func TestHandleChatSessionCancelDelegatesEndpoint(t *testing.T) {
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

	session, err := app.store.GetOrCreateChatSession("cancel-delegates-project")
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions/"+session.ID+"/cancel-delegates", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := intFromAny(payload["canceled"], -1); got != 0 {
		t.Fatalf("expected canceled=0, got %v", payload["canceled"])
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

	s1, err := app.store.GetOrCreateChatSession("clear-all-project-1")
	if err != nil {
		t.Fatalf("create chat session 1: %v", err)
	}
	s2, err := app.store.GetOrCreateChatSession("clear-all-project-2")
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
