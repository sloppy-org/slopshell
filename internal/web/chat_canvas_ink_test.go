package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCanvasInkPNGDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3xwAAAAASUVORK5CYII="

func TestAppendCanvasInkPrompt_PrependsStructuredEvents(t *testing.T) {
	prompt := appendCanvasInkPrompt("Conversation transcript:\nUSER:\nwhat does this mean?", []*chatCanvasInkEvent{
		{
			Cursor: &chatCursorContext{
				Title:       "notes.md",
				Line:        4,
				Surrounding: "3: beta\n4: gamma\n5: delta",
			},
			Gesture:          "question_mark",
			ArtifactKind:     "text",
			StrokeCount:      2,
			Requested:        true,
			OverlappingLines: &chatCanvasInkLineRange{Start: 4, End: 4},
			OverlappingText:  "3: beta\n4: gamma\n5: delta",
			SnapshotPath:     ".tabura/artifacts/tmp/live-ink/sample.png",
		},
	})
	if !strings.HasPrefix(prompt, "## Canvas Ink Events") {
		t.Fatalf("prompt should start with canvas ink context, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Continue from the ink instead of asking the user to repeat or point again.") {
		t.Fatalf("prompt missing continuation guidance, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "question mark ink over line 4 of \"notes.md\"") {
		t.Fatalf("prompt missing gesture target, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "snapshot path `.tabura/artifacts/tmp/live-ink/sample.png`") {
		t.Fatalf("prompt missing snapshot path, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Conversation transcript:\nUSER:\nwhat does this mean?") {
		t.Fatalf("prompt missing original body, got:\n%s", prompt)
	}
}

func TestRecognizeChatCanvasInkGesture(t *testing.T) {
	tests := []struct {
		name    string
		strokes []inkSubmitStroke
		want    string
	}{
		{
			name: "circle",
			strokes: []inkSubmitStroke{{
				Points: []inkSubmitPoint{
					{X: 10, Y: 20}, {X: 20, Y: 10}, {X: 30, Y: 20}, {X: 20, Y: 30}, {X: 10, Y: 20},
				},
			}},
			want: "circle",
		},
		{
			name: "underline",
			strokes: []inkSubmitStroke{{
				Points: []inkSubmitPoint{
					{X: 10, Y: 20}, {X: 30, Y: 21}, {X: 50, Y: 20},
				},
			}},
			want: "underline",
		},
		{
			name: "cross",
			strokes: []inkSubmitStroke{
				{Points: []inkSubmitPoint{{X: 10, Y: 10}, {X: 30, Y: 30}}},
				{Points: []inkSubmitPoint{{X: 30, Y: 10}, {X: 10, Y: 30}}},
			},
			want: "cross",
		},
		{
			name: "arrow",
			strokes: []inkSubmitStroke{
				{Points: []inkSubmitPoint{{X: 10, Y: 20}, {X: 40, Y: 20}}},
				{Points: []inkSubmitPoint{{X: 32, Y: 14}, {X: 40, Y: 20}}},
				{Points: []inkSubmitPoint{{X: 32, Y: 26}, {X: 40, Y: 20}}},
			},
			want: "arrow",
		},
		{
			name: "question mark",
			strokes: []inkSubmitStroke{
				{Points: []inkSubmitPoint{{X: 10, Y: 10}, {X: 20, Y: 6}, {X: 28, Y: 12}, {X: 20, Y: 20}, {X: 20, Y: 26}}},
				{Points: []inkSubmitPoint{{X: 20, Y: 34}, {X: 21, Y: 35}}},
			},
			want: "question_mark",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := recognizeChatCanvasInkGesture(tc.strokes); got != tc.want {
				t.Fatalf("recognizeChatCanvasInkGesture() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHandleChatWSTextMessage_CanvasInkQueuesRequestedTurnAndPersistsSnapshot(t *testing.T) {
	app := newAuthedTestApp(t)
	sessionID := testSessionForCanvasPosition(t, app)
	holdAssistantTurnWorker(t, app, sessionID)

	handleChatWSTextMessage(app, newChatWSConn(nil), sessionID, []byte(`{
		"type": "canvas_ink",
		"artifact_kind": "text",
		"output_mode": "voice",
		"request_response": true,
		"snapshot_data_url": "`+testCanvasInkPNGDataURL+`",
		"total_strokes": 2,
		"bounding_box": {
			"relative_x": 0.2,
			"relative_y": 0.3,
			"relative_width": 0.25,
			"relative_height": 0.1
		},
		"overlapping_lines": { "start": 3, "end": 4 },
		"overlapping_text": "2: beta\n3: gamma\n4: delta",
		"cursor": {
			"title": "test.txt",
			"line": 3,
			"surrounding_text": "2: beta\n3: gamma\n4: delta"
		},
		"strokes": [
			{ "points": [ { "x": 10, "y": 20 }, { "x": 30, "y": 21 }, { "x": 50, "y": 20 } ] }
		]
	}`))

	if got := app.turns.queuedCount(sessionID); got != 1 {
		t.Fatalf("queuedCount = %d, want 1", got)
	}
	events := app.chatCanvasInk.consume(sessionID)
	if len(events) != 1 {
		t.Fatalf("consume len = %d, want 1", len(events))
	}
	event := events[0]
	if event == nil {
		t.Fatal("expected event")
	}
	if event.Gesture != "underline" {
		t.Fatalf("gesture = %q, want underline", event.Gesture)
	}
	if event.OverlappingLines == nil || event.OverlappingLines.Start != 3 || event.OverlappingLines.End != 4 {
		t.Fatalf("overlapping lines = %#v", event.OverlappingLines)
	}
	if strings.TrimSpace(event.SnapshotPath) == "" {
		t.Fatal("expected snapshot path")
	}

	session, err := app.store.GetChatSession(sessionID)
	if err != nil {
		t.Fatalf("GetChatSession: %v", err)
	}
	project, err := app.store.GetWorkspaceByStoredPath(session.WorkspacePath)
	if err != nil {
		t.Fatalf("GetProjectByWorkspacePath: %v", err)
	}
	snapshotBytes, err := os.ReadFile(filepath.Join(project.RootPath, filepath.FromSlash(event.SnapshotPath)))
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(snapshotBytes) == 0 {
		t.Fatal("expected snapshot bytes")
	}
}

func TestHandleChatWSTextMessage_InkStrokeAliasQueuesRequestedTurn(t *testing.T) {
	app := newAuthedTestApp(t)
	sessionID := testSessionForCanvasPosition(t, app)
	holdAssistantTurnWorker(t, app, sessionID)

	handleChatWSTextMessage(app, newChatWSConn(nil), sessionID, []byte(`{
		"type": "ink_stroke",
		"artifact_kind": "text",
		"output_mode": "voice",
		"request_response": true,
		"total_strokes": 1,
		"cursor": {
			"title": "notes.md",
			"line": 8,
			"surrounding_text": "7: beta\n8: gamma\n9: delta"
		},
		"strokes": [
			{
				"pointer_type": "pencil",
				"width": 2.4,
				"points": [
					{ "x": 10, "y": 10, "pressure": 0.5, "tilt_x": 12, "tilt_y": 45, "roll": 0, "timestamp_ms": 1 },
					{ "x": 22, "y": 10.5, "pressure": 0.6, "tilt_x": 12, "tilt_y": 45, "roll": 0, "timestamp_ms": 2 },
					{ "x": 36, "y": 10, "pressure": 0.7, "tilt_x": 12, "tilt_y": 45, "roll": 0, "timestamp_ms": 3 }
				]
			}
		]
	}`))

	if got := app.turns.queuedCount(sessionID); got != 1 {
		t.Fatalf("queuedCount = %d, want 1", got)
	}
	events := app.chatCanvasInk.consume(sessionID)
	if len(events) != 1 {
		t.Fatalf("consume len = %d, want 1", len(events))
	}
	event := events[0]
	if event == nil {
		t.Fatal("expected event")
	}
	if event.Gesture != "underline" {
		t.Fatalf("gesture = %q, want underline", event.Gesture)
	}
	if event.Cursor == nil || event.Cursor.Line != 8 {
		t.Fatalf("cursor = %#v", event.Cursor)
	}
	if event.StrokeCount != 1 {
		t.Fatalf("stroke count = %d, want 1", event.StrokeCount)
	}
}
