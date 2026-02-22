package web

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleChatSessionCancelStopsActiveTurn(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", false)
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
}

func TestHandleChatSessionActivityReportsActiveTurns(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", false)
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
}

func TestHandleChatSessionCancelClearsQueuedTurns(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", false)
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
}
