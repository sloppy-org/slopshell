package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type fakeMailTriageProvider struct {
	messageIDs    []string
	messages      map[string]*providerdata.EmailMessage
	filters       []email.ServerFilter
	movedFolders  []string
	appliedLabels []string
	trashed       []string
	archived      []string
	inboxed       []string
}

func (f *fakeMailTriageProvider) ListLabels(context.Context) ([]providerdata.Label, error) {
	return nil, nil
}

func (f *fakeMailTriageProvider) ListMessages(context.Context, email.SearchOptions) ([]string, error) {
	return append([]string(nil), f.messageIDs...), nil
}

func (f *fakeMailTriageProvider) GetMessage(_ context.Context, messageID, _ string) (*providerdata.EmailMessage, error) {
	return f.messages[messageID], nil
}

func (f *fakeMailTriageProvider) GetMessages(_ context.Context, messageIDs []string, _ string) ([]*providerdata.EmailMessage, error) {
	out := make([]*providerdata.EmailMessage, 0, len(messageIDs))
	for _, id := range messageIDs {
		if message := f.messages[id]; message != nil {
			out = append(out, message)
		}
	}
	return out, nil
}

func (f *fakeMailTriageProvider) MarkRead(context.Context, []string) (int, error)   { return 0, nil }
func (f *fakeMailTriageProvider) MarkUnread(context.Context, []string) (int, error) { return 0, nil }
func (f *fakeMailTriageProvider) Archive(_ context.Context, messageIDs []string) (int, error) {
	f.archived = append(f.archived, messageIDs...)
	return len(messageIDs), nil
}
func (f *fakeMailTriageProvider) MoveToInbox(_ context.Context, messageIDs []string) (int, error) {
	f.inboxed = append(f.inboxed, messageIDs...)
	return len(messageIDs), nil
}
func (f *fakeMailTriageProvider) Trash(_ context.Context, messageIDs []string) (int, error) {
	f.trashed = append(f.trashed, messageIDs...)
	return len(messageIDs), nil
}
func (f *fakeMailTriageProvider) Delete(context.Context, []string) (int, error) { return 0, nil }
func (f *fakeMailTriageProvider) ProviderName() string                          { return "fake" }
func (f *fakeMailTriageProvider) Close() error                                  { return nil }

func (f *fakeMailTriageProvider) MoveToFolder(_ context.Context, messageIDs []string, folder string) (int, error) {
	f.movedFolders = append(f.movedFolders, folder)
	return len(messageIDs), nil
}

func (f *fakeMailTriageProvider) ApplyNamedLabel(_ context.Context, messageIDs []string, label string, _ bool) (int, error) {
	f.appliedLabels = append(f.appliedLabels, label)
	return len(messageIDs), nil
}

func (f *fakeMailTriageProvider) ServerFilterCapabilities() email.ServerFilterCapabilities {
	return email.ServerFilterCapabilities{
		Provider:        "fake",
		SupportsList:    true,
		SupportsUpsert:  true,
		SupportsDelete:  true,
		SupportsArchive: true,
	}
}

func (f *fakeMailTriageProvider) ListServerFilters(context.Context) ([]email.ServerFilter, error) {
	return append([]email.ServerFilter(nil), f.filters...), nil
}

func (f *fakeMailTriageProvider) UpsertServerFilter(_ context.Context, filter email.ServerFilter) (email.ServerFilter, error) {
	if filter.ID == "" {
		filter.ID = "filter-new"
	}
	f.filters = []email.ServerFilter{filter}
	return filter, nil
}

func (f *fakeMailTriageProvider) DeleteServerFilter(_ context.Context, id string) error {
	kept := make([]email.ServerFilter, 0, len(f.filters))
	for _, filter := range f.filters {
		if filter.ID != id {
			kept = append(kept, filter)
		}
	}
	f.filters = kept
	return nil
}

func TestMailTriagePreviewClassifiesAndAutoApplies(t *testing.T) {
	var prompt string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error: %v", err)
		}
		for _, message := range payload.Messages {
			if message.Role == "user" {
				prompt = message.Content
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"action\":\"archive\",\"archive_label\":\"simons24\",\"confidence\":0.97,\"reason\":\"reference only\"}"}}]}`))
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	provider := &fakeMailTriageProvider{
		messageIDs: []string{"m1"},
		messages: map[string]*providerdata.EmailMessage{
			"m1": {
				ID:         "m1",
				Subject:    "Project update",
				Sender:     "boss@example.com",
				Recipients: []string{"ert@example.com"},
				Snippet:    "FYI",
				Date:       time.Now(),
			},
		},
	}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{"username": "ert@example.com"})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	if _, err := app.store.CreateMailTriageReview(store.MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "old-1",
		Folder:    "Junk-E-Mail",
		Subject:   "Win a prize",
		Sender:    "spam@example.com",
		Action:    "trash",
	}); err != nil {
		t.Fatalf("CreateMailTriageReview() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/preview", map[string]any{
		"phase":            "auto_apply",
		"apply":            true,
		"primary_base_url": llm.URL,
		"primary_model":    "qwen3.5-9b",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST preview status = %d: %s", rr.Code, rr.Body.String())
	}
	data := decodeJSONDataResponse(t, rr)
	results, ok := data["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("results payload = %#v", data["results"])
	}
	applied, ok := data["applied"].([]any)
	if !ok || len(applied) != 1 {
		t.Fatalf("applied payload = %#v", data["applied"])
	}
	if len(provider.movedFolders) != 1 || provider.movedFolders[0] != "Archive/simons24" {
		t.Fatalf("movedFolders = %#v, want Archive/simons24", provider.movedFolders)
	}
	if !strings.Contains(prompt, "Recent reviewed examples from this mailbox:") {
		t.Fatalf("prompt missing manual examples: %q", prompt)
	}
	if !strings.Contains(prompt, "action=trash; folder=Junk-E-Mail; from=spam@example.com; subject=Win a prize") {
		t.Fatalf("prompt missing expected example: %q", prompt)
	}
}

func TestMailTriageApplyRoutesCCToNamedFolder(t *testing.T) {
	app := newAuthedTestApp(t)
	provider := &fakeMailTriageProvider{}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/apply", map[string]any{
		"decisions": []map[string]any{
			{"message_id": "m1", "action": "cc"},
			{"message_id": "m2", "action": "trash"},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST apply status = %d: %s", rr.Code, rr.Body.String())
	}
	if len(provider.movedFolders) != 1 || provider.movedFolders[0] != "CC" {
		t.Fatalf("movedFolders = %#v, want CC", provider.movedFolders)
	}
	if len(provider.trashed) != 1 || provider.trashed[0] != "m2" {
		t.Fatalf("trashed = %#v, want [m2]", provider.trashed)
	}
}

func TestMailServerFiltersGenericAPIUsesProviderAbstraction(t *testing.T) {
	app := newAuthedTestApp(t)
	provider := &fakeMailTriageProvider{
		filters: []email.ServerFilter{{
			ID:      "filter-1",
			Name:    "Project mail",
			Enabled: true,
			Criteria: email.ServerFilterCriteria{
				From: "boss@example.com",
			},
			Action: email.ServerFilterAction{
				Archive: true,
				MoveTo:  "Archive",
			},
		}},
	}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Gmail", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/external-accounts/"+itoa(account.ID)+"/mail-server-filters", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("GET filters status = %d: %s", rrList.Code, rrList.Body.String())
	}
	data := decodeJSONDataResponse(t, rrList)
	filters, ok := data["filters"].([]any)
	if !ok || len(filters) != 1 {
		t.Fatalf("filters payload = %#v", data["filters"])
	}

	rrUpsert := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-server-filters", map[string]any{
		"filter": map[string]any{
			"name":    "Lists",
			"enabled": true,
			"criteria": map[string]any{
				"query": "list:physics.example",
			},
			"action": map[string]any{
				"archive": true,
				"move_to": "lists",
			},
		},
	})
	if rrUpsert.Code != http.StatusOK {
		t.Fatalf("POST filters status = %d: %s", rrUpsert.Code, rrUpsert.Body.String())
	}
	if len(provider.filters) != 1 || provider.filters[0].Name != "Lists" {
		encoded, _ := json.Marshal(provider.filters)
		t.Fatalf("filters = %s, want Lists", encoded)
	}
}
