package web

import (
	"strings"
	"testing"
	"time"
)

func testSessionForCanvasPosition(t *testing.T, app *App) string {
	t.Helper()
	projects, err := app.store.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	for _, project := range projects {
		if isHubProject(project) {
			continue
		}
		session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
		if err != nil {
			t.Fatalf("GetOrCreateChatSession: %v", err)
		}
		return session.ID
	}
	t.Fatal("no non-hub project available")
	return ""
}

func TestAppendCanvasPositionPrompt_PrependsStructuredEvents(t *testing.T) {
	prompt := appendCanvasPositionPrompt("Conversation transcript:\nUSER:\nfix this", []*chatCanvasPositionEvent{
		{
			Cursor: &chatCursorContext{
				Title:       "test.txt",
				Line:        3,
				Surrounding: "2: beta\n3: gamma\n4: delta",
			},
			Gesture:   "tap",
			Requested: true,
		},
	})
	if !strings.HasPrefix(prompt, "## Canvas Position Events") {
		t.Fatalf("prompt should start with canvas position context, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Continue from that request instead of asking for the position again.") {
		t.Fatalf("prompt missing request continuation, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, `1. tap at line 3 of "test.txt"; surrounding text "2: beta 3: gamma 4: delta"`) {
		t.Fatalf("prompt missing position description, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Conversation transcript:\nUSER:\nfix this") {
		t.Fatalf("prompt missing original body, got:\n%s", prompt)
	}
}

func TestChatCanvasPositionTracker_RateLimitsBurst(t *testing.T) {
	tracker := newChatCanvasPositionTracker()
	for i := 0; i < chatCanvasPositionMaxEventsPerSecond; i++ {
		if ok := tracker.enqueue("session-1", &chatCanvasPositionEvent{
			Cursor:     &chatCursorContext{Title: "test.txt", Line: i + 1},
			Gesture:    "tap",
			OccurredAt: time.Unix(100, 0).UTC(),
		}); !ok {
			t.Fatalf("enqueue(%d) rejected inside limit", i)
		}
	}
	if ok := tracker.enqueue("session-1", &chatCanvasPositionEvent{
		Cursor:     &chatCursorContext{Title: "test.txt", Line: 99},
		Gesture:    "tap",
		OccurredAt: time.Unix(100, 0).UTC(),
	}); ok {
		t.Fatal("expected burst over limit to be rejected")
	}
	events := tracker.consume("session-1")
	if len(events) != chatCanvasPositionMaxEventsPerSecond {
		t.Fatalf("consume len = %d, want %d", len(events), chatCanvasPositionMaxEventsPerSecond)
	}
}

func TestHandleChatWSTextMessage_CanvasPositionQueuesRequestedTurn(t *testing.T) {
	app := newAuthedTestApp(t)
	sessionID := testSessionForCanvasPosition(t, app)

	handleChatWSTextMessage(app, newChatWSConn(nil), sessionID, []byte(`{
		"type": "canvas_position",
		"gesture": "tap",
		"output_mode": "voice",
		"request_response": true,
		"cursor": {
			"title": "test.txt",
			"line": 3,
			"surrounding_text": "2: beta\n3: gamma\n4: delta"
		}
	}`))

	if got := app.turns.queuedCount(sessionID); got != 1 {
		t.Fatalf("queuedCount = %d, want 1", got)
	}
	events := app.chatCanvasPositions.consume(sessionID)
	if len(events) != 1 {
		t.Fatalf("consume len = %d, want 1", len(events))
	}
	if events[0] == nil || events[0].Cursor == nil || events[0].Cursor.Line != 3 {
		t.Fatalf("event = %#v", events[0])
	}
	if !events[0].Requested {
		t.Fatal("expected requested position flag")
	}
}

func TestFinalizeAssistantResponse_StripsPositionMarkerBeforePersist(t *testing.T) {
	app := newAuthedTestApp(t)
	projects, err := app.store.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	var projectKey string
	for _, project := range projects {
		if !isHubProject(project) {
			projectKey = project.ProjectKey
			break
		}
	}
	if projectKey == "" {
		t.Fatal("missing non-hub project")
	}
	session, err := app.store.GetOrCreateChatSession(projectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	var persistedID int64
	var persistedText string
	app.finalizeAssistantResponse(
		session.ID,
		projectKey,
		"Tap once.\n\n[[request_position:Tap where the change should land.]]",
		&persistedID,
		&persistedText,
		"turn-1",
		"",
		"thread-1",
		turnOutputModeVoice,
	)

	messages, err := app.store.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	found := false
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		found = true
		if strings.Contains(msg.ContentMarkdown, "request_position") {
			t.Fatalf("stored assistant markdown leaked control marker: %q", msg.ContentMarkdown)
		}
		if strings.TrimSpace(msg.ContentMarkdown) != "Tap once." {
			t.Fatalf("stored assistant markdown = %q, want %q", msg.ContentMarkdown, "Tap once.")
		}
	}
	if !found {
		t.Fatal("expected stored assistant message")
	}
}
