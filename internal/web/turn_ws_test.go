package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestTurnWSRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/turn/"+session.ID, nil)
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestTurnWSProducesYieldAndFinalizeActions(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	srv := httptest.NewServer(app.Router())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/turn/" + session.ID
	header := http.Header{}
	header.Add("Cookie", SessionCookie+"="+testAuthToken)
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial turn websocket: %v", err)
	}
	defer clientConn.Close()

	readMessage := func() map[string]any {
		t.Helper()
		if err := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		defer func() {
			_ = clientConn.SetReadDeadline(time.Time{})
		}()
		_, data, err := clientConn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("unmarshal websocket payload: %v", err)
		}
		return payload
	}

	ready := readMessage()
	if ready["type"] != "turn_ready" {
		t.Fatalf("ready type = %#v, want turn_ready", ready["type"])
	}

	if err := clientConn.WriteJSON(map[string]any{
		"type":      "turn_playback",
		"playing":   true,
		"played_ms": 480,
	}); err != nil {
		t.Fatalf("WriteJSON(turn_playback): %v", err)
	}
	if err := clientConn.WriteJSON(map[string]any{
		"type":                  "turn_speech_start",
		"interrupted_assistant": true,
	}); err != nil {
		t.Fatalf("WriteJSON(turn_speech_start): %v", err)
	}

	yield := readMessage()
	if yield["type"] != "turn_action" {
		t.Fatalf("yield type = %#v, want turn_action", yield["type"])
	}
	if yield["action"] != "yield" {
		t.Fatalf("yield action = %#v, want yield", yield["action"])
	}
	if got := int(yield["rollback_audio_ms"].(float64)); got <= 0 {
		t.Fatalf("rollback_audio_ms = %d, want > 0", got)
	}

	if err := clientConn.WriteJSON(map[string]any{
		"type":        "turn_transcript_segment",
		"text":        "I think",
		"duration_ms": 300,
	}); err != nil {
		t.Fatalf("WriteJSON(turn_transcript_segment #1): %v", err)
	}
	continueMsg := readMessage()
	if continueMsg["action"] != "continue_listening" {
		t.Fatalf("continue action = %#v, want continue_listening", continueMsg["action"])
	}

	if err := clientConn.WriteJSON(map[string]any{
		"type":        "turn_transcript_segment",
		"text":        "that's enough.",
		"duration_ms": 1400,
	}); err != nil {
		t.Fatalf("WriteJSON(turn_transcript_segment #2): %v", err)
	}
	finalize := readMessage()
	if finalize["action"] != "finalize_user_turn" {
		t.Fatalf("finalize action = %#v, want finalize_user_turn", finalize["action"])
	}
	if finalize["text"] != "I think that's enough." {
		t.Fatalf("finalize text = %#v", finalize["text"])
	}
}
