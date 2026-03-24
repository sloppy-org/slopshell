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
	if got := app.assistantBackendForTurn(searchReq).mode(); got != assistantModeCodex {
		t.Fatalf("backend for search turn = %q, want %q", got, assistantModeCodex)
	}

	app.assistantLLMURL = ""
	app.intentLLMURL = ""
	if got := app.assistantBackendForTurn(localReq).mode(); got != assistantModeLocal {
		t.Fatalf("backend without local assistant config = %q, want %q", got, assistantModeLocal)
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

func TestExecuteLocalAssistantMCPToolUsesConfiguredEndpoint(t *testing.T) {
	var calls atomic.Int32
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode mcp payload: %v", err)
		}
		params, _ := payload["params"].(map[string]any)
		if got := strings.TrimSpace(strFromAny(params["name"])); got != "echo_status" {
			t.Fatalf("tool name = %q, want echo_status", got)
		}
		args, _ := params["arguments"].(map[string]any)
		if got := strings.TrimSpace(strFromAny(args["status"])); got != "ready" {
			t.Fatalf("tool args status = %q, want ready", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"structuredContent": map[string]any{
					"ok":     true,
					"status": "ready",
				},
			},
		})
	}))
	defer mcp.Close()

	app := newAuthedTestApp(t)
	state := localAssistantTurnState{
		mcpURL: mcp.URL,
	}
	result, err := app.executeLocalAssistantMCPTool(context.Background(), &state, localAssistantToolCall{
		ID:   "call-mcp",
		Name: "mcp",
		Arguments: map[string]any{
			"name": "echo_status",
			"arguments": map[string]any{
				"status": "ready",
			},
		},
	})
	if err != nil {
		t.Fatalf("executeLocalAssistantMCPTool() error: %v", err)
	}
	if result.IsError {
		t.Fatalf("mcp tool returned error: %+v", result)
	}
	if calls.Load() != 1 {
		t.Fatalf("mcp call count = %d, want 1", calls.Load())
	}
	if got := strings.TrimSpace(strFromAny(result.StructuredContent["status"])); got != "ready" {
		t.Fatalf("structured status = %q, want ready", got)
	}
}

func TestRunAssistantTurnLocalAssistantCompletesMultiToolLoop(t *testing.T) {
	var llmCalls atomic.Int32
	mcpCalls := atomic.Int32{}

	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mcpCalls.Add(1)
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode mcp payload: %v", err)
		}
		params, _ := payload["params"].(map[string]any)
		if got := strings.TrimSpace(strFromAny(params["name"])); got != "echo_status" {
			t.Fatalf("tool name = %q, want echo_status", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"result": map[string]any{
				"structuredContent": map[string]any{
					"ok":     true,
					"status": "ready",
				},
			},
		})
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
						"tool_calls": []map[string]any{{
							"id":   "call-shell",
							"type": "function",
							"function": map[string]any{
								"name":      "shell",
								"arguments": `{"command":"printf 'shell-step'"}`,
							},
						}},
					},
				}},
			})
		case 2:
			last, _ := messages[len(messages)-1].(map[string]any)
			if got := strings.TrimSpace(strFromAny(last["content"])); !strings.Contains(got, "shell-step") {
				t.Fatalf("second llm call missing shell output: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"tool_calls": []map[string]any{{
							"id":   "call-mcp",
							"type": "function",
							"function": map[string]any{
								"name":      "mcp",
								"arguments": `{"name":"echo_status","arguments":{"status":"ready"}}`,
							},
						}},
					},
				}},
			})
		default:
			last, _ := messages[len(messages)-1].(map[string]any)
			if got := strings.TrimSpace(strFromAny(last["content"])); !strings.Contains(got, `"status":"ready"`) {
				t.Fatalf("final llm call missing mcp result: %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "Local backend completed shell and MCP steps.",
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
	if _, err := app.store.AddChatMessage(session.ID, "user", "inspect the workspace and use MCP", "inspect the workspace and use MCP", "text"); err != nil {
		t.Fatalf("AddChatMessage: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if got := latestAssistantMessage(t, app, session.ID); got != "Local backend completed shell and MCP steps." {
		t.Fatalf("assistant message = %q", got)
	}
	if llmCalls.Load() != 3 {
		t.Fatalf("llm call count = %d, want 3", llmCalls.Load())
	}
	if mcpCalls.Load() != 1 {
		t.Fatalf("mcp call count = %d, want 1", mcpCalls.Load())
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
						"tool_calls": []map[string]any{{
							"id":   "call-bad",
							"type": "function",
							"function": map[string]any{
								"name":      "shll",
								"arguments": `{"command":"printf 'broken'"}`,
							},
						}},
					},
				}},
			})
		case 2:
			last, _ := messages[len(messages)-1].(map[string]any)
			if got := strings.TrimSpace(strFromAny(last["content"])); !strings.Contains(got, "could not be executed") {
				t.Fatalf("repair prompt = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"tool_calls": []map[string]any{{
							"id":   "call-shell",
							"type": "function",
							"function": map[string]any{
								"name":      "shell",
								"arguments": `{"command":"printf 'recovered'"}`,
							},
						}},
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
