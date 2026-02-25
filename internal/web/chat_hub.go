package web

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

const (
	HubProjectKey  = "__hub__"
	HubProjectKind = "hub"
)

const hubSystemPrompt = `You are Tabura Hub, a fast voice assistant coordinator.
Respond concisely. For system actions, return JSON:
{"action":"<action>", ...params}

Available actions:
- {"action":"switch_project","name":"..."}
- {"action":"switch_model","alias":"codex|gpt|spark","effort":"low|medium|high|extra_high"}
- {"action":"toggle_silent"}
- {"action":"toggle_conversation"}
- {"action":"cancel_work"}
- {"action":"show_status"}

For conversational responses, reply with plain text.`

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

	rootPath := strings.TrimSpace(a.localProjectDir)
	if rootPath == "" {
		rootPath = filepath.Join(a.dataDir, "projects", "hub")
	}
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

func (a *App) runHubTurn(sessionID string, session store.ChatSession, messages []store.ChatMessage, outputMode string) {
	if a.appServerClient == nil {
		errText := "app-server is not configured"
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}

	userText := latestUserMessage(messages)
	if userText == "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "hub message is empty"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), assistantTurnTimeout)
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

	model := modelprofile.ModelForAlias(modelprofile.AliasSpark)
	reasoning := appServerReasoningParamsForModel(model, modelprofile.ReasoningLow)
	resp, err := a.appServerClient.SendPrompt(ctx, appserver.PromptRequest{
		CWD:          a.cwdForProjectKey(session.ProjectKey),
		Prompt:       hubSystemPrompt + "\n\nUser message:\n" + userText,
		Model:        model,
		ThreadParams: reasoning,
		TurnParams:   reasoning,
		Timeout:      assistantTurnTimeout,
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

	assistantText := strings.TrimSpace(resp.Message)
	if assistantText == "" {
		assistantText = "(assistant returned no content)"
	}
	action, _ := parseSystemAction(assistantText)
	if action != nil {
		actionMessage, actionPayload, actionErr := a.executeSystemAction(sessionID, session, action)
		if actionErr != nil {
			assistantText = fmt.Sprintf("Hub action failed: %s", actionErr.Error())
		} else {
			assistantText = strings.TrimSpace(actionMessage)
			if assistantText == "" {
				assistantText = "Done."
			}
			if actionPayload != nil {
				a.broadcastChatEvent(sessionID, map[string]interface{}{
					"type":   "system_action",
					"action": actionPayload,
				})
			}
		}
	}

	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	a.finalizeAssistantResponse(
		sessionID,
		session.ProjectKey,
		assistantText,
		&persistedAssistantID,
		&persistedAssistantText,
		resp.TurnID,
		runID,
		resp.ThreadID,
		outputMode,
	)
}
