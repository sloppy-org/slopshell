package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

func TestMailTriageManualReviewCreateStoresDecisionAndAppliesAction(t *testing.T) {
	app := newAuthedTestApp(t)
	provider := &fakeMailTriageProvider{
		messages: map[string]*providerdata.EmailMessage{
			"m1": {ID: "m1", Subject: "Need triage", Sender: "alice@example.com"},
			"m2": {ID: "m2", Subject: "Suspicious", Sender: "spam@example.com"},
		},
	}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}

	keepRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/manual/reviews", map[string]any{
		"message_id": "m1",
		"folder":     "Posteingang",
		"action":     "keep",
	})
	if keepRR.Code != http.StatusOK {
		t.Fatalf("keep status = %d body=%s", keepRR.Code, keepRR.Body.String())
	}
	if len(provider.inboxed) != 0 || len(provider.archived) != 0 || len(provider.trashed) != 0 {
		t.Fatalf("keep unexpectedly moved mail: inboxed=%#v archived=%#v trashed=%#v", provider.inboxed, provider.archived, provider.trashed)
	}

	rescueRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/manual/reviews", map[string]any{
		"message_id": "m2",
		"folder":     "Junk-E-Mail",
		"action":     "rescue",
	})
	if rescueRR.Code != http.StatusOK {
		t.Fatalf("rescue status = %d body=%s", rescueRR.Code, rescueRR.Body.String())
	}
	if len(provider.inboxed) != 1 || provider.inboxed[0] != "m2" {
		t.Fatalf("inboxed = %#v, want [m2]", provider.inboxed)
	}

	reviews, err := app.store.ListMailTriageReviews(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailTriageReviews() error: %v", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("reviews len = %d, want 2", len(reviews))
	}
	if reviews[0].Action != "rescue" || reviews[1].Action != "keep" {
		t.Fatalf("reviews = %#v", reviews)
	}
}

func TestMailTriageManualReviewsListReturnsRecentReviews(t *testing.T) {
	app := newAuthedTestApp(t)
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	if _, err := app.store.CreateMailTriageReview(store.MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m1",
		Folder:    "Posteingang",
		Subject:   "Hello",
		Sender:    "alice@example.com",
		Action:    "keep",
	}); err != nil {
		t.Fatalf("CreateMailTriageReview() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/manual/reviews?limit=5", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	data := decodeJSONDataResponse(t, rr)
	reviews, _ := data["reviews"].([]any)
	if len(reviews) != 1 {
		t.Fatalf("reviews len = %d, want 1", len(reviews))
	}
}
