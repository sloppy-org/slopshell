package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/store"
)

func newCompanionAppServerClient(t *testing.T, assistantMessage string) *appserver.Client {
	client, _ := newCompanionAppServerClientWithCapture(t, assistantMessage)
	return client
}

func newCompanionAppServerClientWithCapture(t *testing.T, assistantMessage string) (*appserver.Client, <-chan string) {
	t.Helper()
	promptCh := make(chan string, 4)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode message: %v", err)
			}
			switch strings.TrimSpace(msg["method"].(string)) {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{
					"id":     msg["id"],
					"result": map[string]interface{}{"userAgent": "test-client"},
				})
			case "initialized":
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-companion"},
					},
				})
			case "turn/start":
				if prompt := appServerPromptFromRPC(msg); strings.TrimSpace(prompt) != "" {
					select {
					case promptCh <- prompt:
					default:
					}
				}
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-companion"},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "agentMessage",
							"text": assistantMessage,
						},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "turn/completed",
					"params": map[string]interface{}{
						"turn": map[string]interface{}{
							"id":     "turn-companion",
							"status": "completed",
						},
					},
				})
			}
		}
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := appserver.NewClient(wsURL)
	if err != nil {
		t.Fatalf("new appserver client: %v", err)
	}
	return client, promptCh
}

func appServerPromptFromRPC(msg map[string]interface{}) string {
	params, _ := msg["params"].(map[string]interface{})
	if params == nil {
		return ""
	}
	input, _ := params["input"].([]interface{})
	if len(input) == 0 {
		return ""
	}
	first, _ := input[0].(map[string]interface{})
	if first == nil {
		return ""
	}
	text, _ := first["text"].(string)
	return strings.TrimSpace(text)
}

func waitForAssistantMessage(t *testing.T, app *App, sessionID, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := latestAssistantMessage(t, app, sessionID); got == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for assistant message %q", want)
}

func waitForCapturedPrompt(t *testing.T, promptCh <-chan string) string {
	t.Helper()
	select {
	case prompt := <-promptCh:
		return prompt
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for captured prompt")
		return ""
	}
}

func TestCompanionResponseTriggerExecutesAssistantTurn(t *testing.T) {
	t.Setenv("TABURA_INTENT_LLM_URL", "off")
	app := newAuthedTestApp(t)
	app.appServerClient = newCompanionAppServerClient(t, "Companion reply.")

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	cfg.DirectedSpeechGateEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}

	participantSession, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("add participant session: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, 0, "session_started", "{}"); err != nil {
		t.Fatalf("add participant event: %v", err)
	}
	seg, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   participantSession.ID,
		StartTS:     100,
		EndTS:       101,
		Text:        "Tabura, tell me something helpful.",
		CommittedAt: 102,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("add participant segment: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, seg.ID, "segment_committed", `{"text":"Tabura, tell me something helpful."}`); err != nil {
		t.Fatalf("add participant committed event: %v", err)
	}

	app.maybeTriggerCompanionResponse(participantSession.ID, seg)

	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("get chat session: %v", err)
	}
	waitForAssistantMessage(t, app, chatSession.ID, "Companion reply.")

	messages, err := app.store.ListChatMessages(chatSession.ID, 10)
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("chat message count = %d, want 2", len(messages))
	}
	if strings.TrimSpace(messages[0].Role) != "user" {
		t.Fatalf("first message role = %q, want user", messages[0].Role)
	}
	if strings.TrimSpace(messages[0].ContentPlain) != "Tabura, tell me something helpful." {
		t.Fatalf("first message text = %q", messages[0].ContentPlain)
	}
	if strings.TrimSpace(messages[1].Role) != "assistant" {
		t.Fatalf("second message role = %q, want assistant", messages[1].Role)
	}

	events, err := app.store.ListParticipantEvents(participantSession.ID)
	if err != nil {
		t.Fatalf("list participant events: %v", err)
	}
	foundTrigger := false
	for _, event := range events {
		if event.SegmentID == seg.ID && event.EventType == "assistant_triggered" {
			foundTrigger = true
			break
		}
	}
	if !foundTrigger {
		t.Fatal("expected assistant_triggered participant event")
	}
}

func TestCompanionResponseTriggerSkipsWhenCompanionDisabled(t *testing.T) {
	t.Setenv("TABURA_INTENT_LLM_URL", "off")
	app := newAuthedTestApp(t)
	app.appServerClient = newCompanionAppServerClient(t, "unexpected reply")

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = false
	cfg.DirectedSpeechGateEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}

	participantSession, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("add participant session: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, 0, "session_started", "{}"); err != nil {
		t.Fatalf("add participant event: %v", err)
	}
	seg, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   participantSession.ID,
		StartTS:     100,
		EndTS:       101,
		Text:        "Tabura, tell me something helpful.",
		CommittedAt: 102,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("add participant segment: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, seg.ID, "segment_committed", `{"text":"Tabura, tell me something helpful."}`); err != nil {
		t.Fatalf("add participant committed event: %v", err)
	}

	app.maybeTriggerCompanionResponse(participantSession.ID, seg)

	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("get chat session: %v", err)
	}
	messages, err := app.store.ListChatMessages(chatSession.ID, 10)
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("chat message count = %d, want 0", len(messages))
	}
}

func TestCompanionResponseTriggerSkipsFalseTriggerTranscript(t *testing.T) {
	t.Setenv("TABURA_INTENT_LLM_URL", "off")
	app := newAuthedTestApp(t)
	app.appServerClient = newCompanionAppServerClient(t, "unexpected reply")

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	cfg.DirectedSpeechGateEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}

	participantSession, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("add participant session: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, 0, "session_started", "{}"); err != nil {
		t.Fatalf("add participant event: %v", err)
	}
	seg, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   participantSession.ID,
		StartTS:     100,
		EndTS:       101,
		Text:        "Please summarize the meeting.",
		CommittedAt: 102,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("add participant segment: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, seg.ID, "segment_committed", `{"text":"Please summarize the meeting."}`); err != nil {
		t.Fatalf("add participant committed event: %v", err)
	}

	app.maybeTriggerCompanionResponse(participantSession.ID, seg)

	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("get chat session: %v", err)
	}
	messages, err := app.store.ListChatMessages(chatSession.ID, 10)
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("chat message count = %d, want 0", len(messages))
	}
	events, err := app.store.ListParticipantEvents(participantSession.ID)
	if err != nil {
		t.Fatalf("list participant events: %v", err)
	}
	for _, event := range events {
		if event.SegmentID == seg.ID && event.EventType == "assistant_triggered" {
			t.Fatal("did not expect assistant_triggered participant event")
		}
	}
}

func TestCompanionResponseTriggerUsesSilentModeOutputQueue(t *testing.T) {
	t.Setenv("TABURA_INTENT_LLM_URL", "off")
	app := newAuthedTestApp(t)
	app.appServerClient = newCompanionAppServerClient(t, "Silent companion reply.")
	if err := app.setSilentModeEnabled(true); err != nil {
		t.Fatalf("set silent mode: %v", err)
	}

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	cfg.DirectedSpeechGateEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}

	participantSession, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("add participant session: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, 0, "session_started", "{}"); err != nil {
		t.Fatalf("add participant event: %v", err)
	}
	seg, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   participantSession.ID,
		StartTS:     100,
		EndTS:       101,
		Text:        "Tabura, tell me something helpful.",
		CommittedAt: 102,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("add participant segment: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, seg.ID, "segment_committed", `{"text":"Tabura, tell me something helpful."}`); err != nil {
		t.Fatalf("add participant committed event: %v", err)
	}

	app.maybeTriggerCompanionResponse(participantSession.ID, seg)

	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("get chat session: %v", err)
	}
	waitForAssistantMessage(t, app, chatSession.ID, "Silent companion reply.")
	if queued := app.queuedChatTurnCount(chatSession.ID); queued != 0 {
		t.Fatalf("queued chat turns = %d, want 0", queued)
	}
}

func TestCompanionResponseTriggerDoesNotDuplicateSegment(t *testing.T) {
	t.Setenv("TABURA_INTENT_LLM_URL", "off")
	app := newAuthedTestApp(t)
	app.appServerClient = newCompanionAppServerClient(t, "Companion reply.")

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	cfg.DirectedSpeechGateEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}

	participantSession, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("add participant session: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, 0, "session_started", "{}"); err != nil {
		t.Fatalf("add participant event: %v", err)
	}
	seg, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   participantSession.ID,
		StartTS:     100,
		EndTS:       101,
		Text:        "Tabura, tell me something helpful.",
		CommittedAt: 102,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("add participant segment: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, seg.ID, "segment_committed", `{"text":"Tabura, tell me something helpful."}`); err != nil {
		t.Fatalf("add participant committed event: %v", err)
	}

	app.maybeTriggerCompanionResponse(participantSession.ID, seg)
	app.maybeTriggerCompanionResponse(participantSession.ID, seg)

	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("get chat session: %v", err)
	}
	waitForAssistantMessage(t, app, chatSession.ID, "Companion reply.")

	messages, err := app.store.ListChatMessages(chatSession.ID, 10)
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("chat message count = %d, want 2", len(messages))
	}
}

func TestCompanionResponseTriggerInterruptsPendingTurn(t *testing.T) {
	t.Setenv("TABURA_INTENT_LLM_URL", "off")
	app := newAuthedTestApp(t)

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	cfg.DirectedSpeechGateEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}

	participantSession, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("add participant session: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, 0, "session_started", "{}"); err != nil {
		t.Fatalf("add participant event: %v", err)
	}
	firstSeg, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   participantSession.ID,
		StartTS:     100,
		EndTS:       101,
		Text:        "Tabura, summarize that.",
		CommittedAt: 102,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("add first participant segment: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, firstSeg.ID, "segment_committed", `{"text":"Tabura, summarize that."}`); err != nil {
		t.Fatalf("add first participant committed event: %v", err)
	}

	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("get chat session: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, firstSeg.ID, "assistant_triggered", `{"chat_session_id":"`+chatSession.ID+`"}`); err != nil {
		t.Fatalf("add assistant_triggered event: %v", err)
	}
	app.noteCompanionPendingTurn(chatSession.ID, participantSession.ID, firstSeg.ID)
	cancelCalled := false
	app.registerActiveChatTurn(chatSession.ID, "run-1", func() {
		cancelCalled = true
	})
	defer app.unregisterActiveChatTurn(chatSession.ID, "run-1")

	secondSeg, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   participantSession.ID,
		StartTS:     103,
		EndTS:       104,
		Text:        "Tabura, open the transcript.",
		CommittedAt: 105,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("add second participant segment: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, secondSeg.ID, "segment_committed", `{"text":"Tabura, open the transcript."}`); err != nil {
		t.Fatalf("add second participant committed event: %v", err)
	}

	app.maybeTriggerCompanionResponse(participantSession.ID, secondSeg)

	if !cancelCalled {
		t.Fatal("expected pending turn cancel callback to be invoked")
	}
	messages, err := app.store.ListChatMessages(chatSession.ID, 10)
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	foundReplacement := false
	for _, message := range messages {
		if strings.TrimSpace(message.Role) == "user" && strings.TrimSpace(message.ContentPlain) == "Tabura, open the transcript." {
			foundReplacement = true
			break
		}
	}
	if !foundReplacement {
		t.Fatal("expected replacement participant transcript to be queued as a user message")
	}

	events, err := app.store.ListParticipantEvents(participantSession.ID)
	if err != nil {
		t.Fatalf("list participant events: %v", err)
	}
	foundInterrupted := false
	foundReplacementTrigger := false
	for _, event := range events {
		if event.SegmentID == firstSeg.ID && event.EventType == "assistant_interrupted" {
			foundInterrupted = true
		}
		if event.SegmentID == secondSeg.ID && event.EventType == "assistant_triggered" {
			foundReplacementTrigger = true
		}
	}
	if !foundInterrupted {
		t.Fatal("expected assistant_interrupted event for the pending segment")
	}
	if !foundReplacementTrigger {
		t.Fatal("expected assistant_triggered event for the replacement segment")
	}
}

func TestCompanionResponseTriggerIncludesProjectScopedCompanionContext(t *testing.T) {
	t.Setenv("TABURA_INTENT_LLM_URL", "off")
	app := newAuthedTestApp(t)
	client, promptCh := newCompanionAppServerClientWithCapture(t, "Companion reply.")
	app.appServerClient = client

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	cfg := app.loadCompanionConfig(project)
	cfg.CompanionEnabled = true
	cfg.DirectedSpeechGateEnabled = true
	if err := app.saveCompanionConfig(project.ID, cfg); err != nil {
		t.Fatalf("save companion config: %v", err)
	}

	participantSession, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("add participant session: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, 0, "session_started", "{}"); err != nil {
		t.Fatalf("add participant event: %v", err)
	}
	if err := app.store.UpsertParticipantRoomState(
		participantSession.ID,
		"Budget review remains blocked on owner confirmation.",
		`["Acme Cloud","Budget","Alice"]`,
		`[{"topic":"Budget review"},{"topic":"Owner follow-up"}]`,
	); err != nil {
		t.Fatalf("upsert participant room state: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := app.store.AddParticipantSegment(store.ParticipantSegment{
			SessionID:   participantSession.ID,
			StartTS:     int64(100 + i),
			EndTS:       int64(100 + i),
			CommittedAt: int64(100 + i),
			Speaker:     "Alice",
			Text:        strings.Join([]string{"budget-segment", strings.Repeat("detail", 40)}, "-"),
			Status:      "final",
		}); err != nil {
			t.Fatalf("add participant segment %d: %v", i, err)
		}
	}

	otherProject, err := app.store.CreateProject("Other Project", "other-project", t.TempDir(), "managed", "", "", false)
	if err != nil {
		t.Fatalf("create other project: %v", err)
	}
	otherSession, err := app.store.AddParticipantSession(otherProject.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("add other participant session: %v", err)
	}
	if err := app.store.UpsertParticipantRoomState(otherSession.ID, "Zeus takeover planning.", `["Zeus","Mallory"]`, `[{"topic":"Zeus"}]`); err != nil {
		t.Fatalf("upsert other participant room state: %v", err)
	}
	if _, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   otherSession.ID,
		StartTS:     500,
		EndTS:       500,
		CommittedAt: 500,
		Speaker:     "Mallory",
		Text:        "Zeus takeover planning should stay isolated.",
		Status:      "final",
	}); err != nil {
		t.Fatalf("add other participant segment: %v", err)
	}

	seg, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   participantSession.ID,
		StartTS:     200,
		EndTS:       201,
		Text:        "Tabura, what changed in the budget review?",
		CommittedAt: 202,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("add participant segment: %v", err)
	}
	if err := app.store.AddParticipantEvent(participantSession.ID, seg.ID, "segment_committed", `{"text":"Tabura, what changed in the budget review?"}`); err != nil {
		t.Fatalf("add participant committed event: %v", err)
	}

	app.maybeTriggerCompanionResponse(participantSession.ID, seg)

	chatSession, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("get chat session: %v", err)
	}
	waitForAssistantMessage(t, app, chatSession.ID, "Companion reply.")

	prompt := waitForCapturedPrompt(t, promptCh)
	if !strings.Contains(prompt, "## Companion Context") {
		t.Fatalf("prompt missing companion context: %q", prompt)
	}
	if !strings.Contains(prompt, "Summary: Budget review remains blocked on owner confirmation.") {
		t.Fatalf("prompt missing summary: %q", prompt)
	}
	if !strings.Contains(prompt, "Entities: Acme Cloud, Budget, Alice") {
		t.Fatalf("prompt missing entities: %q", prompt)
	}
	if !strings.Contains(prompt, "Recent topics: Budget review; Owner follow-up") {
		t.Fatalf("prompt missing recent topics: %q", prompt)
	}
	if !strings.Contains(prompt, "Older transcript omitted:") {
		t.Fatalf("prompt missing transcript compaction marker: %q", prompt)
	}
	if strings.Contains(prompt, "Zeus") || strings.Contains(prompt, "Mallory") {
		t.Fatalf("prompt leaked cross-project context: %q", prompt)
	}
}
