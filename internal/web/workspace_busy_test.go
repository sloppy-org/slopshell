package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type workspaceBusyResponse struct {
	OK     bool                 `json:"ok"`
	States []workspaceBusyState `json:"states"`
}

func TestParseInlineWorkspaceIntentRecognizesBusyStateQuery(t *testing.T) {
	action := parseInlineWorkspaceIntent("what's running?")
	if action == nil {
		t.Fatal("expected busy-state workspace intent")
	}
	if action.Action != "show_busy_state" {
		t.Fatalf("action = %q, want show_busy_state", action.Action)
	}
}

func TestWorkspaceBusyListIncludesAnchorFocusAndRunState(t *testing.T) {
	app := newAuthedTestApp(t)
	anchor, err := app.ensureTodayDailyWorkspace()
	if err != nil {
		t.Fatalf("ensureTodayDailyWorkspace: %v", err)
	}
	alpha, err := app.store.CreateWorkspace("Alpha", t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace(Alpha): %v", err)
	}
	if err := app.setFocusedWorkspace(alpha.ID); err != nil {
		t.Fatalf("setFocusedWorkspace(alpha): %v", err)
	}
	alphaSession, err := app.store.GetOrCreateChatSessionForWorkspace(alpha.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(alpha): %v", err)
	}
	app.turns.mu.Lock()
	app.turns.queue[alphaSession.ID] = 2
	app.turns.mu.Unlock()
	app.registerActiveChatTurn(alphaSession.ID, "run-alpha", func() {})

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/busy", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("workspace busy status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var payload workspaceBusyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode workspace busy response: %v", err)
	}
	anchorState := workspaceByID(payload.States, anchor.ID)
	if anchorState == nil {
		t.Fatalf("anchor workspace %d missing from busy response", anchor.ID)
	}
	if !anchorState.IsAnchor {
		t.Fatal("anchor workspace is_anchor = false, want true")
	}
	if anchorState.Status != "idle" {
		t.Fatalf("anchor status = %q, want idle", anchorState.Status)
	}
	alphaState := workspaceByName(payload.States, "Alpha")
	if alphaState == nil {
		t.Fatal("alpha workspace missing from busy response")
	}
	if !alphaState.IsFocused {
		t.Fatal("alpha workspace is_focused = false, want true")
	}
	if alphaState.Status != "running" {
		t.Fatalf("alpha status = %q, want running", alphaState.Status)
	}
	if alphaState.ActiveTurns != 1 {
		t.Fatalf("alpha active_turns = %d, want 1", alphaState.ActiveTurns)
	}
	if alphaState.QueuedTurns != 2 {
		t.Fatalf("alpha queued_turns = %d, want 2", alphaState.QueuedTurns)
	}
}

func TestEvaluateLocalTurnAnswersWhatsRunning(t *testing.T) {
	app := newAuthedTestApp(t)
	anchor, err := app.ensureTodayDailyWorkspace()
	if err != nil {
		t.Fatalf("ensureTodayDailyWorkspace: %v", err)
	}
	alpha, err := app.store.CreateWorkspace("Alpha", t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace(Alpha): %v", err)
	}
	if err := app.setFocusedWorkspace(alpha.ID); err != nil {
		t.Fatalf("setFocusedWorkspace(alpha): %v", err)
	}
	alphaSession, err := app.store.GetOrCreateChatSessionForWorkspace(alpha.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(alpha): %v", err)
	}
	app.registerActiveChatTurn(alphaSession.ID, "run-alpha", func() {})

	anchorSession, err := app.store.GetOrCreateChatSessionForWorkspace(anchor.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(anchor): %v", err)
	}
	evaluation := app.evaluateLocalTurn(context.Background(), anchorSession.ID, anchorSession, "what's running?", nil, "")
	if !evaluation.handled {
		t.Fatal("expected local busy-state answer to be handled")
	}
	if evaluation.localAnswerConfidence != "high" {
		t.Fatalf("confidence = %q, want high", evaluation.localAnswerConfidence)
	}
	if len(evaluation.payloads) != 0 {
		t.Fatalf("payload count = %d, want 0", len(evaluation.payloads))
	}
	if !strings.Contains(evaluation.text, "Alpha (focus): running (1 active)") {
		t.Fatalf("busy-state answer missing focused workspace summary: %q", evaluation.text)
	}
	if !strings.Contains(strings.ToLower(evaluation.text), "daily, anchor") {
		t.Fatalf("busy-state answer missing daily anchor summary: %q", evaluation.text)
	}
}

func TestWorkspaceBusyChangedBroadcastsOnRunStateChange(t *testing.T) {
	app := newAuthedTestApp(t)
	anchor, err := app.ensureTodayDailyWorkspace()
	if err != nil {
		t.Fatalf("ensureTodayDailyWorkspace: %v", err)
	}
	alpha, err := app.store.CreateWorkspace("Alpha", t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace(Alpha): %v", err)
	}
	alphaSession, err := app.store.GetOrCreateChatSessionForWorkspace(alpha.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(alpha): %v", err)
	}
	anchorSession, err := app.store.GetOrCreateChatSessionForWorkspace(anchor.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(anchor): %v", err)
	}
	app.turns.mu.Lock()
	app.turns.queue[alphaSession.ID] = 1
	app.turns.mu.Unlock()

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(anchorSession.ID, conn)
	defer app.hub.unregisterChat(anchorSession.ID, conn)

	app.registerActiveChatTurn(alphaSession.ID, "run-alpha", func() {})

	payloads := collectWSJSONTypesUntil(t, clientConn, 2*time.Second, "workspace_busy_changed")
	last := payloads[len(payloads)-1]
	states, ok := last["states"].([]interface{})
	if !ok {
		t.Fatalf("states payload has unexpected type: %#v", last["states"])
	}
	foundAlpha := false
	for _, raw := range states {
		state, _ := raw.(map[string]interface{})
		if strings.TrimSpace(strFromAny(state["workspace_name"])) != "Alpha" {
			continue
		}
		foundAlpha = true
		if got := strings.TrimSpace(strFromAny(state["status"])); got != "running" {
			t.Fatalf("alpha websocket status = %q, want running", got)
		}
		if got := intFromAny(state["queued_turns"], -1); got != 1 {
			t.Fatalf("alpha websocket queued_turns = %d, want 1", got)
		}
	}
	if !foundAlpha {
		t.Fatalf("alpha workspace missing from websocket payload: %#v", states)
	}
}
