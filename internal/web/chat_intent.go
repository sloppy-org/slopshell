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
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
Allowed actions: switch_project, switch_model, toggle_silent, toggle_live_dialogue, cancel_work, show_status, shell, open_file_canvas, make_item, delegate_item, snooze_item, split_items, chat.
Use {"action":"chat"} unless user clearly requests a system action.
For current-information requests (weather, web search, news, prices, schedules, latest/current updates), use {"action":"chat"} and MUST NOT use shell.
For shell-like requests use {"action":"shell","command":"..."}.
For open/show/display file requests, end with {"action":"open_file_canvas","path":"..."}.
If exact path is uncertain, use multi-step {"actions":[...]}: shell search first, then open_file_canvas with path="$last_shell_path".
For item materialization requests use make_item, delegate_item, snooze_item, or split_items.
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
	case "switch_project", "switch_model", "toggle_silent", "toggle_live_dialogue", "cancel_work", "show_status", "shell", "open_file_canvas", "make_item", "delegate_item", "snooze_item", "split_items":
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
	_ = fallbackText
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
	clean = strings.Trim(clean, "/")
	return clean
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
			Params: map[string]interface{}{
				"command": command,
			},
		},
		{
			Action: "open_file_canvas",
			Params: map[string]interface{}{
				"path": systemActionLastShellPathPlaceholder,
			},
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
		Params: map[string]interface{}{
			"path": systemActionLastShellPathPlaceholder,
		},
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
			return "Canceled dangerous action.", []map[string]interface{}{{
				"type": "confirmation_canceled",
			}}, true
		}
	}

	if inlineItemAction := parseInlineItemIntent(trimmedText, time.Now().UTC()); inlineItemAction != nil {
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
			if isItemSystemAction(normalized.Action) {
				enforced := enforceRoutingPolicy(trimmedText, []*SystemAction{normalized})
				if len(enforced) == 0 {
					return "", nil, false
				}
				message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
				if err != nil {
					return itemActionFailurePrefix(normalized.Action) + err.Error(), nil, true
				}
				return message, payloads, true
			}
			// Route tool actions through Qwen plan decoding so one prompt can trigger
			// multiple coordinated actions (e.g., shell + open_file_canvas).
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

func firstShellPathFromOutput(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		candidate := strings.TrimSpace(line)
		if candidate == "" {
			continue
		}
		if candidate == "(no output)" {
			continue
		}
		candidate = strings.TrimPrefix(candidate, "./")
		if candidate == "." || candidate == ".." {
			continue
		}
		return candidate
	}
	return ""
}

type shellPathCandidate struct {
	Title         string
	HintScore     int
	HiddenPenalty int
	NoisyPenalty  int
	Depth         int
	Length        int
}

var quotedTextPattern = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)

func normalizeOpenHintToken(raw string) string {
	token := strings.ToLower(strings.TrimSpace(raw))
	token = strings.Trim(token, " \t\r\n`'\".,:;!?()[]{}<>")
	token = strings.TrimPrefix(token, "./")
	token = strings.Trim(token, "/")
	token = strings.ReplaceAll(token, "\\", "/")
	return token
}

func extractOpenRequestHints(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	stopwords := map[string]struct{}{
		"open": {}, "show": {}, "display": {}, "read": {}, "view": {}, "edit": {},
		"the": {}, "a": {}, "an": {}, "please": {}, "file": {}, "files": {},
		"on": {}, "in": {}, "at": {}, "to": {}, "from": {}, "for": {},
		"canvas": {}, "project": {}, "this": {}, "that": {}, "my": {},
	}
	addable := func(token string, seen map[string]struct{}, out *[]string) {
		token = normalizeOpenHintToken(token)
		if token == "" {
			return
		}
		if _, blocked := stopwords[token]; blocked {
			return
		}
		if len(token) < 3 && !strings.Contains(token, ".") && !strings.Contains(token, "/") {
			return
		}
		if _, exists := seen[token]; exists {
			return
		}
		seen[token] = struct{}{}
		*out = append(*out, token)
	}

	hints := make([]string, 0, 8)
	seen := map[string]struct{}{}

	// Quoted file/path fragments are usually strong user hints.
	for _, match := range quotedTextPattern.FindAllStringSubmatch(trimmed, -1) {
		for _, group := range match[1:] {
			if strings.TrimSpace(group) == "" {
				continue
			}
			addable(group, seen, &hints)
		}
	}

	fields := strings.Fields(strings.ToLower(trimmed))
	verbs := map[string]struct{}{"open": {}, "show": {}, "display": {}, "read": {}, "view": {}, "edit": {}}
	for i, field := range fields {
		verb := normalizeOpenHintToken(field)
		if _, isVerb := verbs[verb]; !isVerb {
			continue
		}
		for j := i + 1; j < len(fields) && j <= i+6; j++ {
			addable(fields[j], seen, &hints)
		}
	}

	// Extract explicit file/path-like tokens anywhere in the request.
	for _, field := range strings.Fields(trimmed) {
		token := normalizeOpenHintToken(field)
		if token == "" {
			continue
		}
		if strings.Contains(token, ".") || strings.Contains(token, "/") {
			addable(token, seen, &hints)
			base := normalizeOpenHintToken(filepath.Base(token))
			addable(base, seen, &hints)
			stem := normalizeOpenHintToken(strings.TrimSuffix(base, filepath.Ext(base)))
			addable(stem, seen, &hints)
		}
	}
	return hints
}

func scoreShellPathCandidate(title string, hints []string) int {
	if len(hints) == 0 {
		return 0
	}
	cleanTitle := filepath.ToSlash(strings.ToLower(strings.TrimSpace(title)))
	if cleanTitle == "" {
		return 0
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(cleanTitle)))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	score := 0
	for _, rawHint := range hints {
		hint := normalizeOpenHintToken(rawHint)
		if hint == "" {
			continue
		}
		hintBase := strings.ToLower(strings.TrimSpace(filepath.Base(hint)))
		hintStem := strings.TrimSuffix(hintBase, filepath.Ext(hintBase))
		switch {
		case cleanTitle == hint || base == hintBase:
			score += 120
		case stem != "" && (stem == hintBase || stem == hintStem):
			score += 90
		case strings.HasSuffix(cleanTitle, "/"+hintBase):
			score += 70
		case strings.Contains(base, hintBase):
			score += 55
		case strings.Contains(cleanTitle, hint):
			score += 35
		}
	}
	return score
}

func shellPathNoisyPenalty(title string) int {
	clean := filepath.ToSlash(strings.ToLower(strings.TrimSpace(title)))
	if clean == "" {
		return 0
	}
	penalty := 0
	segments := strings.Split(clean, "/")
	for _, segment := range segments {
		switch strings.TrimSpace(segment) {
		case "node_modules", ".venv", "vendor", "dist", "build", "target", "gcc-build", "__pycache__":
			penalty += 2
		}
	}
	return penalty
}

func selectBestShellPathFromOutput(cwd, output string, hints []string) string {
	root := strings.TrimSpace(cwd)
	if root == "" {
		return firstShellPathFromOutput(output)
	}
	lines := strings.Split(output, "\n")
	candidates := make([]shellPathCandidate, 0, len(lines))
	for _, line := range lines {
		candidate := strings.TrimSpace(line)
		if candidate == "" || candidate == "(no output)" {
			continue
		}
		candidate = strings.TrimPrefix(candidate, "./")
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == "." || candidate == ".." {
			continue
		}
		absPath, canvasTitle, err := resolveCanvasFilePath(root, candidate)
		if err != nil {
			continue
		}
		info, statErr := os.Stat(absPath)
		if statErr != nil || info.IsDir() {
			continue
		}
		title := filepath.ToSlash(strings.TrimSpace(canvasTitle))
		if title == "" {
			continue
		}
		segments := strings.Split(title, "/")
		hiddenPenalty := 0
		for _, segment := range segments {
			seg := strings.TrimSpace(segment)
			if strings.HasPrefix(seg, ".") {
				hiddenPenalty++
			}
		}
		depth := strings.Count(title, "/")
		candidates = append(candidates, shellPathCandidate{
			Title:         title,
			HintScore:     scoreShellPathCandidate(title, hints),
			HiddenPenalty: hiddenPenalty,
			NoisyPenalty:  shellPathNoisyPenalty(title),
			Depth:         depth,
			Length:        len(title),
		})
	}
	if len(candidates) == 0 {
		return firstShellPathFromOutput(output)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.HintScore != right.HintScore {
			return left.HintScore > right.HintScore
		}
		if left.HiddenPenalty != right.HiddenPenalty {
			return left.HiddenPenalty < right.HiddenPenalty
		}
		if left.NoisyPenalty != right.NoisyPenalty {
			return left.NoisyPenalty < right.NoisyPenalty
		}
		if left.Depth != right.Depth {
			return left.Depth < right.Depth
		}
		return left.Length < right.Length
	})
	return candidates[0].Title
}

func resolveRootTopLevelFile(root, rel string) (string, bool) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(rel) == "" {
		return "", false
	}
	cleanRel := strings.TrimSpace(filepath.Base(filepath.Clean(rel)))
	if cleanRel == "" || cleanRel == "." || cleanRel == ".." {
		return "", false
	}
	abs := filepath.Clean(filepath.Join(root, cleanRel))
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		return filepath.ToSlash(cleanRel), true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		if strings.EqualFold(name, cleanRel) {
			return filepath.ToSlash(name), true
		}
	}
	return "", false
}

func preferTopLevelSiblingPath(cwd, candidate string) string {
	cleanCandidate := filepath.ToSlash(strings.TrimSpace(candidate))
	if cleanCandidate == "" {
		return cleanCandidate
	}
	base := strings.TrimSpace(filepath.Base(cleanCandidate))
	if base == "" || base == "." || base == ".." {
		return cleanCandidate
	}
	root := strings.TrimSpace(cwd)
	if root == "" {
		return cleanCandidate
	}

	if resolved, ok := resolveRootTopLevelFile(root, base); ok {
		return resolved
	}

	ext := strings.TrimSpace(filepath.Ext(base))
	if ext == "" {
		stem := strings.TrimSpace(base)
		variants := []string{
			stem + ".md",
			stem + ".markdown",
			stem + ".txt",
			stem + ".rst",
			stem + ".adoc",
		}
		for _, variant := range variants {
			if resolved, ok := resolveRootTopLevelFile(root, variant); ok {
				return resolved
			}
		}
	}
	return cleanCandidate
}

func (a *App) executeSystemActionPlan(sessionID string, session store.ChatSession, userText string, actions []*SystemAction) (string, []map[string]interface{}, error) {
	actions = enforceRoutingPolicy(userText, actions)
	if len(actions) == 0 {
		return "", nil, errors.New("action plan is empty")
	}
	if guardMessage, guardPayloads, blocked := a.guardDangerousSystemActionPlan(sessionID, userText, actions); blocked {
		return guardMessage, guardPayloads, nil
	}
	return a.executeSystemActionPlanUnsafe(sessionID, session, userText, actions)
}

func (a *App) executeSystemActionPlanUnsafe(sessionID string, session store.ChatSession, userText string, actions []*SystemAction) (string, []map[string]interface{}, error) {
	if len(actions) == 0 {
		return "", nil, errors.New("action plan is empty")
	}
	messages := make([]string, 0, len(actions))
	payloads := make([]map[string]interface{}, 0, len(actions))
	lastShellPath := ""
	targetProject, targetErr := a.systemActionTargetProject(session)
	targetCWD := ""
	if targetErr == nil {
		targetCWD = strings.TrimSpace(targetProject.RootPath)
		if targetCWD == "" {
			targetCWD = strings.TrimSpace(a.cwdForProjectKey(targetProject.ProjectKey))
		}
	}
	requestHints := extractOpenRequestHints(userText)
	for _, action := range actions {
		if action == nil {
			continue
		}
		resolved := &SystemAction{
			Action: action.Action,
			Params: map[string]interface{}{},
		}
		for key, value := range action.Params {
			resolved.Params[key] = value
		}
		if resolved.Action == "open_file_canvas" {
			path := systemActionOpenPath(resolved.Params)
			if strings.EqualFold(strings.TrimSpace(path), systemActionLastShellPathPlaceholder) {
				if strings.TrimSpace(lastShellPath) == "" {
					return "", nil, errors.New("open_file_canvas requires a resolved shell path")
				}
				resolved.Params["path"] = preferTopLevelSiblingPath(targetCWD, lastShellPath)
			}
		}
		message, payload, err := a.executeSystemAction(sessionID, session, resolved)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(message) != "" {
			messages = append(messages, strings.TrimSpace(message))
		}
		if payload != nil {
			payloads = append(payloads, payload)
			payloadType := strings.TrimSpace(fmt.Sprint(payload["type"]))
			payloadOutput := strings.TrimSpace(fmt.Sprint(payload["output"]))
			if strings.EqualFold(payloadType, "shell") && payloadOutput != "" && payloadOutput != "<nil>" {
				lastShellPath = selectBestShellPathFromOutput(targetCWD, payloadOutput, requestHints)
			}
		}
	}
	if len(messages) == 0 {
		messages = append(messages, "Done.")
	}
	return strings.Join(messages, "\n\n"), payloads, nil
}

func (a *App) systemActionTargetProject(session store.ChatSession) (store.Project, error) {
	projectKey := strings.TrimSpace(session.ProjectKey)
	if projectKey != "" {
		project, err := a.store.GetProjectByProjectKey(projectKey)
		if err == nil && !isHubProject(project) {
			return project, nil
		}
	}
	return a.hubPrimaryProject()
}

func truncateSystemActionOutput(text string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = systemActionShellOutputLimit
	}
	if len(text) <= maxBytes {
		return text
	}
	if maxBytes <= 24 {
		return text[:maxBytes]
	}
	return text[:maxBytes] + "\n...(truncated)"
}

type shellCommandExecution struct {
	Output   string
	ExitCode int
	TimedOut bool
	RunErr   error
}

func executeShellCommand(command string, cwd string) shellCommandExecution {
	commandCtx, cancel := context.WithTimeout(context.Background(), systemActionShellTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "bash", "-lc", command)
	cmd.Dir = cwd

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	runErr := cmd.Run()

	rawOutput := strings.TrimSpace(output.String())
	rawOutput = truncateSystemActionOutput(rawOutput, systemActionShellOutputLimit)
	if rawOutput == "" {
		rawOutput = "(no output)"
	}

	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		return shellCommandExecution{
			Output:   rawOutput,
			ExitCode: -1,
			TimedOut: true,
			RunErr:   runErr,
		}
	}

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	return shellCommandExecution{
		Output:   rawOutput,
		ExitCode: exitCode,
		TimedOut: false,
		RunErr:   runErr,
	}
}

func suggestShellCommandRetry(command string, output string) (string, string, bool) {
	cleanCommand := strings.TrimSpace(command)
	if cleanCommand == "" || !strings.Contains(cleanCommand, "jq") {
		return "", "", false
	}
	cleanOutput := strings.TrimSpace(output)
	if cleanOutput == "" {
		return "", "", false
	}
	lowerOutput := strings.ToLower(cleanOutput)
	if !strings.Contains(lowerOutput, "jq: error: syntax error") || !strings.Contains(lowerOutput, "compile error") {
		return "", "", false
	}
	lines := strings.Split(cleanOutput, "\n")
	for _, line := range lines {
		candidate := strings.TrimSpace(line)
		if candidate == "" || !strings.HasPrefix(candidate, ".") {
			continue
		}
		if !(strings.HasSuffix(candidate, "}") || strings.HasSuffix(candidate, "]")) {
			continue
		}
		fixedCandidate := strings.TrimSuffix(strings.TrimSuffix(candidate, "}"), "]")
		if strings.TrimSpace(fixedCandidate) == "" || fixedCandidate == candidate {
			continue
		}
		fixedCommand := strings.Replace(cleanCommand, candidate, fixedCandidate, 1)
		if fixedCommand == cleanCommand {
			continue
		}
		return fixedCommand, fmt.Sprintf("fixed jq filter typo (%s -> %s)", candidate, fixedCandidate), true
	}
	return "", "", false
}

func (a *App) executeSystemAction(sessionID string, session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	if action == nil {
		return "", nil, errors.New("system action is required")
	}
	switch action.Action {
	case "switch_project":
		targetName := systemActionStringParam(action.Params, "name")
		project, err := a.hubFindProjectByName(targetName)
		if err != nil {
			return "", nil, err
		}
		activated, err := a.activateProject(project.ID)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Switched to %s.", activated.Name), map[string]interface{}{
			"type":       "switch_project",
			"project_id": activated.ID,
		}, nil
	case "switch_model":
		targetProject, err := a.systemActionTargetProject(session)
		if err != nil {
			return "", nil, err
		}
		updated, err := a.updateProjectChatModel(
			targetProject.ID,
			systemActionStringParam(action.Params, "alias"),
			systemActionStringParam(action.Params, "effort"),
		)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf(
				"Model for %s set to %s (%s).",
				updated.Name,
				updated.ChatModel,
				updated.ChatModelReasoningEffort,
			), map[string]interface{}{
				"type":       "switch_model",
				"project_id": updated.ID,
				"alias":      updated.ChatModel,
				"effort":     updated.ChatModelReasoningEffort,
			}, nil
	case "toggle_silent":
		return "Toggled silent mode.", map[string]interface{}{"type": "toggle_silent"}, nil
	case "toggle_live_dialogue":
		return "Toggled Live Dialogue.", map[string]interface{}{"type": "toggle_live_dialogue"}, nil
	case "cancel_work":
		activeCanceled, queuedCanceled := a.cancelChatWork(sessionID)
		total := activeCanceled + queuedCanceled
		return fmt.Sprintf("Canceled %d running task(s).", total), nil, nil
	case "show_status":
		status, err := a.fetchCodexStatusMessage(session.ProjectKey)
		if err != nil {
			return "", nil, err
		}
		return status, nil, nil
	case "make_item", "delegate_item", "snooze_item", "split_items":
		return a.createConversationItem(sessionID, session, action)
	case "shell":
		targetProject, err := a.systemActionTargetProject(session)
		if err != nil {
			return "", nil, err
		}
		command := systemActionShellCommand(action.Params)
		if command == "" {
			return "", nil, errors.New("shell command is required")
		}
		cwd := strings.TrimSpace(targetProject.RootPath)
		if cwd == "" {
			cwd = strings.TrimSpace(a.cwdForProjectKey(targetProject.ProjectKey))
		}
		if cwd == "" {
			return "", nil, errors.New("shell cwd is not available")
		}
		execResult := executeShellCommand(command, cwd)
		if execResult.TimedOut {
			return fmt.Sprintf("Shell command timed out after %s.\n\n%s", systemActionShellTimeout, execResult.Output), map[string]interface{}{
				"type":       "shell",
				"command":    command,
				"cwd":        cwd,
				"exit_code":  -1,
				"timed_out":  true,
				"output":     execResult.Output,
				"project_id": targetProject.ID,
			}, nil
		}

		if execResult.RunErr != nil && execResult.ExitCode == 0 {
			return "", nil, execResult.RunErr
		}

		if execResult.ExitCode != 0 {
			if fixedCommand, fixReason, retry := suggestShellCommandRetry(command, execResult.Output); retry {
				retryResult := executeShellCommand(fixedCommand, cwd)
				if !retryResult.TimedOut && retryResult.RunErr == nil && retryResult.ExitCode == 0 {
					return fmt.Sprintf("Shell command auto-corrected (%s).\n\n%s", fixReason, retryResult.Output), map[string]interface{}{
						"type":                "shell",
						"command":             fixedCommand,
						"original_command":    command,
						"cwd":                 cwd,
						"exit_code":           0,
						"output":              retryResult.Output,
						"project_id":          targetProject.ID,
						"auto_corrected":      true,
						"auto_correct_reason": fixReason,
					}, nil
				}
			}
			return fmt.Sprintf("Shell command failed (exit %d).\n\n%s", execResult.ExitCode, execResult.Output), map[string]interface{}{
				"type":       "shell",
				"command":    command,
				"cwd":        cwd,
				"exit_code":  execResult.ExitCode,
				"output":     execResult.Output,
				"project_id": targetProject.ID,
			}, nil
		}
		return execResult.Output, map[string]interface{}{
			"type":       "shell",
			"command":    command,
			"cwd":        cwd,
			"exit_code":  execResult.ExitCode,
			"output":     execResult.Output,
			"project_id": targetProject.ID,
		}, nil
	case "open_file_canvas":
		targetProject, err := a.systemActionTargetProject(session)
		if err != nil {
			return "", nil, err
		}
		rawPath := systemActionOpenPath(action.Params)
		if rawPath == "" {
			return "", nil, errors.New("open_file_canvas path is required")
		}
		cwd := strings.TrimSpace(targetProject.RootPath)
		if cwd == "" {
			cwd = strings.TrimSpace(a.cwdForProjectKey(targetProject.ProjectKey))
		}
		if cwd == "" {
			return "", nil, errors.New("open_file_canvas cwd is not available")
		}
		absPath, canvasTitle, err := resolveCanvasFilePath(cwd, rawPath)
		if err != nil {
			return "", nil, err
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return "", nil, err
		}
		if info.IsDir() {
			return "", nil, fmt.Errorf("path %q is a directory", rawPath)
		}
		if info.Size() > systemActionOpenFileSizeLimit {
			return "", nil, fmt.Errorf("file %q exceeds %d bytes", rawPath, systemActionOpenFileSizeLimit)
		}
		contentBytes, err := os.ReadFile(absPath)
		if err != nil {
			return "", nil, err
		}
		canvasSessionID := strings.TrimSpace(a.canvasSessionIDForProject(targetProject))
		if canvasSessionID == "" {
			return "", nil, errors.New("canvas session is not available")
		}
		port, ok := a.tunnels.getPort(canvasSessionID)
		if !ok {
			return "", nil, fmt.Errorf("no active MCP tunnel for project %q", targetProject.Name)
		}
		if _, err := a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
			"session_id":       canvasSessionID,
			"kind":             "text",
			"title":            canvasTitle,
			"markdown_or_text": string(contentBytes),
		}); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Opened %s on canvas.", canvasTitle), map[string]interface{}{
			"type":       "open_file_canvas",
			"path":       canvasTitle,
			"project_id": targetProject.ID,
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported action: %s", action.Action)
	}
}
