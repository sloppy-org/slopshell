package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/krystophny/tabura/internal/googleauth"
)

const googleAuthCallbackPath = "/api/google/callback"

func (a *App) handleGoogleAuthStart(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	session, err := googleauth.New("", "", googleauth.DefaultScopes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectURI := fmt.Sprintf("http://localhost:%d%s", DefaultPort, googleAuthCallbackPath)
	authURL := session.GetAuthURLWithRedirect(redirectURI)
	if authURL == "" {
		http.Error(w, "could not generate auth URL", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (a *App) handleGoogleAuthCallback(w http.ResponseWriter, r *http.Request) {
	errMsg := strings.TrimSpace(r.URL.Query().Get("error"))
	if errMsg != "" {
		http.Error(w, fmt.Sprintf("Google auth denied: %s", errMsg), http.StatusForbidden)
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}
	session, err := googleauth.New("", "", googleauth.DefaultScopes)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectURI := fmt.Sprintf("http://localhost:%d%s", DefaultPort, googleAuthCallbackPath)
	if err := session.ExchangeCodeWithRedirect(context.Background(), code, redirectURI); err != nil {
		http.Error(w, fmt.Sprintf("Google auth failed: %v", err), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `<!DOCTYPE html><html><body style="font:18px sans-serif;text-align:center;margin-top:20vh"><h1>Google connected</h1><p>You can close this tab.</p></body></html>`)
}
