package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newPasswordTestApp(t *testing.T) *App {
	t.Helper()
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.SetAdminPassword("secret-password"); err != nil {
		t.Fatalf("SetAdminPassword() error: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})
	return app
}

func TestHandleLoginJSONPreservesAPIContract(t *testing.T) {
	app := newPasswordTestApp(t)

	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"secret-password"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/login JSON status = %d, want 200", rr.Code)
	}
	assertJSONContentType(t, rr)
	if !strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Fatalf("POST /api/login JSON body = %q, want ok=true", rr.Body.String())
	}
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("POST /api/login JSON returned no cookies")
	}
	if cookies[0].Name != SessionCookie {
		t.Fatalf("cookie name = %q, want %q", cookies[0].Name, SessionCookie)
	}
	if !app.store.HasAuthSession(cookies[0].Value) {
		t.Fatal("auth session was not stored for JSON login")
	}
}

func TestServeIndexShowsMainViewWhenAuthenticated(t *testing.T) {
	app := newAuthedTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `id="view-login" class="view" style="display:none"`) {
		t.Fatalf("GET / body did not hide login view for authenticated request: %q", body)
	}
	if strings.Contains(body, `id="view-main" class="view" style="display:none"`) {
		t.Fatalf("GET / body kept main view hidden for authenticated request: %q", body)
	}
}

func TestHandleLoginFormRedirectsAndSetsCookie(t *testing.T) {
	app := newPasswordTestApp(t)

	form := url.Values{}
	form.Set("password", "secret-password")
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Prefix", "/tabura")
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("POST /api/login form status = %d, want 303", rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "/tabura/" {
		t.Fatalf("POST /api/login form Location = %q, want %q", got, "/tabura/")
	}
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("POST /api/login form returned no cookies")
	}
	if cookies[0].Name != SessionCookie {
		t.Fatalf("cookie name = %q, want %q", cookies[0].Name, SessionCookie)
	}
	if !app.store.HasAuthSession(cookies[0].Value) {
		t.Fatal("auth session was not stored for form login")
	}
}

func TestServeIndexLoginFormIncludesPasswordFieldName(t *testing.T) {
	app := newPasswordTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `id="login-password" name="password"`) {
		t.Fatalf("GET / body did not include named login password field: %q", body)
	}
}
