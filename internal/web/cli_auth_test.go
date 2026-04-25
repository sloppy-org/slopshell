package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitCLITokenWritesFileWithRestrictivePerms(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv(cliTokenEnv, "")
	path, token, err := initCLIToken(dir)
	if err != nil {
		t.Fatalf("initCLIToken: %v", err)
	}
	wantDir := filepath.Clean(dir)
	if filepath.Dir(path) != wantDir {
		t.Fatalf("token dir = %q, want %q", filepath.Dir(path), wantDir)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := stat.Mode().Perm(); perm != 0o600 {
			t.Fatalf("token perms = %o, want 0600", perm)
		}
	}
	if len(token) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars", len(token))
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != token {
		t.Fatalf("file contents = %q, want %q", got, token)
	}
}

func TestInitCLITokenAdoptsExistingValidToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv(cliTokenEnv, "")

	// First call mints a fresh token.
	path1, token1, err := initCLIToken(dir)
	if err != nil {
		t.Fatalf("initCLIToken first call: %v", err)
	}
	statBefore, err := os.Stat(path1)
	if err != nil {
		t.Fatalf("stat after first init: %v", err)
	}

	// Second call (simulating a server restart or a second slopshell on the
	// same path) must adopt the same token and not rewrite the file.
	path2, token2, err := initCLIToken(dir)
	if err != nil {
		t.Fatalf("initCLIToken second call: %v", err)
	}
	if path2 != path1 {
		t.Fatalf("path mismatch: %q vs %q", path2, path1)
	}
	if token2 != token1 {
		t.Fatalf("token mismatch: adopt-if-exists should reuse the on-disk token (got %q, want %q)", token2, token1)
	}
	statAfter, err := os.Stat(path1)
	if err != nil {
		t.Fatalf("stat after second init: %v", err)
	}
	if !statAfter.ModTime().Equal(statBefore.ModTime()) {
		t.Fatalf("token file was rewritten: mtime %v -> %v", statBefore.ModTime(), statAfter.ModTime())
	}
}

func TestInitCLITokenReplacesMalformedFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv(cliTokenEnv, "")
	path := filepath.Join(dir, "cli-token")
	if err := os.WriteFile(path, []byte("not-hex garbage\n"), 0o600); err != nil {
		t.Fatalf("seed malformed file: %v", err)
	}
	gotPath, token, err := initCLIToken(dir)
	if err != nil {
		t.Fatalf("initCLIToken: %v", err)
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	if len(token) != 64 {
		t.Fatalf("token length = %d, want 64 hex chars", len(token))
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != token {
		t.Fatalf("file not rewritten: contents = %q, want %q", got, token)
	}
}

func TestInitCLITokenReplacesFileWithLoosePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("perm semantics differ on windows")
	}
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv(cliTokenEnv, "")
	path := filepath.Join(dir, "cli-token")
	fakeToken := strings.Repeat("a", 64)
	if err := os.WriteFile(path, []byte(fakeToken+"\n"), 0o644); err != nil {
		t.Fatalf("seed loose-perm file: %v", err)
	}
	gotPath, token, err := initCLIToken(dir)
	if err != nil {
		t.Fatalf("initCLIToken: %v", err)
	}
	if gotPath != path {
		t.Fatalf("path = %q, want %q", gotPath, path)
	}
	if token == fakeToken {
		t.Fatalf("expected fresh token, got the loose-perm file token")
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := stat.Mode().Perm(); perm != 0o600 {
		t.Fatalf("replaced file perms = %o, want 0600", perm)
	}
}

func TestResolveCLITokenPathPrefersEnvThenXDGRuntimeDir(t *testing.T) {
	t.Setenv(cliTokenEnv, "/custom/path/cli-token")
	if got := resolveCLITokenPath("/data"); got != "/custom/path/cli-token" {
		t.Fatalf("env override path = %q, want /custom/path/cli-token", got)
	}
	t.Setenv(cliTokenEnv, "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := resolveCLITokenPath("/data"); got != "/run/user/1000/slopshell/cli-token" {
		t.Fatalf("xdg path = %q, want /run/user/1000/slopshell/cli-token", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if got := resolveCLITokenPath("/data"); got != "/data/cli-token" {
		t.Fatalf("datadir fallback = %q, want /data/cli-token", got)
	}
}

func postCLILogin(t *testing.T, app *App, remoteAddr, token string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/cli/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)
	return rr
}

func TestCLILoginSucceedsOnLoopbackAndMintsCookie(t *testing.T) {
	app := newAuthedTestApp(t)
	app.cliToken = "secret-cli-token"
	app.cliTokenPath = "/tmp/slopshell/cli-token"

	rr := postCLILogin(t, app, "127.0.0.1:54321", "secret-cli-token")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == SessionCookie {
			cookie = c
			break
		}
	}
	if cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		t.Fatalf("expected %s cookie, got %+v", SessionCookie, rr.Result().Cookies())
	}
	if !app.store.HasAuthSession(cookie.Value) {
		t.Fatalf("cookie token %q not registered as auth session", cookie.Value)
	}

	// Follow-up request using the cookie must succeed against the authed API.
	req := httptest.NewRequest(http.MethodGet, "/api/runtime", nil)
	req.AddCookie(cookie)
	r2 := httptest.NewRecorder()
	app.Router().ServeHTTP(r2, req)
	if r2.Code != http.StatusOK {
		t.Fatalf("authed runtime status = %d, body = %s", r2.Code, r2.Body.String())
	}
}

func TestCLILoginRejectsNonLoopback(t *testing.T) {
	app := newAuthedTestApp(t)
	app.cliToken = "secret-cli-token"

	rr := postCLILogin(t, app, "10.0.0.2:44444", "secret-cli-token")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rr.Code, rr.Body.String())
	}
}

func TestCLILoginRejectsForwardedForSpoof(t *testing.T) {
	app := newAuthedTestApp(t)
	app.cliToken = "secret-cli-token"

	body, _ := json.Marshal(map[string]string{"token": "secret-cli-token"})
	req := httptest.NewRequest(http.MethodPost, "/api/cli/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.RemoteAddr = "203.0.113.5:44444"
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("forwarded-for spoof not rejected: status = %d body = %s", rr.Code, rr.Body.String())
	}
}

func TestCLILoginRejectsWrongToken(t *testing.T) {
	app := newAuthedTestApp(t)
	app.cliToken = "secret-cli-token"

	rr := postCLILogin(t, app, "127.0.0.1:54321", "wrong-value")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body = %s", rr.Code, rr.Body.String())
	}
}

func TestCLILoginWhenTokenNotInitialised(t *testing.T) {
	app := newAuthedTestApp(t)
	app.cliToken = ""

	rr := postCLILogin(t, app, "127.0.0.1:54321", "anything")
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body = %s", rr.Code, rr.Body.String())
	}
}
