package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// cliTokenEnv lets CLI clients override the token file location. When unset,
// the runtime falls back to $XDG_RUNTIME_DIR/slopshell/cli-token, then to
// <dataDir>/cli-token.
const cliTokenEnv = "SLOPSHELL_CLI_TOKEN_FILE"

// resolveCLITokenPath picks the preferred on-disk location for the CLI token.
// Callers should be able to discover this path without talking to the server.
func resolveCLITokenPath(dataDir string) string {
	if override := strings.TrimSpace(os.Getenv(cliTokenEnv)); override != "" {
		return override
	}
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
		return filepath.Join(runtimeDir, "slopshell", "cli-token")
	}
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	return filepath.Join(dataDir, "cli-token")
}

// initCLIToken resolves the CLI token file path, adopting an existing 0600
// token on disk when present, and otherwise minting a fresh random 32-byte
// hex token and writing it with 0600 perms. Adopt-if-exists keeps the
// in-memory token and the on-disk file in sync across restarts and avoids
// clobbering the file when a second slopshell instance starts against the
// same $XDG_RUNTIME_DIR path (which previously desynced slsh from a
// long-running server).
func initCLIToken(dataDir string) (string, string, error) {
	path := resolveCLITokenPath(dataDir)
	if path == "" {
		return "", "", errors.New("cli token path not resolvable")
	}
	if existing, ok, err := readExistingCLIToken(path); err != nil {
		return "", "", err
	} else if ok {
		return path, existing, nil
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", fmt.Errorf("generate cli token: %w", err)
	}
	token := hex.EncodeToString(raw[:])
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", fmt.Errorf("mkdir cli token dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", "", fmt.Errorf("write cli token: %w", err)
	}
	// Force 0600 in case the file already existed with looser perms, since
	// os.WriteFile only applies the mode on file creation.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", "", fmt.Errorf("chmod cli token: %w", err)
		}
	}
	return path, token, nil
}

// readExistingCLIToken returns (token, true, nil) when the file at path
// already holds a valid 64-char hex token with 0600 perms (non-windows).
// Returns (_, false, nil) when the file is missing, empty, malformed, or
// has loose permissions — so the caller can mint a fresh one. A read error
// other than os.IsNotExist is surfaced.
func readExistingCLIToken(path string) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat cli token: %w", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			return "", false, nil
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read cli token: %w", err)
	}
	token := strings.TrimSpace(string(body))
	if len(token) != 64 {
		return "", false, nil
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", false, nil
	}
	return token, true, nil
}

// isLoopbackRequest returns true only for connections whose remote peer is
// bound to a loopback address. It ignores X-Forwarded-For and any other
// forwarded headers deliberately: the CLI login endpoint is for same-host
// callers only, so trusting proxy headers would be unsafe.
func isLoopbackRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

// handleCLILogin accepts a CLI token previously written to the cli-token file
// and, on match, issues a standard auth-session cookie so subsequent requests
// and websocket upgrades authenticate normally. Loopback-only.
func (a *App) handleCLILogin(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeAPIError(w, http.StatusForbidden, "cli login is loopback-only")
		return
	}
	if strings.TrimSpace(a.cliToken) == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "cli token not initialised")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	provided := strings.TrimSpace(req.Token)
	if provided == "" {
		writeAPIError(w, http.StatusBadRequest, "token is required")
		return
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(a.cliToken)) != 1 {
		writeAPIError(w, http.StatusUnauthorized, "invalid cli token")
		return
	}
	sessionToken := randomToken()
	if err := a.store.AddAuthSession(sessionToken); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	a.setAuthCookieForRequest(w, r, sessionToken)
	writeJSON(w, map[string]interface{}{
		"ok":         true,
		"token_path": a.cliTokenPath,
	})
}
