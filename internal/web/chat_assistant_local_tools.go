package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

var (
	errLocalAssistantNotConfigured       = errors.New("local assistant is not configured")
	errLocalAssistantUnsupportedResponse = errors.New("local assistant returned unsupported control envelope")
)

type localAssistantLLMToolCall struct {
	ID       string                        `json:"id,omitempty"`
	Type     string                        `json:"type,omitempty"`
	Function localAssistantLLMFunctionCall `json:"function"`
}

type localAssistantLLMFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type localAssistantDecision struct {
	FinalText string
	ToolCalls []localAssistantToolCall
}

type localAssistantToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type localAssistantToolResult struct {
	ToolCallID        string         `json:"tool_call_id,omitempty"`
	Name              string         `json:"name,omitempty"`
	Arguments         map[string]any `json:"arguments,omitempty"`
	CWD               string         `json:"cwd,omitempty"`
	Output            string         `json:"output,omitempty"`
	ExitCode          int            `json:"exit_code,omitempty"`
	TimedOut          bool           `json:"timed_out,omitempty"`
	IsError           bool           `json:"is_error,omitempty"`
	Error             string         `json:"error,omitempty"`
	StructuredContent map[string]any `json:"structured_content,omitempty"`
}

type localAssistantTurnState struct {
	workspace    store.Workspace
	workspaceDir string
	currentDir   string
	mcpURL       string
}

func localAssistantToolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "shell",
				"description": "Run a shell command inside the active workspace and inspect the result.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{
							"type":        "string",
							"description": "Shell command to execute.",
						},
						"cwd": map[string]any{
							"type":        "string",
							"description": "Optional relative or absolute directory inside the workspace.",
						},
					},
					"required": []string{"command"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "mcp",
				"description": "Call an MCP tool on the active workspace MCP endpoint or the local fallback endpoint.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Tool name to call.",
						},
						"arguments": map[string]any{
							"type":        "object",
							"description": "Tool arguments.",
						},
						"mcp_url": map[string]any{
							"type":        "string",
							"description": "Optional MCP URL override.",
						},
					},
					"required": []string{"name"},
				},
			},
		},
	}
}

func (a *App) requestLocalAssistantCompletion(ctx context.Context, messages []map[string]any, enableThinking bool) (localIntentLLMMessage, error) {
	baseURL := a.assistantLLMBaseURL()
	if baseURL == "" {
		return localIntentLLMMessage{}, errLocalAssistantNotConfigured
	}
	return a.requestLocalAssistantCompletionWithConfig(ctx, messages, localAssistantToolDefinitions(), "auto", enableThinking)
}

func (a *App) requestLocalAssistantCompletionWithConfig(ctx context.Context, messages []map[string]any, tools []map[string]any, toolChoice string, enableThinking bool) (localIntentLLMMessage, error) {
	baseURL := a.assistantLLMBaseURL()
	if baseURL == "" {
		return localIntentLLMMessage{}, errLocalAssistantNotConfigured
	}
	request := map[string]any{
		"model":       a.localAssistantLLMModel(),
		"temperature": 0,
		"max_tokens":  assistantLLMMaxTokens,
		"chat_template_kwargs": map[string]any{
			"enable_thinking": enableThinking,
		},
		"messages": messages,
	}
	if len(tools) > 0 {
		request["tools"] = tools
		request["tool_choice"] = firstNonEmptyCursorText(strings.TrimSpace(toolChoice), "auto")
	}
	requestBody, _ := json.Marshal(request)
	requestCtx, cancel := context.WithTimeout(ctx, assistantLLMRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		baseURL+"/v1/chat/completions",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return localIntentLLMMessage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return localIntentLLMMessage{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, assistantLLMResponseLimit))
		return localIntentLLMMessage{}, fmt.Errorf("assistant llm HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload localIntentLLMChatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, assistantLLMResponseLimit)).Decode(&payload); err != nil {
		return localIntentLLMMessage{}, err
	}
	if len(payload.Choices) == 0 {
		return localIntentLLMMessage{}, errors.New("assistant llm returned no choices")
	}
	message := payload.Choices[0].Message
	message.Content = stripLocalAssistantThinkingPreamble(message.Content)
	return message, nil
}

func localAssistantThinkingEnabled(req *assistantTurnRequest) bool {
	if req == nil {
		return false
	}
	effort := modelprofile.NormalizeReasoningEffort(modelprofile.AliasLocal, req.reasoningEffort)
	return effort != "" && effort != modelprofile.ReasoningNone
}

func localAssistantReasoningHint(req *assistantTurnRequest) string {
	if !localAssistantThinkingEnabled(req) {
		return ""
	}
	switch modelprofile.NormalizeReasoningEffort(modelprofile.AliasLocal, req.reasoningEffort) {
	case modelprofile.ReasoningLow:
		return "Think briefly before answering."
	case modelprofile.ReasoningHigh:
		return "Think carefully and thoroughly before answering."
	default:
		return "Think before answering."
	}
}

func parseLocalAssistantDecision(message localIntentLLMMessage) (localAssistantDecision, error) {
	if len(message.ToolCalls) > 0 {
		calls, err := parseLocalAssistantToolCalls(message.ToolCalls)
		if err != nil {
			return localAssistantDecision{}, err
		}
		return localAssistantDecision{ToolCalls: calls}, nil
	}
	if message.FunctionCall != nil && strings.TrimSpace(message.FunctionCall.Name) != "" {
		calls, err := parseLocalAssistantToolCalls([]localAssistantLLMToolCall{{
			ID:       randomToken(),
			Type:     "function",
			Function: *message.FunctionCall,
		}})
		if err != nil {
			return localAssistantDecision{}, err
		}
		return localAssistantDecision{ToolCalls: calls}, nil
	}
	content := strings.TrimSpace(stripCodeFence(message.Content))
	if content == "" {
		return localAssistantDecision{}, errors.New("assistant llm returned empty content")
	}
	if localAssistantUnsupportedControlEnvelope(content) {
		return localAssistantDecision{}, errLocalAssistantUnsupportedResponse
	}
	if classification, err := parseIntentPlanClassification(content); err == nil && classification.LocalAnswer != nil {
		if text := strings.TrimSpace(classification.LocalAnswer.Text); text != "" {
			return localAssistantDecision{FinalText: text}, nil
		}
	}
	if decoded, ok := decodeLocalAssistantEnvelope(content); ok {
		switch typed := decoded.(type) {
		case map[string]any:
			if final := strings.TrimSpace(fmt.Sprint(typed["final"])); final != "" && final != "<nil>" {
				return localAssistantDecision{FinalText: final}, nil
			}
			if text := strings.TrimSpace(fmt.Sprint(typed["text"])); text != "" && text != "<nil>" {
				return localAssistantDecision{FinalText: text}, nil
			}
			if rawCalls, ok := typed["tool_calls"]; ok {
				calls, err := parseLocalAssistantToolCallsAny(rawCalls)
				if err != nil {
					return localAssistantDecision{}, err
				}
				return localAssistantDecision{ToolCalls: calls}, nil
			}
		}
	}
	if looksLikeMalformedLocalAssistantToolResponse(content) {
		return localAssistantDecision{}, errors.New("assistant emitted malformed tool JSON")
	}
	return localAssistantDecision{FinalText: content}, nil
}

func decodeLocalAssistantEnvelope(raw string) (any, bool) {
	decoded, ok := decodeSystemActionCandidate(raw)
	if ok {
		return decoded, true
	}
	embedded := extractEmbeddedJSON(raw)
	if embedded == "" {
		return nil, false
	}
	return decodeSystemActionCandidate(embedded)
}

func looksLikeMalformedLocalAssistantToolResponse(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "tool_calls") || strings.Contains(lower, "function_call") || strings.Contains(lower, "\"arguments\"")
}

func localAssistantUnsupportedControlEnvelope(raw string) bool {
	decoded, ok := decodeLocalAssistantEnvelope(strings.TrimSpace(stripCodeFence(raw)))
	if !ok {
		return false
	}
	obj, ok := decoded.(map[string]any)
	if !ok || obj == nil {
		return false
	}
	for _, allowed := range []string{"final", "text", "tool_calls"} {
		if _, ok := obj[allowed]; ok {
			return false
		}
	}
	for _, unsupported := range []string{"action", "actions", "addressed", "local_answer", "ack"} {
		if _, ok := obj[unsupported]; ok {
			return true
		}
	}
	return false
}

func parseLocalAssistantToolCallsAny(raw any) ([]localAssistantToolCall, error) {
	switch typed := raw.(type) {
	case []any:
		calls := make([]localAssistantToolCall, 0, len(typed))
		for _, item := range typed {
			obj, _ := item.(map[string]any)
			if obj == nil {
				return nil, errors.New("tool_calls entry must be an object")
			}
			call, err := parseLocalAssistantToolCallMap(obj)
			if err != nil {
				return nil, err
			}
			calls = append(calls, call)
		}
		return calls, nil
	case map[string]any:
		call, err := parseLocalAssistantToolCallMap(typed)
		if err != nil {
			return nil, err
		}
		return []localAssistantToolCall{call}, nil
	default:
		return nil, errors.New("tool_calls must be an object or array")
	}
}

func parseLocalAssistantToolCalls(raw []localAssistantLLMToolCall) ([]localAssistantToolCall, error) {
	calls := make([]localAssistantToolCall, 0, len(raw))
	for _, item := range raw {
		name := normalizeLocalAssistantToolName(item.Function.Name)
		if name == "" {
			return nil, errors.New("tool call is missing a supported name")
		}
		args, err := parseLocalAssistantToolArguments(item.Function.Arguments)
		if err != nil {
			return nil, err
		}
		callID := strings.TrimSpace(item.ID)
		if callID == "" {
			callID = randomToken()
		}
		calls = append(calls, localAssistantToolCall{
			ID:        callID,
			Name:      name,
			Arguments: args,
		})
	}
	return calls, nil
}

func parseLocalAssistantToolCallMap(obj map[string]any) (localAssistantToolCall, error) {
	name := normalizeLocalAssistantToolName(fmt.Sprint(obj["name"]))
	argsValue := obj["arguments"]
	if function, ok := obj["function"].(map[string]any); ok {
		if name == "" {
			name = normalizeLocalAssistantToolName(fmt.Sprint(function["name"]))
		}
		if argsValue == nil {
			argsValue = function["arguments"]
		}
	}
	if name == "" {
		return localAssistantToolCall{}, errors.New("tool call is missing a supported name")
	}
	args, err := parseLocalAssistantToolArguments(argsValue)
	if err != nil {
		return localAssistantToolCall{}, err
	}
	callID := strings.TrimSpace(fmt.Sprint(obj["id"]))
	if callID == "" || callID == "<nil>" {
		callID = randomToken()
	}
	return localAssistantToolCall{
		ID:        callID,
		Name:      name,
		Arguments: args,
	}, nil
}

func parseLocalAssistantToolArguments(raw any) (map[string]any, error) {
	switch typed := raw.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return typed, nil
	case string:
		clean := strings.TrimSpace(typed)
		if clean == "" {
			return map[string]any{}, nil
		}
		decoded, ok := decodeLocalAssistantEnvelope(clean)
		if !ok {
			return nil, fmt.Errorf("tool arguments are not valid JSON: %s", clean)
		}
		obj, ok := decoded.(map[string]any)
		if !ok {
			return nil, errors.New("tool arguments must decode to an object")
		}
		return obj, nil
	default:
		return nil, fmt.Errorf("tool arguments have unsupported type %T", raw)
	}
}

func normalizeLocalAssistantToolName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "shell":
		return "shell"
	case "mcp", "mcp_tool", "mcp_call":
		return "mcp"
	default:
		return ""
	}
}

func localAssistantAssistantMessage(message localIntentLLMMessage) map[string]any {
	payload := map[string]any{
		"role":    "assistant",
		"content": strings.TrimSpace(message.Content),
	}
	if len(message.ToolCalls) > 0 {
		payload["tool_calls"] = message.ToolCalls
	}
	if message.FunctionCall != nil && strings.TrimSpace(message.FunctionCall.Name) != "" {
		payload["function_call"] = message.FunctionCall
	}
	return payload
}

func localAssistantRepairPrompt(err error) string {
	return fmt.Sprintf(
		"Your last tool response could not be executed: %s. Return a valid tool call or a valid {\"final\":\"...\"} object now.",
		strings.TrimSpace(err.Error()),
	)
}

func stripLocalAssistantThinkingPreamble(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return clean
	}
	if strings.HasPrefix(clean, "<think>") {
		if idx := strings.Index(clean, "</think>"); idx >= 0 {
			clean = clean[idx+len("</think>"):]
		}
	}
	if strings.HasPrefix(clean, "</think>") {
		clean = clean[len("</think>"):]
	}
	return strings.TrimSpace(clean)
}

func localAssistantNeedsTools(req *assistantTurnRequest, visual *chatVisualAttachment) bool {
	if req == nil {
		return false
	}
	if visual != nil {
		return true
	}
	if req.cursorCtx != nil {
		if req.cursorCtx.hasPointedItem() || strings.TrimSpace(req.cursorCtx.SelectedText) != "" {
			return true
		}
	}
	if len(req.inkCtx) > 0 || len(req.positionCtx) > 0 {
		return true
	}
	return localAssistantAutoRouteCandidate(req.userText)
}

func (a *App) runLocalAssistantToolLoop(ctx context.Context, req *assistantTurnRequest, prompt string, visual *chatVisualAttachment) (string, error) {
	if a == nil || req == nil {
		return "", errors.New("assistant turn request is required")
	}
	state, err := a.newLocalAssistantTurnState(req)
	if err != nil {
		return "", err
	}
	conversation := []map[string]any{
		{"role": "system", "content": strings.TrimSpace(strings.Join([]string{localAssistantDialoguePrompt, localAssistantReasoningHint(req)}, "\n"))},
		{"role": "user", "content": buildLocalAssistantUserContent(prompt, visual)},
	}
	enableThinking := localAssistantThinkingEnabled(req)
	if req.fastMode {
		message, err := a.requestLocalAssistantCompletionWithConfig(ctx, []map[string]any{
			{"role": "user", "content": strings.TrimSpace(req.promptText)},
		}, nil, "", enableThinking)
		if err != nil {
			return "", err
		}
		if localAssistantUnsupportedControlEnvelope(message.Content) {
			return "", errLocalAssistantUnsupportedResponse
		}
		return strings.TrimSpace(message.Content), nil
	}
	if !localAssistantNeedsTools(req, visual) {
		message, err := a.requestLocalAssistantCompletionWithConfig(ctx, []map[string]any{
			{"role": "system", "content": strings.TrimSpace(strings.Join([]string{localAssistantDirectPrompt, localAssistantReasoningHint(req)}, "\n"))},
			{"role": "user", "content": buildLocalAssistantUserContent(prompt, visual)},
		}, nil, "", enableThinking)
		if err != nil {
			return "", err
		}
		if localAssistantUnsupportedControlEnvelope(message.Content) {
			return "", errLocalAssistantUnsupportedResponse
		}
		return strings.TrimSpace(message.Content), nil
	}
	malformedRetries := 0
	for round := 0; round < assistantLLMMaxToolRounds; round++ {
		message, err := a.requestLocalAssistantCompletion(ctx, conversation, enableThinking)
		if err != nil {
			return "", err
		}
		decision, err := parseLocalAssistantDecision(message)
		if err != nil {
			if malformedRetries >= assistantLLMMalformedRetries {
				return "", fmt.Errorf("local assistant emitted malformed tool output: %w", err)
			}
			malformedRetries++
			conversation = append(conversation, localAssistantAssistantMessage(message))
			conversation = append(conversation, map[string]any{
				"role":    "user",
				"content": localAssistantRepairPrompt(err),
			})
			continue
		}
		if text := strings.TrimSpace(decision.FinalText); text != "" {
			return text, nil
		}
		if len(decision.ToolCalls) == 0 {
			return "", errors.New("assistant returned neither tool calls nor a final response")
		}
		malformedRetries = 0
		conversation = append(conversation, localAssistantAssistantMessage(message))
		for _, call := range decision.ToolCalls {
			result, execErr := a.executeLocalAssistantToolCall(ctx, &state, call)
			if execErr != nil {
				return "", execErr
			}
			if resultPayload := localAssistantToolPayload(result, state.workspace.ID); resultPayload != nil {
				a.broadcastSystemActionEvent(req.sessionID, resultPayload)
			}
			conversation = append(conversation, map[string]any{
				"role":         "tool",
				"tool_call_id": call.ID,
				"content":      result.content(),
			})
		}
	}
	return "", fmt.Errorf("local assistant exceeded %d tool rounds", assistantLLMMaxToolRounds)
}

func (a *App) newLocalAssistantTurnState(req *assistantTurnRequest) (localAssistantTurnState, error) {
	workspace, err := a.effectiveWorkspaceForChatSession(req.session)
	if err != nil {
		return localAssistantTurnState{}, err
	}
	workspaceDir := strings.TrimSpace(workspace.DirPath)
	if workspaceDir == "" {
		return localAssistantTurnState{}, errors.New("workspace path is required")
	}
	mcpURL := strings.TrimSpace(workspace.MCPURL)
	if mcpURL == "" {
		mcpURL = strings.TrimSpace(a.localMCPURL)
	}
	return localAssistantTurnState{
		workspace:    workspace,
		workspaceDir: workspaceDir,
		currentDir:   workspaceDir,
		mcpURL:       mcpURL,
	}, nil
}

func (a *App) executeLocalAssistantToolCall(ctx context.Context, state *localAssistantTurnState, call localAssistantToolCall) (localAssistantToolResult, error) {
	if err := ctx.Err(); err != nil {
		return localAssistantToolResult{}, err
	}
	switch call.Name {
	case "shell":
		return executeLocalAssistantShellTool(state, call), nil
	case "mcp":
		return a.executeLocalAssistantMCPTool(ctx, state, call)
	default:
		return localAssistantToolResult{
			ToolCallID: call.ID,
			Name:       call.Name,
			Arguments:  call.Arguments,
			IsError:    true,
			Error:      "unsupported local assistant tool",
		}, nil
	}
}

func executeLocalAssistantShellTool(state *localAssistantTurnState, call localAssistantToolCall) localAssistantToolResult {
	result := localAssistantToolResult{
		ToolCallID: call.ID,
		Name:       call.Name,
		Arguments:  call.Arguments,
	}
	command := strings.TrimSpace(fmt.Sprint(call.Arguments["command"]))
	if command == "" || command == "<nil>" {
		result.IsError = true
		result.Error = "shell command is required"
		return result
	}
	cwd, err := resolveLocalAssistantToolCWD(state.workspaceDir, state.currentDir, fmt.Sprint(call.Arguments["cwd"]))
	if err != nil {
		result.IsError = true
		result.Error = err.Error()
		return result
	}
	result.CWD = cwd
	execResult := executeShellCommand(command, cwd)
	result.Output = execResult.Output
	result.ExitCode = execResult.ExitCode
	result.TimedOut = execResult.TimedOut
	if execResult.TimedOut {
		result.IsError = true
		result.Error = fmt.Sprintf("shell command timed out after %s", systemActionShellTimeout)
		return result
	}
	if execResult.RunErr != nil && execResult.ExitCode == 0 {
		result.IsError = true
		result.Error = execResult.RunErr.Error()
		return result
	}
	if execResult.ExitCode != 0 {
		result.IsError = true
		result.Error = fmt.Sprintf("shell command failed with exit %d", execResult.ExitCode)
	}
	if nextDir := localAssistantNextWorkingDir(command, state.workspaceDir, cwd); nextDir != "" {
		state.currentDir = nextDir
		result.CWD = nextDir
	} else {
		state.currentDir = cwd
	}
	return result
}

func (a *App) executeLocalAssistantMCPTool(ctx context.Context, state *localAssistantTurnState, call localAssistantToolCall) (localAssistantToolResult, error) {
	result := localAssistantToolResult{
		ToolCallID: call.ID,
		Name:       call.Name,
		Arguments:  call.Arguments,
	}
	mcpURL := strings.TrimSpace(fmt.Sprint(call.Arguments["mcp_url"]))
	if mcpURL == "" || mcpURL == "<nil>" {
		mcpURL = state.mcpURL
	}
	if mcpURL == "" {
		result.IsError = true
		result.Error = "MCP URL is not configured"
		return result, nil
	}
	toolName := strings.TrimSpace(fmt.Sprint(call.Arguments["name"]))
	if toolName == "" || toolName == "<nil>" {
		result.IsError = true
		result.Error = "MCP tool name is required"
		return result, nil
	}
	toolArgs, err := parseLocalAssistantToolArguments(call.Arguments["arguments"])
	if err != nil {
		result.IsError = true
		result.Error = err.Error()
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return localAssistantToolResult{}, err
	}
	structuredContent, err := mcpToolsCallURL(mcpURL, toolName, toolArgs)
	if err != nil {
		result.IsError = true
		result.Error = err.Error()
		return result, nil
	}
	result.StructuredContent = structuredContent
	return result, nil
}

func localAssistantToolPayload(result localAssistantToolResult, workspaceID int64) map[string]any {
	if strings.TrimSpace(result.Name) == "" {
		return nil
	}
	switch result.Name {
	case "shell":
		return map[string]any{
			"type":         "shell",
			"command":      strings.TrimSpace(fmt.Sprint(result.Arguments["command"])),
			"cwd":          result.CWD,
			"exit_code":    result.ExitCode,
			"timed_out":    result.TimedOut,
			"output":       result.Output,
			"is_error":     result.IsError,
			"error":        result.Error,
			"workspace_id": workspaceID,
		}
	case "mcp":
		return map[string]any{
			"type":         "mcp_tool",
			"name":         strings.TrimSpace(fmt.Sprint(result.Arguments["name"])),
			"arguments":    result.Arguments["arguments"],
			"result":       result.StructuredContent,
			"is_error":     result.IsError,
			"error":        result.Error,
			"workspace_id": workspaceID,
		}
	default:
		return nil
	}
}

func (r localAssistantToolResult) content() string {
	body, _ := json.Marshal(r)
	return string(body)
}

func resolveLocalAssistantToolCWD(workspaceDir, currentDir, raw string) (string, error) {
	base := strings.TrimSpace(currentDir)
	if base == "" {
		base = strings.TrimSpace(workspaceDir)
	}
	target := strings.TrimSpace(raw)
	if target == "" || target == "<nil>" {
		target = base
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target = filepath.Clean(target)
	root := filepath.Clean(strings.TrimSpace(workspaceDir))
	if root != "" {
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return "", err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("cwd %q escapes the workspace", raw)
		}
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd %q is not a directory", raw)
	}
	return target, nil
}

func localAssistantNextWorkingDir(command string, workspaceDir string, currentDir string) string {
	trimmed := strings.TrimSpace(command)
	if !strings.HasPrefix(trimmed, "cd ") {
		return ""
	}
	next := strings.TrimSpace(strings.TrimPrefix(trimmed, "cd"))
	for _, separator := range []string{"&&", ";", "\n"} {
		if idx := strings.Index(next, separator); idx >= 0 {
			next = strings.TrimSpace(next[:idx])
		}
	}
	next = strings.Trim(next, `"'`)
	if next == "" {
		return ""
	}
	resolved, err := resolveLocalAssistantToolCWD(workspaceDir, currentDir, next)
	if err != nil {
		return ""
	}
	return resolved
}
