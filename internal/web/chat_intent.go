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

const intentLLMSystemPrompt = `You are Tabura's local intent and delegation router. Return JSON only.
Allowed actions:
- switch_project (name)
- switch_model (alias, effort)
- toggle_silent
- toggle_conversation
- cancel_work
- show_status
- delegate
- shell (command)
- open_file_canvas (path)
- chat

Policy:
- Prefer {"action":"chat"} unless the user clearly requests one of the allowed system actions.
- Use {"action":"delegate"} only for explicit delegation requests (for example: "let codex ...", "ask gpt ...", "delegate ...") or clearly complex long-running coding tasks.
- For delegate without an explicit model, set model="codex".
- Keep delegated task text concise and faithful to user intent.
- Use {"action":"shell","command":"..."} only for explicit shell-like requests (for example list/find/grep/read operations).
- Use {"action":"open_file_canvas","path":"..."} when user asks to open/show a specific file on canvas.
- For multi-step tasks, return {"actions":[{"action":"..."}, {"action":"..."}]}.
- For open/show file requests where the exact path is uncertain, prefer a two-step plan: shell find/list first, then open_file_canvas.
- When chaining shell -> open_file_canvas, set path="$last_shell_path".
- In JSON command strings, prefer single quotes inside shell command arguments.

For system actions, return {"action":"<action>", ...params}.
For non-system conversation, return {"action":"chat"}.`

type localIntentClassifierResponse struct {
	Action     string                 `json:"action"`
	Intent     string                 `json:"intent"`
	Confidence float64                `json:"confidence"`
	Entities   map[string]interface{} `json:"entities"`
	Params     map[string]interface{} `json:"params"`
	Name       string                 `json:"name"`
	Alias      string                 `json:"alias"`
	Effort     string                 `json:"effort"`
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
	case "switch_project", "switch_model", "toggle_silent", "toggle_conversation", "cancel_work", "show_status", "delegate", "shell", "open_file_canvas":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func normalizeDelegateModel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "codex":
		return "codex"
	case "gpt", "spark":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func systemActionDelegateTask(params map[string]interface{}) string {
	for _, key := range []string{"task", "prompt", "text"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
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
	if strings.TrimSpace(payload.Effort) != "" {
		params["effort"] = strings.TrimSpace(payload.Effort)
	}
	if actionName == "delegate" {
		model := normalizeDelegateModel(systemActionStringParam(params, "model"))
		if model == "" {
			model = "codex"
		}
		params["model"] = model
		if systemActionDelegateTask(params) == "" {
			params["task"] = trimmedText
		}
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
	switch action.Action {
	case "delegate":
		model := normalizeDelegateModel(systemActionStringParam(action.Params, "model"))
		if model == "" {
			model = "codex"
		}
		action.Params["model"] = model
		if systemActionDelegateTask(action.Params) == "" {
			action.Params["task"] = fallbackText
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

func (a *App) classifyIntentPlanWithLLM(ctx context.Context, text string) ([]*SystemAction, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(a.intentLLMURL), "/")
	if baseURL == "" {
		return nil, nil
	}
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return nil, nil
	}
	if hint := detectDelegationHint(trimmedText); hint.Detected {
		model := normalizeDelegateModel(hint.Model)
		if model == "" {
			model = "codex"
		}
		task := strings.TrimSpace(hint.Task)
		if task == "" {
			task = trimmedText
		}
		return []*SystemAction{{
			Action: "delegate",
			Params: map[string]interface{}{
				"model": model,
				"task":  task,
			},
		}}, nil
	}
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

	actions, err := requestPlan(intentLLMSystemPrompt, trimmedText)
	if err != nil || len(actions) == 0 {
		return actions, err
	}

	if requestRequiresOpenCanvasAction(trimmedText) && !planContainsAction(actions, "open_file_canvas") {
		previousPlanJSON, _ := json.Marshal(actions)
		retrySystemPrompt := intentLLMSystemPrompt + "\n\nConstraint: for explicit open/show/display file requests, final step MUST be open_file_canvas."
		retryUserPrompt := "User request:\n" + trimmedText + "\n\nPrevious invalid plan (missing open_file_canvas):\n" + string(previousPlanJSON)
		if repaired, repairErr := requestPlan(retrySystemPrompt, retryUserPrompt); repairErr == nil && len(repaired) > 0 {
			actions = repaired
		}
		if !planContainsAction(actions, "open_file_canvas") {
			return nil, nil
		}
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
		if len(actions) == 0 {
			return "", nil, false
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, actions)
		if err != nil {
			return "", nil, false
		}
		return message, payloads, true
	}

	if strings.TrimSpace(a.intentLLMURL) != "" {
		llmActions, llmErr := a.classifyIntentPlanWithLLM(ctx, trimmedText)
		if llmErr == nil && len(llmActions) > 0 {
			if message, payloads, ok := tryExecutePlan(llmActions); ok {
				return message, payloads, true
			}
		}
		if requestRequiresOpenCanvasAction(trimmedText) {
			return "I couldn't open that file on canvas. Please provide an exact relative path (for example: docs/CLAUDE.md).", nil, true
		}
		return "", nil, false
	}

	localAction, localConfidence, localErr := a.classifyIntentLocally(ctx, trimmedText)
	if localErr == nil && localAction != nil && localConfidence >= intentClassifierMinConfidence {
		if normalized := normalizeSystemActionForExecution(localAction, trimmedText); normalized != nil {
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
	HiddenPenalty int
	Depth         int
	Length        int
}

func selectBestShellPathFromOutput(cwd, output string) string {
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
			HiddenPenalty: hiddenPenalty,
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
		if left.HiddenPenalty != right.HiddenPenalty {
			return left.HiddenPenalty < right.HiddenPenalty
		}
		if left.Depth != right.Depth {
			return left.Depth < right.Depth
		}
		return left.Length < right.Length
	})
	return candidates[0].Title
}

func preferTopLevelSiblingPath(cwd, candidate string) string {
	cleanCandidate := filepath.ToSlash(strings.TrimSpace(candidate))
	if cleanCandidate == "" {
		return cleanCandidate
	}
	if !strings.Contains(cleanCandidate, "/") {
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
	preferredAbs := filepath.Clean(filepath.Join(root, base))
	info, err := os.Stat(preferredAbs)
	if err != nil || info.IsDir() {
		return cleanCandidate
	}
	return filepath.ToSlash(base)
}

func (a *App) executeSystemActionPlan(sessionID string, session store.ChatSession, actions []*SystemAction) (string, []map[string]interface{}, error) {
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
				lastShellPath = selectBestShellPathFromOutput(targetCWD, payloadOutput)
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
	case "toggle_conversation":
		return "Toggled conversation mode.", map[string]interface{}{"type": "toggle_conversation"}, nil
	case "cancel_work":
		activeCanceled, queuedCanceled := a.cancelChatWork(sessionID)
		delegateCanceled := a.cancelDelegatedJobsForProject(session.ProjectKey)
		total := activeCanceled + queuedCanceled + delegateCanceled
		return fmt.Sprintf("Canceled %d running task(s).", total), nil, nil
	case "show_status":
		status, err := a.fetchCodexStatusMessage(session.ProjectKey)
		if err != nil {
			return "", nil, err
		}
		return status, nil, nil
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
			return fmt.Sprintf("Shell command timed out after %s.\n\n%s", systemActionShellTimeout, rawOutput), map[string]interface{}{
				"type":       "shell",
				"command":    command,
				"cwd":        cwd,
				"exit_code":  -1,
				"timed_out":  true,
				"output":     rawOutput,
				"project_id": targetProject.ID,
			}, nil
		}
		exitCode := 0
		if runErr != nil {
			var exitErr *exec.ExitError
			if errors.As(runErr, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				return "", nil, runErr
			}
			return fmt.Sprintf("Shell command failed (exit %d).\n\n%s", exitCode, rawOutput), map[string]interface{}{
				"type":       "shell",
				"command":    command,
				"cwd":        cwd,
				"exit_code":  exitCode,
				"output":     rawOutput,
				"project_id": targetProject.ID,
			}, nil
		}
		return rawOutput, map[string]interface{}{
			"type":       "shell",
			"command":    command,
			"cwd":        cwd,
			"exit_code":  exitCode,
			"output":     rawOutput,
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
	case "delegate":
		targetProject, err := a.systemActionTargetProject(session)
		if err != nil {
			return "", nil, err
		}
		model := normalizeDelegateModel(
			firstNonEmptyPrompt(
				systemActionStringParam(action.Params, "model"),
				systemActionStringParam(action.Params, "alias"),
			),
		)
		if model == "" {
			return "", nil, errors.New("delegate model must be codex, gpt, or spark")
		}
		task := systemActionDelegateTask(action.Params)
		if task == "" {
			return "", nil, errors.New("delegate task is required")
		}
		cwd := strings.TrimSpace(targetProject.RootPath)
		if cwd == "" {
			cwd = strings.TrimSpace(a.cwdForProjectKey(targetProject.ProjectKey))
		}
		if cwd == "" {
			return "", nil, errors.New("delegate cwd is not available")
		}
		canvasSessionID := strings.TrimSpace(a.canvasSessionIDForProject(targetProject))
		if canvasSessionID == "" {
			return "", nil, errors.New("delegate canvas session is not available")
		}
		port, ok := a.tunnels.getPort(canvasSessionID)
		if !ok {
			return "", nil, fmt.Errorf("no active MCP tunnel for project %q", targetProject.Name)
		}
		status, err := a.mcpToolsCall(port, "delegate_to_model", map[string]interface{}{
			"model":  model,
			"prompt": task,
			"cwd":    cwd,
		})
		if err != nil {
			return "", nil, err
		}
		jobID := strings.TrimSpace(fmt.Sprint(status["job_id"]))
		if jobID == "" || jobID == "<nil>" {
			return "", nil, errors.New("delegate_to_model did not return job_id")
		}
		return fmt.Sprintf("Delegated to %s as job %s.", model, jobID), map[string]interface{}{
			"type":       "delegate",
			"job_id":     jobID,
			"model":      model,
			"project_id": targetProject.ID,
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported action: %s", action.Action)
	}
}
