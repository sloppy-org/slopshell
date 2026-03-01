package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFilesProxyAllowsSameOriginEmbedding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/docs/test.pdf" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4\n%test\n"))
	}))
	defer upstream.Close()

	app := newAuthedTestApp(t)
	port, err := extractPort(upstream.URL)
	if err != nil {
		t.Fatalf("extract port: %v", err)
	}
	app.tunnels.setPort("s1", port)

	req := httptest.NewRequest(http.MethodGet, "/api/files/s1/docs/test.pdf", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/pdf") {
		t.Fatalf("expected pdf content-type, got %q", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "SAMEORIGIN" {
		t.Fatalf("expected X-Frame-Options SAMEORIGIN, got %q", got)
	}
	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("expected frame-ancestors 'self' in csp, got %q", csp)
	}
	if strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("did not expect frame-ancestors 'none' in csp: %q", csp)
	}
}

func TestFilesProxyDecodesEncodedNestedPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/files/docs/test.pdf" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4\n%encoded\n"))
	}))
	defer upstream.Close()

	app := newAuthedTestApp(t)
	port, err := extractPort(upstream.URL)
	if err != nil {
		t.Fatalf("extract port: %v", err)
	}
	app.tunnels.setPort("s1", port)

	req := httptest.NewRequest(http.MethodGet, "/api/files/s1/docs%2Ftest.pdf", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if got := rr.Body.String(); !strings.Contains(got, "%encoded") {
		t.Fatalf("expected proxied body, got %q", got)
	}
}

func TestFilesProxyRejectsEncodedTraversal(t *testing.T) {
	app := newAuthedTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/files/s1/%2e%2e%2Fsecret.pdf", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}
