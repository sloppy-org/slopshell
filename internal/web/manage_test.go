package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func doAuthedHTMLRequest(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestManagePageServesDashboardShell(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedHTMLRequest(t, app.Router(), "/manage")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("content type = %q, want text/html", rr.Header().Get("Content-Type"))
	}
	for _, needle := range []string{
		"<title>tabura manage</title>",
		"Tabura Manage",
		`href="./manage/hotword"`,
		`src="./static/manage.js`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("body missing %q", needle)
		}
	}
}

func TestManageSubroutesServeRootBasedShell(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedHTMLRequest(t, app.Router(), "/manage/hotword")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `<base href="/">`) {
		t.Fatalf("expected root base href in manage subroute, body=%s", body)
	}
	if !strings.Contains(body, `href="./manage/models"`) {
		t.Fatalf("expected manage navigation links in body=%s", body)
	}
}
