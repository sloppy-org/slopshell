package ptyd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleOpenRejectsInvalidRequest(t *testing.T) {
	app := New(t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/pty/open", strings.NewReader(`{"session_id":""}`))
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestHandleCloseInvalidAndNotFound(t *testing.T) {
	app := New(t.TempDir())

	badReq := httptest.NewRequest(http.MethodPost, "/api/pty/close", strings.NewReader(`{"session_id":`))
	badRR := httptest.NewRecorder()
	app.Router().ServeHTTP(badRR, badReq)
	if badRR.Code != http.StatusBadRequest {
		t.Fatalf("invalid close status = %d, want 400", badRR.Code)
	}

	notFoundReq := httptest.NewRequest(http.MethodPost, "/api/pty/close", strings.NewReader(`{"session_id":"missing"}`))
	notFoundRR := httptest.NewRecorder()
	app.Router().ServeHTTP(notFoundRR, notFoundReq)
	if notFoundRR.Code != http.StatusOK {
		t.Fatalf("not-found close status = %d, want 200", notFoundRR.Code)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(notFoundRR.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode close response: %v", err)
	}
	if payload["closed"] != false {
		t.Fatalf("closed = %v, want false", payload["closed"])
	}
}

func TestHandleListAndHealthWhenEmpty(t *testing.T) {
	app := New(t.TempDir())

	listReq := httptest.NewRequest(http.MethodGet, "/api/pty/list", nil)
	listRR := httptest.NewRecorder()
	app.Router().ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listRR.Code)
	}
	var listPayload map[string]interface{}
	if err := json.Unmarshal(listRR.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	sessions, ok := listPayload["sessions"].([]interface{})
	if !ok {
		t.Fatalf("sessions field missing or invalid: %#v", listPayload["sessions"])
	}
	if len(sessions) != 0 {
		t.Fatalf("expected zero sessions, got %d", len(sessions))
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	healthRR := httptest.NewRecorder()
	app.Router().ServeHTTP(healthRR, healthReq)
	if healthRR.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", healthRR.Code)
	}
	var healthPayload map[string]interface{}
	if err := json.Unmarshal(healthRR.Body.Bytes(), &healthPayload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if healthPayload["status"] != "ok" {
		t.Fatalf("status field = %v, want ok", healthPayload["status"])
	}
}

func TestHandleWSReturnsNotFoundForMissingSession(t *testing.T) {
	app := New(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/ws/pty/missing", nil)
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestIntFromAnyAndStopNoop(t *testing.T) {
	if got := intFromAny(12.0, 3); got != 12 {
		t.Fatalf("intFromAny(float64) = %d, want 12", got)
	}
	if got := intFromAny(7, 3); got != 7 {
		t.Fatalf("intFromAny(int) = %d, want 7", got)
	}
	if got := intFromAny("x", 3); got != 3 {
		t.Fatalf("intFromAny(default) = %d, want 3", got)
	}

	app := New(t.TempDir())
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("Stop(nil server) error: %v", err)
	}
}
