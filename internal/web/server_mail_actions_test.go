package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMailReadProxy(t *testing.T) {
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode producer request: %v", err)
		}
		params, _ := req["params"].(map[string]any)
		name, _ := params["name"].(string)
		if name != "email_read" {
			t.Fatalf("unexpected tool call: %s", name)
		}
		args, _ := params["arguments"].(map[string]any)
		if args["message_id"] != "m42" {
			t.Fatalf("expected message_id=m42, got %#v", args["message_id"])
		}
		if args["format"] != "full" {
			t.Fatalf("expected format=full, got %#v", args["format"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"isError": false,
				"structuredContent": map[string]any{
					"message": map[string]any{
						"ID":      "m42",
						"Subject": "hello",
						"Snippet": "world",
					},
				},
			},
		})
	}))
	defer producer.Close()

	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/read", map[string]any{
		"provider":         "gmail",
		"message_id":       "m42",
		"producer_mcp_url": producer.URL,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	message, _ := payload["message"].(map[string]any)
	if message == nil {
		t.Fatalf("missing message: %#v", payload)
	}
	if got := message["ID"]; got != "m42" {
		t.Fatalf("expected ID=m42, got %#v", got)
	}
}

func TestMailReadRequiresMessageID(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/read", map[string]any{
		"provider":         "gmail",
		"producer_mcp_url": "http://127.0.0.1:8090/mcp",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestMailMarkReadProxy(t *testing.T) {
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode producer request: %v", err)
		}
		params, _ := req["params"].(map[string]any)
		name, _ := params["name"].(string)
		if name != "email_mark_read" {
			t.Fatalf("unexpected tool call: %s", name)
		}
		args, _ := params["arguments"].(map[string]any)
		if args["provider"] != "gmail" {
			t.Fatalf("expected provider=gmail, got %#v", args["provider"])
		}
		ids, _ := args["message_ids"].([]any)
		if len(ids) != 1 || ids[0] != "m42" {
			t.Fatalf("expected message_ids=[m42], got %#v", args["message_ids"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"isError": false,
				"structuredContent": map[string]any{
					"marked": 1,
				},
			},
		})
	}))
	defer producer.Close()

	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/mark-read", map[string]any{
		"provider":         "gmail",
		"message_id":       "m42",
		"producer_mcp_url": producer.URL,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := payload["marked"]; got != float64(1) {
		t.Fatalf("expected marked=1, got %#v", got)
	}
}

func TestMailMarkReadRequiresMessageID(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/mark-read", map[string]any{
		"provider":         "gmail",
		"producer_mcp_url": "http://127.0.0.1:8090/mcp",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
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

func TestMailDraftReplyUsesProducerDraftTool(t *testing.T) {
	calls := []string{}
	callArgs := []map[string]any{}
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode producer request: %v", err)
		}
		params, _ := req["params"].(map[string]any)
		name, _ := params["name"].(string)
		calls = append(calls, name)
		args, _ := params["arguments"].(map[string]any)
		callArgs = append(callArgs, args)
		if name != "draft_reply" {
			t.Fatalf("unexpected tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"isError": false,
				"structuredContent": map[string]any{
					"draft_text": "Hi team,\\n\\nHere is a draft reply.\\n\\nBest,\\nMe",
				},
			},
		})
	}))
	defer producer.Close()

	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/draft-reply", map[string]any{
		"provider":         "gmail",
		"message_id":       "m42",
		"subject":          "Question",
		"sender":           "Alice <alice@example.com>",
		"selection_text":   "Can you reply by Friday?",
		"producer_mcp_url": producer.URL,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := payload["source"]; got != "llm" {
		t.Fatalf("expected source=llm, got %#v", got)
	}
	if got := payload["draft_text"]; got == "" {
		t.Fatalf("expected non-empty draft_text")
	}
	if len(calls) != 1 || calls[0] != "draft_reply" {
		t.Fatalf("unexpected producer tool calls: %#v", calls)
	}
	if len(callArgs) != 1 {
		t.Fatalf("expected one argument payload, got %#v", callArgs)
	}
	args := callArgs[0]
	if got := args["provider"]; got != "gmail" {
		t.Fatalf("expected provider=gmail, got %#v", got)
	}
	if got := args["message_id"]; got != "m42" {
		t.Fatalf("expected message_id=m42, got %#v", got)
	}
	if got := args["subject"]; got != "Question" {
		t.Fatalf("expected subject=Question, got %#v", got)
	}
	if got := args["sender"]; got != "Alice <alice@example.com>" {
		t.Fatalf("expected sender=Alice <alice@example.com>, got %#v", got)
	}
	if got := args["selection_text"]; got != "Can you reply by Friday?" {
		t.Fatalf("expected selection_text forwarded, got %#v", got)
	}
}

func TestMailDraftReplyDisabled(t *testing.T) {
	t.Setenv("TABULA_DRAFT_REPLY_DISABLED", "1")
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/draft-reply", map[string]any{
		"provider":         "gmail",
		"message_id":       "m42",
		"producer_mcp_url": "http://127.0.0.1:8090/mcp",
	})
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestMailDraftReplyFallsBackWhenToolUnavailable(t *testing.T) {
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode producer request: %v", err)
		}
		params, _ := req["params"].(map[string]any)
		name, _ := params["name"].(string)
		if name == "draft_reply" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"error": map[string]any{
					"code":    -32601,
					"message": "method not found",
				},
			})
			return
		}
		if name != "email_read" {
			t.Fatalf("unexpected tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"isError": false,
				"structuredContent": map[string]any{
					"message": map[string]any{
						"snippet": "Could we align on next steps this week?",
					},
				},
			},
		})
	}))
	defer producer.Close()

	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/draft-reply", map[string]any{
		"provider":         "gmail",
		"message_id":       "m42",
		"subject":          "Planning",
		"sender":           "Bob <bob@example.com>",
		"producer_mcp_url": producer.URL,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := payload["source"]; got != "fallback" {
		t.Fatalf("expected source=fallback, got %#v", got)
	}
	if draft, _ := payload["draft_text"].(string); strings.TrimSpace(draft) == "" {
		t.Fatalf("expected non-empty fallback draft_text")
	}
}

func TestMailSTTUsesVoxTypeMCPFlowAndReturnsTranscript(t *testing.T) {
	type sessionState struct {
		buf []byte
	}
	sessions := map[string]*sessionState{}
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		params, _ := req["params"].(map[string]any)
		method := req["method"]
		if method != "tools/call" {
			t.Fatalf("expected tools/call, got %#v", method)
		}
		name := fmt.Sprint(params["name"])
		args, _ := params["arguments"].(map[string]any)
		sid := strings.TrimSpace(fmt.Sprint(args["session_id"]))
		switch name {
		case "push_to_prompt_start":
			sessions[sid] = &sessionState{buf: nil}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]any{"structuredContent": map[string]any{"ok": true, "session_id": sid}},
			})
		case "push_to_prompt_append":
			raw := strings.TrimSpace(fmt.Sprint(args["audio_chunk_base64"]))
			chunk, err := base64.StdEncoding.DecodeString(raw)
			if err != nil {
				t.Fatalf("decode append payload: %v", err)
			}
			s := sessions[sid]
			if s == nil {
				t.Fatalf("missing session %q", sid)
			}
			s.buf = append(s.buf, chunk...)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result":  map[string]any{"structuredContent": map[string]any{"ok": true, "session_id": sid}},
			})
		case "push_to_prompt_stop":
			s := sessions[sid]
			if s == nil || len(s.buf) == 0 {
				t.Fatalf("expected buffered audio for %q", sid)
			}
			delete(sessions, sid)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req["id"],
				"result": map[string]any{
					"structuredContent": map[string]any{
						"ok":                   true,
						"text":                 "reply by tomorrow",
						"language":             "en",
						"language_probability": 0.91,
						"source":               "voxtype_mcp",
					},
				},
			})
		default:
			t.Fatalf("unexpected tool call: %s", name)
		}
	}))
	defer mcp.Close()

	audioPayload := base64.StdEncoding.EncodeToString([]byte("audio-bytes"))
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/stt", map[string]any{
		"voxtype_mcp_url": mcp.URL,
		"mime_type":       "audio/webm",
		"audio_base64":    audioPayload,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := payload["text"]; got != "reply by tomorrow" {
		t.Fatalf("expected transcript text, got %#v", got)
	}
	if got := payload["source"]; got != "voxtype_mcp" {
		t.Fatalf("expected source=voxtype_mcp, got %#v", got)
	}
}

func TestMailSTTRejectsMalformedAudio(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/stt", map[string]any{
		"mime_type":    "audio/webm",
		"audio_base64": "$$$not-base64$$$",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "valid base64") {
		t.Fatalf("expected explicit base64 error, got %s", rr.Body.String())
	}
}

func TestMailSTTRejectsNonLoopbackVoxTypeMCPURL(t *testing.T) {
	audioPayload := base64.StdEncoding.EncodeToString([]byte("x"))
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/stt", map[string]any{
		"voxtype_mcp_url": "http://example.com/mcp",
		"mime_type":       "audio/webm",
		"audio_base64":    audioPayload,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "loopback") {
		t.Fatalf("expected loopback validation error, got %s", rr.Body.String())
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
