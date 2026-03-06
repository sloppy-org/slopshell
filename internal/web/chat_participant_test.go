package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/store"
)

func enableCompanionForTestProject(t *testing.T, app *App, projectKey string) {
	t.Helper()
	project, err := app.store.GetProjectByProjectKey(strings.TrimSpace(projectKey))
	if err != nil {
		t.Fatalf("GetProjectByProjectKey(%q): %v", projectKey, err)
	}
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}
}

func newParticipantTestWSConn(t *testing.T) (*chatWSConn, *websocket.Conn, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	serverConn := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConn <- ws
	}))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial test websocket: %v", err)
	}

	var ws *websocket.Conn
	select {
	case ws = <-serverConn:
	case <-time.After(2 * time.Second):
		_ = clientConn.Close()
		srv.Close()
		t.Fatal("timed out waiting for server websocket")
	}

	cleanup := func() {
		_ = ws.Close()
		_ = clientConn.Close()
		srv.Close()
	}
	return newChatWSConn(ws), clientConn, cleanup
}

func readParticipantMessage(t *testing.T, clientConn *websocket.Conn, timeout time.Duration) participantMessage {
	t.Helper()
	if err := clientConn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	defer func() {
		_ = clientConn.SetReadDeadline(time.Time{})
	}()

	_, data, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var msg participantMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal participant message: %v", err)
	}
	return msg
}

func assertNoParticipantMessage(t *testing.T, clientConn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	if err := clientConn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	defer func() {
		_ = clientConn.SetReadDeadline(time.Time{})
	}()

	_, _, err := clientConn.ReadMessage()
	if err == nil {
		t.Fatal("unexpected participant message")
	}
	netErr, ok := err.(net.Error)
	if !ok || !netErr.Timeout() {
		t.Fatalf("ReadMessage error = %v, want timeout", err)
	}
}

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
	if cfg.CaptureSource != "microphone" {
		t.Fatalf("capture_source = %q, want microphone", cfg.CaptureSource)
	}
	if cfg.Language == "" {
		t.Fatal("language is empty")
	}
	if cfg.MaxSegmentDurationMS <= 0 {
		t.Fatalf("max_segment_duration_ms = %d", cfg.MaxSegmentDurationMS)
	}
	if cfg.CompanionEnabled {
		t.Fatal("companion_enabled = true, want false")
	}
}

func TestParticipantConfigPutAudioPersistenceInvariant(t *testing.T) {
	app := newAuthedTestApp(t)

	payload := map[string]interface{}{
		"companion_enabled": false,
		"language":          "de",
		"stt_model":         "whisper-large",
		"idle_surface":      "black",
		"audio_persistence": "disk",
		"capture_source":    "line-in",
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
	if cfg.CaptureSource != "microphone" {
		t.Fatalf("capture_source = %q after PUT, want microphone", cfg.CaptureSource)
	}
	if cfg.Language != "de" {
		t.Fatalf("language = %q, want de", cfg.Language)
	}
	if cfg.IdleSurface != "black" {
		t.Fatalf("idle_surface = %q, want black", cfg.IdleSurface)
	}
	if cfg.CompanionEnabled {
		t.Fatal("companion_enabled = true after PUT, want false")
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/participant/config", nil)
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config after round-trip: %v", err)
	}
	if cfg.AudioPersistence != "none" {
		t.Fatalf("audio_persistence = %q after round-trip, want none", cfg.AudioPersistence)
	}
	if cfg.CaptureSource != "microphone" {
		t.Fatalf("capture_source = %q after round-trip, want microphone", cfg.CaptureSource)
	}
	if cfg.CompanionEnabled {
		t.Fatal("companion_enabled = true after round-trip, want false")
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
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}

	cfg := app.loadCompanionConfig(project)
	if cfg.AudioPersistence != "none" {
		t.Fatalf("default audio_persistence = %q, want none", cfg.AudioPersistence)
	}

	cfg.AudioPersistence = "disk"
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	reloadedProject, err := app.store.GetProject(project.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	loaded := app.loadCompanionConfig(reloadedProject)
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

func TestParticipantBinaryChunkTranscribesWAVSegmentImmediately(t *testing.T) {
	app := newAuthedTestApp(t)
	t.Setenv("PATH", t.TempDir())

	type upload struct {
		path     string
		filename string
		body     []byte
	}
	uploads := make(chan upload, 1)
	sttSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(2 * 1024 * 1024); err != nil {
			uploads <- upload{path: "parse-error", filename: err.Error()}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			uploads <- upload{path: "form-file-error", filename: err.Error()}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		body, err := io.ReadAll(file)
		if err != nil {
			uploads <- upload{path: "read-error", filename: err.Error()}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		uploads <- upload{
			path:     r.URL.Path,
			filename: header.Filename,
			body:     body,
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"participant transcript"}`))
	}))
	defer sttSrv.Close()
	app.sttURL = sttSrv.URL
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	enableCompanionForTestProject(t, app, project.ProjectKey)
	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, chatSession.ID)

	conn.participantMu.Lock()
	sessionID := conn.participantSessionID
	conn.participantMu.Unlock()
	if sessionID == "" {
		t.Fatal("expected non-empty participant session id")
	}

	wav := buildParticipantSpeechWAV(240, 16000)
	handleParticipantBinaryChunk(app, conn, wav)

	select {
	case req := <-uploads:
		if req.path != "/v1/audio/transcriptions" {
			t.Fatalf("stt path = %q, want /v1/audio/transcriptions", req.path)
		}
		if req.filename != "audio.wav" {
			t.Fatalf("stt filename = %q, want audio.wav", req.filename)
		}
		if !bytes.Equal(req.body, wav) {
			t.Fatal("uploaded WAV payload does not match the participant chunk")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for participant chunk upload")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		segments, err := app.store.ListParticipantSegments(sessionID, 0, 0)
		if err != nil {
			t.Fatalf("list participant segments: %v", err)
		}
		if len(segments) == 1 {
			if segments[0].Text != "participant transcript" {
				t.Fatalf("segment text = %q, want participant transcript", segments[0].Text)
			}
			if segments[0].Model != "whisper-1" {
				t.Fatalf("segment model = %q, want whisper-1", segments[0].Model)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("segments count = %d, want 1", len(segments))
		}
		time.Sleep(10 * time.Millisecond)
	}

	conn.participantMu.Lock()
	defer conn.participantMu.Unlock()
	if conn.participantBuf != nil {
		t.Fatal("participantBuf should be cleared after immediate chunk transcription")
	}
}

func TestParticipantBinaryChunkTranscribeFailureSendsParticipantError(t *testing.T) {
	app := newAuthedTestApp(t)
	t.Setenv("PATH", t.TempDir())
	sttSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forced sidecar failure", http.StatusBadGateway)
	}))
	defer sttSrv.Close()
	app.sttURL = sttSrv.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	enableCompanionForTestProject(t, app, project.ProjectKey)
	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, chatSession.ID)
	started := readParticipantMessage(t, clientConn, 2*time.Second)
	if started.Type != "participant_started" {
		t.Fatalf("start message type = %q, want participant_started", started.Type)
	}

	handleParticipantBinaryChunk(app, conn, buildParticipantSpeechWAV(240, 16000))

	msg := readParticipantMessage(t, clientConn, 2*time.Second)
	if msg.Type != "participant_error" {
		t.Fatalf("message type = %q, want participant_error", msg.Type)
	}
	if !strings.Contains(msg.Error, "transcription failed") {
		t.Fatalf("participant error = %q, want transcription failure", msg.Error)
	}

	segments, err := app.store.ListParticipantSegments(started.SessionID, 0, 0)
	if err != nil {
		t.Fatalf("ListParticipantSegments: %v", err)
	}
	if len(segments) != 0 {
		t.Fatalf("segments count = %d, want 0 after failed transcription", len(segments))
	}
}

func TestParticipantStopDropsLateTranscriptCommit(t *testing.T) {
	app := newAuthedTestApp(t)
	t.Setenv("PATH", t.TempDir())

	requestStarted := make(chan struct{}, 1)
	releaseResponse := make(chan struct{})
	sttSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requestStarted <- struct{}{}:
		default:
		}
		<-releaseResponse
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"late participant transcript"}`))
	}))
	defer sttSrv.Close()
	app.sttURL = sttSrv.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	enableCompanionForTestProject(t, app, project.ProjectKey)
	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, chatSession.ID)
	started := readParticipantMessage(t, clientConn, 2*time.Second)
	if started.Type != "participant_started" {
		t.Fatalf("start message type = %q, want participant_started", started.Type)
	}

	handleParticipantBinaryChunk(app, conn, buildParticipantSpeechWAV(240, 16000))

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for participant STT request")
	}

	handleParticipantStop(app, conn)
	stopped := readParticipantMessage(t, clientConn, 2*time.Second)
	if stopped.Type != "participant_stopped" {
		t.Fatalf("stop message type = %q, want participant_stopped", stopped.Type)
	}

	close(releaseResponse)

	deadline := time.Now().Add(2 * time.Second)
	for {
		segments, err := app.store.ListParticipantSegments(started.SessionID, 0, 0)
		if err != nil {
			t.Fatalf("ListParticipantSegments: %v", err)
		}
		if len(segments) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("segments count = %d, want 0 after stop", len(segments))
		}
		time.Sleep(10 * time.Millisecond)
	}

	assertNoParticipantMessage(t, clientConn, 250*time.Millisecond)
}

func TestParticipantStartUsesChatSessionProjectKey(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	enableCompanionForTestProject(t, app, project.ProjectKey)
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, session.ID)

	conn.participantMu.Lock()
	participantSessionID := conn.participantSessionID
	conn.participantMu.Unlock()
	if participantSessionID == "" {
		t.Fatal("expected participant session id")
	}
	participantSession, err := app.store.GetParticipantSession(participantSessionID)
	if err != nil {
		t.Fatalf("GetParticipantSession: %v", err)
	}
	if participantSession.ProjectKey != project.ProjectKey {
		t.Fatalf("participant session project_key = %q, want %q", participantSession.ProjectKey, project.ProjectKey)
	}
}

func TestParticipantReleaseSessionEndsPersistedSession(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	enableCompanionForTestProject(t, app, project.ProjectKey)
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, session.ID)

	conn.participantMu.Lock()
	participantSessionID := conn.participantSessionID
	conn.participantMu.Unlock()
	if participantSessionID == "" {
		t.Fatal("expected participant session id")
	}

	releasedSessionID, ok := releaseParticipantSession(app, conn)
	if !ok {
		t.Fatal("releaseParticipantSession() = false, want true")
	}
	if releasedSessionID != participantSessionID {
		t.Fatalf("released session id = %q, want %q", releasedSessionID, participantSessionID)
	}

	persisted, err := app.store.GetParticipantSession(participantSessionID)
	if err != nil {
		t.Fatalf("GetParticipantSession: %v", err)
	}
	if persisted.EndedAt == 0 {
		t.Fatal("persisted session should be ended after release")
	}

	conn.participantMu.Lock()
	defer conn.participantMu.Unlock()
	if conn.participantActive {
		t.Fatal("participantActive should be false after release")
	}
	if conn.participantSessionID != "" {
		t.Fatalf("participantSessionID = %q, want empty", conn.participantSessionID)
	}
	if conn.participantBuf != nil {
		t.Fatal("participantBuf should be nil after release")
	}
}

func TestParticipantWSStartStop(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	enableCompanionForTestProject(t, app, project.ProjectKey)
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, session.ID)

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
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	enableCompanionForTestProject(t, app, project.ProjectKey)
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, session.ID)
	handleParticipantStart(app, conn, session.ID)

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

func TestParticipantStartRequiresCompanionEnabled(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleParticipantStart(app, conn, "test-session")

	conn.participantMu.Lock()
	active := conn.participantActive
	sessionID := conn.participantSessionID
	conn.participantMu.Unlock()
	if active {
		t.Fatal("participantActive = true, want false when companion is disabled")
	}
	if sessionID != "" {
		t.Fatalf("participantSessionID = %q, want empty", sessionID)
	}
	sessions, err := app.store.ListParticipantSessions(project.ProjectKey)
	if err != nil {
		t.Fatalf("ListParticipantSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("participant sessions = %d, want 0", len(sessions))
	}
}

func TestParticipantConfigPutDisableStopsActiveSession(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	enableCompanionForTestProject(t, app, project.ProjectKey)
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	conn, cleanup := newTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	handleParticipantStart(app, conn, session.ID)

	conn.participantMu.Lock()
	participantSessionID := conn.participantSessionID
	conn.participantMu.Unlock()
	if participantSessionID == "" {
		t.Fatal("expected participant session id")
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/participant/config", map[string]any{
		"companion_enabled": false,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", rr.Code)
	}

	conn.participantMu.Lock()
	active := conn.participantActive
	currentSessionID := conn.participantSessionID
	conn.participantMu.Unlock()
	if active {
		t.Fatal("participantActive = true, want false after disabling companion")
	}
	if currentSessionID != "" {
		t.Fatalf("participantSessionID = %q, want empty after disabling companion", currentSessionID)
	}

	persisted, err := app.store.GetParticipantSession(participantSessionID)
	if err != nil {
		t.Fatalf("GetParticipantSession: %v", err)
	}
	if persisted.EndedAt == 0 {
		t.Fatal("participant session should be ended after disabling companion")
	}
}

func buildParticipantSpeechWAV(durationMS, sampleRate int) []byte {
	numSamples := sampleRate * durationMS / 1000
	dataSize := numSamples * 2
	buf := bytes.NewBuffer(make([]byte, 0, 44+dataSize))

	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataSize))

	for i := 0; i < numSamples; i++ {
		sample := int16(12000)
		if i%8 < 4 {
			sample = -12000
		}
		_ = binary.Write(buf, binary.LittleEndian, sample)
	}

	return buf.Bytes()
}
