package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testAuthToken = "token-test"

func newAuthedTestApp(t *testing.T) *App {
	t.Helper()
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.AddAuthSession(testAuthToken); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})
	return app
}

func doAuthedJSONRequest(t *testing.T, handler http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func decodeJSONResponse(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response body: %v\nbody=%s", err, rr.Body.String())
	}
	return payload
}

func assertJSONContentType(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content type = %q, want application/json", got)
	}
}

func decodeJSONDataResponse(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	assertJSONContentType(t, rr)
	payload := decodeJSONResponse(t, rr)
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data payload = %#v", payload)
	}
	return data
}
