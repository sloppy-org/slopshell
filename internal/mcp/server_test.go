package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/canvas"
)

func TestCanvasImportHandoffFileText(t *testing.T) {
	content := []byte("hello from handoff")
	sum := sha256.Sum256(content)

	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		var structured map[string]interface{}
		switch name {
		case "handoff.peek":
			structured = map[string]interface{}{"handoff_id": "h1", "kind": "file"}
		case "handoff.consume":
			structured = map[string]interface{}{
				"spec_version": "handoff.v1",
				"handoff_id":   "h1",
				"kind":         "file",
				"meta": map[string]interface{}{
					"filename":   "note.txt",
					"mime_type":  "text/plain",
					"size_bytes": len(content),
					"sha256":     stringHex(sum[:]),
				},
				"payload": map[string]interface{}{
					"content_base64": base64.StdEncoding.EncodeToString(content),
				},
			}
		default:
			t.Fatalf("unexpected tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": structured,
			},
		})
	}))
	defer producer.Close()

	projectDir := t.TempDir()
	adapter := canvas.NewAdapter(projectDir, nil)
	s := NewServer(adapter)
	got, err := s.callTool("canvas_import_handoff", map[string]interface{}{
		"session_id":       "s1",
		"handoff_id":       "h1",
		"producer_mcp_url": producer.URL,
		"title":            "Imported File",
	})
	if err != nil {
		t.Fatalf("canvas_import_handoff failed: %v", err)
	}
	if got["kind"] != "file" {
		t.Fatalf("expected kind=file, got %#v", got["kind"])
	}
	if got["artifact_id"] == nil {
		t.Fatalf("missing artifact_id: %#v", got)
	}

	matches, err := filepath.Glob(filepath.Join(projectDir, ".tabura", "artifacts", "imports", "h1-*.txt"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one imported file, found %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read imported file: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("imported content mismatch")
	}
}

func TestCanvasImportHandoffUnsupportedKind(t *testing.T) {
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		structured := map[string]interface{}{"handoff_id": "h1", "kind": "nope"}
		if name == "handoff.consume" {
			structured["payload"] = map[string]interface{}{}
			structured["meta"] = map[string]interface{}{}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": structured,
			},
		})
	}))
	defer producer.Close()

	adapter := canvas.NewAdapter(t.TempDir(), nil)
	s := NewServer(adapter)
	_, err := s.callTool("canvas_import_handoff", map[string]interface{}{
		"session_id":       "s1",
		"handoff_id":       "h1",
		"producer_mcp_url": producer.URL,
	})
	if err == nil {
		t.Fatalf("expected unsupported kind error")
	}
}

func stringHex(b []byte) string {
	const hextable = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hextable[v>>4]
		out[i*2+1] = hextable[v&0x0f]
	}
	return string(out)
}

func TestResolveModelAlias(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"codex", "gpt-5.3-codex"},
		{"CODEX", "gpt-5.3-codex"},
		{"spark", "gpt-5.3-codex-spark"},
		{"gpt", "gpt-5.2"},
		{"", "gpt-5.3-codex"},
		{"  ", "gpt-5.3-codex"},
		{"gpt-5.2", "gpt-5.2"},
		{"some-custom-model", "some-custom-model"},
	}
	for _, tc := range tests {
		got := resolveModelAlias(tc.input)
		if got != tc.want {
			t.Errorf("resolveModelAlias(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestAssembleDelegatePrompt(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		got := assembleDelegatePrompt("Be thorough.", "User asked about X.", "Analyze the code.")
		if !strings.Contains(got, "## Instructions") {
			t.Error("missing Instructions section")
		}
		if !strings.Contains(got, "Be thorough.") {
			t.Error("missing system_prompt content")
		}
		if !strings.Contains(got, "## Context") {
			t.Error("missing Context section")
		}
		if !strings.Contains(got, "User asked about X.") {
			t.Error("missing context content")
		}
		if !strings.Contains(got, "## Task") {
			t.Error("missing Task section")
		}
		if !strings.Contains(got, "Analyze the code.") {
			t.Error("missing prompt content")
		}
	})

	t.Run("prompt only uses default system prompt", func(t *testing.T) {
		got := assembleDelegatePrompt("", "", "Do something.")
		if !strings.Contains(got, "## Instructions") {
			t.Error("should have Instructions section with default preamble")
		}
		if !strings.Contains(got, "Edit files directly") {
			t.Error("default preamble should tell delegate to edit files directly")
		}
		if !strings.Contains(got, "Do NOT output patches") {
			t.Error("default preamble should tell delegate not to output patches")
		}
		if strings.Contains(got, "## Context") {
			t.Error("should not have Context section when empty")
		}
		if !strings.Contains(got, "## Task") {
			t.Error("missing Task section")
		}
		if !strings.Contains(got, "Do something.") {
			t.Error("missing prompt content")
		}
	})

	t.Run("explicit system_prompt overrides default", func(t *testing.T) {
		got := assembleDelegatePrompt("Custom instructions.", "", "Do something.")
		if !strings.Contains(got, "Custom instructions.") {
			t.Error("explicit system_prompt should be used")
		}
		if strings.Contains(got, "Edit files directly") {
			t.Error("default preamble should not appear when explicit system_prompt provided")
		}
	})
}

func TestToolDefinitionsEmitsProperties(t *testing.T) {
	defs := toolDefinitions()
	var delegateDef map[string]interface{}
	var statusDef map[string]interface{}
	var cancelDef map[string]interface{}
	var activeCountDef map[string]interface{}
	var cancelAllDef map[string]interface{}
	var tempCreateDef map[string]interface{}
	var tempRemoveDef map[string]interface{}
	for _, d := range defs {
		switch d["name"] {
		case "delegate_to_model":
			delegateDef = d
		case "delegate_to_model_status":
			statusDef = d
		case "delegate_to_model_cancel":
			cancelDef = d
		case "delegate_to_model_active_count":
			activeCountDef = d
		case "delegate_to_model_cancel_all":
			cancelAllDef = d
		case "temp_file_create":
			tempCreateDef = d
		case "temp_file_remove":
			tempRemoveDef = d
		}
	}
	if delegateDef == nil {
		t.Fatal("delegate_to_model not found in tool definitions")
	}
	if statusDef == nil {
		t.Fatal("delegate_to_model_status not found in tool definitions")
	}
	if cancelDef == nil {
		t.Fatal("delegate_to_model_cancel not found in tool definitions")
	}
	if activeCountDef == nil {
		t.Fatal("delegate_to_model_active_count not found in tool definitions")
	}
	if cancelAllDef == nil {
		t.Fatal("delegate_to_model_cancel_all not found in tool definitions")
	}
	if tempCreateDef == nil {
		t.Fatal("temp_file_create not found in tool definitions")
	}
	if tempRemoveDef == nil {
		t.Fatal("temp_file_remove not found in tool definitions")
	}
	schema, _ := delegateDef["inputSchema"].(map[string]interface{})
	if schema == nil {
		t.Fatal("missing inputSchema")
	}
	props, _ := schema["properties"].(map[string]interface{})
	if props == nil {
		t.Fatal("missing properties in inputSchema")
	}
	for _, key := range []string{"prompt", "model", "context", "system_prompt", "cwd", "timeout_seconds"} {
		if props[key] == nil {
			t.Errorf("missing property %q", key)
		}
	}
	modelProp, _ := props["model"].(map[string]interface{})
	if modelProp == nil {
		t.Fatal("model property is not a map")
	}
	if modelProp["enum"] == nil {
		t.Error("model property missing enum")
	}

	statusSchema, _ := statusDef["inputSchema"].(map[string]interface{})
	statusProps, _ := statusSchema["properties"].(map[string]interface{})
	if statusProps["job_id"] == nil || statusProps["after_seq"] == nil || statusProps["max_events"] == nil {
		t.Fatalf("status tool missing expected properties: %#v", statusProps)
	}
	cancelSchema, _ := cancelDef["inputSchema"].(map[string]interface{})
	cancelProps, _ := cancelSchema["properties"].(map[string]interface{})
	if cancelProps["job_id"] == nil {
		t.Fatalf("cancel tool missing job_id property: %#v", cancelProps)
	}
	activeCountSchema, _ := activeCountDef["inputSchema"].(map[string]interface{})
	activeCountProps, _ := activeCountSchema["properties"].(map[string]interface{})
	if activeCountProps["cwd_prefix"] == nil {
		t.Fatalf("active count tool missing cwd_prefix property: %#v", activeCountProps)
	}
	cancelAllSchema, _ := cancelAllDef["inputSchema"].(map[string]interface{})
	cancelAllProps, _ := cancelAllSchema["properties"].(map[string]interface{})
	if cancelAllProps["cwd_prefix"] == nil {
		t.Fatalf("cancel all tool missing cwd_prefix property: %#v", cancelAllProps)
	}
	tempCreateSchema, _ := tempCreateDef["inputSchema"].(map[string]interface{})
	tempCreateProps, _ := tempCreateSchema["properties"].(map[string]interface{})
	if tempCreateProps["prefix"] == nil || tempCreateProps["suffix"] == nil || tempCreateProps["content"] == nil {
		t.Fatalf("temp_file_create missing expected properties: %#v", tempCreateProps)
	}
	tempRemoveSchema, _ := tempRemoveDef["inputSchema"].(map[string]interface{})
	tempRemoveProps, _ := tempRemoveSchema["properties"].(map[string]interface{})
	if tempRemoveProps["path"] == nil {
		t.Fatalf("temp_file_remove missing path property: %#v", tempRemoveProps)
	}
}

func TestTempFileCreateAndRemove(t *testing.T) {
	adapter := canvas.NewAdapter(t.TempDir(), nil)
	s := NewServer(adapter)
	created, err := s.callTool("temp_file_create", map[string]interface{}{
		"prefix":  "spec",
		"suffix":  ".md",
		"content": "# temp",
	})
	if err != nil {
		t.Fatalf("temp_file_create failed: %v", err)
	}
	path, _ := created["path"].(string)
	if !strings.HasPrefix(path, ".tabura/artifacts/tmp/") {
		t.Fatalf("expected temp path under artifacts/tmp, got %q", path)
	}
	absPath := filepath.Join(adapter.ProjectDir(), filepath.FromSlash(path))
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read temp file failed: %v", err)
	}
	if string(data) != "# temp" {
		t.Fatalf("unexpected temp file content: %q", string(data))
	}
	removed, err := s.callTool("temp_file_remove", map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("temp_file_remove failed: %v", err)
	}
	if ok, _ := removed["removed"].(bool); !ok {
		t.Fatalf("expected removed=true, got %#v", removed["removed"])
	}
	if _, err := os.Stat(absPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected temp file removed, stat err=%v", err)
	}
}

func TestTempFileRemoveRejectsOutsideTmp(t *testing.T) {
	projectDir := t.TempDir()
	adapter := canvas.NewAdapter(projectDir, nil)
	s := NewServer(adapter)
	outside := filepath.Join(projectDir, "outside.md")
	if err := os.WriteFile(outside, []byte("x"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	_, err := s.callTool("temp_file_remove", map[string]interface{}{"path": "outside.md"})
	if err == nil {
		t.Fatal("expected temp_file_remove to reject non-temp path")
	}
}

func TestDelegateToModelRequiresAppServer(t *testing.T) {
	adapter := canvas.NewAdapter(t.TempDir(), nil)
	s := NewServer(adapter)
	_, err := s.callTool("delegate_to_model", map[string]interface{}{"prompt": "test"})
	if err == nil {
		t.Fatal("expected error for missing app-server client")
	}
	if !strings.Contains(err.Error(), "app-server client is not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelegateToModelDefaultsToCodex(t *testing.T) {
	got := resolveModelAlias("")
	if got != "gpt-5.3-codex" {
		t.Errorf("empty model should default to codex, got %q", got)
	}
}

func TestDelegateToModelAsyncProgressAndCompletion(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				t.Fatalf("decode message: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"userAgent": "test-client",
					},
				})
			case "initialized":
				// notification; no response
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-delegate"},
					},
				})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-delegate"},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "agentMessage",
							"text": "phase 1",
						},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "fileChange",
							"file": "delegate.txt",
						},
					},
				})
				time.Sleep(40 * time.Millisecond)
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "agentMessage",
							"text": "phase 2 done",
						},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "turn/completed",
					"params": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-delegate", "status": "completed"},
					},
				})
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := appserver.NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	adapter := canvas.NewAdapter(t.TempDir(), nil)
	s := NewServer(adapter, client)

	start, err := s.callTool("delegate_to_model", map[string]interface{}{
		"prompt":          "do work",
		"timeout_seconds": 10,
	})
	if err != nil {
		t.Fatalf("delegate_to_model failed: %v", err)
	}
	jobID, _ := start["job_id"].(string)
	if strings.TrimSpace(jobID) == "" {
		t.Fatalf("missing job_id: %#v", start)
	}

	var status map[string]interface{}
	for i := 0; i < 80; i++ {
		status, err = s.callTool("delegate_to_model_status", map[string]interface{}{"job_id": jobID})
		if err != nil {
			t.Fatalf("status failed: %v", err)
		}
		done, _ := status["done"].(bool)
		if done {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	done, _ := status["done"].(bool)
	if !done {
		t.Fatalf("expected job done, got %#v", status)
	}
	if got := status["status"]; got != delegateStatusCompleted {
		t.Fatalf("expected status=%s, got %#v", delegateStatusCompleted, got)
	}
	if got := strings.TrimSpace(status["message"].(string)); got != "phase 2 done" {
		t.Fatalf("expected final message phase 2 done, got %q", got)
	}
	files, _ := status["files_changed"].([]string)
	if len(files) != 1 || files[0] != "delegate.txt" {
		t.Fatalf("unexpected files_changed: %#v", status["files_changed"])
	}
	afterSeq := 0
	switch v := status["after_seq"].(type) {
	case float64:
		afterSeq = int(v)
	case int:
		afterSeq = v
	case int64:
		afterSeq = int(v)
	}
	next, err := s.callTool("delegate_to_model_status", map[string]interface{}{
		"job_id":    jobID,
		"after_seq": afterSeq,
	})
	if err != nil {
		t.Fatalf("status with after_seq failed: %v", err)
	}
	events, _ := next["events"].([]map[string]interface{})
	if events == nil {
		raw, _ := next["events"].([]interface{})
		if len(raw) != 0 {
			t.Fatalf("expected no new events, got %#v", next["events"])
		}
	} else if len(events) != 0 {
		t.Fatalf("expected no new events, got %#v", events)
	}
}

func TestDelegateToModelCancel(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	turnStarted := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				t.Fatalf("decode message: %v", err)
			}
			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"userAgent": "test-client",
					},
				})
			case "initialized":
				// notification; no response
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{"id": "thread-cancel"},
					},
				})
			case "turn/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{"id": "turn-cancel"},
					},
				})
				select {
				case turnStarted <- struct{}{}:
				default:
				}
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := appserver.NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	adapter := canvas.NewAdapter(t.TempDir(), nil)
	s := NewServer(adapter, client)

	start, err := s.callTool("delegate_to_model", map[string]interface{}{
		"prompt":          "long running work",
		"timeout_seconds": 30,
	})
	if err != nil {
		t.Fatalf("delegate_to_model failed: %v", err)
	}
	jobID, _ := start["job_id"].(string)
	if strings.TrimSpace(jobID) == "" {
		t.Fatalf("missing job_id: %#v", start)
	}

	select {
	case <-turnStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("delegate turn did not start")
	}

	cancelResp, err := s.callTool("delegate_to_model_cancel", map[string]interface{}{
		"job_id": jobID,
	})
	if err != nil {
		t.Fatalf("cancel failed: %v", err)
	}
	canceled, _ := cancelResp["canceled"].(bool)
	if !canceled {
		t.Fatalf("expected canceled=true, got %#v", cancelResp)
	}

	var status map[string]interface{}
	for i := 0; i < 80; i++ {
		status, err = s.callTool("delegate_to_model_status", map[string]interface{}{"job_id": jobID})
		if err != nil {
			t.Fatalf("status failed: %v", err)
		}
		done, _ := status["done"].(bool)
		if done {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	done, _ := status["done"].(bool)
	if !done {
		t.Fatalf("expected canceled job to finish, got %#v", status)
	}
	if got := status["status"]; got != delegateStatusCanceled {
		t.Fatalf("expected status=%s, got %#v", delegateStatusCanceled, got)
	}
}

func TestDelegateToModelActiveCountAndCancelAll(t *testing.T) {
	adapter := canvas.NewAdapter(t.TempDir(), nil)
	s := NewServer(adapter)
	rootA := filepath.Join(t.TempDir(), "proj-a")
	rootB := filepath.Join(t.TempDir(), "proj-b")
	cancelA := 0
	cancelB := 0
	s.delegateJobs["job-a"] = &delegateJob{
		ID:        "job-a",
		Status:    delegateStatusRunning,
		CWD:       filepath.Join(rootA, "sub"),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Cancel:    func() { cancelA++ },
	}
	s.delegateJobs["job-b"] = &delegateJob{
		ID:        "job-b",
		Status:    delegateStatusRunning,
		CWD:       rootB,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Cancel:    func() { cancelB++ },
	}
	s.delegateJobs["job-done"] = &delegateJob{
		ID:         "job-done",
		Status:     delegateStatusCompleted,
		CWD:        rootA,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}

	countAll, err := s.callTool("delegate_to_model_active_count", map[string]interface{}{})
	if err != nil {
		t.Fatalf("active_count all failed: %v", err)
	}
	if got := intFromAny(countAll["active"], -1); got != 2 {
		t.Fatalf("expected active=2, got %v", countAll["active"])
	}

	countA, err := s.callTool("delegate_to_model_active_count", map[string]interface{}{
		"cwd_prefix": rootA,
	})
	if err != nil {
		t.Fatalf("active_count scoped failed: %v", err)
	}
	if got := intFromAny(countA["active"], -1); got != 1 {
		t.Fatalf("expected scoped active=1, got %v", countA["active"])
	}

	cancelScoped, err := s.callTool("delegate_to_model_cancel_all", map[string]interface{}{
		"cwd_prefix": rootA,
	})
	if err != nil {
		t.Fatalf("cancel_all scoped failed: %v", err)
	}
	if got := intFromAny(cancelScoped["canceled"], -1); got != 1 {
		t.Fatalf("expected scoped canceled=1, got %v", cancelScoped["canceled"])
	}
	if cancelA != 1 || cancelB != 0 {
		t.Fatalf("expected cancelA=1 cancelB=0, got %d %d", cancelA, cancelB)
	}
}

func intFromAny(v interface{}, d int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return d
	}
}
