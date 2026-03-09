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
	listFunc  func(email.SearchOptions) ([]string, error)
	messages  map[string]*providerdata.EmailMessage
	contacts  []providerdata.Contact
	listCalls []email.SearchOptions
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

func (f *fakeEmailSyncProvider) Close() error {
	return nil
}

func (f *fakeEmailSyncProvider) ListContacts(_ context.Context) ([]providerdata.Contact, error) {
	return append([]providerdata.Contact(nil), f.contacts...), nil
}

func stringPointer(value string) *string {
	return &value
}

type exchangeEmailSyncFixture struct {
	app         *App
	projectID   string
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
			case opts.IsRead != nil && !*opts.IsRead:
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
			case opts.IsRead != nil && !*opts.IsRead:
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
	project, err := app.store.CreateProject("Contracts", "contracts", filepath.Join(t.TempDir(), "contracts"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Contracts", filepath.Join(t.TempDir(), "workspace", "contracts"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderExchange, "label", "Contracts", &workspace.ID, &project.ID, nil); err != nil {
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
		projectID:   project.ID,
		workspaceID: workspace.ID,
	}
}

func exchangeEmailSyncProvider() *fakeEmailSyncProvider {
	return &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.IsRead != nil && !*opts.IsRead:
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
	if item.ProjectID == nil || *item.ProjectID != fixture.projectID {
		t.Fatalf("exchange item project_id = %v, want %q", item.ProjectID, fixture.projectID)
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

func TestSyncEmailAccountCreatesFollowUpItemsFromRules(t *testing.T) {
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
			case opts.IsRead != nil && !*opts.IsRead:
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

	item, err := app.store.GetItemBySource(store.ExternalProviderGmail, "message:gmail-contract")
	if err != nil {
		t.Fatalf("GetItemBySource(rule) error: %v", err)
	}
	if item.Title != "contract review needed" {
		t.Fatalf("rule item title = %q, want subject", item.Title)
	}
}

func TestSyncEmailAccountUsesMappedNonPrimaryLabelForAssignment(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Legal Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	project, err := app.store.CreateProject("Contracts", "contracts", filepath.Join(t.TempDir(), "contracts"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Contracts", filepath.Join(t.TempDir(), "workspace", "contracts"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderGmail, "label", "Contracts", &workspace.ID, &project.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping() error: %v", err)
	}

	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.IsRead != nil && !*opts.IsRead:
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
	if item.ProjectID == nil || *item.ProjectID != project.ID {
		t.Fatalf("item project_id = %v, want %q", item.ProjectID, project.ID)
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

func TestSyncEmailAccountLeavesDoneItemsClosed(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Done Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	provider := &fakeEmailSyncProvider{
		listFunc: func(opts email.SearchOptions) ([]string, error) {
			switch {
			case opts.IsRead != nil && !*opts.IsRead:
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
	if item.State != store.ItemStateDone {
		t.Fatalf("item state after resync = %q, want done", item.State)
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
			case opts.IsRead != nil && !*opts.IsRead:
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
			case opts.IsRead != nil && !*opts.IsRead:
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
			case opts.IsRead != nil && !*opts.IsRead:
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

func strFromPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
