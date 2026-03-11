package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func requireWorkspaceForProject(t *testing.T, app *App, project store.Project) store.Workspace {
	t.Helper()
	workspace, err := app.ensureWorkspaceForProject(project, false)
	if err != nil {
		t.Fatalf("ensureWorkspaceForProject(%q): %v", project.ID, err)
	}
	return workspace
}

func TestProjectCompanionConfigRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	workspace := requireWorkspaceForProject(t, app, project)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/companion/config", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("authed GET status = %d, want 200", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/companion/config", nil)
	unauth := httptest.NewRecorder()
	app.Router().ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth GET status = %d, want 401", unauth.Code)
	}
}

func TestProjectCompanionConfigPutAndState(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	workspace := requireWorkspaceForProject(t, app, project)
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	rrPut := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/workspaces/"+itoa(workspace.ID)+"/companion/config", map[string]any{
		"companion_enabled":            false,
		"directed_speech_gate_enabled": true,
		"language":                     "de",
		"max_segment_duration_ms":      45000,
		"session_ram_cap_mb":           96,
		"stt_model":                    "whisper-large-v3",
		"idle_surface":                 "black",
		"audio_persistence":            "disk",
		"capture_source":               "desktop",
	})
	if rrPut.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", rrPut.Code)
	}
	var cfg companionConfig
	if err := json.Unmarshal(rrPut.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode companion config: %v", err)
	}
	if cfg.CompanionEnabled {
		t.Fatal("companion_enabled = true, want false")
	}
	if !cfg.DirectedSpeechGateEnabled {
		t.Fatal("directed_speech_gate_enabled = false, want true")
	}
	if cfg.Language != "de" {
		t.Fatalf("language = %q, want de", cfg.Language)
	}
	if cfg.IdleSurface != "black" {
		t.Fatalf("idle_surface = %q, want black", cfg.IdleSurface)
	}
	if cfg.AudioPersistence != "none" {
		t.Fatalf("audio_persistence = %q, want none", cfg.AudioPersistence)
	}
	if cfg.CaptureSource != "microphone" {
		t.Fatalf("capture_source = %q, want microphone", cfg.CaptureSource)
	}

	conn, cleanup := newTestWSConn(t)
	defer cleanup()
	handleParticipantStart(app, conn, session.ID)

	conn.participantMu.Lock()
	activeSessionID := conn.participantSessionID
	active := conn.participantActive
	conn.participantMu.Unlock()
	if active {
		t.Fatal("participantActive = true, want false when companion is disabled")
	}
	if activeSessionID != "" {
		t.Fatalf("active participant session id = %q, want empty", activeSessionID)
	}

	rrState := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/companion/state", nil)
	if rrState.Code != http.StatusOK {
		t.Fatalf("GET state status = %d, want 200", rrState.Code)
	}
	var state companionStateResponse
	if err := json.Unmarshal(rrState.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode companion state: %v", err)
	}
	if state.ProjectID != project.ID {
		t.Fatalf("project_id = %q, want %q", state.ProjectID, project.ID)
	}
	if state.ProjectKey != project.ProjectKey {
		t.Fatalf("project_key = %q, want %q", state.ProjectKey, project.ProjectKey)
	}
	if state.State != companionRuntimeStateIdle {
		t.Fatalf("state = %q, want %q", state.State, companionRuntimeStateIdle)
	}
	if state.ActiveSessions != 0 {
		t.Fatalf("active_sessions = %d, want 0", state.ActiveSessions)
	}
	if state.ActiveSessionID != "" {
		t.Fatalf("active_session_id = %q, want empty", state.ActiveSessionID)
	}
	if state.Config.IdleSurface != "black" {
		t.Fatalf("state config idle_surface = %q, want black", state.Config.IdleSurface)
	}
	if state.Config.CompanionEnabled {
		t.Fatal("state config companion_enabled = true, want false")
	}
	if state.DirectedSpeechGate.Enabled != cfg.DirectedSpeechGateEnabled {
		t.Fatalf("state directed_speech_gate.enabled = %v, want %v", state.DirectedSpeechGate.Enabled, cfg.DirectedSpeechGateEnabled)
	}
	if state.DirectedSpeechGate.Decision != companionGateDecisionDisabled {
		t.Fatalf("state directed_speech_gate.decision = %q, want %q", state.DirectedSpeechGate.Decision, companionGateDecisionDisabled)
	}
	if state.DirectedSpeechGate.Reason != "companion_disabled" {
		t.Fatalf("state directed_speech_gate.reason = %q, want companion_disabled", state.DirectedSpeechGate.Reason)
	}
	if state.InteractionPolicy.Decision != companionInteractionDecisionDisabled {
		t.Fatalf("state interaction_policy.decision = %q, want %q", state.InteractionPolicy.Decision, companionInteractionDecisionDisabled)
	}
}

func TestProjectCompanionStateReportsListeningWhenEnabled(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	workspace := requireWorkspaceForProject(t, app, project)
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}
	setLivePolicyForTest(t, app, LivePolicyMeeting)
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, session.ID)
	defer handleParticipantStop(app, conn)

	conn.participantMu.Lock()
	activeSessionID := conn.participantSessionID
	conn.participantMu.Unlock()
	if activeSessionID == "" {
		t.Fatal("expected active participant session id")
	}

	rrState := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/companion/state", nil)
	if rrState.Code != http.StatusOK {
		t.Fatalf("GET state status = %d, want 200", rrState.Code)
	}
	var state companionStateResponse
	if err := json.Unmarshal(rrState.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode companion state: %v", err)
	}
	if state.State != companionRuntimeStateListening {
		t.Fatalf("state = %q, want %q", state.State, companionRuntimeStateListening)
	}
	if state.ActiveSessions != 1 {
		t.Fatalf("active_sessions = %d, want 1", state.ActiveSessions)
	}
	if state.ActiveSessionID != activeSessionID {
		t.Fatalf("active_session_id = %q, want %q", state.ActiveSessionID, activeSessionID)
	}
	if state.LatestSession == nil {
		t.Fatal("expected latest_session")
	}
}

func TestProjectCompanionStateExposesDirectedSpeechGateMetadata(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	workspace := requireWorkspaceForProject(t, app, project)
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	cfg.DirectedSpeechGateEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}
	setLivePolicyForTest(t, app, LivePolicyMeeting)

	sess, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession: %v", err)
	}
	if err := app.store.AddParticipantEvent(sess.ID, 0, "session_started", "{}"); err != nil {
		t.Fatalf("AddParticipantEvent session_started: %v", err)
	}
	seg, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   sess.ID,
		StartTS:     100,
		EndTS:       101,
		Text:        "Tabura, open the meeting transcript.",
		CommittedAt: 102,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("AddParticipantSegment: %v", err)
	}
	if err := app.store.AddParticipantEvent(sess.ID, seg.ID, "segment_committed", `{"text":"Tabura, open the meeting transcript."}`); err != nil {
		t.Fatalf("AddParticipantEvent segment_committed: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/companion/state", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET state status = %d, want 200", rr.Code)
	}
	var state companionStateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode companion state: %v", err)
	}
	if state.DirectedSpeechGate.Decision != companionGateDecisionDirect {
		t.Fatalf("directed_speech_gate.decision = %q, want %q", state.DirectedSpeechGate.Decision, companionGateDecisionDirect)
	}
	if state.DirectedSpeechGate.Reason != "assistant_name_mentioned" {
		t.Fatalf("directed_speech_gate.reason = %q, want assistant_name_mentioned", state.DirectedSpeechGate.Reason)
	}
	if state.DirectedSpeechGate.SessionID != sess.ID {
		t.Fatalf("directed_speech_gate.session_id = %q, want %q", state.DirectedSpeechGate.SessionID, sess.ID)
	}
	if state.DirectedSpeechGate.SegmentID != seg.ID {
		t.Fatalf("directed_speech_gate.segment_id = %d, want %d", state.DirectedSpeechGate.SegmentID, seg.ID)
	}
	if state.DirectedSpeechGate.LastEventType != "segment_committed" {
		t.Fatalf("directed_speech_gate.last_event_type = %q, want segment_committed", state.DirectedSpeechGate.LastEventType)
	}
	if state.DirectedSpeechGate.EvaluatedText != "Tabura, open the meeting transcript." {
		t.Fatalf("directed_speech_gate.evaluated_text = %q", state.DirectedSpeechGate.EvaluatedText)
	}
	if state.InteractionPolicy.Decision != companionInteractionDecisionRespond {
		t.Fatalf("interaction_policy.decision = %q, want %q", state.InteractionPolicy.Decision, companionInteractionDecisionRespond)
	}
	if state.InteractionPolicy.Reason != "direct_address_ready" {
		t.Fatalf("interaction_policy.reason = %q, want direct_address_ready", state.InteractionPolicy.Reason)
	}
}
