package web

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type fakeEmailSyncProvider struct {
	listFunc         func(email.SearchOptions) ([]string, error)
	listPageFunc     func(email.SearchOptions, string) (email.MessagePage, error)
	incrementalFunc  func(string, string, int) (email.FolderIncrementalSyncResult, error)
	messages         map[string]*providerdata.EmailMessage
	contacts         []providerdata.Contact
	labels           []providerdata.Label
	listCalls        []email.SearchOptions
	incrementalCalls []string
	archiveCalls     [][]string
	moveToInboxCalls [][]string
}

func (f *fakeEmailSyncProvider) ListMessages(_ context.Context, opts email.SearchOptions) ([]string, error) {
	f.listCalls = append(f.listCalls, opts)
	if f.listFunc == nil {
		return nil, nil
	}
	return f.listFunc(opts)
}

func (f *fakeEmailSyncProvider) GetMessages(_ context.Context, messageIDs []string, _ string) ([]*providerdata.EmailMessage, error) {
	out := make([]*providerdata.EmailMessage, 0, len(messageIDs))
	for _, id := range messageIDs {
		if message, ok := f.messages[id]; ok {
			out = append(out, message)
		}
	}
	return out, nil
}

func (f *fakeEmailSyncProvider) ListMessagesPage(_ context.Context, opts email.SearchOptions, pageToken string) (email.MessagePage, error) {
	f.listCalls = append(f.listCalls, opts)
	if f.listPageFunc == nil {
		return email.MessagePage{}, nil
	}
	return f.listPageFunc(opts, pageToken)
}

func (f *fakeEmailSyncProvider) SyncFolderChanges(_ context.Context, folder, cursor string, maxChanges int) (email.FolderIncrementalSyncResult, error) {
	f.incrementalCalls = append(f.incrementalCalls, strings.TrimSpace(folder)+"|"+strings.TrimSpace(cursor))
	if f.incrementalFunc == nil {
		return email.FolderIncrementalSyncResult{}, nil
	}
	return f.incrementalFunc(folder, cursor, maxChanges)
}

func (f *fakeEmailSyncProvider) ListLabels(_ context.Context) ([]providerdata.Label, error) {
	return append([]providerdata.Label(nil), f.labels...), nil
}

func (f *fakeEmailSyncProvider) Close() error {
	return nil
}

func (f *fakeEmailSyncProvider) Archive(_ context.Context, messageIDs []string) (int, error) {
	f.archiveCalls = append(f.archiveCalls, append([]string(nil), messageIDs...))
	return len(messageIDs), nil
}

func (f *fakeEmailSyncProvider) MoveToInbox(_ context.Context, messageIDs []string) (int, error) {
	f.moveToInboxCalls = append(f.moveToInboxCalls, append([]string(nil), messageIDs...))
	return len(messageIDs), nil
}

func (f *fakeEmailSyncProvider) ListContacts(_ context.Context) ([]providerdata.Contact, error) {
	return append([]providerdata.Contact(nil), f.contacts...), nil
}

func stringPointer(value string) *string {
	return &value
}

type exchangeEmailSyncFixture struct {
	app         *App
	workspaceID int64
}

func TestSourceSyncRunnerPollsGmailAndIMAPAccounts(t *testing.T) {
	app := newAuthedTestApp(t)

	gmailAccount, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount(gmail) error: %v", err)
	}
	imapAccount, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderIMAP, "Private IMAP", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount(imap) error: %v", err)
	}

	gmailProvider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"gmail-1"}, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				return []string{"gmail-1"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-1": {
				ID:         "gmail-1",
				ThreadID:   "thread-gmail-1",
				Subject:    "Review release notes",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"team@example.com"},
				Date:       time.Date(2026, time.March, 9, 10, 0, 0, 0, time.UTC),
				Labels:     []string{"INBOX"},
			},
		},
		contacts: []providerdata.Contact{{
			ProviderRef:  "people/c1",
			Name:         "Ada Lovelace",
			Email:        "ada@example.com",
			Organization: "Analytical Engines",
			Phones:       []string{"+1 555 0100"},
		}},
	}
	imapProvider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"INBOX:7"}, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				return []string{"INBOX:7"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"INBOX:7": {
				ID:         "INBOX:7",
				ThreadID:   "thread-imap-7",
				Subject:    "Schedule site visit",
				Sender:     "Bob <bob@example.com>",
				Recipients: []string{"ops@example.com"},
				Date:       time.Date(2026, time.March, 9, 11, 0, 0, 0, time.UTC),
				Labels:     []string{"INBOX"},
			},
		},
	}
	app.newEmailSyncProvider = func(_ context.Context, account store.ExternalAccount) (emailSyncProvider, error) {
		switch account.ID {
		case gmailAccount.ID:
			return gmailProvider, nil
		case imapAccount.ID:
			return imapProvider, nil
		default:
			t.Fatalf("unexpected account id: %d", account.ID)
			return nil, nil
		}
	}
	app.newContactSyncProvider = func(_ context.Context, account store.ExternalAccount) (contactSyncProvider, error) {
		switch account.ID {
		case gmailAccount.ID:
			return gmailProvider, nil
		default:
			t.Fatalf("unexpected contact sync account id: %d", account.ID)
			return nil, nil
		}
	}
	app.sourceSync = app.newSourceSyncRunner()

	result, err := app.syncSourcesNow(context.Background())
	if err != nil {
		t.Fatalf("syncSourcesNow() error: %v", err)
	}
	if len(result.Accounts) != 2 {
		t.Fatalf("len(result.Accounts) = %d, want 2", len(result.Accounts))
	}
	for _, account := range result.Accounts {
		if account.Skipped {
			t.Fatalf("account %#v was skipped, want sync", account)
		}
		if account.Err != nil {
			t.Fatalf("account %#v returned error", account)
		}
	}

	gmailItem, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-1")
	if err != nil {
		t.Fatalf("GetItemBySource(gmail) error: %v", err)
	}
	if gmailItem.State != store.ItemStateInbox {
		t.Fatalf("gmail item state = %q, want inbox", gmailItem.State)
	}
	if gmailItem.Sphere != store.SphereWork {
		t.Fatalf("gmail item sphere = %q, want work", gmailItem.Sphere)
	}

	imapItem, err := app.store.GetItemBySource(store.ExternalProviderIMAP, "message:INBOX:7")
	if err != nil {
		t.Fatalf("GetItemBySource(imap) error: %v", err)
	}
	if imapItem.Sphere != store.SpherePrivate {
		t.Fatalf("imap item sphere = %q, want private", imapItem.Sphere)
	}

	artifacts, err := app.store.ListArtifactsByKind(store.ArtifactKindEmail)
	if err != nil {
		t.Fatalf("ListArtifactsByKind(email) error: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("len(email artifacts) = %d, want 2", len(artifacts))
	}

	itemArtifacts, err := app.store.ListItemArtifacts(gmailItem.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts(gmail) error: %v", err)
	}
	if len(itemArtifacts) != 2 {
		t.Fatalf("len(gmail item artifacts) = %d, want 2", len(itemArtifacts))
	}
	if itemArtifacts[0].Artifact.Kind != store.ArtifactKindEmail {
		t.Fatalf("gmail primary artifact kind = %q, want email", itemArtifacts[0].Artifact.Kind)
	}
	if itemArtifacts[1].Artifact.Kind != store.ArtifactKindEmailThread || itemArtifacts[1].Role != "related" {
		t.Fatalf("gmail related thread artifact = %+v, want related email_thread", itemArtifacts[1])
	}

	var gmailMeta map[string]any
	if err := json.Unmarshal([]byte(strFromPointer(itemArtifacts[0].Artifact.MetaJSON)), &gmailMeta); err != nil {
		t.Fatalf("Unmarshal(gmail meta) error: %v", err)
	}
	if got := strFromAny(gmailMeta["thread_id"]); got != "thread-gmail-1" {
		t.Fatalf("gmail thread_id = %q, want thread-gmail-1", got)
	}
	if got := strFromAny(gmailMeta["sender"]); got != "Ada <ada@example.com>" {
		t.Fatalf("gmail sender = %q, want Ada <ada@example.com>", got)
	}

	gmailBinding, err := app.store.GetBindingByRemote(gmailAccount.ID, store.ExternalProviderGmail, "email", "gmail-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(gmail) error: %v", err)
	}
	if gmailBinding.ItemID == nil || gmailBinding.ArtifactID == nil {
		t.Fatalf("gmail binding = %#v, want item and artifact ids", gmailBinding)
	}
	gmailItem, err = app.store.GetItem(*gmailBinding.ItemID)
	if err != nil {
		t.Fatalf("GetItem(gmail binding) error: %v", err)
	}
	if gmailItem.ActorID == nil {
		t.Fatal("gmail item actor_id = nil, want sender actor")
	}
	gmailActor, err := app.store.GetActor(*gmailItem.ActorID)
	if err != nil {
		t.Fatalf("GetActor(gmail sender) error: %v", err)
	}
	if gmailActor.Email == nil || *gmailActor.Email != "ada@example.com" {
		t.Fatalf("gmail actor email = %v, want ada@example.com", gmailActor.Email)
	}
	if gmailActor.ProviderRef == nil || *gmailActor.ProviderRef != "people/c1" {
		t.Fatalf("gmail actor provider_ref = %v, want people/c1", gmailActor.ProviderRef)
	}
	imapActor, err := app.store.GetActorByEmail("bob@example.com")
	if err != nil {
		t.Fatalf("GetActorByEmail(imap sender) error: %v", err)
	}
	if imapActor.Provider == nil || *imapActor.Provider != store.ExternalProviderIMAP {
		t.Fatalf("imap actor provider = %v, want imap", imapActor.Provider)
	}
	if got := int64FromAny(gmailMeta["sender_actor_id"]); got != gmailActor.ID {
		t.Fatalf("gmail sender_actor_id = %d, want %d", got, gmailActor.ID)
	}
}

func TestSyncEmailAccountWithProviderUsesExchangeEWSIncrementalInboxSync(t *testing.T) {
	app := newAuthedTestApp(t)
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz Exchange", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(exchange_ews) error: %v", err)
	}

	title := "Old inbox mail"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindEmail, nil, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Old inbox mail", store.ItemOptions{State: store.ItemStateInbox})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	containerRef := "Posteingang"
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:    account.ID,
		Provider:     account.Provider,
		ObjectType:   emailBindingObjectType,
		RemoteID:     "gone-1",
		ArtifactID:   &artifact.ID,
		ItemID:       &item.ID,
		ContainerRef: &containerRef,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding(gone-1) error: %v", err)
	}

	provider := &fakeEmailSyncProvider{
		listFunc: func(email.SearchOptions) ([]string, error) {
			t.Fatal("legacy recent-mail list path should not be used for exchange_ews")
			return nil, nil
		},
		listPageFunc: func(opts email.SearchOptions, pageToken string) (email.MessagePage, error) {
			if strings.EqualFold(strings.TrimSpace(opts.Folder), "INBOX") || strings.EqualFold(strings.TrimSpace(opts.Folder), "Posteingang") {
				return email.MessagePage{IDs: []string{"ews-1", "gone-1"}}, nil
			}
			return email.MessagePage{}, nil
		},
		incrementalFunc: func(folder, cursor string, maxChanges int) (email.FolderIncrementalSyncResult, error) {
			if !strings.EqualFold(strings.TrimSpace(folder), "INBOX") {
				t.Fatalf("incremental folder = %q, want INBOX", folder)
			}
			if cursor != "" {
				t.Fatalf("incremental cursor = %q, want empty", cursor)
			}
			if maxChanges <= 0 {
				t.Fatalf("maxChanges = %d, want positive", maxChanges)
			}
			return email.FolderIncrementalSyncResult{
				Cursor:     "sync-state-1",
				IDs:        []string{"ews-1"},
				DeletedIDs: []string{"gone-1"},
				More:       true,
			}, nil
		},
		messages: map[string]*providerdata.EmailMessage{
			"ews-1": {
				ID:                "ews-1",
				ThreadID:          "thread-1",
				InternetMessageID: "<ews-1@example.test>",
				Subject:           "Fresh inbox mail",
				Sender:            "Ada <ada@example.com>",
				Recipients:        []string{"ert@example.com"},
				Date:              time.Date(2026, time.March, 17, 15, 0, 0, 0, time.UTC),
				Labels:            []string{"Posteingang", "INBOX"},
			},
		},
	}

	result, err := app.syncEmailAccountWithProvider(context.Background(), account, provider)
	if err != nil {
		t.Fatalf("syncEmailAccountWithProvider() error: %v", err)
	}
	if result.MessageCount != 1 {
		t.Fatalf("MessageCount = %d, want 1", result.MessageCount)
	}
	if len(provider.incrementalCalls) != 1 {
		t.Fatalf("incrementalCalls = %v, want 1 call", provider.incrementalCalls)
	}

	updatedAccount, err := app.store.GetExternalAccount(account.ID)
	if err != nil {
		t.Fatalf("GetExternalAccount() error: %v", err)
	}
	state, err := decodeEmailInboxSyncState(updatedAccount)
	if err != nil {
		t.Fatalf("decodeEmailInboxSyncState() error: %v", err)
	}
	if state.Cursor != "sync-state-1" || !state.HasMore || !state.Enabled {
		t.Fatalf("inbox sync state = %+v, want enabled cursor sync-state-1 has_more", state)
	}

	updatedItem, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(gone) error: %v", err)
	}
	if updatedItem.State != store.ItemStateDone {
		t.Fatalf("gone item state = %q, want done", updatedItem.State)
	}

	freshItem, err := app.store.GetItemBySource(store.ExternalProviderExchangeEWS, "message:ews-1")
	if err != nil {
		t.Fatalf("GetItemBySource(ews-1) error: %v", err)
	}
	if freshItem.State != store.ItemStateInbox {
		t.Fatalf("fresh item state = %q, want inbox", freshItem.State)
	}
}

func TestSyncEmailAccountBackfillsHistoryAcrossRuns(t *testing.T) {
	app := newAuthedTestApp(t)
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{
		"history_page_size":     2,
		"history_pages_per_run": 1,
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount(exchange_ews) error: %v", err)
	}

	provider := &fakeEmailSyncProvider{
		labels: []providerdata.Label{{
			ID:            "folder-inbox",
			Name:          "Posteingang",
			MessagesTotal: 4,
		}},
		listPageFunc: func(opts email.SearchOptions, pageToken string) (email.MessagePage, error) {
			switch {
			case opts.Folder == "INBOX" && pageToken == "":
				return email.MessagePage{IDs: []string{"msg-1", "msg-2"}, NextPageToken: "2"}, nil
			case opts.Folder == "INBOX" && pageToken == "2":
				return email.MessagePage{IDs: []string{"msg-3", "msg-4"}}, nil
			case opts.Folder == "folder-inbox" && pageToken == "":
				return email.MessagePage{IDs: []string{"msg-1", "msg-2"}, NextPageToken: "2"}, nil
			case opts.Folder == "folder-inbox" && pageToken == "2":
				return email.MessagePage{IDs: []string{"msg-3", "msg-4"}}, nil
			default:
				return email.MessagePage{}, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"msg-1": {ID: "msg-1", ThreadID: "thread-1", Subject: "One", Sender: "Ada <ada@example.com>", Date: time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC), Labels: []string{"INBOX"}},
			"msg-2": {ID: "msg-2", ThreadID: "thread-2", Subject: "Two", Sender: "Ada <ada@example.com>", Date: time.Date(2026, time.March, 16, 8, 0, 0, 0, time.UTC), Labels: []string{"INBOX"}},
			"msg-3": {ID: "msg-3", ThreadID: "thread-3", Subject: "Three", Sender: "Ada <ada@example.com>", Date: time.Date(2026, time.March, 15, 9, 0, 0, 0, time.UTC), Labels: []string{"INBOX"}},
			"msg-4": {ID: "msg-4", ThreadID: "thread-4", Subject: "Four", Sender: "Ada <ada@example.com>", Date: time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC), Labels: []string{"INBOX"}},
		},
	}

	first, err := app.syncEmailAccountWithProvider(context.Background(), account, provider)
	if err != nil {
		t.Fatalf("syncEmailAccountWithProvider(first) error: %v", err)
	}
	if first.MessageCount != 2 || first.ItemCount != 2 {
		t.Fatalf("first sync result = %+v, want 2 messages and 2 items", first)
	}

	account, err = app.store.GetExternalAccount(account.ID)
	if err != nil {
		t.Fatalf("GetExternalAccount(after first) error: %v", err)
	}
	state, err := decodeEmailHistorySyncState(account)
	if err != nil {
		t.Fatalf("decodeEmailHistorySyncState(first) error: %v", err)
	}
	if state.Complete {
		t.Fatal("history state marked complete after first page, want pending")
	}
	if state.CurrentContainer != "folder-inbox" || state.Cursor != "2" {
		t.Fatalf("history state after first sync = %+v, want folder-inbox cursor 2", state)
	}

	second, err := app.syncEmailAccountWithProvider(context.Background(), account, provider)
	if err != nil {
		t.Fatalf("syncEmailAccountWithProvider(second) error: %v", err)
	}
	if second.MessageCount != 2 || second.ItemCount != 2 {
		t.Fatalf("second sync result = %+v, want 2 messages and 2 items", second)
	}

	account, err = app.store.GetExternalAccount(account.ID)
	if err != nil {
		t.Fatalf("GetExternalAccount(after second) error: %v", err)
	}
	state, err = decodeEmailHistorySyncState(account)
	if err != nil {
		t.Fatalf("decodeEmailHistorySyncState(second) error: %v", err)
	}
	if !state.Complete {
		t.Fatalf("history state after second sync = %+v, want complete", state)
	}
	if _, err := app.store.GetItemBySource(store.ExternalProviderExchangeEWS, "message:msg-4"); err != nil {
		t.Fatalf("GetItemBySource(msg-4) error: %v", err)
	}
}

func TestSourceSyncRunnerPollsExchangeEmailAccounts(t *testing.T) {
	fixture := newExchangeEmailSyncFixture(t)
	result, err := fixture.app.syncSourcesNow(context.Background())
	if err != nil {
		t.Fatalf("syncSourcesNow() error: %v", err)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("len(result.Accounts) = %d, want 1", len(result.Accounts))
	}
	if result.Accounts[0].Skipped || result.Accounts[0].Err != nil {
		t.Fatalf("result.Accounts[0] = %#v, want successful sync", result.Accounts[0])
	}
	assertExchangeEmailSyncArtifacts(t, fixture)
}

func newExchangeEmailSyncFixture(t *testing.T) exchangeEmailSyncFixture {
	t.Helper()
	app := newAuthedTestApp(t)
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchange, "Work Exchange", map[string]any{
		"client_id": "client-id",
		"tenant_id": "tenant-id",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount(exchange) error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Contracts", filepath.Join(t.TempDir(), "workspace", "contracts"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderExchange, "label", "Contracts", &workspace.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping() error: %v", err)
	}
	provider := exchangeEmailSyncProvider()
	app.newEmailSyncProvider = func(_ context.Context, externalAccount store.ExternalAccount) (emailSyncProvider, error) {
		if externalAccount.ID != account.ID {
			t.Fatalf("unexpected exchange email account id: %d", externalAccount.ID)
		}
		return provider, nil
	}
	app.newContactSyncProvider = func(_ context.Context, externalAccount store.ExternalAccount) (contactSyncProvider, error) {
		if externalAccount.ID != account.ID {
			t.Fatalf("unexpected exchange contact account id: %d", externalAccount.ID)
		}
		return provider, nil
	}
	app.sourceSync = app.newSourceSyncRunner()
	return exchangeEmailSyncFixture{
		app:         app,
		workspaceID: workspace.ID,
	}
}

func exchangeEmailSyncProvider() *fakeEmailSyncProvider {
	return &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"exchange-1"}, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				return []string{"exchange-1"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"exchange-1": {
				ID:         "exchange-1",
				ThreadID:   "thread-exchange-1",
				Subject:    "Budget follow-up",
				Sender:     "Carol <carol@example.com>",
				Recipients: []string{"finance@example.com"},
				Date:       time.Date(2026, time.March, 9, 13, 0, 0, 0, time.UTC),
				Labels:     []string{"Contracts"},
				BodyText:   stringPointer("Please schedule meeting about budget by March 12."),
			},
		},
		contacts: []providerdata.Contact{{
			ProviderRef:  "exchange-contact-1",
			Name:         "Carol",
			Email:        "carol@example.com",
			Organization: "Example Corp",
		}},
	}
}

func assertExchangeEmailSyncArtifacts(t *testing.T, fixture exchangeEmailSyncFixture) {
	t.Helper()
	item, err := fixture.app.store.GetItemBySource(store.ExternalProviderExchange, "message:exchange-1")
	if err != nil {
		t.Fatalf("GetItemBySource(exchange) error: %v", err)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != fixture.workspaceID {
		t.Fatalf("exchange item workspace_id = %v, want %d", item.WorkspaceID, fixture.workspaceID)
	}
	itemArtifacts, err := fixture.app.store.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts(exchange) error: %v", err)
	}
	if len(itemArtifacts) != 2 {
		t.Fatalf("len(exchange item artifacts) = %d, want 2", len(itemArtifacts))
	}
	if itemArtifacts[0].Artifact.Kind != store.ArtifactKindEmail {
		t.Fatalf("exchange primary artifact kind = %q, want email", itemArtifacts[0].Artifact.Kind)
	}
	if itemArtifacts[1].Artifact.Kind != store.ArtifactKindEmailThread || itemArtifacts[1].Role != "related" {
		t.Fatalf("exchange related artifact = %+v, want related email_thread", itemArtifacts[1])
	}
	actionItem, err := fixture.app.store.GetItemBySource(store.ExternalProviderExchange, emailActionSourceRef("thread-exchange-1", "Schedule meeting about budget"))
	if err != nil {
		t.Fatalf("GetItemBySource(exchange action) error: %v", err)
	}
	if actionItem.FollowUpAt == nil || *actionItem.FollowUpAt != "2026-03-12T09:00:00Z" {
		t.Fatalf("exchange action follow_up_at = %v, want 2026-03-12T09:00:00Z", actionItem.FollowUpAt)
	}
	actor, err := fixture.app.store.GetActorByProviderRef(store.ExternalProviderExchange, "exchange-contact-1")
	if err != nil {
		t.Fatalf("GetActorByProviderRef(exchange) error: %v", err)
	}
	if actor.Email == nil || *actor.Email != "carol@example.com" {
		t.Fatalf("exchange actor email = %v, want carol@example.com", actor.Email)
	}
}

func TestSyncEmailAccountDoesNotCreateInboxItemsFromRulesOutsideInbox(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Legal Gmail", map[string]any{
		"follow_up_rules": []any{
			map[string]any{"subject": "contract"},
		},
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Subject == "contract":
				return []string{"gmail-contract"}, nil
			case opts.Folder == "INBOX":
				return nil, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				return []string{"gmail-contract"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-contract": {
				ID:         "gmail-contract",
				ThreadID:   "thread-contract",
				Subject:    "contract review needed",
				Sender:     "Counsel <counsel@example.com>",
				Recipients: []string{"legal@example.com"},
				Date:       time.Date(2026, time.March, 8, 16, 0, 0, 0, time.UTC),
				Labels:     []string{"Archive"},
				IsRead:     true,
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("syncEmailAccount() error: %v", err)
	}

	if _, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-contract"); err == nil {
		t.Fatal("archived rule-matched message created inbox item, want no item")
	}

	artifacts, err := app.store.ListArtifactsByKind(store.ArtifactKindEmail)
	if err != nil {
		t.Fatalf("ListArtifactsByKind(email) error: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(email artifacts) = %d, want 1", len(artifacts))
	}
	if got := strFromPointer(artifacts[0].Title); got != "contract review needed" {
		t.Fatalf("email artifact title = %q, want contract review needed", got)
	}
}

func TestSyncEmailAccountUsesMappedNonPrimaryLabelForAssignment(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Legal Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Contracts", filepath.Join(t.TempDir(), "workspace", "contracts"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderGmail, "label", "Contracts", &workspace.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping() error: %v", err)
	}

	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"gmail-contracts"}, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				return []string{"gmail-contracts"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-contracts": {
				ID:         "gmail-contracts",
				ThreadID:   "thread-contracts",
				Subject:    "Review contract terms",
				Sender:     "Counsel <counsel@example.com>",
				Recipients: []string{"legal@example.com"},
				Date:       time.Date(2026, time.March, 9, 12, 0, 0, 0, time.UTC),
				Labels:     []string{"INBOX", "Contracts"},
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("syncEmailAccount() error: %v", err)
	}

	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-contracts")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("item workspace_id = %v, want %d", item.WorkspaceID, workspace.ID)
	}
	binding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderGmail, emailBindingObjectType, "gmail-contracts")
	if err != nil {
		t.Fatalf("GetBindingByRemote(email) error: %v", err)
	}
	if binding.ContainerRef == nil || *binding.ContainerRef != "Contracts" {
		t.Fatalf("binding container_ref = %v, want Contracts", binding.ContainerRef)
	}

	artifacts, err := app.store.ListArtifactsForWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("ListArtifactsForWorkspace() error: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("len(workspace artifacts) = %d, want 2", len(artifacts))
	}

	threadBinding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderGmail, emailThreadBindingObjectType, "thread-contracts")
	if err != nil {
		t.Fatalf("GetBindingByRemote(thread) error: %v", err)
	}
	if threadBinding.ContainerRef == nil || *threadBinding.ContainerRef != "Contracts" {
		t.Fatalf("thread binding container_ref = %v, want Contracts", threadBinding.ContainerRef)
	}
}

func TestSyncEmailAccountRemoteInboxReopensDoneItems(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Done Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"gmail-done"}, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				return []string{"gmail-done"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-done": {
				ID:         "gmail-done",
				ThreadID:   "thread-done",
				Subject:    "Already handled",
				Sender:     "Ops <ops@example.com>",
				Recipients: []string{"team@example.com"},
				Date:       time.Date(2026, time.March, 9, 9, 0, 0, 0, time.UTC),
				Labels:     []string{"INBOX"},
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("first syncEmailAccount() error: %v", err)
	}
	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-done")
	if err != nil {
		t.Fatalf("GetItemBySource(first) error: %v", err)
	}
	if err := app.store.UpdateItemState(item.ID, store.ItemStateDone); err != nil {
		t.Fatalf("UpdateItemState(done) error: %v", err)
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("second syncEmailAccount() error: %v", err)
	}
	item, err = app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(second) error: %v", err)
	}
	if item.State != store.ItemStateInbox {
		t.Fatalf("item state after resync = %q, want inbox", item.State)
	}
}

func TestSyncEmailAccountRemoteArchiveClosesInboxItems(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Archive Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	inInbox := true
	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				if inInbox {
					return []string{"gmail-remote-archive"}, nil
				}
				return nil, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				if inInbox {
					return []string{"gmail-remote-archive"}, nil
				}
				return nil, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-remote-archive": {
				ID:         "gmail-remote-archive",
				ThreadID:   "thread-remote-archive",
				Subject:    "Archive me elsewhere",
				Sender:     "Ops <ops@example.com>",
				Recipients: []string{"team@example.com"},
				Date:       time.Date(2026, time.March, 9, 9, 30, 0, 0, time.UTC),
				Labels:     []string{"INBOX"},
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("first syncEmailAccount() error: %v", err)
	}
	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-remote-archive")
	if err != nil {
		t.Fatalf("GetItemBySource(first) error: %v", err)
	}
	if item.State != store.ItemStateInbox {
		t.Fatalf("item state after first sync = %q, want inbox", item.State)
	}

	inInbox = false
	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("second syncEmailAccount() error: %v", err)
	}
	item, err = app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(second) error: %v", err)
	}
	if item.State != store.ItemStateDone {
		t.Fatalf("item state after remote archive = %q, want done", item.State)
	}
}

func TestSyncExchangeEWSAccountRemoteMoveToInboxUpdatesContainerAndCreatesItem(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	inInbox := false
	includeCC := true
	provider := &fakeEmailSyncProvider{
		labels: []providerdata.Label{
			{ID: "INBOX", Name: "Posteingang"},
			{ID: "CC", Name: "CC"},
		},
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			if opts.Folder == "INBOX" && inInbox {
				return []string{"ews-move-in"}, nil
			}
			return nil, nil
		},
		listPageFunc: func(opts email.SearchOptions, pageToken string) (email.MessagePage, error) {
			if pageToken != "" || opts.Folder != "CC" || !includeCC {
				return email.MessagePage{}, nil
			}
			return email.MessagePage{IDs: []string{"ews-move-in"}}, nil
		},
		incrementalFunc: func(folder, _ string, _ int) (email.FolderIncrementalSyncResult, error) {
			if folder == "INBOX" && inInbox {
				return email.FolderIncrementalSyncResult{IDs: []string{"ews-move-in"}}, nil
			}
			return email.FolderIncrementalSyncResult{}, nil
		},
		messages: map[string]*providerdata.EmailMessage{
			"ews-move-in": {
				ID:         "ews-move-in",
				ThreadID:   "thread-ews-move-in",
				Subject:    "Move me to inbox",
				Sender:     "Ops <ops@example.com>",
				Recipients: []string{"team@example.com"},
				Date:       time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC),
				Labels:     []string{"CC"},
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("first syncEmailAccount() error: %v", err)
	}
	if _, err := app.store.GetItemBySource(store.ExternalProviderExchangeEWS, "message:ews-move-in"); err == nil {
		t.Fatal("CC message created inbox item before move, want no item")
	}
	binding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, emailBindingObjectType, "ews-move-in")
	if err != nil {
		t.Fatalf("GetBindingByRemote(first email) error: %v", err)
	}
	if binding.ContainerRef == nil || *binding.ContainerRef != "CC" {
		t.Fatalf("first binding container_ref = %v, want CC", binding.ContainerRef)
	}

	inInbox = true
	includeCC = false
	provider.messages["ews-move-in"].Labels = []string{"Posteingang", "INBOX"}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("second syncEmailAccount() error: %v", err)
	}
	item, err := app.store.GetItemBySource(store.ExternalProviderExchangeEWS, "message:ews-move-in")
	if err != nil {
		t.Fatalf("GetItemBySource(second) error: %v", err)
	}
	if item.State != store.ItemStateInbox {
		t.Fatalf("item state after move to inbox = %q, want inbox", item.State)
	}
	binding, err = app.store.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, emailBindingObjectType, "ews-move-in")
	if err != nil {
		t.Fatalf("GetBindingByRemote(second email) error: %v", err)
	}
	if binding.ContainerRef == nil || *binding.ContainerRef != "Posteingang" {
		t.Fatalf("second binding container_ref = %v, want Posteingang", binding.ContainerRef)
	}
	threadBinding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, emailThreadBindingObjectType, "thread-ews-move-in")
	if err != nil {
		t.Fatalf("GetBindingByRemote(thread) error: %v", err)
	}
	if threadBinding.ContainerRef == nil || *threadBinding.ContainerRef != "Posteingang" {
		t.Fatalf("thread binding container_ref = %v, want Posteingang", threadBinding.ContainerRef)
	}
}

func TestSyncExchangeEWSAccountRemoteMoveOutOfInboxUpdatesContainerAndClosesItem(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	inInbox := true
	provider := &fakeEmailSyncProvider{
		labels: []providerdata.Label{
			{ID: "INBOX", Name: "Posteingang"},
			{ID: "CC", Name: "CC"},
		},
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			if opts.Folder == "INBOX" && inInbox {
				return []string{"ews-move-out"}, nil
			}
			return nil, nil
		},
		incrementalFunc: func(folder, _ string, _ int) (email.FolderIncrementalSyncResult, error) {
			if folder != "INBOX" {
				return email.FolderIncrementalSyncResult{}, nil
			}
			if inInbox {
				return email.FolderIncrementalSyncResult{IDs: []string{"ews-move-out"}}, nil
			}
			return email.FolderIncrementalSyncResult{DeletedIDs: []string{"ews-move-out"}}, nil
		},
		messages: map[string]*providerdata.EmailMessage{
			"ews-move-out": {
				ID:         "ews-move-out",
				ThreadID:   "thread-ews-move-out",
				Subject:    "Move me out",
				Sender:     "Ops <ops@example.com>",
				Recipients: []string{"team@example.com"},
				Date:       time.Date(2026, time.March, 16, 9, 30, 0, 0, time.UTC),
				Labels:     []string{"Posteingang", "INBOX"},
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("first syncEmailAccount() error: %v", err)
	}
	item, err := app.store.GetItemBySource(store.ExternalProviderExchangeEWS, "message:ews-move-out")
	if err != nil {
		t.Fatalf("GetItemBySource(first) error: %v", err)
	}
	if item.State != store.ItemStateInbox {
		t.Fatalf("item state after first sync = %q, want inbox", item.State)
	}

	inInbox = false
	provider.messages["ews-move-out"].Labels = []string{"CC"}
	app.emailRefreshes.add(account.ID, "ews-move-out")

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("second syncEmailAccount() error: %v", err)
	}
	item, err = app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(second) error: %v", err)
	}
	if item.State != store.ItemStateDone {
		t.Fatalf("item state after move out of inbox = %q, want done", item.State)
	}
	binding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, emailBindingObjectType, "ews-move-out")
	if err != nil {
		t.Fatalf("GetBindingByRemote(second email) error: %v", err)
	}
	if binding.ContainerRef == nil || *binding.ContainerRef != "CC" {
		t.Fatalf("second binding container_ref = %v, want CC", binding.ContainerRef)
	}
	threadBinding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, emailThreadBindingObjectType, "thread-ews-move-out")
	if err != nil {
		t.Fatalf("GetBindingByRemote(thread) error: %v", err)
	}
	if threadBinding.ContainerRef == nil || *threadBinding.ContainerRef != "CC" {
		t.Fatalf("thread binding container_ref = %v, want CC", threadBinding.ContainerRef)
	}
}

func TestSyncExchangeEWSAccountRepairsMissingContainerRefWithoutRecentSignals(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	includeHistory := true
	provider := &fakeEmailSyncProvider{
		labels: []providerdata.Label{
			{ID: "CC", Name: "CC"},
		},
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			if opts.Folder == "INBOX" {
				return nil, nil
			}
			return nil, nil
		},
		listPageFunc: func(opts email.SearchOptions, pageToken string) (email.MessagePage, error) {
			if pageToken != "" || opts.Folder != "CC" || !includeHistory {
				return email.MessagePage{}, nil
			}
			return email.MessagePage{IDs: []string{"ews-repair-null"}}, nil
		},
		messages: map[string]*providerdata.EmailMessage{
			"ews-repair-null": {
				ID:         "ews-repair-null",
				ThreadID:   "thread-ews-repair-null",
				Subject:    "Repair my folder",
				Sender:     "Ops <ops@example.com>",
				Recipients: []string{"team@example.com"},
				Date:       time.Date(2026, time.March, 16, 10, 0, 0, 0, time.UTC),
				Labels:     nil,
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("first syncEmailAccount() error: %v", err)
	}
	binding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, emailBindingObjectType, "ews-repair-null")
	if err != nil {
		t.Fatalf("GetBindingByRemote(first email) error: %v", err)
	}
	if binding.ContainerRef != nil {
		t.Fatalf("first binding container_ref = %v, want nil", binding.ContainerRef)
	}

	includeHistory = false
	provider.messages["ews-repair-null"].Labels = []string{"CC"}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("second syncEmailAccount() error: %v", err)
	}
	binding, err = app.store.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, emailBindingObjectType, "ews-repair-null")
	if err != nil {
		t.Fatalf("GetBindingByRemote(second email) error: %v", err)
	}
	if binding.ContainerRef == nil || *binding.ContainerRef != "CC" {
		t.Fatalf("second binding container_ref = %v, want CC", binding.ContainerRef)
	}
	threadBinding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderExchangeEWS, emailThreadBindingObjectType, "thread-ews-repair-null")
	if err != nil {
		t.Fatalf("GetBindingByRemote(thread) error: %v", err)
	}
	if threadBinding.ContainerRef == nil || *threadBinding.ContainerRef != "CC" {
		t.Fatalf("thread binding container_ref = %v, want CC", threadBinding.ContainerRef)
	}
}

func TestSyncEmailAccountCreatesThreadArtifactsAndLinksEmailItems(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	firstBody := "Please review the contract summary."
	secondBody := "The archive copy is attached."

	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"gmail-1"}, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				return []string{"gmail-1", "gmail-2"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-1": {
				ID:         "gmail-1",
				ThreadID:   "thread-contract",
				Subject:    "Re: Contract review",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"legal@example.com"},
				Date:       time.Date(2026, time.March, 9, 10, 0, 0, 0, time.UTC),
				Labels:     []string{"INBOX"},
				BodyText:   &firstBody,
			},
			"gmail-2": {
				ID:         "gmail-2",
				ThreadID:   "thread-contract",
				Subject:    "Contract review",
				Sender:     "Bob <bob@example.com>",
				Recipients: []string{"legal@example.com"},
				Date:       time.Date(2026, time.March, 8, 9, 0, 0, 0, time.UTC),
				Labels:     []string{"Archive"},
				IsRead:     true,
				BodyText:   &secondBody,
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("syncEmailAccount() error: %v", err)
	}

	threads, err := app.store.ListArtifactsByKind(store.ArtifactKindEmailThread)
	if err != nil {
		t.Fatalf("ListArtifactsByKind(email_thread) error: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("len(email_thread artifacts) = %d, want 1", len(threads))
	}

	var threadMeta map[string]any
	if err := json.Unmarshal([]byte(strFromPointer(threads[0].MetaJSON)), &threadMeta); err != nil {
		t.Fatalf("Unmarshal(thread meta) error: %v", err)
	}
	if got := strFromAny(threadMeta["thread_id"]); got != "thread-contract" {
		t.Fatalf("thread_id = %q, want thread-contract", got)
	}
	if got := int(threadMeta["message_count"].(float64)); got != 2 {
		t.Fatalf("message_count = %d, want 2", got)
	}
	if got := strFromAny(threadMeta["subject"]); got != "Contract review" {
		t.Fatalf("subject = %q, want Contract review", got)
	}
	messages, ok := threadMeta["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("thread messages = %#v, want 2 entries", threadMeta["messages"])
	}
	firstMessage, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("thread first message = %#v", messages[0])
	}
	if got := strFromAny(firstMessage["body"]); got != "Please review the contract summary." {
		t.Fatalf("thread first message body = %q, want contract summary", got)
	}

	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-1")
	if err != nil {
		t.Fatalf("GetItemBySource(message) error: %v", err)
	}
	itemArtifacts, err := app.store.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts() error: %v", err)
	}
	if len(itemArtifacts) != 2 {
		t.Fatalf("len(item artifacts) = %d, want 2", len(itemArtifacts))
	}
	if itemArtifacts[0].Artifact.Kind != store.ArtifactKindEmail {
		t.Fatalf("primary artifact kind = %q, want email", itemArtifacts[0].Artifact.Kind)
	}
	if itemArtifacts[1].Artifact.Kind != store.ArtifactKindEmailThread || itemArtifacts[1].Role != "related" {
		t.Fatalf("thread artifact link = %+v, want related email_thread", itemArtifacts[1])
	}
}

func TestSyncEmailAccountExtractsThreadActionItems(t *testing.T) {
	app := newAuthedTestApp(t)
	app.calendarNow = func() time.Time {
		return time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC)
	}

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	body := strings.Join([]string{
		"Please send the draft contract by 2026-03-12.",
		"Please schedule the contract review meeting.",
	}, "\n")
	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"gmail-action"}, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				return []string{"gmail-action"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-action": {
				ID:         "gmail-action",
				ThreadID:   "thread-action",
				Subject:    "Contract review",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"legal@example.com"},
				Date:       time.Date(2026, time.March, 9, 7, 30, 0, 0, time.UTC),
				Labels:     []string{"INBOX"},
				BodyText:   &body,
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("syncEmailAccount() error: %v", err)
	}

	sendItem, err := app.store.GetItemBySource(store.ExternalProviderGmail, "thread:thread-action:action:send-the-draft-contract")
	if err != nil {
		t.Fatalf("GetItemBySource(send) error: %v", err)
	}
	if sendItem.Title != "Send the draft contract" {
		t.Fatalf("send item title = %q, want Send the draft contract", sendItem.Title)
	}
	if sendItem.FollowUpAt == nil || *sendItem.FollowUpAt != "2026-03-12T09:00:00Z" {
		t.Fatalf("send item follow_up_at = %v, want 2026-03-12T09:00:00Z", sendItem.FollowUpAt)
	}

	meetingItem, err := app.store.GetItemBySource(store.ExternalProviderGmail, "thread:thread-action:action:schedule-the-contract-review-meeting")
	if err != nil {
		t.Fatalf("GetItemBySource(meeting) error: %v", err)
	}
	if meetingItem.Title != "Schedule the contract review meeting" {
		t.Fatalf("meeting item title = %q, want Schedule the contract review meeting", meetingItem.Title)
	}
	if meetingItem.FollowUpAt != nil {
		t.Fatalf("meeting item follow_up_at = %v, want nil", meetingItem.FollowUpAt)
	}

	threads, err := app.store.ListArtifactsByKind(store.ArtifactKindEmailThread)
	if err != nil {
		t.Fatalf("ListArtifactsByKind(email_thread) error: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("len(email_thread artifacts) = %d, want 1", len(threads))
	}
	if sendItem.ArtifactID == nil || meetingItem.ArtifactID == nil || *sendItem.ArtifactID != threads[0].ID || *meetingItem.ArtifactID != threads[0].ID {
		t.Fatalf("action artifact ids = %v and %v, want thread artifact %d", sendItem.ArtifactID, meetingItem.ArtifactID, threads[0].ID)
	}
}

func TestSyncEmailAccountOnlyCreatesInboxItemsForFollowUpMessages(t *testing.T) {
	app := newAuthedTestApp(t)
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Work Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	staleSourceRef := "thread:thread-recent:action:schedule-the-status-review"
	_, err = app.store.CreateItem("Schedule the status review", store.ItemOptions{
		State:     store.ItemStateInbox,
		Source:    stringPointer(store.ExternalProviderGmail),
		SourceRef: &staleSourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(stale action) error: %v", err)
	}

	body := "Please schedule the status review."
	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return nil, nil
			case opts.IsFlagged != nil && *opts.IsFlagged:
				return nil, nil
			case !opts.Since.IsZero():
				return []string{"gmail-recent"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-recent": {
				ID:         "gmail-recent",
				ThreadID:   "thread-recent",
				Subject:    "Status review",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"team@example.com"},
				Date:       time.Date(2026, time.March, 9, 7, 30, 0, 0, time.UTC),
				Labels:     []string{"Updates"},
				BodyText:   &body,
				IsRead:     true,
			},
		},
	}
	app.newEmailSyncProvider = func(context.Context, store.ExternalAccount) (emailSyncProvider, error) {
		return provider, nil
	}

	if _, err := app.syncEmailAccount(context.Background(), account); err != nil {
		t.Fatalf("syncEmailAccount() error: %v", err)
	}

	if _, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-recent"); err == nil {
		t.Fatal("recent non-follow-up message created inbox item, want no item")
	}
	if _, err := app.store.GetItemBySource(store.ExternalProviderGmail, staleSourceRef); err == nil {
		t.Fatal("stale inbox action item still exists after sync")
	}

	artifacts, err := app.store.ListArtifactsByKind(store.ArtifactKindEmail)
	if err != nil {
		t.Fatalf("ListArtifactsByKind(email) error: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("len(email artifacts) = %d, want 1", len(artifacts))
	}
	var emailMeta map[string]any
	if err := json.Unmarshal([]byte(strFromPointer(artifacts[0].MetaJSON)), &emailMeta); err != nil {
		t.Fatalf("Unmarshal(email meta) error: %v", err)
	}
	if got := strFromAny(emailMeta["body"]); got != body {
		t.Fatalf("email body = %q, want %q", got, body)
	}
}

func TestItemTriageDoneArchivesRemoteGmailMessage(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"gmail-archive"}, nil
			case !opts.Since.IsZero():
				return []string{"gmail-archive"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-archive": {
				ID:         "gmail-archive",
				ThreadID:   "thread-archive",
				Subject:    "Archive me",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"chr.albert@gmail.com"},
				Date:       time.Date(2026, time.March, 9, 21, 0, 0, 0, time.UTC),
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
	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-archive")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/items/"+itoa(item.ID)+"/triage", map[string]any{
		"action": "done",
	})
	if rr.Code != 200 {
		t.Fatalf("triage done status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if len(provider.archiveCalls) != 1 || len(provider.archiveCalls[0]) != 1 || provider.archiveCalls[0][0] != "gmail-archive" {
		t.Fatalf("archive calls = %#v, want gmail-archive", provider.archiveCalls)
	}
	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(updated) error: %v", err)
	}
	if updated.State != store.ItemStateDone {
		t.Fatalf("updated state = %q, want done", updated.State)
	}
}

func TestItemStateUpdateInboxRestoresRemoteGmailMessage(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.Folder == "INBOX":
				return []string{"gmail-restore"}, nil
			case !opts.Since.IsZero():
				return []string{"gmail-restore"}, nil
			default:
				return nil, nil
			}
		},
		messages: map[string]*providerdata.EmailMessage{
			"gmail-restore": {
				ID:         "gmail-restore",
				ThreadID:   "thread-restore",
				Subject:    "Restore me",
				Sender:     "Ada <ada@example.com>",
				Recipients: []string{"chr.albert@gmail.com"},
				Date:       time.Date(2026, time.March, 9, 21, 15, 0, 0, time.UTC),
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
	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-restore")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if err := app.store.TriageItemDone(item.ID); err != nil {
		t.Fatalf("TriageItemDone() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), "PUT", "/api/items/"+itoa(item.ID)+"/state", map[string]any{
		"state": store.ItemStateInbox,
	})
	if rr.Code != 200 {
		t.Fatalf("state inbox status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if len(provider.moveToInboxCalls) != 1 || len(provider.moveToInboxCalls[0]) != 1 || provider.moveToInboxCalls[0][0] != "gmail-restore" {
		t.Fatalf("move to inbox calls = %#v, want gmail-restore", provider.moveToInboxCalls)
	}
	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(updated) error: %v", err)
	}
	if updated.State != store.ItemStateInbox {
		t.Fatalf("updated state = %q, want inbox", updated.State)
	}
}

func strFromPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
