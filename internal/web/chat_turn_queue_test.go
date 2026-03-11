package web

import (
	"context"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

func TestChatTurnTrackerDequeuesQueuedCursorContextInOrder(t *testing.T) {
	tracker := newChatTurnTracker()
	cursor := &chatCursorContext{
		View:      "done",
		ItemID:    42,
		ItemTitle: "Queued mail",
		ItemState: store.ItemStateDone,
	}
	tracker.enqueue("session-1", turnOutputModeVoice, false, 11, chatCaptureModeText, nil)
	tracker.enqueue("session-1", turnOutputModeSilent, true, 22, chatCaptureModeVoice, cursor)

	first, ok := tracker.dequeue("session-1")
	if !ok {
		t.Fatal("expected first turn")
	}
	if first.messageID != 11 || first.captureMode != chatCaptureModeText || first.cursor != nil {
		t.Fatalf("first turn = %#v", first)
	}

	second, ok := tracker.dequeue("session-1")
	if !ok {
		t.Fatal("expected second turn")
	}
	if second.messageID != 22 {
		t.Fatalf("messageID = %d, want 22", second.messageID)
	}
	if second.captureMode != chatCaptureModeVoice {
		t.Fatalf("captureMode = %q, want %q", second.captureMode, chatCaptureModeVoice)
	}
	if second.cursor == nil || second.cursor.ItemID != 42 || second.cursor.ItemState != store.ItemStateDone {
		t.Fatalf("cursor = %#v", second.cursor)
	}
}

func TestRunAssistantTurnUsesQueuedMessageForCursorAction(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"gmail-turn"}, nil
			case !opts.Since.IsZero():
				return []string{"gmail-turn"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-turn": {
				ID:         "gmail-turn",
				ThreadID:   "thread-turn",
				Subject:    "Queued turn mail",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"chr.albert@gmail.com"},
				Date:       time.Date(2026, time.March, 9, 22, 55, 0, 0, time.UTC),
				Labels:     []string{"INBOX"},
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}
	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("syncEmailAccount() error: %v", err)
	}
	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-turn")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if err := app.store.TriageItemDone(item.ID); err != nil {
		t.Fatalf("TriageItemDone() error: %v", err)
	}

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "be quiet", "be quiet", "text"); err != nil {
		t.Fatalf("add leading user message: %v", err)
	}
	targetMessage, err := app.store.AddChatMessage(session.ID, "user", "move this mail back to the inbox", "move this mail back to the inbox", "text")
	if err != nil {
		t.Fatalf("add target user message: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "show me status", "show me status", "text"); err != nil {
		t.Fatalf("add trailing user message: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{
		outputMode:  turnOutputModeVoice,
		messageID:   targetMessage.ID,
		captureMode: chatCaptureModeText,
		cursor: &chatCursorContext{
			View:      "done",
			ItemID:    item.ID,
			ItemTitle: item.Title,
			ItemState: store.ItemStateDone,
		},
	})

	if got := latestAssistantMessage(t, app, session.ID); got != `Moved item "Queued turn mail" back to inbox.` {
		t.Fatalf("assistant message = %q", got)
	}
	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updated.State != store.ItemStateInbox {
		t.Fatalf("state = %q, want %q", updated.State, store.ItemStateInbox)
	}
	if len(provider.moveToInboxCalls) != 1 || len(provider.moveToInboxCalls[0]) != 1 || provider.moveToInboxCalls[0][0] != "gmail-turn" {
		t.Fatalf("moveToInboxCalls = %#v", provider.moveToInboxCalls)
	}
}
