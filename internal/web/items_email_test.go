package web

import (
	"context"
	"encoding/json"
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

func TestSourceSyncRunnerPollsExchangeContacts(t *testing.T) {
	app := newAuthedTestApp(t)

	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchange, "Work Exchange", map[string]any{
		"client_id": "client-id",
		"tenant_id": "tenant-id",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount(exchange) error: %v", err)
	}

	app.newContactSyncProvider = func(_ context.Context, externalAccount store.ExternalAccount) (contactSyncProvider, error) {
		if externalAccount.ID != account.ID {
			t.Fatalf("unexpected external account id: %d", externalAccount.ID)
		}
		return &fakeEmailSyncProvider{
			contacts: []providerdata.Contact{{
				ProviderRef:  "exchange-contact-1",
				Name:         "Carol",
				Email:        "carol@example.com",
				Organization: "Example Corp",
			}},
		}, nil
	}
	app.sourceSync = app.newSourceSyncRunner()

	result, err := app.syncSourcesNow(context.Background())
	if err != nil {
		t.Fatalf("syncSourcesNow() error: %v", err)
	}
	if len(result.Accounts) != 1 {
		t.Fatalf("len(result.Accounts) = %d, want 1", len(result.Accounts))
	}
	actor, err := app.store.GetActorByProviderRef(store.ExternalProviderExchange, "exchange-contact-1")
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

func strFromPointer(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
