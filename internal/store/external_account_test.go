package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestExternalAccountStoreCRUD(t *testing.T) {
	s := newTestStore(t)

	workConfig := map[string]any{
		"host":     "imap.example.com",
		"port":     993,
		"username": "alice@example.com",
	}
	work, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, " Work Mail ", workConfig)
	if err != nil {
		t.Fatalf("CreateExternalAccount(work) error: %v", err)
	}
	if work.Label != "Work Mail" {
		t.Fatalf("work label = %q, want %q", work.Label, "Work Mail")
	}
	if !work.Enabled {
		t.Fatal("expected created external account to be enabled")
	}

	personal, err := s.CreateExternalAccount(SpherePrivate, ExternalProviderGmail, "Personal Gmail", map[string]any{
		"username":   "bob@gmail.com",
		"token_file": "gmail-personal.json",
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount(personal) error: %v", err)
	}

	gotWork, err := s.GetExternalAccount(work.ID)
	if err != nil {
		t.Fatalf("GetExternalAccount(work) error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(gotWork.ConfigJSON), &decoded); err != nil {
		t.Fatalf("unmarshal config_json: %v", err)
	}
	if decoded["host"] != "imap.example.com" {
		t.Fatalf("config host = %v, want imap.example.com", decoded["host"])
	}

	workAccounts, err := s.ListExternalAccounts(SphereWork)
	if err != nil {
		t.Fatalf("ListExternalAccounts(work) error: %v", err)
	}
	if len(workAccounts) != 1 || workAccounts[0].ID != work.ID {
		t.Fatalf("ListExternalAccounts(work) = %+v, want only work account", workAccounts)
	}

	gmailAccounts, err := s.ListExternalAccountsByProvider(ExternalProviderGmail)
	if err != nil {
		t.Fatalf("ListExternalAccountsByProvider(gmail) error: %v", err)
	}
	if len(gmailAccounts) != 1 || gmailAccounts[0].ID != personal.ID {
		t.Fatalf("ListExternalAccountsByProvider(gmail) = %+v, want personal account", gmailAccounts)
	}

	updatedLabel := "Personal Gmail Primary"
	disabled := false
	if err := s.UpdateExternalAccount(personal.ID, ExternalAccountUpdate{
		Label:   &updatedLabel,
		Config:  map[string]any{"username": "bob@gmail.com", "token_path": "/tmp/tokens/personal.json"},
		Enabled: &disabled,
	}); err != nil {
		t.Fatalf("UpdateExternalAccount() error: %v", err)
	}
	gotPersonal, err := s.GetExternalAccount(personal.ID)
	if err != nil {
		t.Fatalf("GetExternalAccount(personal) error: %v", err)
	}
	if gotPersonal.Label != updatedLabel {
		t.Fatalf("updated label = %q, want %q", gotPersonal.Label, updatedLabel)
	}
	if gotPersonal.Enabled {
		t.Fatal("expected updated external account to be disabled")
	}

	if err := s.DeleteExternalAccount(work.ID); err != nil {
		t.Fatalf("DeleteExternalAccount(work) error: %v", err)
	}
	accounts, err := s.ListExternalAccounts("")
	if err != nil {
		t.Fatalf("ListExternalAccounts(all) error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != personal.ID {
		t.Fatalf("ListExternalAccounts(all) = %+v, want only personal account", accounts)
	}
}

func TestExternalAccountStoreRejectsInvalidConfigAndIdentity(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.CreateExternalAccount("", ExternalProviderGmail, "Mail", nil); err == nil {
		t.Fatal("expected missing sphere error")
	}
	if _, err := s.CreateExternalAccount(SphereWork, "smtp", "Mail", nil); err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "", nil); err == nil {
		t.Fatal("expected missing label error")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"password": "secret"}); err == nil {
		t.Fatal("expected password config rejection")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"oauth_token": "raw-token"}); err == nil {
		t.Fatal("expected token config rejection")
	}
	first, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"username": "mail@example.com"})
	if err != nil {
		t.Fatalf("CreateExternalAccount(first) error: %v", err)
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"username": "dupe@example.com"}); err == nil {
		t.Fatal("expected duplicate account identity rejection")
	}
	badSphere := "office"
	if err := s.UpdateExternalAccount(first.ID, ExternalAccountUpdate{Sphere: &badSphere}); err == nil {
		t.Fatal("expected invalid update sphere error")
	}
}

func TestExternalAccountCredentialHelpers(t *testing.T) {
	envVar := ExternalAccountPasswordEnvVar(ExternalProviderGoogleCalendar, "Work Calendar")
	if envVar != "TABURA_GOOGLE_CALENDAR_PASSWORD_WORK_CALENDAR" {
		t.Fatalf("ExternalAccountPasswordEnvVar() = %q", envVar)
	}

	tokenPath := ExternalAccountTokenPath("/home/test/.config/tabura", ExternalProviderGmail, "Work Gmail")
	wantPath := filepath.Join("/home/test/.config/tabura", "tokens", "gmail_work_gmail.json")
	if tokenPath != wantPath {
		t.Fatalf("ExternalAccountTokenPath() = %q, want %q", tokenPath, wantPath)
	}
}
