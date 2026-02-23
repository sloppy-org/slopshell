package web

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

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
	app.chatTurnQueue[session.ID] = 3

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
	app.chatTurnQueue[session.ID] = 2

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
	app.chatTurnQueue[s2.ID] = 1

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
