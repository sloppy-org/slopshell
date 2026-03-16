package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/krystophny/tabura/internal/canvas"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestToolDefinitionsEmitsProperties(t *testing.T) {
	defs := toolDefinitions()
	var tempCreateDef map[string]interface{}
	var tempRemoveDef map[string]interface{}
	var calendarEventsDef map[string]interface{}
	for _, d := range defs {
		switch d["name"] {
		case "temp_file_create":
			tempCreateDef = d
		case "temp_file_remove":
			tempRemoveDef = d
		case "calendar_events":
			calendarEventsDef = d
		}
	}
	if tempCreateDef == nil {
		t.Fatal("temp_file_create not found in tool definitions")
	}
	if tempRemoveDef == nil {
		t.Fatal("temp_file_remove not found in tool definitions")
	}
	if calendarEventsDef == nil {
		t.Fatal("calendar_events not found in tool definitions")
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
	calendarSchema, _ := calendarEventsDef["inputSchema"].(map[string]interface{})
	calendarProps, _ := calendarSchema["properties"].(map[string]interface{})
	if calendarProps["calendar_id"] == nil || calendarProps["days"] == nil || calendarProps["limit"] == nil || calendarProps["query"] == nil {
		t.Fatalf("calendar_events missing expected properties: %#v", calendarProps)
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
