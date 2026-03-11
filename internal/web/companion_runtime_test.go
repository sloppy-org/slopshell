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

func waitForWSJSONMessageType(t *testing.T, clientConn *websocket.Conn, timeout time.Duration, wantType string) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := clientConn.SetReadDeadline(time.Now().Add(250 * time.Millisecond)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		mt, data, err := clientConn.ReadMessage()
		if err != nil {
			netErr, ok := err.(interface{ Timeout() bool })
			if ok && netErr.Timeout() {
				continue
			}
			t.Fatalf("ReadMessage: %v", err)
		}
		if mt != websocket.TextMessage {
			continue
		}
		var payload map[string]interface{}
		if err := json.Unmarshal(data, &payload); err != nil {
			continue
		}
		msgType, _ := payload["type"].(string)
		if strings.TrimSpace(msgType) == wantType {
			return payload
		}
	}
	t.Fatalf("timed out waiting for websocket message type %q", wantType)
	return nil
}

func waitForCompanionState(t *testing.T, clientConn *websocket.Conn, timeout time.Duration, wantState, wantReason string) map[string]interface{} {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		payload := waitForWSJSONMessageType(t, clientConn, time.Until(deadline), companionEventState)
		gotState, _ := payload["state"].(string)
		gotReason, _ := payload["reason"].(string)
		if strings.TrimSpace(gotState) != strings.TrimSpace(wantState) {
			continue
		}
		if strings.TrimSpace(wantReason) != "" && strings.TrimSpace(gotReason) != strings.TrimSpace(wantReason) {
			continue
		}
		return payload
	}
	t.Fatalf("timed out waiting for companion state=%q reason=%q", wantState, wantReason)
	return nil
}

func TestCompanionRuntimeProtocolEmitsListeningAndTranscriptEvents(t *testing.T) {
	app := newAuthedTestApp(t)
	t.Setenv("PATH", t.TempDir())

	sttSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"Companion transcript."}`))
	}))
	defer sttSrv.Close()
	app.sttURL = sttSrv.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	workspace := requireWorkspaceForProject(t, app, project)
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	cfg.DirectedSpeechGateEnabled = false
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}
	setLivePolicyForTest(t, app, LivePolicyMeeting)
	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(chatSession.ID, conn)
	defer app.hub.unregisterChat(chatSession.ID, conn)

	handleParticipantStart(app, conn, chatSession.ID)

	started := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "participant_started")
	if strings.TrimSpace(started["session_id"].(string)) == "" {
		t.Fatal("participant_started.session_id is empty")
	}
	stateStarted := waitForCompanionState(t, clientConn, 2*time.Second, companionRuntimeStateListening, "participant_started")
	if got := strings.TrimSpace(stateStarted["state"].(string)); got != companionRuntimeStateListening {
		t.Fatalf("start companion state = %q, want %q", got, companionRuntimeStateListening)
	}
	if got := strings.TrimSpace(stateStarted["reason"].(string)); got != "participant_started" {
		t.Fatalf("start companion reason = %q, want participant_started", got)
	}

	handleParticipantBinaryChunk(app, conn, buildParticipantSpeechWAV(240, 16000))

	partial := waitForWSJSONMessageType(t, clientConn, 2*time.Second, companionEventTranscriptPartial)
	if got := strings.TrimSpace(partial["text"].(string)); got != "Companion transcript." {
		t.Fatalf("partial transcript text = %q, want %q", got, "Companion transcript.")
	}
	if got := strings.TrimSpace(partial["status"].(string)); got != "partial" {
		t.Fatalf("partial transcript status = %q, want partial", got)
	}

	final := waitForWSJSONMessageType(t, clientConn, 2*time.Second, companionEventTranscriptFinal)
	if got := strings.TrimSpace(final["text"].(string)); got != "Companion transcript." {
		t.Fatalf("final transcript text = %q, want %q", got, "Companion transcript.")
	}
	if got := strings.TrimSpace(final["status"].(string)); got != "final" {
		t.Fatalf("final transcript status = %q, want final", got)
	}

	stateFinal := waitForCompanionState(t, clientConn, 2*time.Second, companionRuntimeStateListening, "transcript_finalized")
	if got := strings.TrimSpace(stateFinal["state"].(string)); got != companionRuntimeStateListening {
		t.Fatalf("final companion state = %q, want %q", got, companionRuntimeStateListening)
	}
	if got := strings.TrimSpace(stateFinal["reason"].(string)); got != "transcript_finalized" {
		t.Fatalf("final companion reason = %q, want transcript_finalized", got)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/companion/state", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET companion state status = %d, want 200", rr.Code)
	}
	var state companionStateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode companion state: %v", err)
	}
	if state.State != companionRuntimeStateListening {
		t.Fatalf("state = %q, want %q", state.State, companionRuntimeStateListening)
	}
	if state.Runtime.Reason != "transcript_finalized" {
		t.Fatalf("runtime.reason = %q, want transcript_finalized", state.Runtime.Reason)
	}
}

func TestCompanionRuntimeProtocolTransitionsThroughTalkingAndBackToListening(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}
	setLivePolicyForTest(t, app, LivePolicyMeeting)
	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	participantSession, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession: %v", err)
	}
	app.noteCompanionPendingTurn(chatSession.ID, participantSession.ID, 77)

	ttsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(buildParticipantSpeechWAV(100, 16000))
	}))
	defer ttsSrv.Close()
	app.ttsURL = ttsSrv.URL

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(chatSession.ID, conn)
	defer app.hub.unregisterChat(chatSession.ID, conn)

	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	response := app.finalizeAssistantResponse(chatSession.ID, project.ProjectKey, "Spoken companion reply.", &persistedAssistantID, &persistedAssistantText, "turn-voice", "", "", turnOutputModeVoice)
	if strings.TrimSpace(response) == "" {
		t.Fatal("finalizeAssistantResponse returned empty content")
	}

	stateTalking := waitForCompanionState(t, clientConn, 2*time.Second, companionRuntimeStateTalking, "assistant_output_ready")
	if got := strings.TrimSpace(stateTalking["state"].(string)); got != companionRuntimeStateTalking {
		t.Fatalf("assistant output companion state = %q, want %q", got, companionRuntimeStateTalking)
	}

	seq := conn.reserveTTSSeq()
	app.handleTTSSpeak(chatSession.ID, conn, seq, "Spoken companion reply.", "en")

	stateTalking = waitForCompanionState(t, clientConn, 2*time.Second, companionRuntimeStateTalking, "tts_started")
	if got := strings.TrimSpace(stateTalking["reason"].(string)); got != "tts_started" {
		t.Fatalf("tts start companion reason = %q, want tts_started", got)
	}
	stateListening := waitForCompanionState(t, clientConn, 2*time.Second, companionRuntimeStateListening, "tts_completed")
	if got := strings.TrimSpace(stateListening["state"].(string)); got != companionRuntimeStateListening {
		t.Fatalf("tts settle companion state = %q, want %q", got, companionRuntimeStateListening)
	}
	if got := strings.TrimSpace(stateListening["reason"].(string)); got != "tts_completed" {
		t.Fatalf("tts settle companion reason = %q, want tts_completed", got)
	}
}

func TestInterruptCompanionPendingTurnBroadcastsListeningState(t *testing.T) {
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
	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	participantSession, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession: %v", err)
	}
	app.noteCompanionPendingTurn(chatSession.ID, participantSession.ID, 99)

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(chatSession.ID, conn)
	defer app.hub.unregisterChat(chatSession.ID, conn)

	app.interruptCompanionPendingTurn(chatSession.ID, participantSession.ID, 99, 1, 0)

	stateInterrupted := waitForCompanionState(t, clientConn, 2*time.Second, companionRuntimeStateListening, "assistant_interrupted")
	if got := strings.TrimSpace(stateInterrupted["state"].(string)); got != companionRuntimeStateListening {
		t.Fatalf("interrupt companion state = %q, want %q", got, companionRuntimeStateListening)
	}
	if got := strings.TrimSpace(stateInterrupted["reason"].(string)); got != "assistant_interrupted" {
		t.Fatalf("interrupt companion reason = %q, want assistant_interrupted", got)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/companion/state", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET companion state status = %d, want 200", rr.Code)
	}
	var state companionStateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &state); err != nil {
		t.Fatalf("decode companion state: %v", err)
	}
	if state.State != companionRuntimeStateListening {
		t.Fatalf("state = %q, want %q", state.State, companionRuntimeStateListening)
	}
	if state.Runtime.Reason != "assistant_interrupted" {
		t.Fatalf("runtime.reason = %q, want assistant_interrupted", state.Runtime.Reason)
	}
}
