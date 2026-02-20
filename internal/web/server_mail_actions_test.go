package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMailActionCapabilitiesProxy(t *testing.T) {
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode producer request: %v", err)
		}
		params, _ := req["params"].(map[string]any)
		name, _ := params["name"].(string)
		if name != "email_action_capabilities" {
			t.Fatalf("unexpected tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"isError": false,
				"structuredContent": map[string]any{
					"capabilities": map[string]any{
						"provider":                 "gmail",
						"supports_open":            true,
						"supports_archive":         true,
						"supports_delete_to_trash": true,
						"supports_native_defer":    true,
					},
				},
			},
		})
	}))
	defer producer.Close()

	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/action-capabilities", map[string]any{
		"provider":         "gmail",
		"producer_mcp_url": producer.URL,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	capabilities, _ := payload["capabilities"].(map[string]any)
	if capabilities == nil {
		t.Fatalf("missing capabilities: %#v", payload)
	}
	if got, _ := capabilities["supports_native_defer"].(bool); !got {
		t.Fatalf("expected supports_native_defer=true, got %#v", capabilities["supports_native_defer"])
	}
}

func TestMailActionProxy(t *testing.T) {
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode producer request: %v", err)
		}
		params, _ := req["params"].(map[string]any)
		name, _ := params["name"].(string)
		if name != "email_action" {
			t.Fatalf("unexpected tool call: %s", name)
		}
		args, _ := params["arguments"].(map[string]any)
		if args["message_id"] != "m42" {
			t.Fatalf("expected message_id=m42, got %#v", args["message_id"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"isError": false,
				"structuredContent": map[string]any{
					"result": map[string]any{
						"provider":                "gmail",
						"action":                  "defer",
						"message_id":              "m42",
						"status":                  "ok",
						"effective_provider_mode": "native",
						"deferred_until_at":       "2026-03-01T10:00:00Z",
					},
				},
			},
		})
	}))
	defer producer.Close()

	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/action", map[string]any{
		"provider":         "gmail",
		"action":           "defer",
		"message_id":       "m42",
		"until_at":         "2026-03-01T10:00:00Z",
		"producer_mcp_url": producer.URL,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	result, _ := payload["result"].(map[string]any)
	if result == nil {
		t.Fatalf("missing result: %#v", payload)
	}
	if got := result["status"]; got != "ok" {
		t.Fatalf("expected status=ok, got %#v", got)
	}
}

func TestMailActionRejectsNonLoopbackProducerURL(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/action", map[string]any{
		"provider":         "gmail",
		"action":           "archive",
		"message_id":       "m42",
		"producer_mcp_url": "http://example.com/mcp",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestMailActionRejectsMalformedUntilAt(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/action", map[string]any{
		"provider":         "gmail",
		"action":           "defer",
		"message_id":       "m42",
		"until_at":         "not-a-time",
		"producer_mcp_url": "http://127.0.0.1:8090/mcp",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestMailActionRejectsNonMCPPath(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/action-capabilities", map[string]any{
		"provider":         "gmail",
		"producer_mcp_url": "http://127.0.0.1:8090/not-mcp",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func newAuthedTestApp(t *testing.T) *App {
	t.Helper()
	app, err := New(t.TempDir(), "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth session: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})
	return app
}

func doAuthedJSONRequest(t *testing.T, handler http.Handler, method, path string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "token-test"})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}
