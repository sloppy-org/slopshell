package web

import (
	"errors"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

func (a *App) chatSessionForProject(project store.Project) (store.ChatSession, error) {
	if isHubProject(project) {
		primary, err := a.hubPrimaryProject()
		if err != nil {
			return store.ChatSession{}, err
		}
		return a.store.GetOrCreateChatSession(primary.ProjectKey)
	}
	return a.store.GetOrCreateChatSession(project.ProjectKey)
}

func (a *App) projectForWorkspace(workspace store.Workspace) (*store.Project, error) {
	if workspace.ProjectID == nil || strings.TrimSpace(*workspace.ProjectID) == "" {
		return nil, nil
	}
	project, err := a.store.GetProject(strings.TrimSpace(*workspace.ProjectID))
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func (a *App) resolveChatSessionTarget(projectID, projectKey string, workspaceID *int64) (store.Workspace, *store.Project, error) {
	if workspaceID != nil {
		workspace, err := a.store.GetWorkspace(*workspaceID)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		project, err := a.projectForWorkspace(workspace)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return workspace, project, nil
	}

	loadProject := func(project store.Project) (store.Workspace, *store.Project, error) {
		session, err := a.chatSessionForProject(project)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		workspace, err := a.store.GetWorkspace(session.WorkspaceID)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		projectForWorkspace, err := a.projectForWorkspace(workspace)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return workspace, projectForWorkspace, nil
	}

	if id := strings.TrimSpace(projectID); id != "" {
		project, err := a.store.GetProject(id)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return loadProject(project)
	}
	if key := strings.TrimSpace(projectKey); key != "" {
		project, err := a.store.GetProjectByProjectKey(key)
		if err == nil {
			return loadProject(project)
		}
		if !isNoRows(err) {
			return store.Workspace{}, nil, err
		}
		workspace, workspaceErr := a.store.GetWorkspaceByPath(key)
		if workspaceErr != nil {
			return store.Workspace{}, nil, workspaceErr
		}
		projectForWorkspace, err := a.projectForWorkspace(workspace)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return workspace, projectForWorkspace, nil
	}

	if workspace, err := a.store.ActiveWorkspace(); err == nil {
		project, err := a.projectForWorkspace(workspace)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return workspace, project, nil
	}

	activeProjectID, err := a.store.ActiveProjectID()
	if err != nil {
		return store.Workspace{}, nil, err
	}
	if strings.TrimSpace(activeProjectID) != "" {
		project, err := a.store.GetProject(activeProjectID)
		if err != nil {
			return store.Workspace{}, nil, err
		}
		return loadProject(project)
	}

	project, err := a.ensureDefaultProjectRecord()
	if err != nil {
		return store.Workspace{}, nil, err
	}
	return loadProject(project)
}

func (a *App) activeProjectIsHub() bool {
	activeProjectID, err := a.store.ActiveProjectID()
	if err != nil || strings.TrimSpace(activeProjectID) == "" {
		return false
	}
	project, err := a.store.GetProject(activeProjectID)
	if err != nil {
		return false
	}
	return isHubProject(project)
}

func (a *App) chatSessionForProjectKey(projectKey string) (store.ChatSession, error) {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return store.ChatSession{}, errors.New("project key is required")
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err == nil {
		return a.chatSessionForProject(project)
	}
	if !isNoRows(err) {
		return store.ChatSession{}, err
	}
	return a.store.GetChatSessionByProjectKey(key)
}
