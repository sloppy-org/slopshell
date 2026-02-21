package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func TestCanvasCommitTriggersAppServerRewriteForTextArtifact(t *testing.T) {
	type capture struct {
		mu             sync.Mutex
		showCallCount  int
		showMarkdown   string
		showArtifactID string
		requestedTools []string
	}
	c := &capture{}

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})

		c.mu.Lock()
		c.requestedTools = append(c.requestedTools, name)
		c.mu.Unlock()

		var sc map[string]interface{}
		switch name {
		case "canvas_commit":
			sc = map[string]interface{}{
				"session_id":              "local",
				"artifact_id":             "artifact-text-1",
				"converted_to_persistent": 1,
				"persistent_count":        1,
				"sidecar_path":            "/tmp/annotations.json",
				"pdf_annotations_written": 0,
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
		case "canvas_artifact_show":
			markdown := strings.TrimSpace(toString(args["markdown_or_text"]))
			c.mu.Lock()
			c.showCallCount++
			c.showMarkdown = markdown
			c.showArtifactID = "artifact-text-2"
			c.mu.Unlock()
			sc = map[string]interface{}{
				"artifact_id": "artifact-text-2",
				"kind":        "text_artifact",
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

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode app-server message: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"userAgent": "test",
					},
				})
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-1"},
					},
				})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-1"},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "agentMessage",
							"text": "# Better Title\n\nRewritten body text.\n",
						},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "turn/completed",
					"params": map[string]interface{}{
						"turn": map[string]interface{}{
							"id":     "turn-1",
							"status": "completed",
						},
					},
				})
				return
			}
		}
	}))
	defer appServer.Close()

	wsURL := "ws" + strings.TrimPrefix(appServer.URL, "http")
	app, err := New(t.TempDir(), t.TempDir(), mcp.URL, wsURL, false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app.Shutdown(context.Background())
	if err := app.store.AddAuthSession("token-test"); err != nil {
		t.Fatalf("add auth: %v", err)
	}
	u, err := url.Parse(mcp.URL)
	if err != nil {
		t.Fatalf("parse mcp url: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse mcp port: %v", err)
	}
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
	aiReview, _ := payload["ai_review"].(map[string]interface{})
	if aiReview == nil {
		t.Fatalf("expected ai_review in response: %#v", payload)
	}
	if got := aiReview["applied"]; got != true {
		t.Fatalf("expected ai_review.applied=true, got %#v", got)
	}
	if got := aiReview["artifact_kind"]; got != "text_artifact" {
		t.Fatalf("expected artifact_kind=text_artifact, got %#v", got)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.showCallCount != 1 {
		t.Fatalf("expected exactly one canvas_artifact_show call, got %d", c.showCallCount)
	}
	if c.showMarkdown != "# Better Title\n\nRewritten body text." {
		t.Fatalf("unexpected rewritten markdown: %q", c.showMarkdown)
	}
}

func TestCanvasCommitTriggersAppServerReviewNotesForPDFArtifact(t *testing.T) {
	type capture struct {
		mu            sync.Mutex
		showCallCount int
		showTitle     string
		showText      string
	}
	c := &capture{}

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode mcp request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]interface{})
		var sc map[string]interface{}
		switch name {
		case "canvas_commit":
			sc = map[string]interface{}{"artifact_id": "artifact-pdf-1"}
		case "canvas_status":
			sc = map[string]interface{}{
				"active_artifact_id": "artifact-pdf-1",
				"active_artifact": map[string]interface{}{
					"kind":     "pdf_artifact",
					"event_id": "artifact-pdf-1",
					"title":    "Spec PDF",
					"path":     "/tmp/spec.pdf",
					"page":     2,
				},
			}
		case "canvas_marks_list":
			sc = map[string]interface{}{
				"marks": []interface{}{
					map[string]interface{}{
						"comment":     "Clarify this requirement language",
						"type":        "comment_point",
						"target_kind": "pdf_point",
						"target": map[string]interface{}{
							"page": 2,
						},
						"updated_at": "2026-02-21T11:00:00Z",
					},
				},
			}
		case "canvas_artifact_show":
			c.mu.Lock()
			c.showCallCount++
			c.showTitle = toString(args["title"])
			c.showText = toString(args["markdown_or_text"])
			c.mu.Unlock()
			sc = map[string]interface{}{"artifact_id": "artifact-pdf-notes"}
		default:
			t.Fatalf("unexpected mcp call: %s", name)
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

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode app-server message: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"userAgent": "test"}})
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"thread": map[string]interface{}{"id": "thread-pdf"}}})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{"id": msg["id"], "result": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-pdf"}}})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "agentMessage",
							"text": "# PDF Review Notes\n\n1. Proposed clarification.",
						},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{"method": "turn/completed", "params": map[string]interface{}{"turn": map[string]interface{}{"id": "turn-pdf", "status": "completed"}}})
				return
			}
		}
	}))
	defer appServer.Close()

	app, err := New(t.TempDir(), t.TempDir(), mcp.URL, "ws"+strings.TrimPrefix(appServer.URL, "http"), false)
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
		"artifact_id":   "artifact-pdf-1",
		"include_draft": true,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	aiReview, _ := payload["ai_review"].(map[string]interface{})
	if aiReview == nil || aiReview["artifact_kind"] != "pdf_artifact" || aiReview["applied"] != true {
		t.Fatalf("unexpected ai_review payload: %#v", payload["ai_review"])
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.showCallCount != 1 {
		t.Fatalf("expected one artifact show for PDF notes, got %d", c.showCallCount)
	}
	if !strings.Contains(c.showTitle, "AI Review Notes") {
		t.Fatalf("expected PDF notes title, got %q", c.showTitle)
	}
	if !strings.Contains(c.showText, "PDF Review Notes") {
		t.Fatalf("expected PDF notes content, got %q", c.showText)
	}
}

func toString(v interface{}) string {
	s, _ := v.(string)
	return s
}
