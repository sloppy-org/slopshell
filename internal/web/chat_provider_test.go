package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func TestLocalSystemActionTurnPublishesLocalProviderMetadata(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	if handled := app.tryRunLocalSystemActionTurn(session.ID, session, "clear focus", nil, "", turnOutputModeVoice, false); !handled {
		t.Fatal("expected local system action turn to be handled")
	}

	payload := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "assistant_output")
	if got := strFromAny(payload["provider"]); got != assistantProviderLocal {
		t.Fatalf("provider = %q, want %q", got, assistantProviderLocal)
	}
	if got := strFromAny(payload["provider_label"]); got != "Local" {
		t.Fatalf("provider_label = %q, want Local", got)
	}
	if got := strFromAny(payload["provider_model"]); got != app.localAssistantModelLabel() {
		t.Fatalf("provider_model = %q, want %q", got, app.localAssistantModelLabel())
	}
	if got := intFromAny(payload["provider_latency_ms"], -1); got < 0 {
		t.Fatalf("provider_latency_ms = %d, want >= 0", got)
	}

	messages, err := app.store.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if got := messages[0].Provider; got != assistantProviderLocal {
		t.Fatalf("stored provider = %q, want %q", got, assistantProviderLocal)
	}
	if got := messages[0].ProviderModel; got != app.localAssistantModelLabel() {
		t.Fatalf("stored provider_model = %q, want %q", got, app.localAssistantModelLabel())
	}
}

func TestLocalAssistantTurnHandlesCanvasWriteTextTool(t *testing.T) {
	var mcpCalls atomic.Int32
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mcpCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode mcp payload: %v", err)
		}
		params, _ := payload["params"].(map[string]any)
		if got := strings.TrimSpace(strFromAny(params["name"])); got != "canvas_artifact_show" {
			t.Fatalf("tool name = %q, want canvas_artifact_show", got)
		}
		args, _ := params["arguments"].(map[string]any)
		if got := strings.TrimSpace(strFromAny(args["markdown_or_text"])); got != "Orbit Canvas" {
			t.Fatalf("canvas body = %q, want Orbit Canvas", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"structuredContent": map[string]any{"ok": true},
			},
		})
	}))
	defer mcp.Close()

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm payload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": "Orbit Canvas",
				},
			}},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.assistantMode = assistantModeLocal
	app.assistantLLMURL = llm.URL
	app.localMCPURL = mcp.URL

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	prompt := "Show a text artifact on the canvas titled Tool Test with the exact body Orbit Canvas. Then reply with the single word DONE."
	if _, err := app.store.AddChatMessage(session.ID, "user", prompt, prompt, "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}
	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeVoice})

	actionPayload := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "system_action")
	action, _ := actionPayload["action"].(map[string]any)
	if got := strings.TrimSpace(strFromAny(action["name"])); got != "canvas_artifact_show" {
		t.Fatalf("system action name = %q, want canvas_artifact_show", got)
	}
	payload := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "assistant_output")
	if got := strFromAny(payload["message"]); got != "Shown on canvas." {
		t.Fatalf("assistant message = %q, want Shown on canvas.", got)
	}
	if mcpCalls.Load() != 1 {
		t.Fatalf("mcp call count = %d, want 1", mcpCalls.Load())
	}
}

func TestLocalAssistantTurnListsDirectoryWithWorkspaceReadTool(t *testing.T) {
	app := newAuthedTestApp(t)
	app.assistantMode = assistantModeLocal

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.DirPath, "alpha.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write alpha.txt: %v", err)
	}
	if err := os.Mkdir(filepath.Join(project.DirPath, "docs"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm payload: %v", err)
		}
		messages, _ := payload["messages"].([]any)
		last, _ := messages[len(messages)-1].(map[string]any)
		if strings.Contains(strings.TrimSpace(strFromAny(last["content"])), `"entries"`) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "Top-level entries in this directory: alpha.txt, docs/",
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": `{"tool_calls":[{"name":"workspace_read","arguments":{"operation":"list_top_level"}}]}`,
				},
			}},
		})
	}))
	defer llm.Close()
	app.assistantLLMURL = llm.URL
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	prompt := "What files are in this directory?"
	if _, err := app.store.AddChatMessage(session.ID, "user", prompt, prompt, "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}
	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeVoice})

	payload := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "assistant_output")
	message := strFromAny(payload["message"])
	if !strings.Contains(message, "alpha.txt") || !strings.Contains(message, "docs/") {
		t.Fatalf("assistant message = %q, want directory entries", message)
	}
}

func TestFinalizeAssistantResponseWithMetadataPublishesProviderMetadata(t *testing.T) {
	app := newAuthedTestApp(t)

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	metadata := assistantResponseMetadata{
		Provider:        assistantProviderOpenAI,
		ProviderModel:   "gpt-5.3-codex-spark",
		ProviderLatency: 321,
	}
	response := app.finalizeAssistantResponseWithMetadata(
		session.ID,
		project.WorkspacePath,
		"OpenAI reply.",
		&persistedAssistantID,
		&persistedAssistantText,
		"turn-openai",
		"",
		"thread-openai",
		turnOutputModeVoice,
		metadata,
	)
	if response != "OpenAI reply." {
		t.Fatalf("response = %q, want OpenAI reply.", response)
	}

	payload := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "assistant_output")
	if got := strFromAny(payload["provider"]); got != assistantProviderOpenAI {
		t.Fatalf("provider = %q, want %q", got, assistantProviderOpenAI)
	}
	if got := strFromAny(payload["provider_label"]); got != "Spark" {
		t.Fatalf("provider_label = %q, want Spark", got)
	}
	if got := strFromAny(payload["provider_model"]); got != metadata.ProviderModel {
		t.Fatalf("provider_model = %q, want %q", got, metadata.ProviderModel)
	}
	if got := intFromAny(payload["provider_latency_ms"], -1); got != metadata.ProviderLatency {
		t.Fatalf("provider_latency_ms = %d, want %d", got, metadata.ProviderLatency)
	}

	messages, err := app.store.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if got := messages[0].Provider; got != assistantProviderOpenAI {
		t.Fatalf("stored provider = %q, want %q", got, assistantProviderOpenAI)
	}
	if got := messages[0].ProviderModel; got != metadata.ProviderModel {
		t.Fatalf("stored provider_model = %q, want %q", got, metadata.ProviderModel)
	}
	if got := messages[0].ProviderLatency; got != metadata.ProviderLatency {
		t.Fatalf("stored provider_latency = %d, want %d", got, metadata.ProviderLatency)
	}
}

func TestProviderForAppServerProfileMapsAliasesToNamedResponders(t *testing.T) {
	cases := []struct {
		name    string
		profile appServerModelProfile
		want    string
	}{
		{name: "spark alias", profile: appServerModelProfile{Alias: "spark"}, want: assistantProviderSpark},
		{name: "gpt alias", profile: appServerModelProfile{Alias: "gpt"}, want: assistantProviderGPT},
		{name: "spark model", profile: appServerModelProfile{Model: "gpt-5.3-codex-spark"}, want: assistantProviderSpark},
		{name: "gpt model", profile: appServerModelProfile{Model: "gpt-5.4"}, want: assistantProviderGPT},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := providerForAppServerProfile(tc.profile); got != tc.want {
				t.Fatalf("providerForAppServerProfile() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAssistantProviderDisplayLabelPrefersSpecificResponderName(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		model    string
		want     string
	}{
		{name: "local model", provider: assistantProviderLocal, model: "qwen3.5-9b", want: "Local"},
		{name: "spark from generic openai", provider: assistantProviderOpenAI, model: "gpt-5.3-codex-spark", want: "Spark"},
		{name: "gpt from generic openai", provider: assistantProviderOpenAI, model: "gpt-5.4", want: "GPT"},
		{name: "unknown provider defaults local", provider: "", model: "", want: "Local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := assistantProviderDisplayLabel(tc.provider, tc.model); got != tc.want {
				t.Fatalf("assistantProviderDisplayLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChatSessionHistoryIncludesProviderMetadata(t *testing.T) {
	app := newAuthedTestApp(t)

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	if _, err := app.store.AddChatMessage(
		session.ID,
		"assistant",
		"History reply.",
		"History reply.",
		"markdown",
		store.WithProviderMetadata(assistantProviderOpenAI, "gpt-5.4", 123),
	); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}

	rr := doAuthedRequest(t, app.Router(), http.MethodGet, "/api/chat/sessions/"+session.ID+"/history")
	if rr.Code != http.StatusOK {
		t.Fatalf("GET history status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages payload = %#v, want one message", payload["messages"])
	}
	msg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("message payload = %#v", messages[0])
	}
	if got := strFromAny(msg["provider"]); got != assistantProviderOpenAI {
		t.Fatalf("history provider = %q, want %q", got, assistantProviderOpenAI)
	}
	if got := strFromAny(msg["provider_model"]); got != "gpt-5.4" {
		t.Fatalf("history provider_model = %q, want gpt-5.4", got)
	}
	if got := intFromAny(msg["provider_latency_ms"], -1); got != 123 {
		t.Fatalf("history provider_latency_ms = %d, want 123", got)
	}
}
