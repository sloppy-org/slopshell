package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestParticipantConfigGetRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/config", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("authed GET status = %d, want 200", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/participant/config", nil)
	unauth := httptest.NewRecorder()
	app.Router().ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth GET status = %d, want 401", unauth.Code)
	}
}

func TestParticipantConfigDefaultValues(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/config", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var cfg participantConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.AudioPersistence != "none" {
		t.Fatalf("audio_persistence = %q, want none", cfg.AudioPersistence)
	}
	if cfg.Language == "" {
		t.Fatal("language is empty")
	}
	if cfg.MaxSegmentDurationMS <= 0 {
		t.Fatalf("max_segment_duration_ms = %d", cfg.MaxSegmentDurationMS)
	}
}

func TestParticipantConfigPutAudioPersistenceInvariant(t *testing.T) {
	app := newAuthedTestApp(t)

	payload := map[string]interface{}{
		"language":          "de",
		"stt_model":         "whisper-large",
		"audio_persistence": "disk",
	}
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/participant/config", payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", rr.Code)
	}

	var cfg participantConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.AudioPersistence != "none" {
		t.Fatalf("audio_persistence = %q after PUT with disk, want none", cfg.AudioPersistence)
	}
	if cfg.Language != "de" {
		t.Fatalf("language = %q, want de", cfg.Language)
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/config", nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config after round-trip: %v", err)
	}
	if cfg.AudioPersistence != "none" {
		t.Fatalf("audio_persistence = %q after round-trip, want none", cfg.AudioPersistence)
	}
}

func TestParticipantStatusRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/status", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("authed GET status = %d, want 200", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/participant/status", nil)
	unauth := httptest.NewRecorder()
	app.Router().ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth GET status = %d, want 401", unauth.Code)
	}
}

func TestParticipantStatusReportsZero(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/status", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["active_sessions"] != float64(0) {
		t.Fatalf("active_sessions = %v, want 0", resp["active_sessions"])
	}
}

func TestParticipantSessionsListRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/participant/sessions", nil)
	unauth := httptest.NewRecorder()
	app.Router().ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth GET status = %d, want 401", unauth.Code)
	}
}

func TestParticipantSessionsListEmpty(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/sessions", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sessions, ok := resp["sessions"].([]interface{})
	if !ok {
		t.Fatal("sessions not an array")
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions count = %d, want 0", len(sessions))
	}
}

func TestParticipantSessionsListWithData(t *testing.T) {
	app := newAuthedTestApp(t)

	_, err := app.store.AddParticipantSession("test-key", "{}")
	if err != nil {
		t.Fatalf("add session: %v", err)
	}
	_, err = app.store.AddParticipantSession("test-key", "{}")
	if err != nil {
		t.Fatalf("add session 2: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/sessions", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sessions := resp["sessions"].([]interface{})
	if len(sessions) != 2 {
		t.Fatalf("sessions count = %d, want 2", len(sessions))
	}
}

func TestParticipantSessionsListFilterByProjectKey(t *testing.T) {
	app := newAuthedTestApp(t)

	_, _ = app.store.AddParticipantSession("key-a", "{}")
	_, _ = app.store.AddParticipantSession("key-b", "{}")

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/sessions?project_key=key-a", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sessions := resp["sessions"].([]interface{})
	if len(sessions) != 1 {
		t.Fatalf("sessions count = %d, want 1", len(sessions))
	}
}

func TestParticipantTranscriptRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/participant/sessions/test-id/transcript", nil)
	unauth := httptest.NewRecorder()
	app.Router().ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth GET status = %d, want 401", unauth.Code)
	}
}

func TestParticipantTranscript(t *testing.T) {
	app := newAuthedTestApp(t)

	sess, err := app.store.AddParticipantSession("proj-t", "{}")
	if err != nil {
		t.Fatalf("add session: %v", err)
	}
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID: sess.ID,
		StartTS:   100,
		EndTS:     110,
		Text:      "hello meeting",
		Status:    "final",
	})
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID: sess.ID,
		StartTS:   200,
		EndTS:     210,
		Text:      "goodbye meeting",
		Status:    "final",
	})

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/sessions/"+sess.ID+"/transcript", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	segments := resp["segments"].([]interface{})
	if len(segments) != 2 {
		t.Fatalf("segments count = %d, want 2", len(segments))
	}
}

func TestParticipantTranscriptTimeFilter(t *testing.T) {
	app := newAuthedTestApp(t)

	sess, _ := app.store.AddParticipantSession("proj-tf", "{}")
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{SessionID: sess.ID, StartTS: 100, Text: "early"})
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{SessionID: sess.ID, StartTS: 200, Text: "late"})

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/sessions/"+sess.ID+"/transcript?from=150", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	segments := resp["segments"].([]interface{})
	if len(segments) != 1 {
		t.Fatalf("filtered segments = %d, want 1", len(segments))
	}
}

func TestParticipantSearch(t *testing.T) {
	app := newAuthedTestApp(t)

	sess, _ := app.store.AddParticipantSession("proj-s", "{}")
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{SessionID: sess.ID, StartTS: 100, Text: "hello world"})
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{SessionID: sess.ID, StartTS: 200, Text: "goodbye world"})

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/sessions/"+sess.ID+"/search?q=hello", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	segments := resp["segments"].([]interface{})
	if len(segments) != 1 {
		t.Fatalf("search results = %d, want 1", len(segments))
	}
}

func TestParticipantExportTxt(t *testing.T) {
	app := newAuthedTestApp(t)

	sess, _ := app.store.AddParticipantSession("proj-e", "{}")
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{SessionID: sess.ID, StartTS: 100, Speaker: "Alice", Text: "hello"})

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/sessions/"+sess.ID+"/export?format=txt", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Alice") || !strings.Contains(body, "hello") {
		t.Fatalf("txt export missing content: %q", body)
	}
}

func TestParticipantExportJSON(t *testing.T) {
	app := newAuthedTestApp(t)

	sess, _ := app.store.AddParticipantSession("proj-ej", "{}")
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{SessionID: sess.ID, StartTS: 100, Text: "hello json"})

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/sessions/"+sess.ID+"/export?format=json", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode json export: %v", err)
	}
	if resp["ok"] != true {
		t.Fatal("expected ok=true")
	}
	segments := resp["segments"].([]interface{})
	if len(segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(segments))
	}
}

func TestParticipantExportMarkdown(t *testing.T) {
	app := newAuthedTestApp(t)

	sess, _ := app.store.AddParticipantSession("proj-em", "{}")
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{SessionID: sess.ID, StartTS: 100, Speaker: "Bob", Text: "hello md"})

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/sessions/"+sess.ID+"/export?format=md", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "# Meeting Transcript") {
		t.Fatalf("md export missing header: %q", body)
	}
	if !strings.Contains(body, "**Bob**") {
		t.Fatalf("md export missing speaker: %q", body)
	}
}

func TestPrivacyParticipantConfigNeverStoresAudioPersistence(t *testing.T) {
	app := newAuthedTestApp(t)

	cfg := app.loadParticipantConfig()
	if cfg.AudioPersistence != "none" {
		t.Fatalf("default audio_persistence = %q, want none", cfg.AudioPersistence)
	}

	cfg.AudioPersistence = "disk"
	if err := app.saveParticipantConfig(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded := app.loadParticipantConfig()
	if loaded.AudioPersistence != "none" {
		t.Fatalf("loaded audio_persistence = %q after save with disk, want none", loaded.AudioPersistence)
	}
}

func TestPrivacyParticipantMessageNoAudioFields(t *testing.T) {
	msg := participantMessage{
		Type:      "participant_segment_text",
		SessionID: "test",
		Text:      "hello world",
		SegmentID: 1,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	banned := []string{"audio", "wav", "pcm", "recording", "sound_blob", "buffer", "bytes"}
	for key := range decoded {
		lower := strings.ToLower(key)
		for _, b := range banned {
			if lower == b {
				t.Errorf("participant message contains banned field %q", key)
			}
		}
	}
}

func TestPrivacyParticipantBufferCleanupOnStop(t *testing.T) {
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	conn.participantMu.Lock()
	conn.participantActive = true
	conn.participantSessionID = "test-sess"
	conn.participantBuf = make([]byte, 512)
	conn.participantMu.Unlock()

	app := newAuthedTestApp(t)
	_, _ = app.store.AddParticipantSession("test-key", "{}")

	handleParticipantStop(app, conn)

	conn.participantMu.Lock()
	defer conn.participantMu.Unlock()
	if conn.participantBuf != nil {
		t.Error("participantBuf should be nil after stop")
	}
	if conn.participantActive {
		t.Error("participantActive should be false after stop")
	}
	if conn.participantSessionID != "" {
		t.Error("participantSessionID should be empty after stop")
	}
}

func TestParticipantWSStartStop(t *testing.T) {
	app := newAuthedTestApp(t)
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, "test-session")

	conn.participantMu.Lock()
	active := conn.participantActive
	sessID := conn.participantSessionID
	conn.participantMu.Unlock()

	if !active {
		t.Fatal("expected participantActive=true after start")
	}
	if sessID == "" {
		t.Fatal("expected non-empty participantSessionID after start")
	}

	handleParticipantStop(app, conn)

	conn.participantMu.Lock()
	active = conn.participantActive
	sessID = conn.participantSessionID
	conn.participantMu.Unlock()

	if active {
		t.Fatal("expected participantActive=false after stop")
	}
	if sessID != "" {
		t.Fatalf("expected empty sessionID after stop, got %q", sessID)
	}
}

func TestParticipantDoubleStartReturnsError(t *testing.T) {
	app := newAuthedTestApp(t)
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, "test-session")
	handleParticipantStart(app, conn, "test-session")

	conn.participantMu.Lock()
	defer conn.participantMu.Unlock()
	if !conn.participantActive {
		t.Fatal("expected participantActive=true")
	}
}

func TestParticipantStopWithoutStartReturnsError(t *testing.T) {
	app := newAuthedTestApp(t)
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStop(app, conn)

	conn.participantMu.Lock()
	defer conn.participantMu.Unlock()
	if conn.participantActive {
		t.Fatal("should not be active after stop-without-start")
	}
}
