package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineItemIntentSomedayCommands(t *testing.T) {
	cases := []struct {
		text        string
		wantAction  string
		wantEnabled bool
		checkBool   bool
	}{
		{text: "review my someday list", wantAction: "review_someday"},
		{text: "zeige irgendwann", wantAction: "review_someday"},
		{text: "what's in someday?", wantAction: "review_someday"},
		{text: "not now", wantAction: "triage_someday"},
		{text: "nicht jetzt", wantAction: "triage_someday"},
		{text: "bring this back", wantAction: "promote_someday"},
		{text: "move this mail back to the inbox", wantAction: "promote_someday"},
		{text: "hol das zurück", wantAction: "promote_someday"},
		{text: "turn off someday reminders", wantAction: "toggle_someday_review_nudge", wantEnabled: false, checkBool: true},
		{text: "schalte irgendwann erinnerungen aus", wantAction: "toggle_someday_review_nudge", wantEnabled: false, checkBool: true},
		{text: "enable someday reminders", wantAction: "toggle_someday_review_nudge", wantEnabled: true, checkBool: true},
		{text: "schalte irgendwann erinnerungen an", wantAction: "toggle_someday_review_nudge", wantEnabled: true, checkBool: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			action := parseInlineItemIntent(tc.text, time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC))
			if action == nil {
				t.Fatal("expected inline item action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if tc.checkBool {
				got, ok := systemActionEnabled(action.Params)
				if !ok {
					t.Fatalf("enabled param missing: %#v", action.Params)
				}
				if got != tc.wantEnabled {
					t.Fatalf("enabled = %v, want %v", got, tc.wantEnabled)
				}
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionReviewSomedayList(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.CreateItem("Review someday flow", store.ItemOptions{
		State:       store.ItemStateSomeday,
		WorkspaceID: &workspace.ID,
	}); err != nil {
		t.Fatalf("CreateItem(someday) error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "review my someday list")
	if !handled {
		t.Fatal("expected someday review command to be handled")
	}
	if message != "Opened someday list with 1 item(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["type"]); got != "show_item_sidebar_view" {
		t.Fatalf("payload type = %q, want show_item_sidebar_view", got)
	}
	if got := strFromAny(payloads[0]["view"]); got != store.ItemStateSomeday {
		t.Fatalf("payload view = %q, want %q", got, store.ItemStateSomeday)
	}
}

func TestClassifyAndExecuteSystemActionTriageSomedayUsesCanvasItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

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

	readmePath := filepath.Join(project.RootPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	title := "README"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &readmePath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Review the someday workflow", store.ItemOptions{
		WorkspaceID: &workspace.ID,
		ArtifactID:  &artifact.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: "README.md",
		artifactKind:  "text_artifact",
		artifactText:  "# notes",
	}
	server := mock.setupServer(t)
	defer server.Close()
	port := serverPort(t, server.Listener.Addr())
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "not now")
	if !handled {
		t.Fatal("expected someday triage command to be handled")
	}
	if message != `Moved "Review the someday workflow" to someday.` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_state_changed" {
		t.Fatalf("payloads = %#v", payloads)
	}
	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updated.State != store.ItemStateSomeday {
		t.Fatalf("state = %q, want %q", updated.State, store.ItemStateSomeday)
	}
}

func TestClassifyAndExecuteSystemActionPromoteSomedayPreservesActor(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

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
	actor, err := app.store.CreateActor("Alice", store.ActorKindHuman)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}

	readmePath := filepath.Join(project.RootPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	title := "README"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &readmePath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Keep Alice assigned", store.ItemOptions{
		State:       store.ItemStateSomeday,
		WorkspaceID: &workspace.ID,
		ArtifactID:  &artifact.ID,
		ActorID:     &actor.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: "README.md",
		artifactKind:  "text_artifact",
		artifactText:  "# notes",
	}
	server := mock.setupServer(t)
	defer server.Close()
	port := serverPort(t, server.Listener.Addr())
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "bring this back")
	if !handled {
		t.Fatal("expected promote command to be handled")
	}
	if message != `Moved "Keep Alice assigned" back to inbox.` {
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
	if updated.ActorID == nil || *updated.ActorID != actor.ID {
		t.Fatalf("actor_id = %v, want %d", updated.ActorID, actor.ID)
	}
}

func TestClassifyAndExecuteSystemActionPromoteDoneEmailRestoresRemoteInbox(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"gmail-promote"}, nil
			case !opts.Since.IsZero():
				return []string{"gmail-promote"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-promote": {
				ID:         "gmail-promote",
				ThreadID:   "thread-promote",
				Subject:    "Promote me",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"chr.albert@gmail.com"},
				Date:       time.Date(2026, time.March, 9, 22, 0, 0, 0, time.UTC),
				Labels:     []string{"INBOX"},
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

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

	readmePath := filepath.Join(project.RootPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	title := "README"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &readmePath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("syncEmailAccount() error: %v", err)
	}
	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-promote")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if err := app.store.UpdateItem(item.ID, store.ItemUpdate{
		WorkspaceID: &workspace.ID,
		ArtifactID:  &artifact.ID,
	}); err != nil {
		t.Fatalf("UpdateItem() error: %v", err)
	}
	if err := app.store.TriageItemDone(item.ID); err != nil {
		t.Fatalf("TriageItemDone() error: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: "README.md",
		artifactKind:  "text_artifact",
		artifactText:  "# notes",
	}
	server := mock.setupServer(t)
	defer server.Close()
	port := serverPort(t, server.Listener.Addr())
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "move this mail back to the inbox")
	if !handled {
		t.Fatal("expected promote command to be handled")
	}
	if message != `Moved "Promote me" back to inbox.` {
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
	if len(provider.moveToInboxCalls) != 1 || len(provider.moveToInboxCalls[0]) != 1 || provider.moveToInboxCalls[0][0] != "gmail-promote" {
		t.Fatalf("move to inbox calls = %#v, want gmail-promote", provider.moveToInboxCalls)
	}
}

func TestClassifyAndExecuteSystemActionToggleSomedayReminder(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "turn off someday reminders")
	if !handled {
		t.Fatal("expected reminder toggle to be handled")
	}
	if message != "Someday review reminders disabled." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["type"]); got != "set_someday_review_nudge" {
		t.Fatalf("payload type = %q, want set_someday_review_nudge", got)
	}
	got, ok := payloads[0]["enabled"].(bool)
	if !ok || got {
		t.Fatalf("enabled payload = %#v, want false", payloads[0]["enabled"])
	}
}
