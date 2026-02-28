package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSTTReplacementsGetRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/stt/replacements", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("authed GET /api/stt/replacements status = %d, want 200", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stt/replacements", nil)
	unauth := httptest.NewRecorder()
	app.Router().ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth GET /api/stt/replacements status = %d, want 401", unauth.Code)
	}
}

func TestSTTReplacementsGetReturnsDefaultMap(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/stt/replacements", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var replacements map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &replacements); err != nil {
		t.Fatalf("decode replacements: %v", err)
	}
	if replacements["mating center"] != "guiding center" {
		t.Fatalf("default replacement missing expected value")
	}
}

func TestSTTReplacementsPutRejectsInvalidJSON(t *testing.T) {
	app := newAuthedTestApp(t)
	req := httptest.NewRequest(http.MethodPut, "/api/stt/replacements", strings.NewReader(`{"bad":`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestSTTReplacementsPutAndGetRoundTrip(t *testing.T) {
	app := newAuthedTestApp(t)
	payload := map[string]string{
		"foo": "bar",
		"x":   "y",
	}
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/stt/replacements", payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", rr.Code)
	}
	var updated map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if len(updated) != 2 || updated["foo"] != "bar" {
		t.Fatalf("unexpected PUT response payload: %#v", updated)
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/stt/replacements", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var fetched map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if fetched["foo"] != "bar" || fetched["x"] != "y" {
		t.Fatalf("persisted replacements mismatch: %#v", fetched)
	}
}

func TestLoadSTTReplacementsFallbackAndSorting(t *testing.T) {
	app := newAuthedTestApp(t)

	if err := app.store.SetAppState(sttReplacementsStateKey, "{invalid"); err != nil {
		t.Fatalf("SetAppState invalid json: %v", err)
	}
	fallback := app.loadSTTReplacementsMap()
	if fallback["mating center"] != "guiding center" {
		t.Fatalf("fallback map should include default replacements")
	}

	raw := map[string]string{
		"zeta":  "",
		"":      "skip-empty-key",
		"alpha": "one",
	}
	body, _ := json.Marshal(raw)
	if err := app.store.SetAppState(sttReplacementsStateKey, string(body)); err != nil {
		t.Fatalf("SetAppState valid replacements: %v", err)
	}
	list := app.loadSTTReplacements()
	if len(list) != 2 {
		t.Fatalf("replacement list len = %d, want 2", len(list))
	}
	if list[0].From != "alpha" || list[1].From != "zeta" {
		t.Fatalf("replacement list not sorted by From: %+v", list)
	}
}
