package web

import (
	"net/http"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

type externalAccountCreateRequest struct {
	Sphere   string         `json:"sphere"`
	Provider string         `json:"provider"`
	Label    string         `json:"label"`
	Config   map[string]any `json:"config"`
	Enabled  *bool          `json:"enabled,omitempty"`
}

type externalAccountUpdateRequest struct {
	Sphere   *string        `json:"sphere,omitempty"`
	Provider *string        `json:"provider,omitempty"`
	Label    *string        `json:"label,omitempty"`
	Config   map[string]any `json:"config,omitempty"`
	Enabled  *bool          `json:"enabled,omitempty"`
}

func (a *App) handleExternalAccountList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	accounts, err := a.store.ListExternalAccounts(strings.TrimSpace(r.URL.Query().Get("sphere")))
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"accounts": accounts,
	})
}

func (a *App) handleExternalAccountCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req externalAccountCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	account, err := a.store.CreateExternalAccount(req.Sphere, req.Provider, req.Label, req.Config)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if req.Enabled != nil && !*req.Enabled {
		if err := a.store.UpdateExternalAccount(account.ID, store.ExternalAccountUpdate{Enabled: req.Enabled}); err != nil {
			writeDomainStoreError(w, err)
			return
		}
		account, err = a.store.GetExternalAccount(account.ID)
		if err != nil {
			writeDomainStoreError(w, err)
			return
		}
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"account": account,
	})
}

func (a *App) handleExternalAccountUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	accountID, err := parseURLInt64Param(r, "account_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req externalAccountUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.store.UpdateExternalAccount(accountID, store.ExternalAccountUpdate{
		Sphere:   req.Sphere,
		Provider: req.Provider,
		Label:    req.Label,
		Config:   req.Config,
		Enabled:  req.Enabled,
	}); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	account, err := a.store.GetExternalAccount(accountID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"account": account,
	})
}

func (a *App) handleExternalAccountDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	accountID, err := parseURLInt64Param(r, "account_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteExternalAccount(accountID); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"deleted":    true,
		"account_id": accountID,
	})
}
