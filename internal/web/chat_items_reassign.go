package web

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

var (
	itemAssignTargetPattern   = regexp.MustCompile(`(?i)^(?:move|assign|reassign)(?:\s+(?:this|it))?\s+to\s+(.+?)$`)
	itemBelongsProjectPattern = regexp.MustCompile(`(?i)^this\s+belongs\s+to\s+(.+?)$`)
)

func parseInlineItemReassignmentIntent(text string) *SystemAction {
	normalized := normalizeItemCommandText(text)
	switch normalized {
	case "remove workspace from this item", "remove workspace from this", "clear workspace for this item", "clear workspace":
		return &SystemAction{Action: "clear_workspace", Params: map[string]interface{}{}}
	case "remove project from this item", "remove project from this", "clear project for this item", "clear project":
		return &SystemAction{Action: "clear_project", Params: map[string]interface{}{}}
	}
	if match := itemBelongsProjectPattern.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 2 {
		if ref := cleanWorkspaceReference(match[1]); ref != "" {
			return &SystemAction{Action: "reassign_project", Params: map[string]interface{}{"project": ref}}
		}
	}
	if match := itemAssignTargetPattern.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 2 {
		target := cleanWorkspaceReference(match[1])
		if target == "" {
			return nil
		}
		lower := strings.ToLower(target)
		if strings.HasSuffix(lower, " workspace") {
			return &SystemAction{Action: "reassign_workspace", Params: map[string]interface{}{"workspace": trimAssignmentSuffix(target, " workspace")}}
		}
		if strings.HasSuffix(lower, " project") {
			return &SystemAction{Action: "reassign_project", Params: map[string]interface{}{"project": trimAssignmentSuffix(target, " project")}}
		}
		if looksLikeWorkspaceReference(target) {
			return &SystemAction{Action: "reassign_workspace", Params: map[string]interface{}{"workspace": target}}
		}
	}
	return nil
}

func systemActionAssignmentTarget(params map[string]interface{}) string {
	for _, key := range []string{"workspace", "project", "target", "name", "path"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func trimAssignmentSuffix(raw, suffix string) string {
	text := strings.TrimSpace(raw)
	lower := strings.ToLower(text)
	if strings.HasSuffix(lower, suffix) {
		return cleanWorkspaceReference(text[:len(text)-len(suffix)])
	}
	return cleanWorkspaceReference(text)
}

func (a *App) resolveConversationTargetItem(session store.ChatSession, project store.Project) (store.Item, error) {
	if item, err := a.resolveCanvasConversationItem(project); err == nil {
		return item, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Item{}, err
	}
	if workspace, err := a.fallbackWorkspaceForProjectKey(session.ProjectKey); err != nil {
		return store.Item{}, err
	} else if workspace != nil {
		items, listErr := a.listOpenWorkspaceItems(workspace.ID)
		if listErr != nil {
			return store.Item{}, listErr
		}
		if len(items) > 0 {
			return items[0], nil
		}
	}
	return store.Item{}, errors.New("no item is available to reassign")
}

func (a *App) resolveProjectReference(raw string) (store.Project, error) {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return store.Project{}, errors.New("project name is required")
	}
	if project, err := a.store.GetProject(ref); err == nil {
		return project, nil
	}
	return a.hubFindProjectByName(ref)
}

func (a *App) executeItemReassignmentAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	targetProject, err := a.systemActionTargetProject(session)
	if err != nil {
		return "", nil, err
	}
	item, err := a.resolveConversationTargetItem(session, targetProject)
	if err != nil {
		return "", nil, err
	}
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "reassign_workspace":
		workspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionAssignmentTarget(action.Params))
		if err != nil {
			return "", nil, err
		}
		warning, err := a.itemWorkspaceChangeWarning(item, &workspace.ID)
		if err != nil {
			return "", nil, err
		}
		if err := a.store.SetItemWorkspace(item.ID, &workspace.ID); err != nil {
			return "", nil, err
		}
		message := fmt.Sprintf("Moved item %q to workspace %s.", item.Title, workspace.Name)
		if warning != "" {
			message += " " + warning
		}
		return message, map[string]interface{}{
			"type":         "item_reassigned",
			"item_id":      item.ID,
			"workspace_id": workspace.ID,
			"warning":      warning,
		}, nil
	case "clear_workspace":
		warning, err := a.itemWorkspaceChangeWarning(item, nil)
		if err != nil {
			return "", nil, err
		}
		if err := a.store.SetItemWorkspace(item.ID, nil); err != nil {
			return "", nil, err
		}
		message := fmt.Sprintf("Cleared the workspace for item %q.", item.Title)
		if warning != "" {
			message += " " + warning
		}
		return message, map[string]interface{}{
			"type":         "item_reassigned",
			"item_id":      item.ID,
			"workspace_id": nil,
			"warning":      warning,
		}, nil
	case "reassign_project":
		project, err := a.resolveProjectReference(systemActionAssignmentTarget(action.Params))
		if err != nil {
			return "", nil, err
		}
		if err := a.store.SetItemProject(item.ID, &project.ID); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Assigned item %q to project %s.", item.Title, project.Name), map[string]interface{}{
			"type":       "item_reassigned",
			"item_id":    item.ID,
			"project_id": project.ID,
		}, nil
	case "clear_project":
		if err := a.store.SetItemProject(item.ID, nil); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Cleared the project for item %q.", item.Title), map[string]interface{}{
			"type":       "item_reassigned",
			"item_id":    item.ID,
			"project_id": nil,
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported item reassignment action: %s", action.Action)
	}
}
