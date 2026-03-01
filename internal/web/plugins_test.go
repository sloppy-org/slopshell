package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePluginManifest(t *testing.T, dir, name string, payload map[string]any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func newAuthedTestAppWithPluginDir(t *testing.T, pluginDir string) *App {
	t.Helper()
	t.Setenv("TABURA_PLUGINS_DIR", pluginDir)
	return newAuthedTestApp(t)
}

func newAuthedTestAppWithExtensionDir(t *testing.T, extensionDir string) *App {
	t.Helper()
	t.Setenv("TABURA_EXTENSIONS_DIR", extensionDir)
	return newAuthedTestApp(t)
}

func TestRuntimeAndPluginInventoryEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer server.Close()

	pluginDir := t.TempDir()
	writePluginManifest(t, pluginDir, "rewrite.json", map[string]any{
		"id":       "rewrite",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{"chat.pre_user_message"},
		"enabled":  true,
	})
	app := newAuthedTestAppWithPluginDir(t, pluginDir)
	handler := app.Router()

	rrRuntime := doAuthedJSONRequest(t, handler, http.MethodGet, "/api/runtime", nil)
	if rrRuntime.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", rrRuntime.Code, rrRuntime.Body.String())
	}
	var runtimePayload map[string]any
	if err := json.Unmarshal(rrRuntime.Body.Bytes(), &runtimePayload); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if got := intFromAny(runtimePayload["plugins_loaded"], -1); got != 1 {
		t.Fatalf("plugins_loaded=%d, want 1", got)
	}
	if gotDir := strings.TrimSpace(strFromAny(runtimePayload["plugins_dir"])); gotDir != pluginDir {
		t.Fatalf("plugins_dir=%q, want %q", gotDir, pluginDir)
	}

	rrPlugins := doAuthedJSONRequest(t, handler, http.MethodGet, "/api/plugins", nil)
	if rrPlugins.Code != http.StatusOK {
		t.Fatalf("plugins status=%d body=%s", rrPlugins.Code, rrPlugins.Body.String())
	}
	var pluginsPayload struct {
		OK      bool `json:"ok"`
		Dir     string
		Count   int `json:"count"`
		Plugins []struct {
			ID string `json:"id"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(rrPlugins.Body.Bytes(), &pluginsPayload); err != nil {
		t.Fatalf("decode plugins response: %v", err)
	}
	if !pluginsPayload.OK {
		t.Fatalf("expected ok=true")
	}
	if pluginsPayload.Count != 1 {
		t.Fatalf("count=%d, want 1", pluginsPayload.Count)
	}
	if len(pluginsPayload.Plugins) != 1 || strings.TrimSpace(pluginsPayload.Plugins[0].ID) != "rewrite" {
		t.Fatalf("unexpected plugins payload: %+v", pluginsPayload.Plugins)
	}
}

func TestHandleChatSessionMessagePreUserPluginRewrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"rewritten by plugin"}`))
	}))
	defer server.Close()

	pluginDir := t.TempDir()
	writePluginManifest(t, pluginDir, "rewrite.json", map[string]any{
		"id":       "rewrite",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{"chat.pre_user_message"},
		"enabled":  true,
	})
	app := newAuthedTestAppWithPluginDir(t, pluginDir)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/chat/sessions/"+session.ID+"/messages",
		map[string]any{"text": "hello world"},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	messages, err := app.store.ListChatMessages(session.ID, 20)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	found := false
	for _, msg := range messages {
		if msg.Role == "user" && strings.TrimSpace(msg.ContentPlain) == "rewritten by plugin" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected rewritten user message in history")
	}
}

func TestHandleChatSessionMessagePreUserPluginBlock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"blocked":true,"reason":"blocked by policy"}`))
	}))
	defer server.Close()

	pluginDir := t.TempDir()
	writePluginManifest(t, pluginDir, "block.json", map[string]any{
		"id":       "block",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{"chat.pre_user_message"},
		"enabled":  true,
	})
	app := newAuthedTestAppWithPluginDir(t, pluginDir)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/chat/sessions/"+session.ID+"/messages",
		map[string]any{"text": "hello world"},
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "blocked by policy") {
		t.Fatalf("expected policy reason, body=%q", rr.Body.String())
	}
	messages, err := app.store.ListChatMessages(session.ID, 20)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	for _, msg := range messages {
		if msg.Role == "user" {
			t.Fatalf("did not expect persisted user message on blocked request")
		}
	}
}

func TestFinalizeAssistantResponseAppliesPostPluginHook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"post-processed output"}`))
	}))
	defer server.Close()

	pluginDir := t.TempDir()
	writePluginManifest(t, pluginDir, "post.json", map[string]any{
		"id":       "post",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{"chat.post_assistant_response"},
		"enabled":  true,
	})
	app := newAuthedTestAppWithPluginDir(t, pluginDir)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	var persistedID int64
	var persistedText string
	got := app.finalizeAssistantResponse(
		session.ID,
		project.ProjectKey,
		"original output",
		&persistedID,
		&persistedText,
		"turn1",
		"",
		"thread1",
		turnOutputModeVoice,
	)
	if strings.TrimSpace(got) != "post-processed output" {
		t.Fatalf("finalized message=%q, want %q", got, "post-processed output")
	}
	if persistedID == 0 {
		t.Fatalf("expected persisted assistant id")
	}
	messages, err := app.store.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	found := false
	for _, msg := range messages {
		if msg.Role == "assistant" && strings.TrimSpace(msg.ContentPlain) == "post-processed output" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected post-processed assistant message in history")
	}
}

func TestApplyPreAssistantPromptHookBlocked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"blocked":true,"reason":"prompt blocked"}`))
	}))
	defer server.Close()

	pluginDir := t.TempDir()
	writePluginManifest(t, pluginDir, "pre.json", map[string]any{
		"id":       "pre",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{"chat.pre_assistant_prompt"},
		"enabled":  true,
	})
	app := newAuthedTestAppWithPluginDir(t, pluginDir)
	_, err := app.applyPreAssistantPromptHook(context.Background(), "s1", "p1", turnOutputModeVoice, "chat", "hello")
	if err == nil {
		t.Fatalf("expected blocked error")
	}
	if !strings.Contains(err.Error(), "prompt blocked") {
		t.Fatalf("error=%q, want contains %q", err.Error(), "prompt blocked")
	}
}

func TestMeetingPartnerDecideEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"meeting_partner":{"decision":"respond","response_text":"Let me summarize.","channel":"voice","urgency":"normal"}}`))
	}))
	defer server.Close()

	pluginDir := t.TempDir()
	writePluginManifest(t, pluginDir, "meeting.json", map[string]any{
		"id":       "meeting-partner",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{"meeting_partner.decide"},
		"enabled":  true,
	})
	app := newAuthedTestAppWithPluginDir(t, pluginDir)
	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/plugins/meeting-partner/decide",
		map[string]any{
			"session_id":  "s1",
			"project_key": "p1",
			"text":        "Could you summarize that?",
			"metadata": map[string]any{
				"source": "meeting_notes",
			},
		},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		OK       bool `json:"ok"`
		Matched  bool `json:"matched"`
		Decision struct {
			Decision     string `json:"decision"`
			ResponseText string `json:"response_text"`
			PluginID     string `json:"plugin_id"`
		} `json:"decision"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK || !payload.Matched {
		t.Fatalf("expected ok+matched true, got %+v", payload)
	}
	if payload.Decision.Decision != "respond" {
		t.Fatalf("decision=%q, want %q", payload.Decision.Decision, "respond")
	}
	if payload.Decision.ResponseText != "Let me summarize." {
		t.Fatalf("response_text=%q, want %q", payload.Decision.ResponseText, "Let me summarize.")
	}
	if payload.Decision.PluginID != "meeting-partner" {
		t.Fatalf("plugin_id=%q, want %q", payload.Decision.PluginID, "meeting-partner")
	}
}

func TestExtensionsInventoryEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	}))
	defer server.Close()

	extensionDir := t.TempDir()
	writePluginManifest(t, extensionDir, "meeting.extension.json", map[string]any{
		"id":       "meeting-partner",
		"version":  "1.0.0",
		"kind":     "webhook",
		"endpoint": server.URL,
		"hooks":    []string{"meeting_partner.decide"},
		"enabled":  true,
	})
	app := newAuthedTestAppWithExtensionDir(t, extensionDir)
	handler := app.Router()

	rrRuntime := doAuthedJSONRequest(t, handler, http.MethodGet, "/api/runtime", nil)
	if rrRuntime.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", rrRuntime.Code, rrRuntime.Body.String())
	}
	var runtimePayload map[string]any
	if err := json.Unmarshal(rrRuntime.Body.Bytes(), &runtimePayload); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if got := intFromAny(runtimePayload["extensions_loaded"], -1); got != 1 {
		t.Fatalf("extensions_loaded=%d, want 1", got)
	}
	if gotDir := strings.TrimSpace(strFromAny(runtimePayload["extensions_dir"])); gotDir != extensionDir {
		t.Fatalf("extensions_dir=%q, want %q", gotDir, extensionDir)
	}

	rrExtensions := doAuthedJSONRequest(t, handler, http.MethodGet, "/api/extensions", nil)
	if rrExtensions.Code != http.StatusOK {
		t.Fatalf("extensions status=%d body=%s", rrExtensions.Code, rrExtensions.Body.String())
	}
	var extensionsPayload struct {
		OK         bool `json:"ok"`
		Dir        string
		Count      int `json:"count"`
		Extensions []struct {
			ID string `json:"id"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(rrExtensions.Body.Bytes(), &extensionsPayload); err != nil {
		t.Fatalf("decode extensions response: %v", err)
	}
	if !extensionsPayload.OK {
		t.Fatalf("expected ok=true")
	}
	if extensionsPayload.Count != 1 {
		t.Fatalf("count=%d, want 1", extensionsPayload.Count)
	}
	if len(extensionsPayload.Extensions) != 1 || strings.TrimSpace(extensionsPayload.Extensions[0].ID) != "meeting-partner" {
		t.Fatalf("unexpected extensions payload: %+v", extensionsPayload.Extensions)
	}
}

func TestExtensionCommandExecuteEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"command":{"success":true,"message":"executed"}}`))
	}))
	defer server.Close()

	extensionDir := t.TempDir()
	writePluginManifest(t, extensionDir, "commands.extension.json", map[string]any{
		"id":       "meeting-partner",
		"version":  "1.0.0",
		"kind":     "webhook",
		"endpoint": server.URL,
		"enabled":  true,
		"commands": []map[string]any{
			{
				"id":          "meeting_partner.respond",
				"title":       "Respond",
				"description": "Respond in meeting mode",
			},
		},
	})
	app := newAuthedTestAppWithExtensionDir(t, extensionDir)
	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/extensions/commands/meeting_partner.respond",
		map[string]any{
			"session_id": "s1",
			"text":       "hello",
		},
	)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		OK     bool `json:"ok"`
		Result struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK || !payload.Result.Success {
		t.Fatalf("expected success payload, got %+v", payload)
	}
}
