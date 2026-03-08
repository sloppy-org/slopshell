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
	"sort"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

const (
	HubProjectKey        = "__hub__"
	HubProjectKind       = "hub"
	hubLLMRequestTimeout = 900 * time.Millisecond
)

const hubSystemPrompt = `You are Tabura Hub, a fast coordinator.
For system actions output JSON only: {"action":"<action>", ...params}.
Allowed actions: switch_project, switch_workspace, list_workspace_items, switch_model, toggle_silent, toggle_live_dialogue, shell, open_file_canvas, cancel_work, show_status, chat.
You may return multi-step actions via {"actions":[...]}.
For current-information requests (weather, web search, news, prices, schedules, latest/current updates), MUST use chat and MUST NOT use shell.
For uncertain open/show-file requests: shell search first, then open_file_canvas with path="$last_shell_path".
Use concise plain text only when the request is conversational.`

func isHubProject(project store.Project) bool {
	if strings.EqualFold(strings.TrimSpace(project.ProjectKey), HubProjectKey) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(project.Kind), HubProjectKind)
}

func (a *App) ensureHubProject() (store.Project, error) {
	if existing, err := a.store.GetProjectByProjectKey(HubProjectKey); err == nil {
		_ = a.store.UpdateProjectChatModel(existing.ID, modelprofile.AliasSpark)
		_ = a.store.UpdateProjectChatModelReasoningEffort(existing.ID, modelprofile.ReasoningLow)
		if refreshed, refreshErr := a.store.GetProject(existing.ID); refreshErr == nil {
			return refreshed, nil
		}
		return existing, nil
	}

	rootPath := filepath.Join(a.dataDir, "projects", "hub")
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return store.Project{}, err
	}
	absRoot = filepath.Clean(absRoot)
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return store.Project{}, err
	}

	created, err := a.store.CreateProject(
		"Hub",
		HubProjectKey,
		absRoot,
		HubProjectKind,
		"",
		"",
		false,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			existing, lookupErr := a.store.GetProjectByProjectKey(HubProjectKey)
			if lookupErr == nil {
				_ = a.store.UpdateProjectChatModel(existing.ID, modelprofile.AliasSpark)
				_ = a.store.UpdateProjectChatModelReasoningEffort(existing.ID, modelprofile.ReasoningLow)
				return existing, nil
			}
			existingByPath, lookupByPathErr := a.store.GetProjectByRootPath(absRoot)
			if lookupByPathErr == nil && isHubProject(existingByPath) {
				_ = a.store.UpdateProjectChatModel(existingByPath.ID, modelprofile.AliasSpark)
				_ = a.store.UpdateProjectChatModelReasoningEffort(existingByPath.ID, modelprofile.ReasoningLow)
				return existingByPath, nil
			}
		}
		return store.Project{}, err
	}
	if err := a.store.UpdateProjectChatModel(created.ID, modelprofile.AliasSpark); err != nil {
		return store.Project{}, err
	}
	if err := a.store.UpdateProjectChatModelReasoningEffort(created.ID, modelprofile.ReasoningLow); err != nil {
		return store.Project{}, err
	}
	return a.store.GetProject(created.ID)
}

func latestUserMessage(messages []store.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if !strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			continue
		}
		text := strings.TrimSpace(messages[i].ContentPlain)
		if text == "" {
			text = strings.TrimSpace(messages[i].ContentMarkdown)
		}
		if text != "" {
			return text
		}
	}
	return ""
}

func (a *App) hubPrimaryProject() (store.Project, error) {
	activeID, err := a.store.ActiveProjectID()
	if err == nil && strings.TrimSpace(activeID) != "" {
		if project, getErr := a.store.GetProject(activeID); getErr == nil && !isHubProject(project) {
			return project, nil
		}
	}
	defaultProject, err := a.ensureDefaultProjectRecord()
	if err == nil && !isHubProject(defaultProject) {
		return defaultProject, nil
	}
	projects, listErr := a.store.ListProjects()
	if listErr != nil {
		return store.Project{}, listErr
	}
	for _, project := range projects {
		if !isHubProject(project) {
			return project, nil
		}
	}
	return store.Project{}, errors.New("no non-hub project is available")
}

func (a *App) hubFindProjectByName(name string) (store.Project, error) {
	query := strings.ToLower(strings.TrimSpace(name))
	if query == "" {
		return store.Project{}, errors.New("project name is required")
	}
	projects, err := a.store.ListProjects()
	if err != nil {
		return store.Project{}, err
	}
	exact := make([]store.Project, 0, 2)
	partial := make([]store.Project, 0, 4)
	for _, project := range projects {
		if isHubProject(project) {
			continue
		}
		candidate := strings.ToLower(strings.TrimSpace(project.Name))
		if candidate == "" {
			continue
		}
		if candidate == query {
			exact = append(exact, project)
			continue
		}
		if strings.Contains(candidate, query) {
			partial = append(partial, project)
		}
	}
	if len(exact) > 0 {
		sort.Slice(exact, func(i, j int) bool {
			return len(exact[i].Name) < len(exact[j].Name)
		})
		return exact[0], nil
	}
	if len(partial) > 0 {
		sort.Slice(partial, func(i, j int) bool {
			return len(partial[i].Name) < len(partial[j].Name)
		})
		return partial[0], nil
	}
	return store.Project{}, fmt.Errorf("project %q was not found", name)
}

func (a *App) runHubTurn(sessionID string, session store.ChatSession, messages []store.ChatMessage, outputMode string, localOnly bool) {
	userText := latestUserMessage(messages)
	if userText == "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "hub message is empty"})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	runID := randomToken()
	a.registerActiveChatTurn(sessionID, runID, cancel)
	defer func() {
		cancel()
		a.unregisterActiveChatTurn(sessionID, runID)
	}()

	a.broadcastChatEvent(sessionID, map[string]interface{}{
		"type":    "turn_started",
		"turn_id": runID,
	})

	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	finalizeHubAssistantText := func(text, turnID, threadID string) {
		assistantText := strings.TrimSpace(text)
		if assistantText == "" {
			assistantText = "(assistant returned no content)"
		}
		actions, _ := parseSystemActions(assistantText)
		if len(actions) > 0 {
			actionMessage, actionPayloads, actionErr := a.executeSystemActionPlan(sessionID, session, userText, actions)
			if actionErr != nil {
				assistantText = fmt.Sprintf("Hub action failed: %s", actionErr.Error())
			} else {
				assistantText = strings.TrimSpace(actionMessage)
				if assistantText == "" {
					assistantText = "Done."
				}
				for _, actionPayload := range actionPayloads {
					if actionPayload == nil {
						continue
					}
					eventType := "system_action"
					actionType := strings.TrimSpace(fmt.Sprint(actionPayload["type"]))
					if strings.EqualFold(actionType, "confirmation_required") {
						eventType = "system_action_confirmation_required"
					}
					a.broadcastChatEvent(sessionID, map[string]interface{}{
						"type":   eventType,
						"action": actionPayload,
					})
				}
			}
		}

		a.finalizeAssistantResponse(
			sessionID,
			session.ProjectKey,
			assistantText,
			&persistedAssistantID,
			&persistedAssistantText,
			turnID,
			runID,
			threadID,
			outputMode,
		)
	}
	executeClassifiedAction := func(actions []*SystemAction) bool {
		actionMessage, actionPayloads, actionErr := a.executeSystemActionPlan(sessionID, session, userText, actions)
		if actionErr != nil {
			return false
		}
		assistantText := strings.TrimSpace(actionMessage)
		if assistantText == "" {
			assistantText = "Done."
		}
		for _, actionPayload := range actionPayloads {
			if actionPayload == nil {
				continue
			}
			eventType := "system_action"
			actionType := strings.TrimSpace(fmt.Sprint(actionPayload["type"]))
			if strings.EqualFold(actionType, "confirmation_required") {
				eventType = "system_action_confirmation_required"
			}
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":   eventType,
				"action": actionPayload,
			})
		}
		a.finalizeAssistantResponse(
			sessionID,
			session.ProjectKey,
			assistantText,
			&persistedAssistantID,
			&persistedAssistantText,
			"",
			runID,
			"",
			outputMode,
		)
		return true
	}

	if actionMessage, actionPayloads, handled := a.classifyAndExecuteSystemAction(ctx, sessionID, session, userText); handled {
		assistantText := strings.TrimSpace(actionMessage)
		if assistantText == "" {
			assistantText = "Done."
		}
		for _, actionPayload := range actionPayloads {
			if actionPayload == nil {
				continue
			}
			eventType := "system_action"
			actionType := strings.TrimSpace(fmt.Sprint(actionPayload["type"]))
			if strings.EqualFold(actionType, "confirmation_required") {
				eventType = "system_action_confirmation_required"
			}
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":   eventType,
				"action": actionPayload,
			})
		}
		a.finalizeAssistantResponse(
			sessionID,
			session.ProjectKey,
			assistantText,
			&persistedAssistantID,
			&persistedAssistantText,
			"",
			runID,
			"",
			outputMode,
		)
		return
	}

	if localOnly {
		assistantText := "I can only handle system actions in local-only mode."
		a.finalizeAssistantResponse(
			sessionID,
			session.ProjectKey,
			assistantText,
			&persistedAssistantID,
			&persistedAssistantText,
			"",
			runID,
			"",
			outputMode,
		)
		return
	}

	assistantText, localLLMErr := a.hubReplyWithLocalLLM(ctx, userText)
	if localLLMErr == nil {
		if actions, _ := parseSystemActions(assistantText); len(actions) > 0 {
			if executeClassifiedAction(actions) {
				return
			}
		} else {
			finalizeHubAssistantText(assistantText, "", "")
			return
		}
	}

	if a.appServerClient == nil {
		errText := fmt.Sprintf("hub local llm failed: %v", localLLMErr)
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}

	baseProfile := a.appServerModelProfileForProjectKey(session.ProjectKey)
	routeProfile := routeProfileForRouting(
		classifyRoutingRoute(userText),
		baseProfile,
		a.appServerSparkReasoningEffort,
	)
	resp, err := a.appServerClient.SendPrompt(ctx, appserver.PromptRequest{
		CWD:          a.cwdForProjectKey(session.ProjectKey),
		Prompt:       hubSystemPrompt + "\n\nUser message:\n" + userText,
		Model:        routeProfile.Model,
		TurnModel:    routeProfile.Model,
		ThreadParams: nil,
		TurnParams:   routeProfile.TurnParams,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":    "turn_cancelled",
				"turn_id": runID,
			})
			return
		}
		errText := normalizeAssistantError(err)
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":    "error",
			"error":   errText,
			"turn_id": runID,
		})
		return
	}

	finalizeHubAssistantText(resp.Message, resp.TurnID, resp.ThreadID)
}

func (a *App) hubReplyWithLocalLLM(ctx context.Context, userText string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(a.intentLLMURL), "/")
	if baseURL == "" {
		return "", errors.New("local llm url is empty")
	}
	trimmedText := strings.TrimSpace(userText)
	if trimmedText == "" {
		return "", errors.New("hub user text is empty")
	}

	requestBody, _ := json.Marshal(map[string]interface{}{
		"model":       a.localIntentLLMModel(),
		"temperature": 0,
		"max_tokens":  256,
		"chat_template_kwargs": map[string]interface{}{
			"enable_thinking": false,
		},
		"messages": []map[string]string{
			{"role": "system", "content": hubSystemPrompt},
			{"role": "user", "content": trimmedText},
		},
	})

	requestCtx, cancel := context.WithTimeout(ctx, hubLLMRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		baseURL+"/v1/chat/completions",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, intentLLMResponseLimit))
		return "", fmt.Errorf("hub local llm HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload localIntentLLMChatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, intentLLMResponseLimit)).Decode(&payload); err != nil {
		return "", err
	}
	if len(payload.Choices) == 0 {
		return "", errors.New("hub local llm returned no choices")
	}
	content := strings.TrimSpace(payload.Choices[0].Message.Content)
	if content == "" {
		return "", errors.New("hub local llm returned empty content")
	}
	return content, nil
}
