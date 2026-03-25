package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssistantBackendForTurnRoutesLocalByDefaultAndCodexOnlyForRemoteTurns(t *testing.T) {
	wsServer := setupMockAppServerStatusServer(t, "codex")
	defer wsServer.Close()
	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")

	app, err := New(t.TempDir(), "", "", wsURL, "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.assistantLLMURL = "http://127.0.0.1:8081"
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	localReq := &assistantTurnRequest{
		userText:    "Wann wurde Isaac Newton geboren?",
		baseProfile: appServerModelProfile{Alias: "local"},
	}
	if got := app.assistantBackendForTurn(localReq).mode(); got != assistantModeLocal {
		t.Fatalf("backend for local default turn = %q, want %q", got, assistantModeLocal)
	}

	explicitRemoteReq := &assistantTurnRequest{
		userText:    "let gpt answer this",
		turnModel:   "gpt",
		baseProfile: appServerModelProfile{Alias: "local"},
	}
	if got := app.assistantBackendForTurn(explicitRemoteReq).mode(); got != assistantModeCodex {
		t.Fatalf("backend for explicit remote turn = %q, want %q", got, assistantModeCodex)
	}

	searchReq := &assistantTurnRequest{
		userText:    "search the web for today's news",
		searchTurn:  true,
		baseProfile: appServerModelProfile{Alias: "local"},
	}
	if got := app.assistantBackendForTurn(searchReq).mode(); got != assistantModeLocal {
		t.Fatalf("backend for local search turn = %q, want %q", got, assistantModeLocal)
	}

	app.assistantLLMURL = ""
	app.intentLLMURL = ""
	if got := app.assistantBackendForTurn(localReq).mode(); got != assistantModeCodex {
		t.Fatalf("backend without local assistant config = %q, want %q", got, assistantModeCodex)
	}
}

func TestParseLocalAssistantDecisionParsesNativeToolCalls(t *testing.T) {
	decision, err := parseLocalAssistantDecision(localIntentLLMMessage{
		ToolCalls: []localAssistantLLMToolCall{{
			ID:   "call-shell",
			Type: "function",
			Function: localAssistantLLMFunctionCall{
				Name:      "shell",
				Arguments: `{"command":"printf 'hi'"}`,
			},
		}},
	})
	if err != nil {
		t.Fatalf("parseLocalAssistantDecision() error: %v", err)
	}
	if len(decision.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(decision.ToolCalls))
	}
	if got := decision.ToolCalls[0].Name; got != "shell" {
		t.Fatalf("tool call name = %q, want shell", got)
	}
	if got := strings.TrimSpace(decision.ToolCalls[0].Arguments["command"].(string)); got != "printf 'hi'" {
		t.Fatalf("tool call command = %q, want printf 'hi'", got)
	}
}

func TestParseLocalAssistantDecisionParsesJSONToolEnvelope(t *testing.T) {
	decision, err := parseLocalAssistantDecision(localIntentLLMMessage{
		Content: `{"tool_calls":[{"name":"mcp__canvas_artifact_show","arguments":{"kind":"text","title":"Tool Test","markdown_or_text":"Orbit Canvas"}}]}`,
	})
	if err != nil {
		t.Fatalf("parseLocalAssistantDecision() error: %v", err)
	}
	if len(decision.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(decision.ToolCalls))
	}
	call := decision.ToolCalls[0]
	if call.Name != "mcp__canvas_artifact_show" {
		t.Fatalf("tool call name = %q, want mcp__canvas_artifact_show", call.Name)
	}
	if got := strings.TrimSpace(strFromAny(call.Arguments["kind"])); got != "text" {
		t.Fatalf("tool kind = %q, want text", got)
	}
	if got := strings.TrimSpace(strFromAny(call.Arguments["title"])); got != "Tool Test" {
		t.Fatalf("tool title = %q, want Tool Test", got)
	}
	if got := strings.TrimSpace(strFromAny(call.Arguments["markdown_or_text"])); got != "Orbit Canvas" {
		t.Fatalf("tool body = %q, want Orbit Canvas", got)
	}
}

func TestExecuteLocalAssistantShellToolTracksWorkingDirectory(t *testing.T) {
	workspaceDir := t.TempDir()
	subdir := workspaceDir + "/nested"
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	state := localAssistantTurnState{
		workspaceDir: workspaceDir,
		currentDir:   workspaceDir,
	}

	result := executeLocalAssistantShellTool(&state, localAssistantToolCall{
		ID:   "call-shell",
		Name: "shell",
		Arguments: map[string]any{
			"command": "cd nested && pwd",
		},
	})
	if result.IsError {
		t.Fatalf("shell tool returned error: %+v", result)
	}
	if got := strings.TrimSpace(result.Output); got != subdir {
		t.Fatalf("shell output = %q, want %q", got, subdir)
	}
	if got := state.currentDir; got != subdir {
		t.Fatalf("state currentDir = %q, want %q", got, subdir)
	}
}

func TestBuildLocalAssistantToolCatalogUsesSmallCanvasFamily(t *testing.T) {
	app := newAuthedTestApp(t)
	state := localAssistantTurnState{
		sessionID:    "local-session",
		canvasID:     "canvas-session",
		workspaceDir: t.TempDir(),
	}
	catalog, err := app.buildLocalAssistantToolCatalog(state, localAssistantToolFamilyCanvas, "Draw a flowchart on the canvas.")
	if err != nil {
		t.Fatalf("buildLocalAssistantToolCatalog() error: %v", err)
	}
	if !catalog.RenderGeneratedText {
		t.Fatal("canvas family should direct-render generated canvas requests")
	}
	for _, name := range []string{"workspace_read", "action__open_file_canvas", "action__navigate_canvas"} {
		if _, ok := catalog.ToolsByName[name]; !ok {
			t.Fatalf("missing canvas family tool %q: %#v", name, catalog.ToolsByName)
		}
	}
	if _, ok := catalog.ToolsByName["canvas_write_text"]; ok {
		t.Fatalf("canvas family should not expose canvas_write_text directly: %#v", catalog.ToolsByName)
	}
	if len(catalog.ToolsByName) != 3 {
		t.Fatalf("canvas family tool count = %d, want 3", len(catalog.ToolsByName))
	}
}

func TestParseLocalAssistantDecisionAcceptsBareToolCallObject(t *testing.T) {
	decision, err := parseLocalAssistantDecision(localIntentLLMMessage{
		Content: `{"name":"canvas_write_text","arguments":{"title":"Fusion","content":"[Fusion]\n  |\n[Plasma]"}}`,
	})
	if err != nil {
		t.Fatalf("parseLocalAssistantDecision() error: %v", err)
	}
	if len(decision.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(decision.ToolCalls))
	}
	if got := decision.ToolCalls[0].Name; got != "canvas_write_text" {
		t.Fatalf("tool call name = %q, want canvas_write_text", got)
	}
	if got := strings.TrimSpace(strFromAny(decision.ToolCalls[0].Arguments["title"])); got != "Fusion" {
		t.Fatalf("tool call title = %q, want Fusion", got)
	}
}

func TestRunAssistantTurnCanvasRequestRepairsPlainGermanAcknowledgement(t *testing.T) {
	var llmCalls atomic.Int32
	var canvasCalls atomic.Int32

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode mcp payload: %v", err)
		}
		switch strings.TrimSpace(strFromAny(payload["method"])) {
		case "tools/call":
			canvasCalls.Add(1)
			params, _ := payload["params"].(map[string]any)
			if got := strings.TrimSpace(strFromAny(params["name"])); got != "canvas_artifact_show" {
				t.Fatalf("canvas tool name = %q, want canvas_artifact_show", got)
			}
			args, _ := params["arguments"].(map[string]any)
			content := strings.TrimSpace(strFromAny(args["markdown_or_text"]))
			if !strings.Contains(strings.ToLower(content), "fusion") && !strings.Contains(strings.ToLower(content), "reaktor") {
				t.Fatalf("canvas content = %q, want fusion-reactor text artifact", content)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"structuredContent": map[string]any{
						"ok": true,
					},
				},
			})
		default:
			t.Fatalf("unexpected MCP method %q", payload["method"])
		}
	}))
	defer mcp.Close()

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := llmCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm payload: %v", err)
		}
		messages, _ := payload["messages"].([]any)
		switch call {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "Ich erstelle jetzt ein Flussdiagramm, das den Ablauf der Kernfusion visualisiert.",
					},
				}},
			})
		case 2:
			last, _ := messages[len(messages)-1].(map[string]any)
			if got := strings.TrimSpace(strFromAny(last["content"])); got != localAssistantCanvasContentRequiredPrompt() {
				t.Fatalf("canvas-content prompt = %q, want %q", got, localAssistantCanvasContentRequiredPrompt())
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "[Fusion Reactor]\n  |\n[Fuel Injection]\n  |\n[Plasma]\n  |\n[Heat Extraction]\n  |\n[Electricity]",
					},
				}},
			})
		default:
			t.Fatalf("unexpected extra llm call %d", call)
		}
	}))
	defer llm.Close()

	app, err := New(t.TempDir(), "", mcp.URL, "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.assistantMode = assistantModeLocal
	app.assistantLLMURL = llm.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.chatSessionForWorkspace(project)
	if err != nil {
		t.Fatalf("chatSessionForWorkspace: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "Bitte zeichne mir wie ein Fusionsreaktor funktioniert als Flowchart auf der Canvas.", "Bitte zeichne mir wie ein Fusionsreaktor funktioniert als Flowchart auf der Canvas.", "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Shown on canvas." {
		t.Fatalf("assistant message = %q", got)
	}
	if llmCalls.Load() != 2 {
		t.Fatalf("llm call count = %d, want 2", llmCalls.Load())
	}
	if canvasCalls.Load() != 1 {
		t.Fatalf("canvas call count = %d, want 1", canvasCalls.Load())
	}
}

func TestExecuteLocalAssistantBoundMCPToolUsesCanvasTunnelForCanvasTools(t *testing.T) {
	var listCalls atomic.Int32
	var canvasCalls atomic.Int32
	canvasMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode canvas mcp payload: %v", err)
		}
		switch strings.TrimSpace(strFromAny(payload["method"])) {
		case "tools/call":
			canvasCalls.Add(1)
			params, _ := payload["params"].(map[string]any)
			if got := strings.TrimSpace(strFromAny(params["name"])); got != "canvas_artifact_show" {
				t.Fatalf("canvas tool name = %q, want canvas_artifact_show", got)
			}
			args, _ := params["arguments"].(map[string]any)
			if got := strings.TrimSpace(strFromAny(args["session_id"])); got != "canvas-session" {
				t.Fatalf("canvas session_id = %q, want canvas-session", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"structuredContent": map[string]any{
						"ok": true,
					},
				},
			})
		default:
			t.Fatalf("unexpected canvas MCP method %q", payload["method"])
		}
	}))
	defer canvasMCP.Close()

	generalMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode general mcp payload: %v", err)
		}
		switch strings.TrimSpace(strFromAny(payload["method"])) {
		case "tools/list":
			listCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "canvas_artifact_show",
						"description": "Show one artifact kind in canvas.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"session_id": map[string]any{"type": "string"},
								"kind":       map[string]any{"type": "string"},
							},
							"required": []string{"session_id", "kind"},
						},
					}},
				},
			})
		case "tools/call":
			t.Fatalf("canvas tools should not call the general MCP endpoint")
		default:
			t.Fatalf("unexpected general MCP method %q", payload["method"])
		}
	}))
	defer generalMCP.Close()

	app := newAuthedTestApp(t)
	port, err := extractPort(canvasMCP.URL)
	if err != nil {
		t.Fatalf("extractPort(canvasMCP): %v", err)
	}
	app.tunnels.setPort("canvas-session", port)
	state := localAssistantTurnState{
		canvasID:     "canvas-session",
		workspaceDir: t.TempDir(),
		mcpURL:       generalMCP.URL,
	}
	catalog := localAssistantToolCatalog{
		Family:      localAssistantToolFamilyCanvas,
		Definitions: []map[string]any{localAssistantCanvasWriteTextTool(state).Definition},
		ToolsByName: map[string]localAssistantExecutableTool{
			"canvas_write_text": localAssistantCanvasWriteTextTool(state),
		},
	}
	result, err := app.executeLocalAssistantToolCall(context.Background(), &state, catalog, localAssistantToolCall{
		ID:   "call-canvas",
		Name: "canvas_write_text",
		Arguments: map[string]any{
			"title":   "Tool Test",
			"content": "Orbit Canvas",
		},
	})
	if err != nil {
		t.Fatalf("executeLocalAssistantToolCall() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("canvas tool returned error: %+v", result)
	}
	if listCalls.Load() != 0 {
		t.Fatalf("general MCP list count = %d, want 0", listCalls.Load())
	}
	if canvasCalls.Load() != 1 {
		t.Fatalf("canvas MCP call count = %d, want 1", canvasCalls.Load())
	}
}

func TestAssistantLLMRequestTimeoutUsesEnvOverride(t *testing.T) {
	t.Setenv("TABURA_ASSISTANT_LLM_TIMEOUT", "")
	if got := assistantLLMRequestTimeout(); got != defaultAssistantLLMTimeout {
		t.Fatalf("assistantLLMRequestTimeout() default = %s, want %s", got, defaultAssistantLLMTimeout)
	}

	t.Setenv("TABURA_ASSISTANT_LLM_TIMEOUT", "45s")
	if got := assistantLLMRequestTimeout(); got != 45*time.Second {
		t.Fatalf("assistantLLMRequestTimeout() override = %s, want %s", got, 45*time.Second)
	}

	t.Setenv("TABURA_ASSISTANT_LLM_TIMEOUT", "nope")
	if got := assistantLLMRequestTimeout(); got != defaultAssistantLLMTimeout {
		t.Fatalf("assistantLLMRequestTimeout() invalid = %s, want %s", got, defaultAssistantLLMTimeout)
	}
}

func TestRunAssistantTurnFastLocalSkipsIntentEvalAndCapsOutput(t *testing.T) {
	var intentCalls atomic.Int32
	var llmCalls atomic.Int32

	intent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		intentCalls.Add(1)
		t.Fatalf("fast local turn should not call intent llm")
	}))
	defer intent.Close()

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm payload: %v", err)
		}
		if got := intFromAny(payload["max_tokens"], -1); got != assistantLLMFastMaxTokens {
			t.Fatalf("fast local max_tokens = %d, want %d", got, assistantLLMFastMaxTokens)
		}
		messages, _ := payload["messages"].([]any)
		if len(messages) != 1 {
			t.Fatalf("fast local message count = %d, want 1", len(messages))
		}
		first, _ := messages[0].(map[string]any)
		if got := strings.TrimSpace(strFromAny(first["role"])); got != "user" {
			t.Fatalf("fast local first role = %q, want user", got)
		}
		gotPrompt := strings.TrimSpace(strFromAny(first["content"]))
		if !strings.Contains(gotPrompt, "User request:\nExplain me who you are") {
			t.Fatalf("fast local prompt = %q, want fast prompt wrapper with user request", gotPrompt)
		}
		if !strings.Contains(gotPrompt, "Answer in plain text only. Be concise, but do not under-answer: default to 2-4 short sentences for normal questions.") {
			t.Fatalf("fast local prompt = %q, want concise fast guidance", gotPrompt)
		}
		templateKwargs, _ := payload["chat_template_kwargs"].(map[string]any)
		if got, ok := templateKwargs["enable_thinking"].(bool); !ok || got {
			t.Fatalf("fast local enable_thinking = %#v, want false", templateKwargs["enable_thinking"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": "Short direct reply.",
				},
			}},
		})
	}))
	defer llm.Close()

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.assistantMode = assistantModeLocal
	app.assistantLLMURL = llm.URL
	app.intentLLMURL = intent.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.chatSessionForWorkspace(project)
	if err != nil {
		t.Fatalf("chatSessionForWorkspace: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "Explain me who you are", "Explain me who you are", "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent, fastMode: true})

	if got := latestAssistantMessage(t, app, session.ID); got != "Short direct reply." {
		t.Fatalf("assistant message = %q, want direct fast reply", got)
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("llm call count = %d, want 1", llmCalls.Load())
	}
	if intentCalls.Load() != 0 {
		t.Fatalf("intent llm call count = %d, want 0", intentCalls.Load())
	}
}

func TestRunAssistantTurnNonFastLocalUsesSinglePromptWithoutToolsForDirectReply(t *testing.T) {
	var intentCalls atomic.Int32
	var llmCalls atomic.Int32
	var mcpListCalls atomic.Int32

	intent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		intentCalls.Add(1)
		t.Fatalf("non-fast local turn should not call intent llm")
	}))
	defer intent.Close()

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mcpListCalls.Add(1)
		t.Fatalf("direct non-fast local turn should not fetch MCP tools")
	}))
	defer mcp.Close()

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm payload: %v", err)
		}
		if got := intFromAny(payload["max_tokens"], -1); got != assistantLLMDirectMaxTokens {
			t.Fatalf("non-fast local max_tokens = %d, want %d", got, assistantLLMDirectMaxTokens)
		}
		tools, _ := payload["tools"].([]any)
		if len(tools) != 0 {
			t.Fatalf("non-fast local direct request tools = %d, want 0", len(tools))
		}
		messages, _ := payload["messages"].([]any)
		if len(messages) != 2 {
			t.Fatalf("non-fast local message count = %d, want 2", len(messages))
		}
		first, _ := messages[0].(map[string]any)
		if got := strings.TrimSpace(strFromAny(first["role"])); got != "system" {
			t.Fatalf("non-fast local first role = %q, want system", got)
		}
		if got := strings.TrimSpace(strFromAny(first["content"])); !strings.Contains(got, "No tools are available in this turn. Answer directly.") {
			t.Fatalf("non-fast local system prompt = %q, want no-tools instruction", got)
		}
		second, _ := messages[1].(map[string]any)
		if got := strings.TrimSpace(strFromAny(second["role"])); got != "user" {
			t.Fatalf("non-fast local second role = %q, want user", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": "Direct non-fast reply.",
				},
			}},
		})
	}))
	defer llm.Close()

	app, err := New(t.TempDir(), "", mcp.URL, "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.assistantMode = assistantModeLocal
	app.assistantLLMURL = llm.URL
	app.intentLLMURL = intent.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.chatSessionForWorkspace(project)
	if err != nil {
		t.Fatalf("chatSessionForWorkspace: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "Explain me who you are", "Explain me who you are", "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Direct non-fast reply." {
		t.Fatalf("assistant message = %q, want direct non-fast reply", got)
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("llm call count = %d, want 1", llmCalls.Load())
	}
	if mcpListCalls.Load() != 0 {
		t.Fatalf("mcp list call count = %d, want 0", mcpListCalls.Load())
	}
	if intentCalls.Load() != 0 {
		t.Fatalf("intent llm call count = %d, want 0", intentCalls.Load())
	}
}

func TestRunAssistantTurnNonFastLocalUsesPrunedExplicitToolPromptForCanvasRequest(t *testing.T) {
	var intentCalls atomic.Int32
	var llmCalls atomic.Int32
	var mcpListCalls atomic.Int32
	var mcpCalls atomic.Int32

	intent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		intentCalls.Add(1)
		t.Fatalf("non-fast local canvas turn should not call intent llm")
	}))
	defer intent.Close()

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode mcp payload: %v", err)
		}
		switch strings.TrimSpace(strFromAny(payload["method"])) {
		case "tools/list":
			mcpListCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "canvas_artifact_show",
						"description": "Show one artifact on canvas.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"session_id": map[string]any{"type": "string"},
								"kind":       map[string]any{"type": "string"},
							},
						},
					}, {
						"name":        "temp_file_create",
						"description": "Create a temp file.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"cwd":    map[string]any{"type": "string"},
								"prefix": map[string]any{"type": "string"},
							},
						},
					}, {
						"name":        "mail_message_list",
						"description": "List mail messages.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"limit": map[string]any{"type": "integer"},
							},
						},
					}},
				},
			})
		case "tools/call":
			mcpCalls.Add(1)
			params, _ := payload["params"].(map[string]any)
			if got := strings.TrimSpace(strFromAny(params["name"])); got != "canvas_artifact_show" {
				t.Fatalf("tool name = %q, want canvas_artifact_show", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"structuredContent": map[string]any{"ok": true},
				},
			})
		default:
			t.Fatalf("unexpected MCP method %q", payload["method"])
		}
	}))
	defer mcp.Close()

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm payload: %v", err)
		}
		if _, ok := payload["tools"]; ok {
			t.Fatal("canvas request should not send OpenAI tool definitions")
		}
		messages, _ := payload["messages"].([]any)
		system, _ := messages[0].(map[string]any)
		systemPrompt := strings.TrimSpace(strFromAny(system["content"]))
		for _, want := range []string{
			"The user wants new generated text to appear on the canvas.",
			"Reply with only the exact canvas text.",
		} {
			if !strings.Contains(systemPrompt, want) {
				t.Fatalf("system prompt missing %q: %q", want, systemPrompt)
			}
		}
		for _, blocked := range []string{
			"Available tools in this turn:",
			"workspace_read",
			"action__open_file_canvas",
			"canvas_write_text",
			"mcp__mail_message_list",
			"mcp__canvas_artifact_show",
			"mcp__temp_file_create",
			"action__toggle_silent",
		} {
			if strings.Contains(systemPrompt, blocked) {
				t.Fatalf("system prompt should not include %q: %q", blocked, systemPrompt)
			}
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

	app, err := New(t.TempDir(), "", mcp.URL, "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.assistantMode = assistantModeLocal
	app.assistantLLMURL = llm.URL
	app.intentLLMURL = intent.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.chatSessionForWorkspace(project)
	if err != nil {
		t.Fatalf("chatSessionForWorkspace: %v", err)
	}
	prompt := "Show a text artifact on canvas with the title Tool Test and body Orbit Canvas."
	if _, err := app.store.AddChatMessage(session.ID, "user", prompt, prompt, "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Shown on canvas." {
		t.Fatalf("assistant message = %q", got)
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("llm call count = %d, want 1", llmCalls.Load())
	}
	if mcpListCalls.Load() != 0 {
		t.Fatalf("mcp list call count = %d, want 0", mcpListCalls.Load())
	}
	if mcpCalls.Load() != 1 {
		t.Fatalf("mcp call count = %d, want 1", mcpCalls.Load())
	}
	if intentCalls.Load() != 0 {
		t.Fatalf("intent llm call count = %d, want 0", intentCalls.Load())
	}
}

func TestRunAssistantTurnNonFastLocalRepairsPlanningTextIntoCanvasOutput(t *testing.T) {
	var llmCalls atomic.Int32
	var mcpCalls atomic.Int32

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode mcp payload: %v", err)
		}
		switch strings.TrimSpace(strFromAny(payload["method"])) {
		case "tools/call":
			mcpCalls.Add(1)
			params, _ := payload["params"].(map[string]any)
			if got := strings.TrimSpace(strFromAny(params["name"])); got != "canvas_artifact_show" {
				t.Fatalf("tool name = %q, want canvas_artifact_show", got)
			}
			args, _ := params["arguments"].(map[string]any)
			if got := strings.TrimSpace(strFromAny(args["kind"])); got != "text" {
				t.Fatalf("tool kind = %q, want text", got)
			}
			if got := strings.TrimSpace(strFromAny(args["markdown_or_text"])); got != "Orbit Canvas" {
				t.Fatalf("tool body = %q, want Orbit Canvas", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"structuredContent": map[string]any{"ok": true},
				},
			})
		default:
			t.Fatalf("unexpected MCP method %q", payload["method"])
		}
	}))
	defer mcp.Close()

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := llmCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm payload: %v", err)
		}
		messages, _ := payload["messages"].([]any)
		switch call {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "I need to find a text artifact titled Tool Test and show it on canvas first.",
					},
				}},
			})
		case 2:
			last, _ := messages[len(messages)-1].(map[string]any)
			if got := strings.TrimSpace(strFromAny(last["content"])); got != localAssistantCanvasContentRequiredPrompt() {
				t.Fatalf("repair prompt = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "Orbit Canvas",
					},
				}},
			})
		default:
			t.Fatalf("unexpected extra llm call %d", call)
		}
	}))
	defer llm.Close()

	app, err := New(t.TempDir(), "", mcp.URL, "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.assistantMode = assistantModeLocal
	app.assistantLLMURL = llm.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.chatSessionForWorkspace(project)
	if err != nil {
		t.Fatalf("chatSessionForWorkspace: %v", err)
	}
	prompt := "Render the exact text Orbit Canvas on the canvas with the title Tool Test. Use tools, then reply DONE."
	if _, err := app.store.AddChatMessage(session.ID, "user", prompt, prompt, "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Shown on canvas." {
		t.Fatalf("assistant message = %q, want Shown on canvas.", got)
	}
	if llmCalls.Load() != 2 {
		t.Fatalf("llm call count = %d, want 2", llmCalls.Load())
	}
	if mcpCalls.Load() != 1 {
		t.Fatalf("mcp call count = %d, want 1", mcpCalls.Load())
	}
}

func TestRunAssistantTurnLocalAssistantCompletesMultiToolLoop(t *testing.T) {
	var intentCalls atomic.Int32
	var llmCalls atomic.Int32
	mcpCalls := atomic.Int32{}

	intent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		intentCalls.Add(1)
		t.Fatalf("non-fast local tool loop should not call intent llm")
	}))
	defer intent.Close()

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode mcp payload: %v", err)
		}
		switch strings.TrimSpace(strFromAny(payload["method"])) {
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "item_list",
						"description": "List items.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"limit": map[string]any{"type": "integer"},
							},
						},
					}},
				},
			})
		case "tools/call":
			mcpCalls.Add(1)
			params, _ := payload["params"].(map[string]any)
			if got := strings.TrimSpace(strFromAny(params["name"])); got != "item_list" {
				t.Fatalf("tool name = %q, want item_list", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"structuredContent": map[string]any{
						"ok":    true,
						"items": []string{"alpha", "beta"},
					},
				},
			})
		default:
			t.Fatalf("unexpected MCP method %q", payload["method"])
		}
	}))
	defer mcp.Close()

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := llmCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm payload: %v", err)
		}
		messages, _ := payload["messages"].([]any)
		switch call {
		case 1:
			if got := intFromAny(payload["max_tokens"], -1); got != assistantLLMToolPlanMaxTokens {
				t.Fatalf("initial tool-aware max_tokens = %d, want %d", got, assistantLLMToolPlanMaxTokens)
			}
			if _, ok := payload["tools"]; ok {
				t.Fatal("initial local request should not send OpenAI tool definitions")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": `{"tool_calls":[{"name":"mcp__item_list","arguments":{"limit":2}}]}`,
					},
				}},
			})
		default:
			if got := intFromAny(payload["max_tokens"], -1); got != assistantLLMToolMaxTokens {
				t.Fatalf("follow-up tool-aware max_tokens = %d, want %d", got, assistantLLMToolMaxTokens)
			}
			last, _ := messages[len(messages)-1].(map[string]any)
			if got := strings.TrimSpace(strFromAny(last["content"])); !strings.Contains(got, `"items":["alpha","beta"]`) {
				t.Fatalf("follow-up llm call missing item_list result: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "Local backend completed the item lookup.",
					},
				}},
			})
		}
	}))
	defer llm.Close()

	app, err := New(t.TempDir(), "", mcp.URL, "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.assistantMode = assistantModeLocal
	app.assistantLLMURL = llm.URL
	app.intentLLMURL = intent.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.chatSessionForWorkspace(project)
	if err != nil {
		t.Fatalf("chatSessionForWorkspace: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "show my items and use MCP", "show my items and use MCP", "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Local backend completed the item lookup." {
		t.Fatalf("assistant message = %q", got)
	}
	if llmCalls.Load() != 2 {
		t.Fatalf("llm call count = %d, want 2", llmCalls.Load())
	}
	if mcpCalls.Load() != 1 {
		t.Fatalf("mcp call count = %d, want 1", mcpCalls.Load())
	}
	if intentCalls.Load() != 0 {
		t.Fatalf("intent llm call count = %d, want 0", intentCalls.Load())
	}
}

func TestRunAssistantTurnLocalAssistantRecoversMalformedToolCall(t *testing.T) {
	var llmCalls atomic.Int32

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := llmCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm payload: %v", err)
		}
		messages, _ := payload["messages"].([]any)
		switch call {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": `{"tool_calls":[{"name":"shll","arguments":{"command":"printf 'broken'"}}]}`,
					},
				}},
			})
		case 2:
			last, _ := messages[len(messages)-1].(map[string]any)
			if got := strings.TrimSpace(strFromAny(last["content"])); !strings.Contains(got, "unsupported local assistant tool") {
				t.Fatalf("tool error content = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": `{"tool_calls":[{"name":"shell","arguments":{"command":"printf 'recovered'"}}]}`,
					},
				}},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "Recovered after repairing the malformed tool call.",
					},
				}},
			})
		}
	}))
	defer llm.Close()

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.assistantMode = assistantModeLocal
	app.assistantLLMURL = llm.URL
	app.intentLLMURL = ""
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.chatSessionForWorkspace(project)
	if err != nil {
		t.Fatalf("chatSessionForWorkspace: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "run the local tool", "run the local tool", "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Recovered after repairing the malformed tool call." {
		t.Fatalf("assistant message = %q", got)
	}
	if llmCalls.Load() != 3 {
		t.Fatalf("llm call count = %d, want 3", llmCalls.Load())
	}
}
