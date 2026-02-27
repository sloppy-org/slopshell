package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
