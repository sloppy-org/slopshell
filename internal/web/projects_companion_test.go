package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectCompanionConfigRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects/"+project.ID+"/companion/config", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("authed GET status = %d, want 200", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/companion/config", nil)
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
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	rrPut := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/projects/"+project.ID+"/companion/config", map[string]any{
		"companion_enabled":       false,
		"language":                "de",
		"max_segment_duration_ms": 45000,
		"session_ram_cap_mb":      96,
		"stt_model":               "whisper-large-v3",
		"idle_surface":            "black",
		"audio_persistence":       "disk",
		"capture_source":          "desktop",
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
	defer handleParticipantStop(app, conn)

	conn.participantMu.Lock()
	activeSessionID := conn.participantSessionID
	conn.participantMu.Unlock()
	if activeSessionID == "" {
		t.Fatal("expected active participant session id")
	}

	rrState := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects/"+project.ID+"/companion/state", nil)
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
	if state.LatestSession.ProjectKey != project.ProjectKey {
		t.Fatalf("latest_session.project_key = %q, want %q", state.LatestSession.ProjectKey, project.ProjectKey)
	}
	if state.Config.IdleSurface != "black" {
		t.Fatalf("state config idle_surface = %q, want black", state.Config.IdleSurface)
	}
	if state.Config.CompanionEnabled {
		t.Fatal("state config companion_enabled = true, want false")
	}
}
