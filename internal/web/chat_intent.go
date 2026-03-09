package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const (
	DefaultIntentClassifierURL     = "http://127.0.0.1:8425"
	DefaultIntentLLMURL            = "http://127.0.0.1:8426"
	DefaultIntentLLMModel          = "local"
	DefaultIntentLLMProfile        = "qwen3.5-9b"
	DefaultIntentLLMProfileOptions = "qwen3.5-9b,qwen3.5-4b"
	intentClassifierMinConfidence  = 0.8
	intentClassifierRequestTimeout = 75 * time.Millisecond
	intentClassifierResponseLimit  = 64 * 1024
	intentLLMRequestTimeout        = 900 * time.Millisecond
	intentLLMResponseLimit         = 128 * 1024
	systemActionShellTimeout       = 8 * time.Second
	systemActionShellOutputLimit   = 16 * 1024
	systemActionOpenFileSizeLimit  = 256 * 1024
)

const intentLLMSystemPrompt = `You are Tabura's local router. Output JSON only.
Allowed actions: switch_project, switch_workspace, list_workspace_items, list_workspaces, create_workspace, create_workspace_from_git, rename_workspace, delete_workspace, show_workspace_details, workspace_watch_start, workspace_watch_stop, workspace_watch_status, assign_workspace_project, show_workspace_project, create_project, list_project_workspaces, link_workspace_artifact, list_linked_artifacts, switch_model, toggle_silent, toggle_live_dialogue, cancel_work, show_status, shell, open_file_canvas, show_calendar, show_briefing, make_item, delegate_item, snooze_item, split_items, reassign_workspace, reassign_project, clear_workspace, clear_project, capture_idea, refine_idea_note, promote_idea, apply_idea_promotion, create_github_issue, create_github_issue_split, print_item, review_someday, triage_someday, promote_someday, toggle_someday_review_nudge, show_filtered_items, sync_project, sync_sources, map_todoist_project, sync_todoist, create_todoist_task, sync_evernote, sync_bear, promote_bear_checklist, sync_zotero, chat.
Use {"action":"chat"} unless user clearly requests a system action.
For current-information requests (weather, web search, news, prices, schedules, latest/current updates), use {"action":"chat"} and MUST NOT use shell.
For shell-like requests use {"action":"shell","command":"..."}.
For open/show/display file requests, end with {"action":"open_file_canvas","path":"..."}.
If exact path is uncertain, use multi-step {"actions":[...]}: shell search first, then open_file_canvas with path="$last_shell_path".
For item materialization and idea/someday-review requests use make_item, delegate_item, snooze_item, split_items, reassign_workspace, reassign_project, clear_workspace, clear_project, capture_idea, refine_idea_note, promote_idea, apply_idea_promotion, create_github_issue, create_github_issue_split, review_someday, triage_someday, promote_someday, toggle_someday_review_nudge, or show_filtered_items.
Prefer case-insensitive filename search (for example -iname) and use single quotes inside JSON command strings.`

type localIntentClassifierResponse struct {
	Action          string                 `json:"action"`
	Intent          string                 `json:"intent"`
	Confidence      float64                `json:"confidence"`
	Entities        map[string]interface{} `json:"entities"`
	Params          map[string]interface{} `json:"params"`
	Name            string                 `json:"name"`
	Alias           string                 `json:"alias"`
	Effort          string                 `json:"effort"`
	ReasoningEffort string                 `json:"reasoning_effort"`
}

type localIntentLLMChatCompletionResponse struct {
	Choices []localIntentLLMChoice `json:"choices"`
}

type localIntentLLMChoice struct {
	Message localIntentLLMMessage `json:"message"`
}

type localIntentLLMMessage struct {
	Content string `json:"content"`
}

type SystemAction struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"-"`
}

const systemActionLastShellPathPlaceholder = "$last_shell_path"

func parseIntentLLMProfileOptions(raw string) []string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil
	}
	parts := strings.Split(clean, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		token := strings.ToLower(strings.TrimSpace(part))
		if token == "" {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func resolveIntentLLMProfile(raw string) string {
	clean := strings.ToLower(strings.TrimSpace(raw))
	if clean == "" {
		return DefaultIntentLLMProfile
	}
	return clean
}

func ensureIntentLLMProfileOption(options []string, profile string) []string {
	cleanProfile := strings.ToLower(strings.TrimSpace(profile))
	if cleanProfile == "" {
		cleanProfile = DefaultIntentLLMProfile
	}
	for _, option := range options {
		if strings.EqualFold(strings.TrimSpace(option), cleanProfile) {
			return options
		}
	}
	return append([]string{cleanProfile}, options...)
}

func (a *App) localIntentLLMModel() string {
	if a == nil {
		return DefaultIntentLLMModel
	}
	clean := strings.TrimSpace(a.intentLLMModel)
	if clean == "" {
		return DefaultIntentLLMModel
	}
	return clean
}

func extractEmbeddedJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	start := -1
	for idx, r := range trimmed {
		if r == '{' || r == '[' {
			start = idx
			break
		}
	}
	if start < 0 {
		return ""
	}
	for idx := start; idx < len(trimmed); idx++ {
		candidate := strings.TrimSpace(trimmed[start : idx+1])
		if candidate == "" {
			continue
		}
		var decoded interface{}
		if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
			return candidate
		}
	}
	return ""
}

func repairMalformedCommandQuotes(raw string) string {
	const marker = `"command":"`
	if !strings.Contains(raw, marker) {
		return raw
	}
	var out strings.Builder
	out.Grow(len(raw))
	cursor := 0
	for cursor < len(raw) {
		rel := strings.Index(raw[cursor:], marker)
		if rel < 0 {
			out.WriteString(raw[cursor:])
			break
		}
		start := cursor + rel
		endStart := start + len(marker)
		out.WriteString(raw[cursor:endStart])
		cursor = endStart
		for cursor < len(raw) {
			ch := raw[cursor]
			if ch == '\\' && cursor+1 < len(raw) {
				out.WriteByte(raw[cursor])
				out.WriteByte(raw[cursor+1])
				cursor += 2
				continue
			}
			if ch == '"' {
				lookahead := cursor + 1
				for lookahead < len(raw) {
					next := raw[lookahead]
					if next == ' ' || next == '\n' || next == '\r' || next == '\t' {
						lookahead++
						continue
					}
					break
				}
				if lookahead >= len(raw) || raw[lookahead] == ',' || raw[lookahead] == '}' {
					out.WriteByte('"')
					cursor++
					break
				}
				out.WriteByte('\'')
				cursor++
				continue
			}
			out.WriteByte(ch)
			cursor++
		}
	}
	return out.String()
}

func parseSystemAction(raw string) (*SystemAction, error) {
	return parseSystemActionJSON(raw)
}

func parseSystemActions(raw string) ([]*SystemAction, error) {
	return parseSystemActionsJSON(raw)
}

func parseSystemActionObject(obj map[string]interface{}) *SystemAction {
	if obj == nil {
		return nil
	}
	action := normalizeSystemActionName(fmt.Sprint(obj["action"]))
	if action == "" {
		action = normalizeSystemActionName(fmt.Sprint(obj["intent"]))
	}
	if action == "" {
		return nil
	}
	params := make(map[string]interface{}, len(obj))
	for key, value := range obj {
		if strings.EqualFold(strings.TrimSpace(key), "action") || strings.EqualFold(strings.TrimSpace(key), "intent") {
			continue
		}
		params[key] = value
	}
	return &SystemAction{Action: action, Params: params}
}

func parseSystemActionJSON(raw string) (*SystemAction, error) {
	actions, err := parseSystemActionsJSON(raw)
	if err != nil {
		return nil, err
	}
	if len(actions) == 0 {
		return nil, nil
	}
	return actions[0], nil
}

func parseSystemActionsJSON(raw string) ([]*SystemAction, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	decodeJSON := func(candidate string) (interface{}, bool) {
		var decoded interface{}
		if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
			return decoded, true
		}
		repaired := repairMalformedCommandQuotes(candidate)
		if repaired != candidate {
			if err := json.Unmarshal([]byte(repaired), &decoded); err == nil {
				return decoded, true
			}
		}
		return nil, false
	}
	decoded, ok := decodeJSON(trimmed)
	if !ok {
		embedded := extractEmbeddedJSON(trimmed)
		if embedded == "" {
			return nil, nil
		}
		decoded, ok = decodeJSON(embedded)
		if !ok {
			return nil, nil
		}
	}
	collect := func(values []interface{}) []*SystemAction {
		actions := make([]*SystemAction, 0, len(values))
		for _, value := range values {
			obj, _ := value.(map[string]interface{})
			if action := parseSystemActionObject(obj); action != nil {
				actions = append(actions, action)
			}
		}
		return actions
	}
	switch typed := decoded.(type) {
	case map[string]interface{}:
		if rawActions, ok := typed["actions"]; ok {
			items, _ := rawActions.([]interface{})
			actions := collect(items)
			if len(actions) == 0 {
				return nil, nil
			}
			return actions, nil
		}
		action := parseSystemActionObject(typed)
		if action == nil {
			return nil, nil
		}
		return []*SystemAction{action}, nil
	case []interface{}:
		actions := collect(typed)
		if len(actions) == 0 {
			return nil, nil
		}
		return actions, nil
	default:
		return nil, nil
	}
}

func systemActionStringParam(params map[string]interface{}, key string) string {
	return strings.TrimSpace(fmt.Sprint(params[key]))
}

func normalizeSystemActionName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "toggle_conversation":
		return "toggle_live_dialogue"
	case "switch_project", "switch_workspace", "list_workspace_items", "list_workspaces", "create_workspace", "create_workspace_from_git", "rename_workspace", "delete_workspace", "show_workspace_details", "workspace_watch_start", "workspace_watch_stop", "workspace_watch_status", "assign_workspace_project", "show_workspace_project", "create_project", "list_project_workspaces", "link_workspace_artifact", "list_linked_artifacts", "switch_model", "toggle_silent", "toggle_live_dialogue", "cancel_work", "show_status", "shell", "open_file_canvas", "show_calendar", "show_briefing", "make_item", "delegate_item", "snooze_item", "split_items", "reassign_workspace", "reassign_project", "clear_workspace", "clear_project", "capture_idea", "refine_idea_note", "promote_idea", "apply_idea_promotion", "create_github_issue", "create_github_issue_split", "print_item", "review_someday", "triage_someday", "promote_someday", "toggle_someday_review_nudge", "show_filtered_items", "sync_project", "sync_sources", "map_todoist_project", "sync_todoist", "create_todoist_task", "sync_evernote", "sync_bear", "promote_bear_checklist", "sync_zotero", "cursor_open_item", "cursor_triage_item", "cursor_open_path":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func systemActionShellCommand(params map[string]interface{}) string {
	for _, key := range []string{"command", "cmd", "text"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func systemActionOpenPath(params map[string]interface{}) string {
	for _, key := range []string{"path", "file", "target"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func mergeSystemActionParams(target map[string]interface{}, source map[string]interface{}) {
	if source == nil {
		return
	}
	for key, value := range source {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		target[trimmed] = value
	}
}

func stripCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return trimmed
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		return trimmed
	}
	end := len(lines)
	if end > 1 && strings.HasPrefix(strings.TrimSpace(lines[end-1]), "```") {
		end--
	}
	if end <= 1 {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[1:end], "\n"))
}

func (a *App) classifyIntentLocally(ctx context.Context, text string) (*SystemAction, float64, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(a.intentClassifierURL), "/")
	if baseURL == "" {
		return nil, 0, nil
	}
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return nil, 0, nil
	}
	requestBody, _ := json.Marshal(map[string]string{"text": trimmedText})
	requestCtx, cancel := context.WithTimeout(ctx, intentClassifierRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		baseURL+"/classify",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, intentClassifierResponseLimit))
		return nil, 0, fmt.Errorf("intent classifier HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload localIntentClassifierResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, intentClassifierResponseLimit)).Decode(&payload); err != nil {
		return nil, 0, err
	}
	actionName := normalizeSystemActionName(payload.Action)
	if actionName == "" {
		actionName = normalizeSystemActionName(payload.Intent)
	}
	if actionName == "" {
		return nil, payload.Confidence, nil
	}
	params := map[string]interface{}{}
	mergeSystemActionParams(params, payload.Entities)
	mergeSystemActionParams(params, payload.Params)
	if strings.TrimSpace(payload.Name) != "" {
		params["name"] = strings.TrimSpace(payload.Name)
	}
	if strings.TrimSpace(payload.Alias) != "" {
		params["alias"] = strings.TrimSpace(payload.Alias)
	}
	if actionName == "switch_model" && strings.TrimSpace(payload.Effort) != "" {
		params["effort"] = strings.TrimSpace(payload.Effort)
	}
	return &SystemAction{Action: actionName, Params: params}, payload.Confidence, nil
}

func normalizeSystemActionForExecution(action *SystemAction, fallbackText string) *SystemAction {
	if action == nil {
		return nil
	}
	if normalizeSystemActionName(action.Action) == "" {
		return nil
	}
	if action.Params == nil {
		action.Params = map[string]interface{}{}
	}
	if strings.EqualFold(strings.TrimSpace(action.Action), "capture_idea") {
		if strings.TrimSpace(systemActionStringParam(action.Params, "text")) == "" {
			action.Params["text"] = strings.TrimSpace(fallbackText)
		}
		if strings.TrimSpace(systemActionStringParam(action.Params, "capture_mode")) == "" {
			action.Params["capture_mode"] = chatCaptureModeText
		}
	}
	if strings.EqualFold(strings.TrimSpace(action.Action), "refine_idea_note") {
		if strings.TrimSpace(systemActionStringParam(action.Params, "text")) == "" {
			action.Params["text"] = strings.TrimSpace(fallbackText)
		}
	}
	return action
}

func requestRequiresOpenCanvasAction(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	prefixes := []string{"open ", "show ", "display "}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func planContainsAction(actions []*SystemAction, actionName string) bool {
	needle := strings.ToLower(strings.TrimSpace(actionName))
	if needle == "" {
		return false
	}
	for _, action := range actions {
		if action == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(action.Action), needle) {
			return true
		}
	}
	return false
}

func safeFindToken(raw string) string {
	token := strings.TrimSpace(strings.ToLower(raw))
	if token == "" {
		return ""
	}
	var out strings.Builder
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '/' {
			out.WriteRune(r)
		}
	}
	clean := strings.TrimSpace(out.String())
	return strings.Trim(clean, "/")
}

func buildOpenCanvasFallbackPlan(text string) []*SystemAction {
	if !requestRequiresOpenCanvasAction(text) {
		return nil
	}
	hints := extractOpenRequestHints(text)
	patterns := make([]string, 0, 16)
	addPattern := func(pattern string) {
		p := strings.TrimSpace(pattern)
		if p == "" {
			return
		}
		for _, existing := range patterns {
			if strings.EqualFold(existing, p) {
				return
			}
		}
		patterns = append(patterns, p)
	}
	for _, rawHint := range hints {
		hint := safeFindToken(normalizeOpenHintToken(rawHint))
		if hint == "" {
			continue
		}
		base := safeFindToken(filepath.Base(hint))
		stem := safeFindToken(strings.TrimSuffix(base, filepath.Ext(base)))
		if base != "" {
			addPattern(base)
			addPattern(base + ".*")
			addPattern("*" + base + "*")
		}
		if stem != "" && stem != base {
			addPattern(stem)
			addPattern(stem + ".*")
			addPattern("*" + stem + "*")
		}
	}
	if len(patterns) == 0 {
		return nil
	}
	parts := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		parts = append(parts, fmt.Sprintf("-iname '%s'", pattern))
	}
	command := "find . -maxdepth 8 -type f \\( " + strings.Join(parts, " -o ") + " \\) | head -n 80"
	return []*SystemAction{
		{
			Action: "shell",
			Params: map[string]interface{}{"command": command},
		},
		{
			Action: "open_file_canvas",
			Params: map[string]interface{}{"path": systemActionLastShellPathPlaceholder},
		},
	}
}

func ensureOpenCanvasTerminalAction(actions []*SystemAction) []*SystemAction {
	if len(actions) == 0 || planContainsAction(actions, "open_file_canvas") {
		return actions
	}
	hasShell := false
	for _, action := range actions {
		if action == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(action.Action), "shell") {
			continue
		}
		if strings.TrimSpace(systemActionShellCommand(action.Params)) == "" {
			continue
		}
		hasShell = true
		break
	}
	if !hasShell {
		return actions
	}
	repaired := make([]*SystemAction, 0, len(actions)+1)
	repaired = append(repaired, actions...)
	repaired = append(repaired, &SystemAction{
		Action: "open_file_canvas",
		Params: map[string]interface{}{"path": systemActionLastShellPathPlaceholder},
	})
	return repaired
}

func (a *App) classifyIntentPlanWithLLM(ctx context.Context, text string) ([]*SystemAction, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(a.intentLLMURL), "/")
	if baseURL == "" {
		return nil, nil
	}
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return nil, nil
	}
	requiresOpenCanvas := requestRequiresOpenCanvasAction(trimmedText)
	requestPlan := func(systemPrompt string, userPrompt string) ([]*SystemAction, error) {
		requestBody, _ := json.Marshal(map[string]interface{}{
			"model":       a.localIntentLLMModel(),
			"temperature": 0,
			"max_tokens":  256,
			"response_format": map[string]interface{}{
				"type": "json_object",
			},
			"chat_template_kwargs": map[string]interface{}{
				"enable_thinking": false,
			},
			"messages": []map[string]string{
				{"role": "system", "content": systemPrompt},
				{"role": "user", "content": userPrompt},
			},
		})
		requestCtx, cancel := context.WithTimeout(ctx, intentLLMRequestTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(
			requestCtx,
			http.MethodPost,
			baseURL+"/v1/chat/completions",
			bytes.NewReader(requestBody),
		)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, intentLLMResponseLimit))
			return nil, fmt.Errorf("intent llm HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var payload localIntentLLMChatCompletionResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, intentLLMResponseLimit)).Decode(&payload); err != nil {
			return nil, err
		}
		if len(payload.Choices) == 0 {
			return nil, nil
		}
		content := strings.TrimSpace(payload.Choices[0].Message.Content)
		if content == "" {
			return nil, nil
		}
		actions, parseErr := parseSystemActionsJSON(stripCodeFence(content))
		if parseErr != nil {
			return nil, parseErr
		}
		if len(actions) == 0 {
			return nil, nil
		}
		normalized := make([]*SystemAction, 0, len(actions))
		for _, action := range actions {
			if normalizedAction := normalizeSystemActionForExecution(action, trimmedText); normalizedAction != nil {
				normalized = append(normalized, normalizedAction)
			}
		}
		if len(normalized) == 0 {
			return nil, nil
		}
		return normalized, nil
	}

	initialSystemPrompt := intentLLMSystemPrompt
	if requiresOpenCanvas {
		initialSystemPrompt += "\n\nConstraint: for explicit open/show/display file requests you MUST return an actions array whose final step is open_file_canvas. If path is uncertain, include a shell search step first and then use path=\"$last_shell_path\"."
	}
	actions, err := requestPlan(initialSystemPrompt, trimmedText)
	if err != nil {
		return nil, err
	}
	if requiresOpenCanvas && !planContainsAction(actions, "open_file_canvas") {
		previousPlanJSON := "null"
		if len(actions) > 0 {
			if encoded, marshalErr := json.Marshal(actions); marshalErr == nil {
				previousPlanJSON = string(encoded)
			}
		}
		hints := extractOpenRequestHints(trimmedText)
		hintText := "(none)"
		if len(hints) > 0 {
			hintText = strings.Join(hints, ", ")
		}
		retrySystemPrompt := intentLLMSystemPrompt + "\n\nConstraint: for explicit open/show/display file requests you MUST return an actions array whose final step is open_file_canvas. If path is uncertain, include a shell search step first and then use path=\"$last_shell_path\"."
		retryUserPrompt := "User request:\n" + trimmedText + "\n\nExtracted filename hints:\n" + hintText + "\n\nPrevious invalid plan (missing open_file_canvas or empty):\n" + previousPlanJSON
		if repaired, repairErr := requestPlan(retrySystemPrompt, retryUserPrompt); repairErr == nil && len(repaired) > 0 {
			actions = repaired
		}
		if !planContainsAction(actions, "open_file_canvas") {
			actions = ensureOpenCanvasTerminalAction(actions)
		}
		if !planContainsAction(actions, "open_file_canvas") {
			return nil, nil
		}
	}
	if len(actions) == 0 {
		return nil, nil
	}
	return actions, nil
}

func (a *App) classifyIntentWithLLM(ctx context.Context, text string) (*SystemAction, error) {
	actions, err := a.classifyIntentPlanWithLLM(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(actions) == 0 {
		return nil, nil
	}
	return actions[0], nil
}

func (a *App) classifyAndExecuteSystemAction(ctx context.Context, sessionID string, session store.ChatSession, text string) (string, []map[string]interface{}, bool) {
	return a.classifyAndExecuteSystemActionWithCursor(ctx, sessionID, session, text, nil)
}

func (a *App) classifyAndExecuteSystemActionWithCursor(ctx context.Context, sessionID string, session store.ChatSession, text string, cursor *chatCursorContext) (string, []map[string]interface{}, bool) {
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return "", nil, false
	}
	tryExecutePlan := func(actions []*SystemAction) (string, []map[string]interface{}, bool) {
		enforced := enforceRoutingPolicy(trimmedText, actions)
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return "", nil, false
		}
		return message, payloads, true
	}

	if pending := a.popPendingDangerousAction(sessionID); pending != nil {
		if isExplicitDangerConfirm(trimmedText) {
			message, payloads, err := a.executeSystemActionPlanUnsafe(sessionID, session, pending.UserText, pending.Actions)
			if err != nil {
				return fmt.Sprintf("Confirmation failed: %v", err), nil, true
			}
			return message, payloads, true
		}
		if isExplicitDangerDecline(trimmedText) {
			return "Canceled dangerous action.", []map[string]interface{}{{"type": "confirmation_canceled"}}, true
		}
	}

	captureMode := a.chatCaptureModes.consume(sessionID)
	if inlineSourceSyncAction := parseInlineSourceSyncIntent(trimmedText); inlineSourceSyncAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineSourceSyncAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return sourceSyncActionFailurePrefix(inlineSourceSyncAction.Action) + err.Error(), nil, true
		}
		return message, payloads, true
	}
	now := time.Now().UTC()
	if a != nil && a.calendarNow != nil {
		now = a.calendarNow().UTC()
	}
	if inlineCalendarAction := parseInlineCalendarIntent(trimmedText, now); inlineCalendarAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineCalendarAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return calendarActionFailurePrefix(inlineCalendarAction.Action) + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineBriefingAction := parseInlineBriefingIntent(trimmedText, now); inlineBriefingAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineBriefingAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return briefingActionFailurePrefix(inlineBriefingAction.Action) + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineTodoistAction := parseInlineTodoistIntent(trimmedText); inlineTodoistAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineTodoistAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return todoistActionFailurePrefix(inlineTodoistAction.Action) + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineEvernoteAction := parseInlineEvernoteIntent(trimmedText); inlineEvernoteAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineEvernoteAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return evernoteActionFailurePrefix(inlineEvernoteAction.Action) + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineBearAction := parseInlineBearIntent(trimmedText); inlineBearAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineBearAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return bearActionFailurePrefix(inlineBearAction.Action) + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineZoteroAction := parseInlineZoteroIntent(trimmedText); inlineZoteroAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineZoteroAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return zoteroActionFailurePrefix(inlineZoteroAction.Action) + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineCursorAction := parseInlineCursorIntent(trimmedText, cursor); inlineCursorAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineCursorAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return "I couldn't resolve the pointed selection: " + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineItemAction := parseInlineItemIntentWithCaptureMode(trimmedText, time.Now().UTC(), captureMode); inlineItemAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineItemAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return itemActionFailurePrefix(inlineItemAction.Action) + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineGitHubActions := parseInlineGitHubIssueActions(trimmedText); len(inlineGitHubActions) > 0 {
		enforced := enforceRoutingPolicy(trimmedText, inlineGitHubActions)
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return githubIssueActionFailurePrefix(enforced) + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineArtifactAction := parseInlineArtifactLinkIntent(trimmedText); inlineArtifactAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineArtifactAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return "I couldn't resolve the artifact linking request: " + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineWorkspaceAction := parseInlineWorkspaceIntent(trimmedText); inlineWorkspaceAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineWorkspaceAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return "I couldn't resolve the workspace request: " + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if inlineProjectAction := parseInlineProjectIntent(trimmedText); inlineProjectAction != nil {
		enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{inlineProjectAction})
		if len(enforced) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return "I couldn't resolve the project request: " + err.Error(), nil, true
		}
		return message, payloads, true
	}
	if strings.TrimSpace(a.intentLLMURL) != "" {
		llmActions, llmErr := a.classifyIntentPlanWithLLM(ctx, trimmedText)
		if llmErr == nil {
			if message, payloads, ok := tryExecutePlan(llmActions); ok {
				return message, payloads, true
			}
		}
		if requestRequiresOpenCanvasAction(trimmedText) {
			if fallbackPlan := buildOpenCanvasFallbackPlan(trimmedText); len(fallbackPlan) > 0 {
				if message, payloads, ok := tryExecutePlan(fallbackPlan); ok {
					return message, payloads, true
				}
			}
			return "I couldn't open that file on canvas. Please provide an exact relative path (for example: docs/CLAUDE.md).", nil, true
		}
	}

	localAction, localConfidence, localErr := a.classifyIntentLocally(ctx, trimmedText)
	if localErr == nil && localAction != nil && localConfidence >= intentClassifierMinConfidence {
		if normalized := normalizeSystemActionForExecution(localAction, trimmedText); normalized != nil {
			if isItemSystemAction(normalized.Action) && strings.TrimSpace(systemActionStringParam(normalized.Params, "capture_mode")) == "" {
				normalized.Params["capture_mode"] = captureMode
			}
			if isItemSystemAction(normalized.Action) {
				enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{normalized})
				if len(enforced) == 0 {
					return "", nil, false
				}
				message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
				if err != nil {
					if strings.HasPrefix(normalized.Action, "create_github_issue") {
						return githubIssueActionFailurePrefix(enforced) + err.Error(), nil, true
					}
					return itemActionFailurePrefix(normalized.Action) + err.Error(), nil, true
				}
				return message, payloads, true
			}
			if normalized.Action != "shell" && normalized.Action != "open_file_canvas" {
				if message, payloads, ok := tryExecutePlan([]*SystemAction{normalized}); ok {
					return message, payloads, true
				}
			}
		}
	}
	if localErr == nil && localAction == nil && localConfidence >= intentClassifierMinConfidence {
		return "", nil, false
	}
	return "", nil, false
}
