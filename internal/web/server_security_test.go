package web

import (
	"net/http"
	"testing"
)

func TestWebRouterDoesNotExposeMCPRoute(t *testing.T) {
	app := newAuthedTestApp(t)

	rrPost := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "ping",
	})
	if rrPost.Code != http.StatusNotFound {
		t.Fatalf("expected POST /mcp to return 404 on web router, got %d", rrPost.Code)
	}

	rrGet := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/mcp", nil)
	if rrGet.Code != http.StatusNotFound {
		t.Fatalf("expected GET /mcp to return 404 on web router, got %d", rrGet.Code)
	}
}
