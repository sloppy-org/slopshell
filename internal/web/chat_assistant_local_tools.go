package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sloppy-org/slopshell/internal/store"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var (
	errLocalAssistantNotConfigured       = errors.New("local assistant is not configured")
	errLocalAssistantUnsupportedResponse = errors.New("local assistant returned unsupported control envelope")
)

const (
	localAssistantWorkspaceReadBytes       = 16 * 1024
	localAssistantWorkspaceFindResultLimit = 12
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
	ToolCallID        string           `json:"tool_call_id,omitempty"`
	ModelName         string           `json:"model_name,omitempty"`
	Name              string           `json:"name,omitempty"`
	Kind              string           `json:"kind,omitempty"`
	Arguments         map[string]any   `json:"arguments,omitempty"`
	CWD               string           `json:"cwd,omitempty"`
	Output            string           `json:"output,omitempty"`
	Payloads          []map[string]any `json:"payloads,omitempty"`
	ExitCode          int              `json:"exit_code,omitempty"`
	TimedOut          bool             `json:"timed_out,omitempty"`
	IsError           bool             `json:"is_error,omitempty"`
	Error             string           `json:"error,omitempty"`
	StructuredContent map[string]any   `json:"structured_content,omitempty"`
}

type localAssistantTurnState struct {
	sessionID    string
	canvasID     string
	session      store.ChatSession
	userText     string
	workspace    store.Workspace
	workspaceDir string
	currentDir   string
	mcpURL       string
}

func parseLocalAssistantDecision(message localIntentLLMMessage) (localAssistantDecision, error) {
	if len(message.ToolCalls) > 0 {
		calls, err := parseLocalAssistantToolCalls(message.ToolCalls)
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
			if _, ok := typed["name"]; ok {
				calls, err := parseLocalAssistantToolCallsAny(typed)
				if err != nil {
					return localAssistantDecision{}, err
				}
				return localAssistantDecision{ToolCalls: calls}, nil
			}
			if _, ok := typed["function"]; ok {
				calls, err := parseLocalAssistantToolCallsAny(typed)
				if err != nil {
					return localAssistantDecision{}, err
				}
				return localAssistantDecision{ToolCalls: calls}, nil
			}
		case []any:
			calls, err := parseLocalAssistantToolCallsAny(typed)
			if err != nil {
				return localAssistantDecision{}, err
			}
			return localAssistantDecision{ToolCalls: calls}, nil
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
	if strings.HasPrefix(trimmed, "{") {
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
		name := strings.TrimSpace(item.Function.Name)
		if name == "" {
			return nil, errors.New("tool call is missing a name")
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
	name := strings.TrimSpace(fmt.Sprint(obj["name"]))
	argsValue := obj["arguments"]
	if function, ok := obj["function"].(map[string]any); ok {
		if name == "" {
			name = strings.TrimSpace(fmt.Sprint(function["name"]))
		}
		if argsValue == nil {
			argsValue = function["arguments"]
		}
	}
	if name == "" {
		return localAssistantToolCall{}, errors.New("tool call is missing a name")
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

func localAssistantAssistantMessage(message localIntentLLMMessage) map[string]any {
	msg := map[string]any{
		"role":    "assistant",
		"content": strings.TrimSpace(message.Content),
	}
	if len(message.ToolCalls) > 0 {
		calls := make([]map[string]any, 0, len(message.ToolCalls))
		for _, tc := range message.ToolCalls {
			calls = append(calls, map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]any{
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				},
			})
		}
		msg["tool_calls"] = calls
	}
	return msg
}

func localAssistantRepairPrompt(err error) string {
	return fmt.Sprintf(
		"Your last response could not be executed: %s. Please try again using the available tools, or answer with plain text.",
		strings.TrimSpace(err.Error()),
	)
}

func (a *App) runLocalAssistantToolLoop(ctx context.Context, req *assistantTurnRequest, prompt string, visual *chatVisualAttachment, onDelta func(fullText string, delta string)) (string, error) {
	if a == nil || req == nil {
		return "", errors.New("assistant turn request is required")
	}
	state, err := a.newLocalAssistantTurnState(req)
	if err != nil {
		return "", err
	}
	toolText := strings.TrimSpace(req.userText)
	if toolText == "" {
		toolText = strings.TrimSpace(req.promptText)
	}
	toolText = normalizeLocalAssistantAddress(toolText)
	toolUserPrompt := localAssistantToolUserPrompt(req, prompt)
	familyInput := toolUserPrompt
	if strings.TrimSpace(familyInput) == "" {
		familyInput = toolText
	}
	family := selectLocalAssistantToolFamily(familyInput)
	catalog, err := a.buildLocalAssistantToolCatalog(state, family, familyInput)
	if err != nil {
		return "", err
	}
	if directPath := strings.TrimSpace(localAssistantDirectOpenFileHint(toolUserPrompt, family)); directPath != "" {
		return a.runLocalAssistantDirectOpenFileCanvas(ctx, &state, catalog, directPath)
	}
	if catalog.RenderGeneratedText {
		return a.runLocalAssistantGeneratedCanvasTurn(ctx, req, visual, state, toolUserPrompt, buildLocalAssistantCanvasPromptContext(req, prompt))
	}
	userPrompt := strings.TrimSpace(prompt)
	if family != localAssistantToolFamilyNone {
		userPrompt = toolUserPrompt
	}
	conversation := []map[string]any{
		{"role": "system", "content": buildLocalAssistantDialoguePrompt(buildLocalAssistantToolPolicy(catalog), localAssistantReasoningHint(req))},
		{"role": "user", "content": buildLocalAssistantUserContent(userPrompt, visual)},
	}
	enableThinking := localAssistantThinkingEnabled(req)
	if req.fastMode {
		fastPrompt := buildLocalAssistantFastPrompt(req.promptText)
		if strings.TrimSpace(fastPrompt) == "" {
			fastPrompt = strings.TrimSpace(req.promptText)
		}
		message, err := a.requestLocalAssistantCompletionWithConfig(ctx, []map[string]any{
			{"role": "user", "content": fastPrompt},
		}, nil, "", enableThinking, assistantLLMFastMaxTokens, onDelta)
		if err != nil {
			return "", err
		}
		if localAssistantUnsupportedControlEnvelope(message.Content) {
			return "", errLocalAssistantUnsupportedResponse
		}
		return strings.TrimSpace(message.Content), nil
	}
	malformedRetries := 0
	toolRequired := family != localAssistantToolFamilyNone && len(catalog.Definitions) > 0
	toolPlanRetries := 0
	toolExecuted := false
	for round := 0; round < assistantLLMMaxToolRounds; round++ {
		maxTokens := assistantLLMDirectMaxTokens
		if toolRequired && !toolExecuted {
			maxTokens = localAssistantInitialToolMaxTokens(family)
		} else if round > 0 {
			maxTokens = assistantLLMToolMaxTokens
		}
		emitDelta := onDelta
		if round > 0 || len(catalog.Definitions) > 0 {
			emitDelta = nil
		}
		message, err := a.requestLocalAssistantCompletionWithConfig(ctx, conversation, catalog.Definitions, "", enableThinking, maxTokens, emitDelta)
		if err != nil {
			return "", err
		}
		decision, err := parseLocalAssistantDecision(message)
		if err != nil {
			if catalog.RenderGeneratedText {
				if recovered, ok := recoverLocalAssistantCanvasTextFromMalformedToolOutput(message.Content); ok {
					confirmation, renderErr := a.renderLocalAssistantCanvasText(ctx, &state, recovered)
					if renderErr != nil {
						return "", renderErr
					}
					return confirmation, nil
				}
			}
			if errors.Is(err, errLocalAssistantUnsupportedResponse) {
				return "", err
			}
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
			if catalog.RenderGeneratedText && !toolExecuted {
				confirmation, retryPrompt, renderErr := a.handleLocalAssistantGeneratedCanvasText(ctx, &state, localAssistantCanvasNeedsStructuredDiagram(toolText), text, toolPlanRetries)
				if renderErr != nil {
					return "", renderErr
				}
				if retryPrompt != "" {
					toolPlanRetries++
					conversation = append(conversation, localAssistantAssistantMessage(message))
					conversation = append(conversation, map[string]any{
						"role":    "user",
						"content": retryPrompt,
					})
					continue
				}
				return confirmation, nil
			}
			if toolRequired && !toolExecuted && toolPlanRetries < assistantLLMToolPlanRetries {
				toolPlanRetries++
				conversation = append(conversation, localAssistantAssistantMessage(message))
				conversation = append(conversation, map[string]any{
					"role":    "user",
					"content": localAssistantToolRequiredPrompt(),
				})
				continue
			}
			// Local model refused to call a tool after retries. Surface its
			// text answer rather than failing the turn; the user still gets
			// something useful and can re-ask with an explicit instruction.
			return text, nil
		}
		if len(decision.ToolCalls) == 0 {
			return "", errors.New("assistant returned neither tool calls nor a final response")
		}
		malformedRetries = 0
		conversation = append(conversation, localAssistantAssistantMessage(message))
		for _, call := range decision.ToolCalls {
			result, execErr := a.executeLocalAssistantToolCall(ctx, &state, catalog, call)
			if execErr != nil {
				return "", execErr
			}
			toolExecuted = true
			for _, resultPayload := range localAssistantToolPayloads(result, state.workspace.ID) {
				if resultPayload == nil {
					continue
				}
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
		sessionID:    req.sessionID,
		canvasID:     strings.TrimSpace(a.canvasSessionIDForWorkspace(workspace)),
		session:      req.session,
		userText:     req.userText,
		workspace:    workspace,
		workspaceDir: workspaceDir,
		currentDir:   workspaceDir,
		mcpURL:       mcpURL,
	}, nil
}

func (a *App) executeLocalAssistantToolCall(ctx context.Context, state *localAssistantTurnState, catalog localAssistantToolCatalog, call localAssistantToolCall) (localAssistantToolResult, error) {
	if err := ctx.Err(); err != nil {
		return localAssistantToolResult{}, err
	}
	executable, ok := catalog.ToolsByName[strings.TrimSpace(call.Name)]
	if !ok {
		return localAssistantToolResult{
			ToolCallID: call.ID,
			ModelName:  call.Name,
			Name:       call.Name,
			Arguments:  call.Arguments,
			IsError:    true,
			Error:      "unsupported local assistant tool",
		}, nil
	}
	args := mergeLocalAssistantToolArguments(executable.DefaultArgs, call.Arguments)
	switch executable.Kind {
	case localAssistantToolKindShell:
		toolCall := call
		toolCall.Name = executable.InternalName
		toolCall.Arguments = args
		result := executeLocalAssistantShellTool(state, toolCall)
		result.ModelName = executable.ModelName
		result.Kind = string(executable.Kind)
		return result, nil
	case localAssistantToolKindSystemAction:
		return a.executeLocalAssistantExplicitSystemActionTool(state, executable, call, args)
	case localAssistantToolKindCanvasWriteText:
		return a.executeLocalAssistantCanvasWriteTextTool(ctx, state, executable, call, args)
	case localAssistantToolKindWorkspaceRead:
		return executeLocalAssistantWorkspaceReadTool(state, executable, call, args), nil
	case localAssistantToolKindMCP:
		return a.executeLocalAssistantBoundMCPTool(ctx, state, executable, call, args)
	default:
		return localAssistantToolResult{
			ToolCallID: call.ID,
			ModelName:  executable.ModelName,
			Name:       executable.InternalName,
			Kind:       string(executable.Kind),
			Arguments:  args,
			IsError:    true,
			Error:      "unsupported local assistant tool",
		}, nil
	}
}

func (a *App) executeLocalAssistantCanvasWriteTextTool(ctx context.Context, state *localAssistantTurnState, tool localAssistantExecutableTool, call localAssistantToolCall, args map[string]any) (localAssistantToolResult, error) {
	text := firstNonEmptyLocalAssistantArgument(args, "content", "text", "body", "markdown_or_text")
	if text == "" || text == "<nil>" {
		return localAssistantToolResult{
			ToolCallID: call.ID,
			ModelName:  tool.ModelName,
			Name:       tool.InternalName,
			Kind:       string(tool.Kind),
			Arguments:  args,
			IsError:    true,
			Error:      "canvas text is required",
		}, nil
	}
	mcpArgs := map[string]any{
		"title":            strings.TrimSpace(fmt.Sprint(args["title"])),
		"markdown_or_text": text,
	}
	for key, value := range tool.DefaultArgs {
		mcpArgs[key] = value
	}
	alias := localAssistantExecutableTool{
		ModelName:    tool.ModelName,
		Kind:         localAssistantToolKindMCP,
		InternalName: tool.InternalName,
	}
	return a.executeLocalAssistantBoundMCPTool(ctx, state, alias, call, mcpArgs)
}

func (a *App) executeLocalAssistantExplicitSystemActionTool(state *localAssistantTurnState, tool localAssistantExecutableTool, call localAssistantToolCall, args map[string]any) (localAssistantToolResult, error) {
	result := localAssistantToolResult{
		ToolCallID: call.ID,
		ModelName:  tool.ModelName,
		Name:       tool.InternalName,
		Kind:       string(tool.Kind),
		Arguments:  args,
	}
	actions := []*SystemAction{{
		Action: tool.InternalName,
		Params: map[string]interface{}{},
	}}
	for key, value := range args {
		actions[0].Params[key] = value
	}
	message, payloads, err := a.executeSystemActionPlan(state.sessionID, state.session, state.userText, actions)
	if err != nil {
		result.IsError = true
		result.Error = err.Error()
		return result, nil
	}
	result.Output = strings.TrimSpace(message)
	result.Payloads = payloads
	result.StructuredContent = map[string]any{
		"actions": len(actions),
	}
	return result, nil
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

func executeLocalAssistantWorkspaceReadTool(state *localAssistantTurnState, tool localAssistantExecutableTool, call localAssistantToolCall, args map[string]any) localAssistantToolResult {
	result := localAssistantToolResult{
		ToolCallID: call.ID,
		ModelName:  tool.ModelName,
		Name:       tool.InternalName,
		Kind:       string(tool.Kind),
		Arguments:  args,
	}
	operation := normalizeLocalAssistantWorkspaceReadOperation(firstNonEmptyLocalAssistantArgument(args, "operation", "mode", "action"))
	switch operation {
	case "list_top_level":
		entries, err := localAssistantWorkspaceTopLevelEntries(state.workspaceDir)
		if err != nil {
			result.IsError = true
			result.Error = err.Error()
			return result
		}
		result.Output = "Top-level entries: " + strings.Join(entries, ", ")
		result.StructuredContent = map[string]any{
			"operation": "list_top_level",
			"entries":   entries,
		}
		return result
	case "read_file":
		path := firstNonEmptyLocalAssistantArgument(args, "path", "file", "target")
		body, resolved, truncated, err := localAssistantReadWorkspaceFile(state.workspaceDir, path)
		if err != nil {
			result.IsError = true
			result.Error = err.Error()
			return result
		}
		result.Output = body
		result.StructuredContent = map[string]any{
			"operation": "read_file",
			"path":      resolved,
			"truncated": truncated,
			"content":   body,
		}
		return result
	case "find_file":
		query := firstNonEmptyLocalAssistantArgument(args, "query", "path", "file", "target")
		limit := intFromAny(args["max_results"], localAssistantWorkspaceFindResultLimit)
		if limit <= 0 {
			limit = localAssistantWorkspaceFindResultLimit
		}
		matches, err := localAssistantFindWorkspaceFiles(state.workspaceDir, query, limit)
		if err != nil {
			result.IsError = true
			result.Error = err.Error()
			return result
		}
		result.Output = "Matches: " + strings.Join(matches, ", ")
		result.StructuredContent = map[string]any{
			"operation": "find_file",
			"query":     query,
			"matches":   matches,
		}
		return result
	default:
		result.IsError = true
		result.Error = "workspace_read operation must be list_top_level, read_file, or find_file"
		return result
	}
}

func (a *App) executeLocalAssistantBoundMCPTool(ctx context.Context, state *localAssistantTurnState, tool localAssistantExecutableTool, call localAssistantToolCall, args map[string]any) (localAssistantToolResult, error) {
	result := localAssistantToolResult{
		ToolCallID: call.ID,
		ModelName:  tool.ModelName,
		Name:       tool.InternalName,
		Kind:       string(tool.Kind),
		Arguments:  args,
	}
	mcpURL := state.mcpURL
	if override := strings.TrimSpace(tool.MCPURL); override != "" {
		mcpURL = override
	}
	if strings.HasPrefix(tool.InternalName, "canvas_") && strings.TrimSpace(state.canvasID) != "" {
		if port, ok := a.tunnels.getPort(state.canvasID); ok && port > 0 {
			mcpURL = fmt.Sprintf("http://127.0.0.1:%d/mcp", port)
		}
	}
	if mcpURL == "" {
		result.IsError = true
		result.Error = "MCP URL is not configured"
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return localAssistantToolResult{}, err
	}
	structuredContent, err := mcpToolsCallURL(mcpURL, tool.InternalName, args)
	if err != nil {
		result.IsError = true
		result.Error = err.Error()
		return result, nil
	}
	result.StructuredContent = structuredContent
	return result, nil
}

func localAssistantToolPayloads(result localAssistantToolResult, workspaceID int64) []map[string]any {
	if strings.TrimSpace(result.Kind) == "" {
		return nil
	}
	switch result.Kind {
	case string(localAssistantToolKindSystemAction):
		return result.Payloads
	case string(localAssistantToolKindShell):
		return []map[string]any{{
			"type":         "shell",
			"command":      strings.TrimSpace(fmt.Sprint(result.Arguments["command"])),
			"cwd":          result.CWD,
			"exit_code":    result.ExitCode,
			"timed_out":    result.TimedOut,
			"output":       result.Output,
			"is_error":     result.IsError,
			"error":        result.Error,
			"workspace_id": workspaceID,
		}}
	case string(localAssistantToolKindMCP):
		return []map[string]any{{
			"type":         "mcp_tool",
			"name":         result.Name,
			"arguments":    result.Arguments,
			"result":       result.StructuredContent,
			"is_error":     result.IsError,
			"error":        result.Error,
			"workspace_id": workspaceID,
		}}
	case string(localAssistantToolKindCanvasWriteText):
		return []map[string]any{{
			"type":         "mcp_tool",
			"name":         result.Name,
			"arguments":    result.Arguments,
			"result":       result.StructuredContent,
			"is_error":     result.IsError,
			"error":        result.Error,
			"workspace_id": workspaceID,
		}}
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

func firstNonEmptyLocalAssistantArgument(args map[string]any, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(args[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func normalizeLocalAssistantWorkspaceReadOperation(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "list", "list_files", "ls", "list_top_level":
		return "list_top_level"
	case "read", "read_file", "open":
		return "read_file"
	case "find", "find_file", "search":
		return "find_file"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func localAssistantWorkspaceTopLevelEntries(workspaceDir string) ([]string, error) {
	entries, err := os.ReadDir(workspaceDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	slices.Sort(names)
	if len(names) == 0 {
		return []string{"(empty)"}, nil
	}
	return names, nil
}

func localAssistantReadWorkspaceFile(workspaceDir string, rawPath string) (string, string, bool, error) {
	resolved, err := resolveLocalAssistantWorkspacePath(workspaceDir, rawPath)
	if err != nil {
		return "", "", false, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", false, err
	}
	if info.IsDir() {
		return "", "", false, fmt.Errorf("%q is a directory", rawPath)
	}
	body, err := os.ReadFile(resolved)
	if err != nil {
		return "", "", false, err
	}
	truncated := false
	if len(body) > localAssistantWorkspaceReadBytes {
		body = body[:localAssistantWorkspaceReadBytes]
		truncated = true
	}
	text := string(body)
	if truncated {
		text += "\n\n[truncated]"
	}
	return text, resolved, truncated, nil
}

func localAssistantFindWorkspaceFiles(workspaceDir string, query string, limit int) ([]string, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		query = "readme"
	}
	matches := make([]string, 0, limit)
	err := filepath.WalkDir(workspaceDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if len(matches) >= limit {
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(workspaceDir, path)
		if err != nil || rel == "." {
			return nil
		}
		normalized := strings.ToLower(filepath.ToSlash(rel))
		if strings.Contains(normalized, query) {
			if d.IsDir() {
				rel += "/"
			}
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return nil, err
	}
	if len(matches) == 0 {
		return []string{"(no matches)"}, nil
	}
	slices.Sort(matches)
	return matches, nil
}

func resolveLocalAssistantWorkspacePath(workspaceDir string, raw string) (string, error) {
	target := strings.TrimSpace(raw)
	if target == "" || target == "<nil>" {
		target = workspaceDir
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(workspaceDir, target)
	}
	target = filepath.Clean(target)
	root := filepath.Clean(strings.TrimSpace(workspaceDir))
	if root == "" {
		return "", errors.New("workspace path is required")
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the workspace", raw)
	}
	return target, nil
}
