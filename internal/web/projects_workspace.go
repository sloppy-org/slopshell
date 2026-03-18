package web

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

func (a *App) workspaceForProject(project store.Project) (*store.Workspace, error) {
	rootPath := filepath.Clean(strings.TrimSpace(project.RootPath))
	workspaces, err := a.store.ListWorkspacesForProject(projectIDString(project.ID))
	if err != nil {
		return nil, err
	}
	var fallback *store.Workspace
	for i := range workspaces {
		workspace := workspaces[i]
		if strings.TrimSpace(workspace.DirPath) == "" {
			continue
		}
		if rootPath != "" && filepath.Clean(workspace.DirPath) == rootPath {
			return &workspace, nil
		}
		if fallback == nil || workspace.IsActive {
			fallback = &workspace
			if workspace.IsActive {
				break
			}
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	if rootPath == "" {
		return nil, nil
	}
	if workspace, err := a.store.GetWorkspaceByPath(rootPath); err == nil {
		return &workspace, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if workspaceID, err := a.store.FindWorkspaceContainingPath(rootPath); err == nil && workspaceID != nil {
		workspace, getErr := a.store.GetWorkspace(*workspaceID)
		if getErr != nil {
			return nil, getErr
		}
		return &workspace, nil
	} else if err != nil {
		return nil, err
	}
	return nil, nil
}

func (a *App) ensureWorkspaceForProject(project store.Project, activate bool) (store.Workspace, error) {
	rootPath := filepath.Clean(strings.TrimSpace(project.RootPath))
	if rootPath == "" {
		return store.Workspace{}, errors.New("project path is required")
	}
	workspaceRef, err := a.workspaceForProject(project)
	if err != nil {
		return store.Workspace{}, err
	}
	if workspaceRef == nil {
		workspace, createErr := a.store.CreateWorkspace(project.Name, rootPath, a.runtimeActiveSphere())
		if createErr != nil {
			return store.Workspace{}, createErr
		}
		workspaceRef = &workspace
	}
	workspace := *workspaceRef
	if activate {
		if err := a.setActiveWorkspaceTracked(workspace.ID, "workspace_switch"); err != nil {
			return store.Workspace{}, err
		}
		workspace, err = a.store.GetWorkspace(workspace.ID)
		if err != nil {
			return store.Workspace{}, err
		}
	}
	if _, err := a.store.GetOrCreateChatSessionForWorkspace(workspace.ID); err != nil {
		return store.Workspace{}, err
	}
	return workspace, nil
}

func (a *App) ensureStartupProjectWithWorkspace() error {
	if strings.TrimSpace(a.localProjectDir) != "" {
		project, err := a.ensureDefaultProjectRecord()
		if err != nil {
			return err
		}
		workspace, err := a.ensureWorkspaceForProject(project, false)
		if err != nil {
			return err
		}
		activeWorkspace, activeErr := a.store.ActiveWorkspace()
		if activeErr != nil || activeWorkspace.ID != workspace.ID {
			if err := a.store.SetActiveWorkspace(workspace.ID); err != nil {
				return err
			}
			a.closeAllAppSessions()
		}
		if err := a.store.SetActiveWorkspaceID(projectIDString(project.ID)); err != nil {
			return err
		}
		return nil
	}
	_, err := a.ensureStartupWorkspace()
	return err
}
