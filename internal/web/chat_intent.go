package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

type SystemAction struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"-"`
}

func parseSystemAction(raw string) (*SystemAction, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return nil, nil
	}
	action := strings.ToLower(strings.TrimSpace(fmt.Sprint(obj["action"])))
	if action == "" {
		return nil, nil
	}
	params := make(map[string]interface{}, len(obj))
	for key, value := range obj {
		if strings.EqualFold(strings.TrimSpace(key), "action") {
			continue
		}
		params[key] = value
	}
	return &SystemAction{Action: action, Params: params}, nil
}

func systemActionStringParam(params map[string]interface{}, key string) string {
	return strings.TrimSpace(fmt.Sprint(params[key]))
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
	default:
		return "", nil, fmt.Errorf("unsupported action: %s", action.Action)
	}
}
