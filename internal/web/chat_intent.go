package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const (
	DefaultIntentClassifierURL     = "http://127.0.0.1:8425"
	DefaultIntentLLMURL            = "http://127.0.0.1:8426"
	intentClassifierMinConfidence  = 0.8
	intentClassifierRequestTimeout = 75 * time.Millisecond
	intentClassifierResponseLimit  = 64 * 1024
	intentLLMRequestTimeout        = 150 * time.Millisecond
	intentLLMResponseLimit         = 128 * 1024
	intentLLMModel                 = "qwen3-0.6b-q4_k_m"
)

const intentLLMSystemPrompt = `Classify the user intent and return JSON only.
Allowed actions:
- switch_project (name)
- switch_model (alias, effort)
- toggle_silent
- toggle_conversation
- cancel_work
- show_status
- delegate
- chat

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

func parseSystemAction(raw string) (*SystemAction, error) {
	return parseSystemActionJSON(raw)
}

func parseSystemActionJSON(raw string) (*SystemAction, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return nil, nil
	}
	action := normalizeSystemActionName(fmt.Sprint(obj["action"]))
	if action == "" {
		action = normalizeSystemActionName(fmt.Sprint(obj["intent"]))
	}
	if action == "" {
		return nil, nil
	}
	params := make(map[string]interface{}, len(obj))
	for key, value := range obj {
		if strings.EqualFold(strings.TrimSpace(key), "action") || strings.EqualFold(strings.TrimSpace(key), "intent") {
			continue
		}
		params[key] = value
	}
	return &SystemAction{Action: action, Params: params}, nil
}

func systemActionStringParam(params map[string]interface{}, key string) string {
	return strings.TrimSpace(fmt.Sprint(params[key]))
}

func normalizeSystemActionName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "switch_project", "switch_model", "toggle_silent", "toggle_conversation", "cancel_work", "show_status", "delegate":
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
	return &SystemAction{Action: actionName, Params: params}, payload.Confidence, nil
}

func (a *App) classifyIntentWithLLM(ctx context.Context, text string) (*SystemAction, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(a.intentLLMURL), "/")
	if baseURL == "" {
		return nil, nil
	}
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return nil, nil
	}

	requestBody, _ := json.Marshal(map[string]interface{}{
		"model":       intentLLMModel,
		"temperature": 0,
		"max_tokens":  128,
		"chat_template_kwargs": map[string]interface{}{
			"enable_thinking": false,
		},
		"messages": []map[string]string{
			{"role": "system", "content": intentLLMSystemPrompt},
			{"role": "user", "content": trimmedText},
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
	action, parseErr := parseSystemActionJSON(stripCodeFence(content))
	if parseErr != nil {
		return nil, parseErr
	}
	if action == nil {
		return nil, nil
	}
	if normalizeSystemActionName(action.Action) == "" {
		return nil, nil
	}
	return action, nil
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
		targetProject, err := a.hubPrimaryProject()
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
	case "delegate":
		targetProject, err := a.hubPrimaryProject()
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
