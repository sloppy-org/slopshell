package web

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestLivePolicyConfig(t *testing.T) {
	dialogue := LivePolicyDialogue.Config()
	if !dialogue.AssumeAddressed {
		t.Fatal("dialogue assume_addressed = false, want true")
	}
	if !dialogue.ProactiveSpeech {
		t.Fatal("dialogue proactive_speech = false, want true")
	}
	if dialogue.CaptureDecisions {
		t.Fatal("dialogue capture_decisions = true, want false")
	}
	if dialogue.CaptureActionItems {
		t.Fatal("dialogue capture_action_items = true, want false")
	}
	if !dialogue.InterruptionAllowed {
		t.Fatal("dialogue interruption_allowed = false, want true")
	}
	if dialogue.RequiresExplicitAddress() {
		t.Fatal("dialogue requires explicit address = true, want false")
	}
	if dialogue.CapturesMeetingNotes() {
		t.Fatal("dialogue captures meeting notes = true, want false")
	}

	meeting := LivePolicyMeeting.Config()
	if meeting.AssumeAddressed {
		t.Fatal("meeting assume_addressed = true, want false")
	}
	if meeting.ProactiveSpeech {
		t.Fatal("meeting proactive_speech = true, want false")
	}
	if !meeting.CaptureDecisions {
		t.Fatal("meeting capture_decisions = false, want true")
	}
	if !meeting.CaptureActionItems {
		t.Fatal("meeting capture_action_items = false, want true")
	}
	if meeting.InterruptionAllowed {
		t.Fatal("meeting interruption_allowed = true, want false")
	}
	if !meeting.RequiresExplicitAddress() {
		t.Fatal("meeting requires explicit address = false, want true")
	}
	if !meeting.CapturesMeetingNotes() {
		t.Fatal("meeting captures meeting notes = false, want true")
	}
}

func TestLivePolicyCapabilities(t *testing.T) {
	if LivePolicyDialogue.RequiresExplicitAddress() {
		t.Fatal("dialogue RequiresExplicitAddress = true, want false")
	}
	if LivePolicyDialogue.CapturesMeetingNotes() {
		t.Fatal("dialogue CapturesMeetingNotes = true, want false")
	}
	if LivePolicyDialogue.UsesParticipantCapture() {
		t.Fatal("dialogue UsesParticipantCapture = true, want false")
	}
	if !LivePolicyMeeting.RequiresExplicitAddress() {
		t.Fatal("meeting RequiresExplicitAddress = false, want true")
	}
	if !LivePolicyMeeting.CapturesMeetingNotes() {
		t.Fatal("meeting CapturesMeetingNotes = false, want true")
	}
	if !LivePolicyMeeting.UsesParticipantCapture() {
		t.Fatal("meeting UsesParticipantCapture = false, want true")
	}
}

func TestLivePolicyDefaultsToDialogueAndPersists(t *testing.T) {
	app := newAuthedTestApp(t)

	rrGet := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/live-policy", nil)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("GET /api/live-policy status = %d, want 200", rrGet.Code)
	}
	var initial map[string]interface{}
	if err := json.Unmarshal(rrGet.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode initial live policy: %v", err)
	}
	if got := strFromAny(initial["policy"]); got != "dialogue" {
		t.Fatalf("initial live policy = %q, want dialogue", got)
	}

	rrPost := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/live-policy", map[string]any{
		"policy": "meeting",
	})
	if rrPost.Code != http.StatusOK {
		t.Fatalf("POST /api/live-policy status = %d, want 200", rrPost.Code)
	}
	var updated map[string]interface{}
	if err := json.Unmarshal(rrPost.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated live policy: %v", err)
	}
	if got := strFromAny(updated["policy"]); got != "meeting" {
		t.Fatalf("updated live policy = %q, want meeting", got)
	}

	stored, err := app.store.AppState(appStateLivePolicyKey)
	if err != nil {
		t.Fatalf("AppState(%q): %v", appStateLivePolicyKey, err)
	}
	if stored != "meeting" {
		t.Fatalf("stored live policy = %q, want meeting", stored)
	}

	rrRuntime := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime", nil)
	if rrRuntime.Code != http.StatusOK {
		t.Fatalf("GET /api/runtime status = %d, want 200", rrRuntime.Code)
	}
	var runtime map[string]interface{}
	if err := json.Unmarshal(rrRuntime.Body.Bytes(), &runtime); err != nil {
		t.Fatalf("decode runtime metadata: %v", err)
	}
	if got := strFromAny(runtime["live_policy"]); got != "meeting" {
		t.Fatalf("runtime live_policy = %q, want meeting", got)
	}
}

func TestLivePolicyBroadcastsWebsocketChanges(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/live-policy", map[string]any{
		"policy": "meeting",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/live-policy status = %d, want 200", rr.Code)
	}

	payload := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "live_policy_changed")
	if got := strFromAny(payload["policy"]); got != "meeting" {
		t.Fatalf("ws live policy = %q, want meeting", got)
	}
}
