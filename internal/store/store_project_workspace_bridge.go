package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
)

func workspaceNameForProject(project Project) string {
	if name := normalizeWorkspaceName(project.Name); name != "" {
		return name
	}
	base := strings.TrimSpace(filepath.Base(normalizeProjectPath(project.RootPath)))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "Workspace"
	}
	return base
}

func sameProjectID(current *string, want string) bool {
	return current != nil && strings.TrimSpace(*current) == strings.TrimSpace(want)
}

func (s *Store) ensureWorkspaceForProject(project Project) (Workspace, error) {
	projectID := strings.TrimSpace(project.ID)
	if projectID == "" {
		return Workspace{}, errors.New("project id is required")
	}
	rootPath := normalizeProjectPath(project.RootPath)
	if rootPath == "" {
		return Workspace{}, errors.New("project path is required")
	}

	workspace, err := s.GetWorkspaceByPath(rootPath)
	switch {
	case err == nil:
		if sameProjectID(workspace.ProjectID, projectID) {
			return workspace, nil
		}
		return s.SetWorkspaceProject(workspace.ID, &projectID)
	case !errors.Is(err, sql.ErrNoRows):
		return Workspace{}, err
	}

	res, err := s.db.Exec(
		`INSERT INTO workspaces (name, dir_path, project_id, sphere)
		 VALUES (?, ?, ?, ?)`,
		workspaceNameForProject(project),
		rootPath,
		projectID,
		SpherePrivate,
	)
	if err != nil {
		return Workspace{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Workspace{}, err
	}
	return s.GetWorkspace(id)
}

func (s *Store) migrateProjectWorkspaces() error {
	projects, err := s.ListProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		return nil
	}

	activeProjectID, err := s.ActiveProjectID()
	if err != nil {
		return err
	}

	var activeWorkspaceID int64
	for _, project := range projects {
		if strings.TrimSpace(project.ID) == "" || normalizeProjectPath(project.RootPath) == "" {
			continue
		}
		workspace, err := s.ensureWorkspaceForProject(project)
		if err != nil {
			return err
		}
		if strings.TrimSpace(activeProjectID) == strings.TrimSpace(project.ID) {
			activeWorkspaceID = workspace.ID
		}
	}
	if activeWorkspaceID == 0 {
		return nil
	}
	return s.SetActiveWorkspace(activeWorkspaceID)
}
