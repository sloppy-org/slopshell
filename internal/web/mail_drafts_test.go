package web

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type fakeMailDraftProvider struct {
	message        *providerdata.EmailMessage
	createdInputs  []email.DraftInput
	replyInputs    []email.DraftInput
	replyMessageID []string
	updatedInputs  []email.DraftInput
	sentDraftIDs   []string
}

func (f *fakeMailDraftProvider) ListLabels(context.Context) ([]providerdata.Label, error) {
	return nil, nil
}

func (f *fakeMailDraftProvider) ListMessages(context.Context, email.SearchOptions) ([]string, error) {
	return nil, nil
}

func (f *fakeMailDraftProvider) GetMessage(_ context.Context, _ string, _ string) (*providerdata.EmailMessage, error) {
	return f.message, nil
}

func (f *fakeMailDraftProvider) GetMessages(context.Context, []string, string) ([]*providerdata.EmailMessage, error) {
	return nil, nil
}

func (f *fakeMailDraftProvider) MarkRead(context.Context, []string) (int, error)    { return 0, nil }
func (f *fakeMailDraftProvider) MarkUnread(context.Context, []string) (int, error)  { return 0, nil }
func (f *fakeMailDraftProvider) Archive(context.Context, []string) (int, error)     { return 0, nil }
func (f *fakeMailDraftProvider) MoveToInbox(context.Context, []string) (int, error) { return 0, nil }
func (f *fakeMailDraftProvider) Trash(context.Context, []string) (int, error)       { return 0, nil }
func (f *fakeMailDraftProvider) Delete(context.Context, []string) (int, error)      { return 0, nil }
func (f *fakeMailDraftProvider) ProviderName() string                               { return "gmail" }
func (f *fakeMailDraftProvider) Close() error                                       { return nil }

func (f *fakeMailDraftProvider) CreateDraft(_ context.Context, input email.DraftInput) (email.Draft, error) {
	f.createdInputs = append(f.createdInputs, input)
	return email.Draft{ID: "draft-created", ThreadID: "thread-created"}, nil
}

func (f *fakeMailDraftProvider) CreateReplyDraft(_ context.Context, messageID string, input email.DraftInput) (email.Draft, error) {
	f.replyMessageID = append(f.replyMessageID, messageID)
	f.replyInputs = append(f.replyInputs, input)
	threadID := input.ThreadID
	if threadID == "" {
		threadID = "thread-reply"
	}
	return email.Draft{ID: "draft-reply", ThreadID: threadID}, nil
}

func (f *fakeMailDraftProvider) UpdateDraft(_ context.Context, _ string, input email.DraftInput) (email.Draft, error) {
	f.updatedInputs = append(f.updatedInputs, input)
	threadID := input.ThreadID
	if threadID == "" {
		threadID = "thread-updated"
	}
	return email.Draft{ID: "draft-updated", ThreadID: threadID}, nil
}

func (f *fakeMailDraftProvider) SendDraft(_ context.Context, draftID string, _ email.DraftInput) error {
	f.sentDraftIDs = append(f.sentDraftIDs, draftID)
	return nil
}

func TestMailDraftCreateUpdateSendAPI(t *testing.T) {
	app := newAuthedTestApp(t)
	mustCreateProject(t, app)
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", map[string]any{
		"username": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeMailDraftProvider{}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts", map[string]any{
		"account_id": account.ID,
	})
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createPayload := decodeJSONDataResponse(t, rrCreate)["draft"].(map[string]any)
	artifactID := int64(createPayload["artifact_id"].(float64))
	itemID := int64(createPayload["item_id"].(float64))
	if len(provider.createdInputs) != 1 {
		t.Fatalf("CreateDraft calls = %d, want 1", len(provider.createdInputs))
	}
	artifact, err := app.store.GetArtifact(artifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if got := string(artifact.Kind); got != string(artifactKindEmailDraft) {
		t.Fatalf("artifact kind = %q, want %q", got, artifactKindEmailDraft)
	}
	if refPath := stringFromPointer(artifact.RefPath); !strings.HasPrefix(filepath.ToSlash(refPath), ".tabura/artifacts/mail/") {
		t.Fatalf("artifact ref_path = %q, want draft artifact path", refPath)
	}

	rrUpdate := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/mail/drafts/"+itoa(artifactID), map[string]any{
		"to":      []string{"bob@example.com", "carol@example.com"},
		"subject": "Quarterly update v2",
		"body":    "Updated body",
	})
	if rrUpdate.Code != http.StatusOK {
		t.Fatalf("update draft status = %d, want 200: %s", rrUpdate.Code, rrUpdate.Body.String())
	}
	if len(provider.updatedInputs) != 1 {
		t.Fatalf("UpdateDraft calls = %d, want 1", len(provider.updatedInputs))
	}

	rrSend := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/"+itoa(artifactID)+"/send", nil)
	if rrSend.Code != http.StatusOK {
		t.Fatalf("send draft status = %d, want 200: %s", rrSend.Code, rrSend.Body.String())
	}
	if len(provider.sentDraftIDs) != 1 || provider.sentDraftIDs[0] != "draft-updated" {
		t.Fatalf("sent drafts = %#v, want [draft-updated]", provider.sentDraftIDs)
	}

	item, err := app.store.GetItem(itemID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if item.State != store.ItemStateDone {
		t.Fatalf("item state = %q, want done", item.State)
	}
	artifact, err = app.store.GetArtifact(artifactID)
	if err != nil {
		t.Fatalf("GetArtifact(sent) error: %v", err)
	}
	var meta mailDraftArtifactMeta
	if err := json.Unmarshal([]byte(stringFromPointer(artifact.MetaJSON)), &meta); err != nil {
		t.Fatalf("unmarshal draft meta: %v", err)
	}
	if meta.Status != "sent" {
		t.Fatalf("draft status = %q, want sent", meta.Status)
	}
	if got := stringFromPointer(artifact.Title); got != "Quarterly update v2" {
		t.Fatalf("artifact title = %q, want updated subject", got)
	}
}

func TestMailDraftReplyAPI(t *testing.T) {
	app := newAuthedTestApp(t)
	project := mustCreateProject(t, app)
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", map[string]any{
		"username": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeMailDraftProvider{
		message: &providerdata.EmailMessage{
			ID:       "remote-1",
			ThreadID: "thread-remote-1",
			Subject:  "Client question",
			Sender:   "client@example.com",
		},
	}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	artifactTitle := "Client question"
	artifactMeta := `{"sender":"client@example.com","subject":"Client question","thread_id":"thread-remote-1"}`
	artifact, err := app.store.CreateArtifact(store.ArtifactKindEmail, nil, nil, &artifactTitle, &artifactMeta)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	projectID := project.ID
	source := store.ExternalProviderGmail
	sourceRef := "remote-1"
	item, err := app.store.CreateItem("Reply to client", store.ItemOptions{
		ProjectID:  &projectID,
		ArtifactID: &artifact.ID,
		Source:     &source,
		SourceRef:  &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:  account.ID,
		Provider:   account.Provider,
		ObjectType: emailBindingObjectType,
		RemoteID:   "remote-1",
		ItemID:     &item.ID,
		ArtifactID: &artifact.ID,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/reply", map[string]any{
		"item_id": item.ID,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("reply draft status = %d, want 201: %s", rr.Code, rr.Body.String())
	}
	if len(provider.replyMessageID) != 1 || provider.replyMessageID[0] != "remote-1" {
		t.Fatalf("reply message IDs = %#v, want [remote-1]", provider.replyMessageID)
	}
	if len(provider.replyInputs) != 1 {
		t.Fatalf("CreateReplyDraft calls = %d, want 1", len(provider.replyInputs))
	}
	reply := provider.replyInputs[0]
	if len(reply.To) != 1 || reply.To[0] != "client@example.com" {
		t.Fatalf("reply To = %#v, want client@example.com", reply.To)
	}
	if reply.Subject != "Re: Client question" {
		t.Fatalf("reply subject = %q, want Re: Client question", reply.Subject)
	}
}

func TestMailDraftReplyThreadAPI(t *testing.T) {
	app := newAuthedTestApp(t)
	project := mustCreateProject(t, app)
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Work Gmail", map[string]any{
		"username": "user@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeMailDraftProvider{}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	artifactTitle := "Client thread"
	artifactMeta := `{"subject":"Client thread","thread_id":"thread-remote-2","messages":[{"id":"msg-1","sender":"owner@example.com"},{"id":"msg-2","sender":"Client <client@example.com>"}]}`
	artifact, err := app.store.CreateArtifact(store.ArtifactKindEmailThread, nil, nil, &artifactTitle, &artifactMeta)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	projectID := project.ID
	source := store.ExternalProviderGmail
	item, err := app.store.CreateItem("Reply to thread", store.ItemOptions{
		ProjectID:  &projectID,
		ArtifactID: &artifact.ID,
		Source:     &source,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:  account.ID,
		Provider:   account.Provider,
		ObjectType: emailThreadBindingObjectType,
		RemoteID:   "thread-remote-2",
		ItemID:     &item.ID,
		ArtifactID: &artifact.ID,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/reply", map[string]any{
		"item_id": item.ID,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("reply draft status = %d, want 201: %s", rr.Code, rr.Body.String())
	}
	if len(provider.replyMessageID) != 1 || provider.replyMessageID[0] != "msg-2" {
		t.Fatalf("reply message IDs = %#v, want [msg-2]", provider.replyMessageID)
	}
	if len(provider.replyInputs) != 1 {
		t.Fatalf("CreateReplyDraft calls = %d, want 1", len(provider.replyInputs))
	}
	reply := provider.replyInputs[0]
	if len(reply.To) != 1 || reply.To[0] != "Client <client@example.com>" {
		t.Fatalf("reply To = %#v, want Client <client@example.com>", reply.To)
	}
	if reply.Subject != "Re: Client thread" {
		t.Fatalf("reply subject = %q, want Re: Client thread", reply.Subject)
	}
}

func TestMailDraftSendRejectsMissingRecipients(t *testing.T) {
	app := newAuthedTestApp(t)
	mustCreateProject(t, app)
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", map[string]any{
		"username": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeMailDraftProvider{}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts", map[string]any{
		"account_id": account.ID,
	})
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createPayload := decodeJSONDataResponse(t, rrCreate)["draft"].(map[string]any)
	artifactID := int64(createPayload["artifact_id"].(float64))

	rrSend := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/"+itoa(artifactID)+"/send", nil)
	if rrSend.Code != http.StatusBadRequest {
		t.Fatalf("send draft status = %d, want 400: %s", rrSend.Code, rrSend.Body.String())
	}
	if len(provider.sentDraftIDs) != 0 {
		t.Fatalf("SendDraft calls = %d, want 0", len(provider.sentDraftIDs))
	}
}

func TestMailDraftForwardAPI(t *testing.T) {
	app := newAuthedTestApp(t)
	project := mustCreateProject(t, app)
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", map[string]any{
		"username": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	bodyText := "Original body text"
	provider := &fakeMailDraftProvider{
		message: &providerdata.EmailMessage{
			ID:       "remote-fwd-1",
			ThreadID: "thread-fwd-1",
			Subject:  "Original subject",
			Sender:   "someone@example.com",
			BodyText: &bodyText,
			Date:     time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
		},
	}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	artifactTitle := "Original subject"
	artifactMeta := `{"sender":"someone@example.com","subject":"Original subject","thread_id":"thread-fwd-1","body":"Original body text"}`
	artifact, err := app.store.CreateArtifact(store.ArtifactKindEmail, nil, nil, &artifactTitle, &artifactMeta)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	projectID := project.ID
	source := store.ExternalProviderGmail
	sourceRef := "remote-fwd-1"
	item, err := app.store.CreateItem("Forward test", store.ItemOptions{
		ProjectID:  &projectID,
		ArtifactID: &artifact.ID,
		Source:     &source,
		SourceRef:  &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:  account.ID,
		Provider:   account.Provider,
		ObjectType: emailBindingObjectType,
		RemoteID:   "remote-fwd-1",
		ItemID:     &item.ID,
		ArtifactID: &artifact.ID,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/forward", map[string]any{
		"item_id": item.ID,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("forward draft status = %d, want 201: %s", rr.Code, rr.Body.String())
	}
	if len(provider.createdInputs) != 1 {
		t.Fatalf("CreateDraft calls = %d, want 1", len(provider.createdInputs))
	}
	fwd := provider.createdInputs[0]
	if fwd.Subject != "Fwd: Original subject" {
		t.Fatalf("forward subject = %q, want Fwd: Original subject", fwd.Subject)
	}
	if len(fwd.To) != 0 {
		t.Fatalf("forward To = %#v, want empty (user picks recipient)", fwd.To)
	}
	if !strings.Contains(fwd.Body, "Forwarded message") {
		t.Fatalf("forward body should contain forwarded quote, got %q", fwd.Body)
	}
}

func TestMailDraftForwardThreadAPI(t *testing.T) {
	app := newAuthedTestApp(t)
	project := mustCreateProject(t, app)
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Work Gmail", map[string]any{
		"username": "user@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeMailDraftProvider{}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	artifactTitle := "Thread forward"
	artifactMeta := `{"subject":"Thread forward","thread_id":"thread-fwd-2","messages":[{"id":"msg-1","sender":"alice@example.com","date":"Mar 8","body":"First message"},{"id":"msg-2","sender":"bob@example.com","date":"Mar 9","body":"Second message"}]}`
	artifact, err := app.store.CreateArtifact(store.ArtifactKindEmailThread, nil, nil, &artifactTitle, &artifactMeta)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	projectID := project.ID
	source := store.ExternalProviderGmail
	item, err := app.store.CreateItem("Forward thread", store.ItemOptions{
		ProjectID:  &projectID,
		ArtifactID: &artifact.ID,
		Source:     &source,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:  account.ID,
		Provider:   account.Provider,
		ObjectType: emailThreadBindingObjectType,
		RemoteID:   "thread-fwd-2",
		ItemID:     &item.ID,
		ArtifactID: &artifact.ID,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/forward", map[string]any{
		"item_id": item.ID,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("forward draft status = %d, want 201: %s", rr.Code, rr.Body.String())
	}
	if len(provider.createdInputs) != 1 {
		t.Fatalf("CreateDraft calls = %d, want 1", len(provider.createdInputs))
	}
	fwd := provider.createdInputs[0]
	if fwd.Subject != "Fwd: Thread forward" {
		t.Fatalf("forward subject = %q, want Fwd: Thread forward", fwd.Subject)
	}
	if len(fwd.To) != 0 {
		t.Fatalf("forward To = %#v, want empty", fwd.To)
	}
	if !strings.Contains(fwd.Body, "Forwarded message") {
		t.Fatalf("forward body should contain forwarded quote, got %q", fwd.Body)
	}
	if !strings.Contains(fwd.Body, "bob@example.com") {
		t.Fatalf("forward body should quote last message sender, got %q", fwd.Body)
	}
}

func TestMailDraftSendAppendsToThread(t *testing.T) {
	app := newAuthedTestApp(t)
	project := mustCreateProject(t, app)
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", map[string]any{
		"username": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeMailDraftProvider{}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	threadTitle := "Discussion"
	threadMeta := `{"subject":"Discussion","thread_id":"thread-send-1","message_count":1,"messages":[{"id":"msg-1","sender":"peer@example.com","date":"Mar 7","body":"Let us discuss"}]}`
	threadArtifact, err := app.store.CreateArtifact(store.ArtifactKindEmailThread, nil, nil, &threadTitle, &threadMeta)
	if err != nil {
		t.Fatalf("CreateArtifact(thread) error: %v", err)
	}
	projectID := project.ID
	source := store.ExternalProviderGmail
	threadItem, err := app.store.CreateItem("Discussion thread", store.ItemOptions{
		ProjectID:  &projectID,
		ArtifactID: &threadArtifact.ID,
		Source:     &source,
	})
	if err != nil {
		t.Fatalf("CreateItem(thread) error: %v", err)
	}
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:  account.ID,
		Provider:   account.Provider,
		ObjectType: emailThreadBindingObjectType,
		RemoteID:   "thread-send-1",
		ItemID:     &threadItem.ID,
		ArtifactID: &threadArtifact.ID,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding(thread) error: %v", err)
	}

	rrReply := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/reply", map[string]any{
		"item_id": threadItem.ID,
	})
	if rrReply.Code != http.StatusCreated {
		t.Fatalf("reply draft status = %d, want 201: %s", rrReply.Code, rrReply.Body.String())
	}
	replyPayload := decodeJSONDataResponse(t, rrReply)["draft"].(map[string]any)
	draftArtifactID := int64(replyPayload["artifact_id"].(float64))

	rrUpdate := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/mail/drafts/"+itoa(draftArtifactID), map[string]any{
		"to":      []string{"peer@example.com"},
		"subject": "Re: Discussion",
		"body":    "Here is my reply",
	})
	if rrUpdate.Code != http.StatusOK {
		t.Fatalf("update draft status = %d, want 200: %s", rrUpdate.Code, rrUpdate.Body.String())
	}

	rrSend := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/"+itoa(draftArtifactID)+"/send", nil)
	if rrSend.Code != http.StatusOK {
		t.Fatalf("send draft status = %d, want 200: %s", rrSend.Code, rrSend.Body.String())
	}

	updatedThread, err := app.store.GetArtifact(threadArtifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(thread) error: %v", err)
	}
	var threadMetaParsed map[string]any
	if err := json.Unmarshal([]byte(stringFromPointer(updatedThread.MetaJSON)), &threadMetaParsed); err != nil {
		t.Fatalf("unmarshal thread meta: %v", err)
	}
	msgs, _ := threadMetaParsed["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("thread messages = %d, want 2", len(msgs))
	}
	lastMsg, _ := msgs[1].(map[string]any)
	if sent := stringAny(lastMsg["sent"]); sent != "true" {
		t.Fatalf("last message sent = %q, want true", sent)
	}
	if body := stringAny(lastMsg["body"]); body != "Here is my reply" {
		t.Fatalf("last message body = %q, want 'Here is my reply'", body)
	}
	mc, _ := threadMetaParsed["message_count"].(float64)
	if mc != 2 {
		t.Fatalf("thread message_count = %v, want 2", mc)
	}
}

func TestMailDraftReplyAllAPI(t *testing.T) {
	app := newAuthedTestApp(t)
	mustCreateProject(t, app)
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Private Gmail", map[string]any{
		"username": "alice@example.com",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	provider := &fakeMailDraftProvider{
		message: &providerdata.EmailMessage{
			ID:         "remote-ra-1",
			ThreadID:   "thread-ra-1",
			Subject:    "Team update",
			Sender:     "boss@example.com",
			Recipients: []string{"alice@example.com", "bob@example.com", "carol@example.com"},
		},
	}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	artifactTitle := "Team update"
	artifactMeta := `{"sender":"boss@example.com","subject":"Team update","thread_id":"thread-ra-1","recipients":["alice@example.com","bob@example.com","carol@example.com"]}`
	artifact, err := app.store.CreateArtifact(store.ArtifactKindEmail, nil, nil, &artifactTitle, &artifactMeta)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	projectID, _ := app.store.ActiveProjectID()
	source := store.ExternalProviderGmail
	sourceRef := "remote-ra-1"
	item, err := app.store.CreateItem("Team update", store.ItemOptions{
		ProjectID:  &projectID,
		ArtifactID: &artifact.ID,
		Source:     &source,
		SourceRef:  &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:  account.ID,
		Provider:   account.Provider,
		ObjectType: emailBindingObjectType,
		RemoteID:   "remote-ra-1",
		ItemID:     &item.ID,
		ArtifactID: &artifact.ID,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/mail/drafts/reply-all", map[string]any{
		"item_id": item.ID,
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("reply-all draft status = %d, want 201: %s", rr.Code, rr.Body.String())
	}
	if len(provider.replyInputs) != 1 {
		t.Fatalf("CreateReplyDraft calls = %d, want 1", len(provider.replyInputs))
	}
	reply := provider.replyInputs[0]
	if len(reply.To) != 1 || reply.To[0] != "boss@example.com" {
		t.Fatalf("reply-all To = %#v, want [boss@example.com]", reply.To)
	}
	if len(reply.Cc) != 2 {
		t.Fatalf("reply-all Cc = %#v, want 2 addresses (bob and carol)", reply.Cc)
	}
	ccSet := map[string]bool{}
	for _, addr := range reply.Cc {
		ccSet[addr] = true
	}
	if !ccSet["bob@example.com"] || !ccSet["carol@example.com"] {
		t.Fatalf("reply-all Cc = %#v, want bob and carol", reply.Cc)
	}
	if ccSet["alice@example.com"] {
		t.Fatalf("reply-all Cc should not include self (alice)")
	}
	if reply.Subject != "Re: Team update" {
		t.Fatalf("reply-all subject = %q, want Re: Team update", reply.Subject)
	}
}

func mustCreateProject(t *testing.T, app *App) store.Project {
	t.Helper()
	project, err := app.store.CreateProject("Mail", "mail-project", filepath.Join(t.TempDir(), "mail-project"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := app.store.SetActiveProjectID(project.ID); err != nil {
		t.Fatalf("SetActiveProjectID() error: %v", err)
	}
	return project
}
