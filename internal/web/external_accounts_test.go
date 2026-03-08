package web

import (
	"net/http"
	"testing"
)

func TestExternalAccountCRUDAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	disabled := false
	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts", map[string]any{
		"sphere":   "work",
		"provider": "gmail",
		"label":    " Work Gmail ",
		"config": map[string]any{
			"username":   "alice@example.com",
			"token_path": "/tmp/tokens/work-gmail.json",
		},
		"enabled": disabled,
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("create external account status = %d, want 200: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createPayload := decodeJSONResponse(t, rrCreate)
	accountPayload, ok := createPayload["account"].(map[string]any)
	if !ok {
		t.Fatalf("create payload = %#v", createPayload)
	}
	accountID := int64(accountPayload["id"].(float64))
	if got := accountPayload["label"]; got != "Work Gmail" {
		t.Fatalf("created label = %#v, want %q", got, "Work Gmail")
	}
	if enabled, _ := accountPayload["enabled"].(bool); enabled {
		t.Fatalf("created account payload = %#v, want disabled account", accountPayload)
	}

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/external-accounts?sphere=work", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("list external accounts status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	listPayload := decodeJSONResponse(t, rrList)
	accounts, ok := listPayload["accounts"].([]any)
	if !ok || len(accounts) != 1 {
		t.Fatalf("list payload = %#v", listPayload)
	}

	enabled := true
	newLabel := "Work Gmail Primary"
	rrUpdate := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/external-accounts/"+itoa(accountID), map[string]any{
		"label":   newLabel,
		"enabled": enabled,
		"config": map[string]any{
			"username":   "alice@example.com",
			"token_file": "gmail-work.json",
		},
	})
	if rrUpdate.Code != http.StatusOK {
		t.Fatalf("update external account status = %d, want 200: %s", rrUpdate.Code, rrUpdate.Body.String())
	}
	updated, err := app.store.GetExternalAccount(accountID)
	if err != nil {
		t.Fatalf("GetExternalAccount(updated) error: %v", err)
	}
	if updated.Label != newLabel {
		t.Fatalf("updated label = %q, want %q", updated.Label, newLabel)
	}
	if !updated.Enabled {
		t.Fatal("expected updated external account to be enabled")
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/external-accounts/"+itoa(accountID), nil)
	if rrDelete.Code != http.StatusOK {
		t.Fatalf("delete external account status = %d, want 200: %s", rrDelete.Code, rrDelete.Body.String())
	}
	rrMissing := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/external-accounts/"+itoa(accountID), nil)
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("missing external account delete status = %d, want 404: %s", rrMissing.Code, rrMissing.Body.String())
	}
}

func TestExternalAccountAPIRejectsInvalidInput(t *testing.T) {
	app := newAuthedTestApp(t)

	rrBadCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/external-accounts", map[string]any{
		"sphere":   "work",
		"provider": "gmail",
		"label":    "Bad config",
		"config": map[string]any{
			"password": "plaintext",
		},
	})
	if rrBadCreate.Code != http.StatusBadRequest {
		t.Fatalf("bad create status = %d, want 400: %s", rrBadCreate.Code, rrBadCreate.Body.String())
	}

	rrBadList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/external-accounts?sphere=office", nil)
	if rrBadList.Code != http.StatusBadRequest {
		t.Fatalf("bad list status = %d, want 400: %s", rrBadList.Code, rrBadList.Body.String())
	}

	rrMissingUpdate := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/external-accounts/9999", map[string]any{
		"label": "Missing",
	})
	if rrMissingUpdate.Code != http.StatusNotFound {
		t.Fatalf("missing update status = %d, want 404: %s", rrMissingUpdate.Code, rrMissingUpdate.Body.String())
	}
}
