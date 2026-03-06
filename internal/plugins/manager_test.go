package plugins

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

func writeManifest(t *testing.T, dir, name string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func TestManagerApplyRewriteAndBlockSequence(t *testing.T) {
	var sawAuth string
	rewriteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = strings.TrimSpace(r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"text":"rewritten message"}`))
	}))
	defer rewriteServer.Close()

	blockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"blocked":true,"reason":"policy block"}`))
	}))
	defer blockServer.Close()

	t.Setenv("TABURA_PLUGIN_TEST_SECRET", "abc123")
	dir := t.TempDir()
	writeManifest(t, dir, "01-rewrite.json", map[string]any{
		"id":         "rewrite",
		"kind":       "webhook",
		"endpoint":   rewriteServer.URL,
		"hooks":      []string{HookChatPreUserMessage},
		"enabled":    true,
		"secret_env": "TABURA_PLUGIN_TEST_SECRET",
	})
	writeManifest(t, dir, "02-block.json", map[string]any{
		"id":       "blocker",
		"kind":     "webhook",
		"endpoint": blockServer.URL,
		"hooks":    []string{HookChatPreUserMessage},
		"enabled":  true,
	})

	mgr, err := New(Options{Dir: dir})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	got := mgr.Apply(context.Background(), HookRequest{
		Hook: HookChatPreUserMessage,
		Text: "hello",
	})
	if !got.Blocked {
		t.Fatalf("expected blocked=true")
	}
	if got.Reason != "policy block" {
		t.Fatalf("blocked reason = %q, want %q", got.Reason, "policy block")
	}
	if got.Text != "rewritten message" {
		t.Fatalf("text = %q, want %q", got.Text, "rewritten message")
	}
	if sawAuth != "Bearer abc123" {
		t.Fatalf("authorization header = %q, want %q", sawAuth, "Bearer abc123")
	}
}

func TestManagerApplyContinuesAfterPluginHTTPError(t *testing.T) {
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusBadGateway)
	}))
	defer errServer.Close()

	rewriteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"second plugin output"}`))
	}))
	defer rewriteServer.Close()

	dir := t.TempDir()
	writeManifest(t, dir, "01-error.json", map[string]any{
		"id":       "erroring",
		"kind":     "webhook",
		"endpoint": errServer.URL,
		"hooks":    []string{HookChatPreAssistantPrompt},
		"enabled":  true,
	})
	writeManifest(t, dir, "02-rewrite.json", map[string]any{
		"id":       "rewrite",
		"kind":     "webhook",
		"endpoint": rewriteServer.URL,
		"hooks":    []string{HookChatPreAssistantPrompt},
		"enabled":  true,
	})

	mgr, err := New(Options{Dir: dir})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	got := mgr.Apply(context.Background(), HookRequest{
		Hook: HookChatPreAssistantPrompt,
		Text: "original",
	})
	if got.Blocked {
		t.Fatalf("expected blocked=false")
	}
	if got.Text != "second plugin output" {
		t.Fatalf("text = %q, want %q", got.Text, "second plugin output")
	}
}

func TestManagerDecideMeetingPartnerFromNestedPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"meeting_partner":{"decision":"respond","response_text":"I can help with that.","channel":"voice","urgency":"normal"}}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	writeManifest(t, dir, "meeting.json", map[string]any{
		"id":       "meeting-partner",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{HookMeetingPartnerDecide},
		"enabled":  true,
	})
	mgr, err := New(Options{Dir: dir})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	decision, ok := mgr.DecideMeetingPartner(context.Background(), HookRequest{
		Hook: HookMeetingPartnerDecide,
		Text: "Can you summarize the last point?",
		Metadata: map[string]interface{}{
			"mode": "meeting_notes",
		},
	})
	if !ok {
		t.Fatalf("expected meeting partner decision")
	}
	if decision.PluginID != "meeting-partner" {
		t.Fatalf("plugin_id = %q, want %q", decision.PluginID, "meeting-partner")
	}
	if decision.Decision != "respond" {
		t.Fatalf("decision = %q, want %q", decision.Decision, "respond")
	}
	if decision.ResponseText != "I can help with that." {
		t.Fatalf("response_text = %q, want %q", decision.ResponseText, "I can help with that.")
	}
}

func TestManagerDecideMeetingPartnerFromTopLevelPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"decision":"action","action":{"type":"create_task","title":"follow up"}}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	writeManifest(t, dir, "meeting.json", map[string]any{
		"id":       "meeting-partner",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{HookMeetingPartnerDecide},
		"enabled":  true,
	})
	mgr, err := New(Options{Dir: dir})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	decision, ok := mgr.DecideMeetingPartner(context.Background(), HookRequest{
		Hook: HookMeetingPartnerDecide,
		Text: "Please create a task for this follow-up.",
	})
	if !ok {
		t.Fatalf("expected meeting partner decision")
	}
	if decision.Decision != "action" {
		t.Fatalf("decision = %q, want %q", decision.Decision, "action")
	}
	if got := strings.TrimSpace(fmt.Sprint(decision.Action["type"])); got != "create_task" {
		t.Fatalf("action.type = %q, want %q", got, "create_task")
	}
}

func TestManagerDecideMeetingPartnerSkipsInvalidDecision(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"decision":"maybe"}`))
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"decision":"noop"}`))
	}))
	defer second.Close()

	dir := t.TempDir()
	writeManifest(t, dir, "01-invalid.json", map[string]any{
		"id":       "invalid",
		"kind":     "webhook",
		"endpoint": first.URL,
		"hooks":    []string{HookMeetingPartnerDecide},
		"enabled":  true,
	})
	writeManifest(t, dir, "02-noop.json", map[string]any{
		"id":       "noop",
		"kind":     "webhook",
		"endpoint": second.URL,
		"hooks":    []string{HookMeetingPartnerDecide},
		"enabled":  true,
	})
	mgr, err := New(Options{Dir: dir})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	decision, ok := mgr.DecideMeetingPartner(context.Background(), HookRequest{
		Hook: HookMeetingPartnerDecide,
	})
	if !ok {
		t.Fatalf("expected meeting partner decision")
	}
	if decision.Decision != "noop" {
		t.Fatalf("decision = %q, want %q", decision.Decision, "noop")
	}
	if decision.PluginID != "noop" {
		t.Fatalf("plugin_id = %q, want %q", decision.PluginID, "noop")
	}
}

func TestManagerIgnoresExtensionManifestFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"plugin response"}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	writeManifest(t, dir, "01-plugin.json", map[string]any{
		"id":       "plugin-only",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{HookChatPreUserMessage},
		"enabled":  true,
	})
	writeManifest(t, dir, "02-ignored.extension.json", map[string]any{
		"id":       "extension-ignored",
		"version":  "1.0.0",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{HookChatPreUserMessage},
		"enabled":  true,
	})

	mgr, err := New(Options{Dir: dir})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if got := mgr.Count(); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
	list := mgr.List()
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	if got := strings.TrimSpace(list[0].ID); got != "plugin-only" {
		t.Fatalf("plugin id = %q, want %q", got, "plugin-only")
	}
}
