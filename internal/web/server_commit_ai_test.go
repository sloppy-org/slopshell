package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestCanvasCommitRoutesToChatWithReviewComments(t *testing.T) {
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		var sc map[string]interface{}
		switch name {
		case "canvas_commit":
			sc = map[string]interface{}{
				"session_id":              "local",
				"artifact_id":             "artifact-text-1",
				"converted_to_persistent": 1,
				"persistent_count":        1,
			}
		case "canvas_status":
			sc = map[string]interface{}{
				"active_artifact_id": "artifact-text-1",
				"active_artifact": map[string]interface{}{
					"kind":     "text_artifact",
					"event_id": "artifact-text-1",
					"title":    "Doc",
					"text":     "# Title\n\nOld body text.\n",
				},
			}
		case "canvas_marks_list":
			sc = map[string]interface{}{
				"marks": []interface{}{
					map[string]interface{}{
						"comment":     "Make the title stronger",
						"type":        "highlight",
						"target_kind": "text_range",
						"target": map[string]interface{}{
							"line_start": 1,
							"line_end":   1,
						},
						"updated_at": "2026-02-21T10:00:00Z",
					},
				},
			}
		default:
			t.Fatalf("unexpected mcp tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": sc,
			},
		})
	}))
	defer mcp.Close()

	app, err := New(t.TempDir(), t.TempDir(), mcp.URL, "ws://127.0.0.1:8787", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app.Shutdown(context.Background())
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth: %v", err)
	}
	u, _ := url.Parse(mcp.URL)
	port, _ := strconv.Atoi(u.Port())
	app.mu.Lock()
	app.tunnelPorts[LocalSessionID] = port
	app.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/canvas/local/commit", map[string]interface{}{
		"artifact_id":   "artifact-text-1",
		"include_draft": true,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["routed_to_chat"] != true {
		t.Fatalf("expected routed_to_chat=true, got %#v", payload)
	}
	chatSessionID, _ := payload["chat_session_id"].(string)
	if strings.TrimSpace(chatSessionID) == "" {
		t.Fatalf("expected chat_session_id in response")
	}

	messages, err := app.store.ListChatMessages(chatSessionID, 10)
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	found := false
	for _, m := range messages {
		if m.Role == "user" && strings.Contains(m.ContentPlain, "Make the title stronger") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected user message with review comment in chat session, got %d messages", len(messages))
	}
}

func TestCanvasCommitWithoutAppServerDoesNotRouteToChat(t *testing.T) {
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		var sc map[string]interface{}
		switch name {
		case "canvas_commit":
			sc = map[string]interface{}{"artifact_id": "artifact-1"}
		default:
			t.Fatalf("unexpected mcp tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": sc,
			},
		})
	}))
	defer mcp.Close()

	app, err := New(t.TempDir(), t.TempDir(), mcp.URL, "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app.Shutdown(context.Background())
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth: %v", err)
	}
	u, _ := url.Parse(mcp.URL)
	port, _ := strconv.Atoi(u.Port())
	app.mu.Lock()
	app.tunnelPorts[LocalSessionID] = port
	app.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/canvas/local/commit", map[string]interface{}{
		"artifact_id":   "artifact-1",
		"include_draft": true,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["routed_to_chat"] != nil {
		t.Fatalf("expected no routed_to_chat when app-server is unavailable, got %#v", payload)
	}
}

func TestCanvasCommitNoCommentsDoesNotRouteToChat(t *testing.T) {
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		var sc map[string]interface{}
		switch name {
		case "canvas_commit":
			sc = map[string]interface{}{"artifact_id": "artifact-text-1"}
		case "canvas_status":
			sc = map[string]interface{}{
				"active_artifact_id": "artifact-text-1",
				"active_artifact": map[string]interface{}{
					"kind":     "text_artifact",
					"event_id": "artifact-text-1",
					"title":    "Doc",
					"text":     "Body text.\n",
				},
			}
		case "canvas_marks_list":
			sc = map[string]interface{}{"marks": []interface{}{}}
		default:
			t.Fatalf("unexpected mcp tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": sc,
			},
		})
	}))
	defer mcp.Close()

	app, err := New(t.TempDir(), t.TempDir(), mcp.URL, "ws://127.0.0.1:8787", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app.Shutdown(context.Background())
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth: %v", err)
	}
	u, _ := url.Parse(mcp.URL)
	port, _ := strconv.Atoi(u.Port())
	app.mu.Lock()
	app.tunnelPorts[LocalSessionID] = port
	app.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/canvas/local/commit", map[string]interface{}{
		"artifact_id":   "artifact-text-1",
		"include_draft": true,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["routed_to_chat"] != nil {
		t.Fatalf("expected no routed_to_chat when there are no comments, got %#v", payload)
	}
}

func TestCanvasCommitUsesExplicitChatSessionID(t *testing.T) {
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		var sc map[string]interface{}
		switch name {
		case "canvas_commit":
			sc = map[string]interface{}{"artifact_id": "artifact-text-1"}
		case "canvas_status":
			sc = map[string]interface{}{
				"active_artifact_id": "artifact-text-1",
				"active_artifact": map[string]interface{}{
					"kind":     "text_artifact",
					"event_id": "artifact-text-1",
					"title":    "Doc",
					"text":     "Hello.\n",
				},
			}
		case "canvas_marks_list":
			sc = map[string]interface{}{
				"marks": []interface{}{
					map[string]interface{}{
						"comment":     "Fix this",
						"type":        "highlight",
						"target_kind": "text_range",
						"target":      map[string]interface{}{"line_start": 1, "line_end": 1},
						"updated_at":  "2026-02-21T10:00:00Z",
					},
				},
			}
		default:
			t.Fatalf("unexpected mcp tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": sc,
			},
		})
	}))
	defer mcp.Close()

	app, err := New(t.TempDir(), t.TempDir(), mcp.URL, "ws://127.0.0.1:8787", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app.Shutdown(context.Background())
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth: %v", err)
	}
	u, _ := url.Parse(mcp.URL)
	port, _ := strconv.Atoi(u.Port())
	app.mu.Lock()
	app.tunnelPorts[LocalSessionID] = port
	app.mu.Unlock()

	session, err := app.store.GetOrCreateChatSession("test-project")
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/canvas/local/commit", map[string]interface{}{
		"artifact_id":     "artifact-text-1",
		"include_draft":   true,
		"chat_session_id": session.ID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["routed_to_chat"] != true {
		t.Fatalf("expected routed_to_chat=true, got %#v", payload)
	}
	if payload["chat_session_id"] != session.ID {
		t.Fatalf("expected chat_session_id=%s, got %v", session.ID, payload["chat_session_id"])
	}

	messages, err := app.store.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	found := false
	for _, m := range messages {
		if m.Role == "user" && strings.Contains(m.ContentPlain, "Fix this") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected user message with review comment in explicit chat session")
	}
}
