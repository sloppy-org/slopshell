package web

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineCursorIntent_ItemAndWorkspaceTargets(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		cursor     *chatCursorContext
		wantAction string
		wantTriage string
		wantPath   string
	}{
		{
			name: "item delete",
			text: "delete this",
			cursor: &chatCursorContext{
				View:      "inbox",
				ItemID:    42,
				ItemTitle: "Fix login bug",
				ItemState: store.ItemStateInbox,
			},
			wantAction: "cursor_triage_item",
			wantTriage: "delete",
		},
		{
			name: "item waiting",
			text: "move this to waiting",
			cursor: &chatCursorContext{
				View:      "inbox",
				ItemID:    42,
				ItemTitle: "Fix login bug",
				ItemState: store.ItemStateInbox,
			},
			wantAction: "cursor_triage_item",
			wantTriage: "waiting",
		},
		{
			name: "item back to inbox",
			text: "move this mail back to the inbox",
			cursor: &chatCursorContext{
				View:      "done",
				ItemID:    42,
				ItemTitle: "Fix login bug",
				ItemState: store.ItemStateDone,
			},
			wantAction: "cursor_triage_item",
			wantTriage: "inbox",
		},
		{
			name: "workspace path",
			text: "open this",
			cursor: &chatCursorContext{
				View:          "workspace_browser",
				WorkspaceID:   7,
				WorkspaceName: "tabura",
				Path:          "docs",
				IsDir:         true,
			},
			wantAction: "cursor_open_path",
			wantPath:   "docs",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action := parseInlineCursorIntent(tc.text, tc.cursor)
			if action == nil {
				t.Fatal("expected cursor action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if tc.wantTriage != "" && systemActionCursorTriage(action.Params) != tc.wantTriage {
				t.Fatalf("triage_action = %q, want %q", systemActionCursorTriage(action.Params), tc.wantTriage)
			}
			if tc.wantPath != "" && systemActionStringParam(action.Params, "path") != tc.wantPath {
				t.Fatalf("path = %q, want %q", systemActionStringParam(action.Params, "path"), tc.wantPath)
			}
		})
	}
}

func TestParseInlineTitledItemIntent_MoveBackToInbox(t *testing.T) {
	intent := parseInlineTitledItemIntent(`Move the item at Line 7 of "Hetzner Online GmbH - Rechnung 086000740636 (K0202503909)" back to the inbox.`)
	if intent == nil {
		t.Fatal("expected titled item action")
	}
	if got := intent.Title; got != `Hetzner Online GmbH - Rechnung 086000740636 (K0202503909)` {
		t.Fatalf("title = %q", got)
	}
	if got := intent.TriageAction; got != "inbox" {
		t.Fatalf("triage_action = %q", got)
	}
}

func TestClassifyAndExecuteSystemActionWithCursorDeletesPointedItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	item, err := app.store.CreateItem("Review parser cleanup", store.ItemOptions{
		WorkspaceID: &workspace.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemActionWithCursor(
		context.Background(),
		session.ID,
		session,
		"delete this",
		&chatCursorContext{
			View:          "inbox",
			ItemID:        item.ID,
			ItemTitle:     item.Title,
			ItemState:     store.ItemStateInbox,
			WorkspaceID:   workspace.ID,
			WorkspaceName: workspace.Name,
		},
	)
	if !handled {
		t.Fatal("expected cursor command to be handled")
	}
	if message != `Deleted item "Review parser cleanup".` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_state_changed" {
		t.Fatalf("payloads = %#v", payloads)
	}
	_, err = app.store.GetItem(item.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetItem() error = %v, want sql.ErrNoRows", err)
	}
}

func TestClassifyAndExecuteSystemActionWithCursorMovesPointedItemToWaiting(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	item, err := app.store.CreateItem("Ping release checklist", store.ItemOptions{
		WorkspaceID: &workspace.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemActionWithCursor(
		context.Background(),
		session.ID,
		session,
		"move this to waiting",
		&chatCursorContext{
			View:      "inbox",
			ItemID:    item.ID,
			ItemTitle: item.Title,
			ItemState: store.ItemStateInbox,
		},
	)
	if !handled {
		t.Fatal("expected cursor command to be handled")
	}
	if message != `Moved item "Ping release checklist" to waiting.` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_state_changed" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["view"]); got != "inbox" {
		t.Fatalf("payload view = %q, want inbox", got)
	}
	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updated.State != store.ItemStateWaiting {
		t.Fatalf("state = %q, want %q", updated.State, store.ItemStateWaiting)
	}
}

func TestClassifyAndExecuteSystemActionWithCursorMovesDoneEmailBackToInbox(t *testing.T) {
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
				return []string{"gmail-cursor"}, nil
			case !opts.Since.IsZero():
				return []string{"gmail-cursor"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-cursor": {
				ID:         "gmail-cursor",
				ThreadID:   "thread-cursor",
				Subject:    "Move me back",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"chr.albert@gmail.com"},
				Date:       time.Date(2026, time.March, 9, 21, 45, 0, 0, time.UTC),
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
	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-cursor")
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

	message, payloads, handled := app.classifyAndExecuteSystemActionWithCursor(
		context.Background(),
		session.ID,
		session,
		"move this mail back to the inbox",
		&chatCursorContext{
			View:      "done",
			ItemID:    item.ID,
			ItemTitle: item.Title,
			ItemState: store.ItemStateDone,
		},
	)
	if !handled {
		t.Fatal("expected cursor command to be handled")
	}
	if message != `Moved item "Move me back" back to inbox.` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_state_changed" {
		t.Fatalf("payloads = %#v", payloads)
	}
	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updated.State != store.ItemStateInbox {
		t.Fatalf("state = %q, want %q", updated.State, store.ItemStateInbox)
	}
	if len(provider.moveToInboxCalls) != 1 || len(provider.moveToInboxCalls[0]) != 1 || provider.moveToInboxCalls[0][0] != "gmail-cursor" {
		t.Fatalf("move to inbox calls = %#v, want gmail-cursor", provider.moveToInboxCalls)
	}
}

func TestClassifyAndExecuteSystemActionWithNamedItemMovesDoneEmailBackToInbox(t *testing.T) {
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
				return []string{"gmail-titled"}, nil
			case !opts.Since.IsZero():
				return []string{"gmail-titled"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-titled": {
				ID:         "gmail-titled",
				ThreadID:   "thread-titled",
				Subject:    "Hetzner Online GmbH - Rechnung 086000740636 (K0202503909)",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"chr.albert@gmail.com"},
				Date:       time.Date(2026, time.March, 9, 22, 45, 0, 0, time.UTC),
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
	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-titled")
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

	message, payloads, handled := app.classifyAndExecuteSystemAction(
		context.Background(),
		session.ID,
		session,
		`Move the item at Line 7 of "Hetzner Online GmbH - Rechnung 086000740636 (K0202503909)" back to the inbox.`,
	)
	if !handled {
		t.Fatal("expected named item command to be handled")
	}
	if message != `Moved item "Hetzner Online GmbH - Rechnung 086000740636 (K0202503909)" back to inbox.` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_state_changed" {
		t.Fatalf("payloads = %#v", payloads)
	}
	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updated.State != store.ItemStateInbox {
		t.Fatalf("state = %q, want %q", updated.State, store.ItemStateInbox)
	}
	if len(provider.moveToInboxCalls) != 1 || provider.moveToInboxCalls[0][0] != "gmail-titled" {
		t.Fatalf("moveToInboxCalls = %#v", provider.moveToInboxCalls)
	}
}

func TestClassifyAndExecuteSystemActionWithCursorOpensPointedWorkspacePath(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemActionWithCursor(
		context.Background(),
		session.ID,
		session,
		"open this",
		&chatCursorContext{
			View:          "workspace_browser",
			WorkspaceID:   workspace.ID,
			WorkspaceName: workspace.Name,
			Path:          "docs/guide.md",
			IsDir:         false,
		},
	)
	if !handled {
		t.Fatal("expected cursor command to be handled")
	}
	if message != `Opened file "docs/guide.md".` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "open_workspace_path" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["path"]); got != "docs/guide.md" {
		t.Fatalf("payload path = %q, want docs/guide.md", got)
	}
}
