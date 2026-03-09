package web

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/stt"
)

func TestWebRouterDoesNotExposeMCPRoute(t *testing.T) {
	app := newAuthedTestApp(t)

	rrPost := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	})
	if rrPost.Code != http.StatusNotFound {
		t.Fatalf("expected POST /mcp to return 404 on web router, got %d", rrPost.Code)
	}

	rrGet := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/mcp", nil)
	if rrGet.Code != http.StatusNotFound {
		t.Fatalf("expected GET /mcp to return 404 on web router, got %d", rrGet.Code)
	}
}

func TestServeIndexUsesRelativeStaticAssets(t *testing.T) {
	app := newAuthedTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, fragment := range []string{
		`<base href="/">`,
		`href="./static/style.css`,
		`src="./static/app.js`,
		`src="./static/polyfill.js"`,
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("GET / body missing %q", fragment)
		}
	}
}

func TestServeIndexUsesForwardedPrefixBaseHref(t *testing.T) {
	app := newAuthedTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Prefix", "/tabura")
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rr.Code)
	}
	if body := rr.Body.String(); !strings.Contains(body, `<base href="/tabura/">`) {
		t.Fatalf("GET / body missing forwarded-prefix base href, body=%q", body)
	}
	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "base-uri 'self'") {
		t.Fatalf("GET / csp missing base-uri 'self': %q", csp)
	}
}

func TestServeCanvasRedirectIsRelative(t *testing.T) {
	app := newAuthedTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/canvas", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusTemporaryRedirect {
		t.Fatalf("GET /canvas status = %d, want %d", rr.Code, http.StatusTemporaryRedirect)
	}
	if got := rr.Header().Get("Location"); got != "./?desktop=1" {
		t.Fatalf("GET /canvas Location = %q, want %q", got, "./?desktop=1")
	}
}

func TestServeCaptureUsesStandaloneAssets(t *testing.T) {
	app := newAuthedTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/capture", nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /capture status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, fragment := range []string{
		`id="capture-page"`,
		`href="./static/capture.css`,
		`src="./static/capture.js`,
		`id="capture-record"`,
		`id="capture-note"`,
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("GET /capture body missing %q", fragment)
		}
	}
	for _, forbidden := range []string{`id="workspace"`, `id="edge-left-tap"`, `src="./static/app.js"`} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("GET /capture body unexpectedly contained %q", forbidden)
		}
	}
}

// newTestWSConn creates a chatWSConn backed by a real websocket for testing.
// The returned cleanup function closes both ends.
func newTestWSConn(t *testing.T) (*chatWSConn, func()) {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Drain messages so writes don't block.
		go func() {
			for {
				if _, _, err := ws.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial test websocket: %v", err)
	}
	conn := newChatWSConn(ws)
	cleanup := func() {
		_ = ws.Close()
		srv.Close()
	}
	return conn, cleanup
}

// Privacy: docs/meeting-notes-privacy.md

func TestPrivacySchemaNoAudioColumns(t *testing.T) {
	app := newAuthedTestApp(t)

	banned := []string{"audio", "wav", "pcm", "recording", "sound_blob"}

	tableColumns, err := app.store.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns: %v", err)
	}
	if len(tableColumns) == 0 {
		t.Fatal("expected at least one table in schema")
	}

	for table, cols := range tableColumns {
		for _, col := range cols {
			lower := strings.ToLower(col)
			for _, b := range banned {
				if strings.Contains(lower, b) {
					t.Errorf("table %q column %q contains banned audio term %q", table, col, b)
				}
			}
		}
	}
}

func TestPrivacySTTBufferCleanupOnCancel(t *testing.T) {
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleSTTStart(conn, "audio/webm")

	// Simulate binary chunks
	handleSTTBinaryChunk(conn, make([]byte, 512))

	// Cancel to clean up buffer (stop requires a live sttURL sidecar)
	handleSTTCancel(conn)

	conn.sttMu.Lock()
	defer conn.sttMu.Unlock()

	if conn.sttBuf != nil {
		t.Error("sttBuf should be nil after cancel")
	}
	if conn.sttActive {
		t.Error("sttActive should be false after cancel")
	}
	if conn.sttMimeType != "" {
		t.Error("sttMimeType should be empty after cancel")
	}
}

func TestPrivacySTTBufferCleanupOnOverflow(t *testing.T) {
	conn, cleanup := newTestWSConn(t)
	defer cleanup()

	handleSTTStart(conn, "audio/webm")

	// Send a chunk that exceeds MaxAudioBytes
	handleSTTBinaryChunk(conn, make([]byte, stt.MaxAudioBytes+1))

	conn.sttMu.Lock()
	defer conn.sttMu.Unlock()

	if conn.sttBuf != nil {
		t.Error("sttBuf should be nil after overflow")
	}
	if conn.sttActive {
		t.Error("sttActive should be false after overflow")
	}
	if conn.sttMimeType != "" {
		t.Error("sttMimeType should be empty after overflow")
	}
}

func TestPrivacySTTResultContainsOnlyText(t *testing.T) {
	msg := sttMessage{Type: "stt_result", Text: "hello world"}
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
				t.Errorf("stt_result message contains banned field %q", key)
			}
		}
	}

	if decoded["type"] != "stt_result" {
		t.Errorf("expected type=stt_result, got %v", decoded["type"])
	}
	if decoded["text"] != "hello world" {
		t.Errorf("expected text=hello world, got %v", decoded["text"])
	}
}

func TestPrivacySTTHTTPCaptureRejectsOversizedUploadWithoutTempFiles(t *testing.T) {
	app := newAuthedTestApp(t)
	app.sttURL = "http://127.0.0.1:1"

	scopedTmp := t.TempDir()
	t.Setenv("TMPDIR", scopedTmp)

	before, err := listScopedEntries(scopedTmp)
	if err != nil {
		t.Fatalf("list temp dir before: %v", err)
	}

	tooLarge := bytes.Repeat([]byte("a"), stt.MaxAudioBytes+(2*1024*1024))
	rr := doAuthedMultipartAudioRequest(t, app.Router(), "/api/stt/transcribe", "audio.webm", "audio/webm", tooLarge)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST /api/stt/transcribe status = %d, want %d: %s", rr.Code, http.StatusRequestEntityTooLarge, rr.Body.String())
	}

	after, err := listScopedEntries(scopedTmp)
	if err != nil {
		t.Fatalf("list temp dir after: %v", err)
	}
	for _, path := range diffScopedEntries(before, after) {
		t.Errorf("oversized upload created unexpected temp file: %s", path)
	}
}

func TestPrivacySTTHTTPCaptureReturnsOnlyTextWithoutTempFiles(t *testing.T) {
	app := newAuthedTestApp(t)

	sttSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"text": "  captured idea  "}); err != nil {
			t.Fatalf("encode stt response: %v", err)
		}
	}))
	defer sttSrv.Close()
	app.sttURL = sttSrv.URL

	scopedTmp := t.TempDir()
	t.Setenv("TMPDIR", scopedTmp)

	before, err := listScopedEntries(scopedTmp)
	if err != nil {
		t.Fatalf("list temp dir before: %v", err)
	}

	audio := buildSpeechLikeWAV(16000, 240, 0.75)
	rr := doAuthedMultipartAudioRequest(t, app.Router(), "/api/stt/transcribe", "audio.wav", "audio/wav", audio)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/stt/transcribe status = %d, want 200: %s", rr.Code, rr.Body.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode STT response: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("STT response fields = %v, want only text", payload)
	}
	if payload["text"] != "captured idea" {
		t.Fatalf("STT response text = %q, want %q", payload["text"], "captured idea")
	}

	after, err := listScopedEntries(scopedTmp)
	if err != nil {
		t.Fatalf("list temp dir after: %v", err)
	}
	for _, path := range diffScopedEntries(before, after) {
		t.Errorf("capture upload created unexpected temp file: %s", path)
	}
}

func TestPrivacyNoChatMessageAudioContent(t *testing.T) {
	app := newAuthedTestApp(t)

	project, err := app.store.CreateProject("test", "test-key", "/tmp/test", "local", "", "local", true)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err = app.store.AddChatMessage(session.ID, "user", "hello world", "hello world", "text")
	if err != nil {
		t.Fatalf("add message: %v", err)
	}

	messages, err := app.store.ListChatMessages(session.ID, 100)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}

	for _, msg := range messages {
		fields := []string{msg.ContentMarkdown, msg.ContentPlain, msg.RenderFormat}
		for _, f := range fields {
			lower := strings.ToLower(f)
			if strings.Contains(lower, "audio/") || strings.Contains(lower, "base64,") {
				t.Errorf("chat message contains audio-like content: %q", f)
			}
		}
	}
}

func doAuthedMultipartAudioRequest(t *testing.T, handler http.Handler, path, filename, mimeType string, audio []byte) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(audio); err != nil {
		t.Fatalf("write audio: %v", err)
	}
	if err := writer.WriteField("mime_type", mimeType); err != nil {
		t.Fatalf("WriteField(mime_type): %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func buildSpeechLikeWAV(sampleRate, durationMS int, amplitude float64) []byte {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if durationMS <= 0 {
		durationMS = 240
	}
	if amplitude <= 0 {
		amplitude = 0.75
	}
	if amplitude > 1 {
		amplitude = 1
	}

	totalSamples := sampleRate * durationMS / 1000
	dataSize := totalSamples * 2
	out := make([]byte, 44+dataSize)

	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+dataSize))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], 1)
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(out[32:34], 2)
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataSize))

	pos := 44
	for i := 0; i < totalSamples; i++ {
		t := float64(i) / float64(sampleRate)
		sample := int16(amplitude * 32767.0 * math.Sin(2*math.Pi*220*t))
		binary.LittleEndian.PutUint16(out[pos:pos+2], uint16(sample))
		pos += 2
	}
	return out
}

func listScopedEntries(dir string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		out[filepath.Join(dir, entry.Name())] = struct{}{}
	}
	return out, nil
}

func diffScopedEntries(before, after map[string]struct{}) []string {
	var added []string
	for path := range after {
		if _, ok := before[path]; !ok {
			added = append(added, path)
		}
	}
	return added
}
