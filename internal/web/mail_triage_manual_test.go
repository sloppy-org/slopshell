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
			"m3": {ID: "m3", Subject: "Newsletter", Sender: "news@example.com"},
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
		"action":     "inbox",
	})
	if keepRR.Code != http.StatusOK {
		t.Fatalf("inbox status = %d body=%s", keepRR.Code, keepRR.Body.String())
	}
	if len(provider.inboxed) != 0 || len(provider.archived) != 0 || len(provider.trashed) != 0 {
		t.Fatalf("inbox unexpectedly moved mail: inboxed=%#v archived=%#v trashed=%#v", provider.inboxed, provider.archived, provider.trashed)
	}

	inboxFromJunkRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/manual/reviews", map[string]any{
		"message_id": "m2",
		"folder":     "Junk-E-Mail",
		"action":     "inbox",
	})
	if inboxFromJunkRR.Code != http.StatusOK {
		t.Fatalf("junk->inbox status = %d body=%s", inboxFromJunkRR.Code, inboxFromJunkRR.Body.String())
	}
	if len(provider.inboxed) != 1 || provider.inboxed[0] != "m2" {
		t.Fatalf("inboxed = %#v, want [m2]", provider.inboxed)
	}

	ccRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/manual/reviews", map[string]any{
		"message_id": "m3",
		"folder":     "Posteingang",
		"action":     "cc",
	})
	if ccRR.Code != http.StatusOK {
		t.Fatalf("cc status = %d body=%s", ccRR.Code, ccRR.Body.String())
	}
	if len(provider.movedFolders) != 1 || provider.movedFolders[0] != "CC" {
		t.Fatalf("movedFolders = %#v, want [CC]", provider.movedFolders)
	}

	reviews, err := app.store.ListMailTriageReviews(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailTriageReviews() error: %v", err)
	}
	if len(reviews) != 3 {
		t.Fatalf("reviews len = %d, want 3", len(reviews))
	}
	if reviews[0].Action != "cc" || reviews[1].Action != "inbox" || reviews[2].Action != "inbox" {
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
		Action:    "inbox",
	}); err != nil {
		t.Fatalf("CreateMailTriageReview() error: %v", err)
	}
	if _, err := app.store.CreateMailTriageReview(store.MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m2",
		Folder:    "Junk-E-Mail",
		Subject:   "Spam",
		Sender:    "spam@example.com",
		Action:    "trash",
	}); err != nil {
		t.Fatalf("CreateMailTriageReview() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/manual/reviews?limit=1&folder=Junk-E-Mail", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	data := decodeJSONDataResponse(t, rr)
	reviews, _ := data["reviews"].([]any)
	if len(reviews) != 1 {
		t.Fatalf("reviews len = %d, want 1", len(reviews))
	}
	distilled, ok := data["distilled"].(map[string]any)
	if !ok {
		t.Fatalf("distilled payload = %#v", data["distilled"])
	}
	if got := int(distilled["review_count"].(float64)); got != 2 {
		t.Fatalf("distilled review_count = %d, want 2", got)
	}
	reviewedIDs, _ := data["reviewed_message_ids"].([]any)
	if len(reviewedIDs) != 1 {
		t.Fatalf("reviewed_message_ids len = %d, want 1", len(reviewedIDs))
	}
}

func TestMailTriageManualReviewUndoRestoresOriginalFolderAndDeletesReview(t *testing.T) {
	app := newAuthedTestApp(t)
	provider := &fakeMailTriageProvider{
		messageIDsByFolder: map[string][]string{
			"Gelöschte Elemente": {"m1-moved"},
			"Posteingang":        {"m1-restored"},
		},
		messages: map[string]*providerdata.EmailMessage{
			"m1-moved": {
				ID:      "m1-moved",
				Subject: "Need triage",
				Sender:  "alice@example.com",
				Labels:  []string{"Gelöschte Elemente"},
			},
			"m1-restored": {
				ID:      "m1-restored",
				Subject: "Need triage",
				Sender:  "alice@example.com",
				Labels:  []string{"Posteingang"},
			},
		},
	}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	review, err := app.store.CreateMailTriageReview(store.MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m1",
		Folder:    "Posteingang",
		Subject:   "Need triage",
		Sender:    "alice@example.com",
		Action:    "trash",
	})
	if err != nil {
		t.Fatalf("CreateMailTriageReview() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/manual/reviews/"+itoa(review.ID)+"/undo", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(provider.inboxed) != 1 || provider.inboxed[0] != "m1-moved" {
		t.Fatalf("inboxed = %#v, want [m1-moved]", provider.inboxed)
	}
	reviews, err := app.store.ListMailTriageReviews(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailTriageReviews() error: %v", err)
	}
	if len(reviews) != 0 {
		t.Fatalf("reviews len = %d, want 0", len(reviews))
	}
	data := decodeJSONDataResponse(t, rr)
	message, _ := data["message"].(map[string]any)
	if message["ID"] != "m1-restored" {
		t.Fatalf("restored message id = %#v, want m1-restored", message["ID"])
	}
}

func TestMailTriageManualReviewUndoRestoresJunkFolderFromInboxDecision(t *testing.T) {
	app := newAuthedTestApp(t)
	provider := &fakeMailTriageProvider{
		messageIDsByFolder: map[string][]string{
			"Posteingang": {"m2-moved"},
		},
		messages: map[string]*providerdata.EmailMessage{
			"m2-moved": {
				ID:      "m2-moved",
				Subject: "Suspicious",
				Sender:  "spam@example.com",
				Labels:  []string{"Posteingang"},
			},
		},
	}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	review, err := app.store.CreateMailTriageReview(store.MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m2",
		Folder:    "Junk-E-Mail",
		Subject:   "Suspicious",
		Sender:    "spam@example.com",
		Action:    "inbox",
	})
	if err != nil {
		t.Fatalf("CreateMailTriageReview() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/manual/reviews/"+itoa(review.ID)+"/undo", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if len(provider.movedFolders) != 1 || provider.movedFolders[0] != "Junk-E-Mail" {
		t.Fatalf("movedFolders = %#v, want [Junk-E-Mail]", provider.movedFolders)
	}
}

func TestMailTriageManualReviewUndoResolvesRestoredMessageWhenStoredBindingStaysInOriginalFolder(t *testing.T) {
	app := newAuthedTestApp(t)
	provider := &fakeMailTriageProvider{
		messageIDsByFolder: map[string][]string{
			"Posteingang": {"m1-restored"},
		},
		messages: map[string]*providerdata.EmailMessage{
			"m1-restored": {
				ID:       "m1-restored",
				Subject:  "Need triage",
				Sender:   "alice@example.com",
				Labels:   []string{"Posteingang"},
				BodyText: stringPtr("Restored body"),
			},
		},
	}
	app.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	account, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:    account.ID,
		Provider:     account.Provider,
		ObjectType:   "email",
		RemoteID:     "m1",
		ContainerRef: stringPtr("Posteingang"),
	}); err != nil {
		t.Fatalf("UpsertExternalBinding() error: %v", err)
	}
	review, err := app.store.CreateMailTriageReview(store.MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m1",
		Folder:    "Posteingang",
		Subject:   "Need triage",
		Sender:    "alice@example.com",
		Action:    "trash",
	})
	if err != nil {
		t.Fatalf("CreateMailTriageReview() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts/"+itoa(account.ID)+"/mail-triage/manual/reviews/"+itoa(review.ID)+"/undo", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	data := decodeJSONDataResponse(t, rr)
	message, _ := data["message"].(map[string]any)
	if message["ID"] != "m1-restored" {
		t.Fatalf("restored message id = %#v, want m1-restored", message["ID"])
	}
	if message["BodyText"] != "Restored body" {
		t.Fatalf("restored message body = %#v, want Restored body", message["BodyText"])
	}
}
