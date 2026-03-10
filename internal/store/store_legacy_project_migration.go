package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
)

func workspaceNameForLegacyProject(project Project) string {
	if name := normalizeWorkspaceName(project.Name); name != "" {
		return name
	}
	base := strings.TrimSpace(filepath.Base(normalizeProjectPath(project.RootPath)))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "Workspace"
	}
	return base
}

func contextNameForLegacyProject(project Project) string {
	if name := normalizeProjectName(project.Name); name != "" {
		return name
	}
	if id := strings.TrimSpace(project.ID); id != "" {
		return id
	}
	return "Context"
}

func sameProjectID(current *string, want string) bool {
	return current != nil && strings.TrimSpace(*current) == strings.TrimSpace(want)
}

func (s *Store) ensureWorkspaceForLegacyProject(project Project) (Workspace, error) {
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
		workspaceNameForLegacyProject(project),
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

func (s *Store) contextIDByName(name string) (int64, error) {
	var contextID int64
	err := s.db.QueryRow(
		`SELECT id
		 FROM contexts
		 WHERE lower(name) = lower(?)`,
		strings.TrimSpace(name),
	).Scan(&contextID)
	return contextID, err
}

func (s *Store) ensureContextByName(name string) (int64, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return 0, errors.New("context name is required")
	}
	if existingID, err := s.contextIDByName(cleanName); err == nil {
		return existingID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO contexts (name) VALUES (?)`, cleanName); err != nil {
		return 0, err
	}
	return s.contextIDByName(cleanName)
}

func (s *Store) copyLegacyProjectRuntimeConfigToWorkspace(project Project) error {
	projectID := strings.TrimSpace(project.ID)
	if projectID == "" {
		return nil
	}
	_, err := s.db.Exec(
		`UPDATE workspaces
		 SET mcp_url = CASE WHEN trim(mcp_url) = '' THEN ? ELSE mcp_url END,
		     canvas_session_id = CASE WHEN trim(canvas_session_id) = '' THEN ? ELSE canvas_session_id END,
		     chat_model = CASE WHEN trim(chat_model) = '' THEN ? ELSE chat_model END,
		     chat_model_reasoning_effort = CASE WHEN trim(chat_model_reasoning_effort) = '' THEN ? ELSE chat_model_reasoning_effort END,
		     updated_at = datetime('now')
		 WHERE project_id = ?`,
		strings.TrimSpace(project.MCPURL),
		strings.TrimSpace(project.CanvasSessionID),
		normalizeProjectChatModel(project.ChatModel),
		normalizeProjectChatModelReasoningEffort(project.ChatModelReasoningEffort),
		projectID,
	)
	return err
}

func (s *Store) linkContextToLegacyProject(contextID int64, project Project) error {
	projectID := strings.TrimSpace(project.ID)
	if contextID <= 0 || projectID == "" {
		return nil
	}
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO context_workspaces (context_id, workspace_id)
		 SELECT ?, id
		 FROM workspaces
		 WHERE project_id = ?`,
		contextID,
		projectID,
	); err != nil {
		return err
	}
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO context_items (context_id, item_id)
		 SELECT ?, id
		 FROM items
		 WHERE project_id = ?`,
		contextID,
		projectID,
	); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO context_artifacts (context_id, artifact_id)
		 SELECT ?, artifact_id
		 FROM items
		 WHERE project_id = ?
		   AND artifact_id IS NOT NULL
		 UNION
		 SELECT ?, wal.artifact_id
		 FROM workspace_artifact_links wal
		 JOIN workspaces w ON w.id = wal.workspace_id
		 WHERE w.project_id = ?`,
		contextID,
		projectID,
		contextID,
		projectID,
	)
	return err
}

func (s *Store) migrateLegacyProjectData() error {
	projects, err := s.ListProjects()
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		return nil
	}

	activeProjectID, err := s.ActiveProjectID()
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var activeWorkspaceID int64
	for _, project := range projects {
		contextID, err := s.ensureContextByName(contextNameForLegacyProject(project))
		if err != nil {
			return err
		}
		if err := s.linkContextToLegacyProject(contextID, project); err != nil {
			return err
		}
		if normalizeProjectPath(project.RootPath) == "" {
			continue
		}
		workspace, err := s.ensureWorkspaceForLegacyProject(project)
		if err != nil {
			return err
		}
		if err := s.copyLegacyProjectRuntimeConfigToWorkspace(project); err != nil {
			return err
		}
		if err := s.linkContextToLegacyProject(contextID, project); err != nil {
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
