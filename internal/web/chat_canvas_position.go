package web

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const chatCanvasPositionMaxEventsPerSecond = 5

type chatCanvasPositionEvent struct {
	Cursor     *chatCursorContext
	Gesture    string
	Requested  bool
	OccurredAt time.Time
}

type chatCanvasPositionTracker struct {
	mu     sync.Mutex
	events map[string][]*chatCanvasPositionEvent
	recent map[string][]time.Time
}

func newChatCanvasPositionTracker() *chatCanvasPositionTracker {
	return &chatCanvasPositionTracker{
		events: map[string][]*chatCanvasPositionEvent{},
		recent: map[string][]time.Time{},
	}
}

func normalizeChatCanvasPositionEvent(raw *chatCanvasPositionEvent) *chatCanvasPositionEvent {
	if raw == nil {
		return nil
	}
	cursor := normalizeChatCursorContext(raw.Cursor)
	if cursor == nil {
		return nil
	}
	gesture := strings.ToLower(strings.TrimSpace(raw.Gesture))
	if gesture == "" {
		gesture = "tap"
	}
	occurredAt := raw.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return &chatCanvasPositionEvent{
		Cursor:     cursor,
		Gesture:    gesture,
		Requested:  raw.Requested,
		OccurredAt: occurredAt,
	}
}

func (t *chatCanvasPositionTracker) enqueue(sessionID string, raw *chatCanvasPositionEvent) bool {
	if t == nil {
		return false
	}
	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		return false
	}
	event := normalizeChatCanvasPositionEvent(raw)
	if event == nil {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := event.OccurredAt.Add(-1 * time.Second)
	recent := t.recent[cleanSessionID][:0]
	for _, ts := range t.recent[cleanSessionID] {
		if ts.After(cutoff) {
			recent = append(recent, ts)
		}
	}
	if len(recent) >= chatCanvasPositionMaxEventsPerSecond {
		t.recent[cleanSessionID] = recent
		return false
	}
	recent = append(recent, event.OccurredAt)
	t.recent[cleanSessionID] = recent
	t.events[cleanSessionID] = append(t.events[cleanSessionID], event)
	return true
}

func (t *chatCanvasPositionTracker) consume(sessionID string) []*chatCanvasPositionEvent {
	if t == nil {
		return nil
	}
	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	events := t.events[cleanSessionID]
	if len(events) == 0 {
		return nil
	}
	delete(t.events, cleanSessionID)
	out := make([]*chatCanvasPositionEvent, 0, len(events))
	for _, event := range events {
		if event != nil {
			out = append(out, event)
		}
	}
	return out
}

func appendCanvasPositionPrompt(prompt string, events []*chatCanvasPositionEvent) string {
	contextBlock := formatCanvasPositionPromptContext(events)
	prompt = strings.TrimSpace(prompt)
	if contextBlock == "" {
		return prompt
	}
	if prompt == "" {
		return contextBlock
	}
	return contextBlock + "\n\n" + prompt
}

func formatCanvasPositionPromptContext(events []*chatCanvasPositionEvent) string {
	filtered := make([]*chatCanvasPositionEvent, 0, len(events))
	requested := false
	for _, event := range events {
		normalized := normalizeChatCanvasPositionEvent(event)
		if normalized == nil {
			continue
		}
		filtered = append(filtered, normalized)
		if normalized.Requested {
			requested = true
		}
	}
	if len(filtered) == 0 {
		return ""
	}

	lines := []string{
		"## Canvas Position Events",
	}
	if requested {
		lines = append(lines, "The latest requested position input has arrived. Continue from that request instead of asking for the position again.")
	} else {
		lines = append(lines, "The user shared live canvas position input during dialogue.")
	}
	for i, event := range filtered {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, describeCanvasPositionEvent(event)))
	}
	return strings.Join(lines, "\n")
}

func describeCanvasPositionEvent(event *chatCanvasPositionEvent) string {
	if event == nil || event.Cursor == nil {
		return ""
	}
	cursor := event.Cursor
	prefix := strings.TrimSpace(event.Gesture)
	if prefix == "" {
		prefix = "tap"
	}
	description := cursorPromptTarget(cursor)
	if description == "" {
		description = "active artifact"
	}
	details := []string{prefix + " at " + description}
	if text := strings.TrimSpace(cursor.SelectedText); text != "" {
		details = append(details, "selected text "+quotePromptText(text, 220))
	}
	if text := strings.TrimSpace(cursor.Surrounding); text != "" {
		details = append(details, "surrounding text "+quotePromptText(text, 420))
	}
	return strings.Join(details, "; ")
}

func cursorPromptTarget(cursor *chatCursorContext) string {
	cursor = normalizeChatCursorContext(cursor)
	if cursor == nil {
		return ""
	}
	if cursor.hasPointedItem() {
		target := fmt.Sprintf("item #%d", cursor.ItemID)
		if title := firstNonEmptyCursorText(cursor.ItemTitle, cursor.Title); title != "" {
			target += fmt.Sprintf(" %q", title)
		}
		return target
	}
	if cursor.hasPointedPath() {
		targetKind := "file"
		if cursor.IsDir {
			targetKind = "folder"
		}
		target := fmt.Sprintf("%s %q", targetKind, cursor.Path)
		if cursor.WorkspaceName != "" {
			target += fmt.Sprintf(" in workspace %q", cursor.WorkspaceName)
		}
		return target
	}
	targetParts := make([]string, 0, 4)
	if cursor.Page > 0 {
		targetParts = append(targetParts, fmt.Sprintf("page %d", cursor.Page))
	}
	if cursor.Line > 0 {
		targetParts = append(targetParts, fmt.Sprintf("line %d", cursor.Line))
	}
	if cursor.RelativeX > 0 || cursor.RelativeY > 0 {
		targetParts = append(targetParts, fmt.Sprintf("point %.0f%%, %.0f%%", cursor.RelativeX*100, cursor.RelativeY*100))
	}
	target := strings.Join(targetParts, ", ")
	if title := strings.TrimSpace(cursor.Title); title != "" {
		if target != "" {
			target += " of "
		}
		target += fmt.Sprintf("%q", title)
	}
	return strings.TrimSpace(target)
}
