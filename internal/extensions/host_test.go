package extensions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeExtensionManifest(t *testing.T, dir, name string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func TestHostApplyRewriteAndBlockSequence(t *testing.T) {
	rewriteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"rewritten message"}`))
	}))
	defer rewriteServer.Close()

	blockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"blocked":true,"reason":"policy block"}`))
	}))
	defer blockServer.Close()

	dir := t.TempDir()
	writeExtensionManifest(t, dir, "01-rewrite.extension.json", map[string]any{
		"id":       "rewrite",
		"version":  "1.0.0",
		"kind":     "webhook",
		"endpoint": rewriteServer.URL,
		"hooks":    []string{HookChatPreUserMessage},
		"enabled":  true,
	})
	writeExtensionManifest(t, dir, "02-block.extension.json", map[string]any{
		"id":       "block",
		"version":  "1.0.0",
		"kind":     "webhook",
		"endpoint": blockServer.URL,
		"hooks":    []string{HookChatPreUserMessage},
		"enabled":  true,
	})

	host, err := New(Options{Dir: dir, RuntimeVersion: "0.1.6"})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	result := host.Apply(context.Background(), HookRequest{Hook: HookChatPreUserMessage, Text: "hello"})
	if !result.Blocked {
		t.Fatalf("expected blocked")
	}
	if got := strings.TrimSpace(result.Reason); got != "policy block" {
		t.Fatalf("reason=%q", got)
	}
	if got := strings.TrimSpace(result.Text); got != "rewritten message" {
		t.Fatalf("text=%q", got)
	}
}

func TestHostEngineVersionCompatibility(t *testing.T) {
	dir := t.TempDir()
	writeExtensionManifest(t, dir, "incompatible.extension.json", map[string]any{
		"id":       "future",
		"version":  "1.0.0",
		"kind":     "webhook",
		"endpoint": "http://127.0.0.1:12345",
		"hooks":    []string{HookChatPreAssistantPrompt},
		"enabled":  true,
		"engine": map[string]any{
			"tabura": ">=9.9.9",
		},
	})
	_, err := New(Options{Dir: dir, RuntimeVersion: "0.1.6"})
	if err == nil {
		t.Fatalf("expected incompatibility error")
	}
	if !strings.Contains(err.Error(), "not compatible") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHostExecuteCommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req HookRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		commandID := strings.TrimSpace(fmt.Sprint(req.Metadata["command_id"]))
		if commandID == "" {
			http.Error(w, "missing command_id", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"command":{"success":true,"message":"executed"}}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	writeExtensionManifest(t, dir, "commands.extension.json", map[string]any{
		"id":       "meeting-partner",
		"version":  "1.1.0",
		"kind":     "webhook",
		"endpoint": server.URL,
		"enabled":  true,
		"commands": []map[string]any{{
			"id":          "meeting_partner.respond",
			"title":       "Respond",
			"description": "Respond to participant",
		}},
	})

	host, err := New(Options{Dir: dir, RuntimeVersion: "0.1.6"})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	result, err := host.ExecuteCommand(context.Background(), CommandRequest{
		CommandID: "meeting_partner.respond",
		SessionID: "session-1",
		Metadata: map[string]interface{}{
			"source": "test",
		},
	})
	if err != nil {
		t.Fatalf("execute command: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success")
	}
	if got := strings.TrimSpace(result.CommandID); got != "meeting_partner.respond" {
		t.Fatalf("command_id=%q", got)
	}
	if got := strings.TrimSpace(result.ExtensionID); got != "meeting-partner" {
		t.Fatalf("extension_id=%q", got)
	}
}

func TestHostDecideMeetingPartner(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"meeting_partner":{"decision":"respond","response_text":"Summary is ready."}}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	writeExtensionManifest(t, dir, "meeting.extension.json", map[string]any{
		"id":       "meeting",
		"version":  "1.0.0",
		"kind":     "webhook",
		"endpoint": server.URL,
		"enabled":  true,
		"hooks":    []string{HookMeetingPartnerDecide},
	})

	host, err := New(Options{Dir: dir, RuntimeVersion: "0.1.6"})
	if err != nil {
		t.Fatalf("new host: %v", err)
	}
	decision, ok := host.DecideMeetingPartner(context.Background(), HookRequest{Hook: HookMeetingPartnerDecide})
	if !ok {
		t.Fatalf("expected decision")
	}
	if decision.Decision != "respond" {
		t.Fatalf("decision=%q", decision.Decision)
	}
	if decision.PluginID != "meeting" {
		t.Fatalf("plugin_id=%q", decision.PluginID)
	}
}
