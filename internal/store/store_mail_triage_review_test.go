package store

import (
	"path/filepath"
	"testing"
)

func TestCreateAndListMailTriageReviews(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "triage.db"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	first, err := s.CreateMailTriageReview(MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m1",
		Folder:    "Posteingang",
		Subject:   "One",
		Sender:    "alice@example.com",
		Action:    "keep",
	})
	if err != nil {
		t.Fatalf("CreateMailTriageReview(first) error: %v", err)
	}
	second, err := s.CreateMailTriageReview(MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m2",
		Folder:    "Junk-E-Mail",
		Subject:   "Two",
		Sender:    "spam@example.com",
		Action:    "trash",
	})
	if err != nil {
		t.Fatalf("CreateMailTriageReview(second) error: %v", err)
	}
	if first.ID <= 0 || second.ID <= 0 {
		t.Fatalf("review ids = %d, %d", first.ID, second.ID)
	}

	reviews, err := s.ListMailTriageReviews(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailTriageReviews() error: %v", err)
	}
	if len(reviews) != 2 {
		t.Fatalf("reviews len = %d, want 2", len(reviews))
	}
	if reviews[0].MessageID != "m2" || reviews[0].Action != "trash" {
		t.Fatalf("reviews[0] = %#v", reviews[0])
	}
	if reviews[1].MessageID != "m1" || reviews[1].Action != "keep" {
		t.Fatalf("reviews[1] = %#v", reviews[1])
	}
}
