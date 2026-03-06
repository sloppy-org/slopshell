package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func normalizeProjectKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "meeting":
		return "meeting"
	case "task":
		return "task"
	case "linked":
		return "linked"
	case "hub":
		return "hub"
	default:
		return "managed"
	}
}

func normalizeProjectPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return filepath.Clean(abs)
}

func normalizeProjectName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeProjectChatModel(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func normalizeProjectChatModelReasoningEffort(effort string) string {
	return strings.ToLower(strings.TrimSpace(effort))
}

func scanProject(
	row interface {
		Scan(dest ...any) error
	},
) (Project, error) {
	var out Project
	var isDefault int
	err := row.Scan(
		&out.ID,
		&out.Name,
		&out.ProjectKey,
		&out.RootPath,
		&out.Kind,
		&out.MCPURL,
		&out.CanvasSessionID,
		&out.ChatModel,
		&out.ChatModelReasoningEffort,
		&out.CompanionConfigJSON,
		&isDefault,
		&out.CreatedAt,
		&out.UpdatedAt,
		&out.LastOpenedAt,
	)
	if err != nil {
		return Project{}, err
	}
	out.Kind = normalizeProjectKind(out.Kind)
	out.Name = normalizeProjectName(out.Name)
	out.RootPath = normalizeProjectPath(out.RootPath)
	out.ProjectKey = strings.TrimSpace(out.ProjectKey)
	out.ChatModel = normalizeProjectChatModel(out.ChatModel)
	out.ChatModelReasoningEffort = normalizeProjectChatModelReasoningEffort(out.ChatModelReasoningEffort)
	out.CompanionConfigJSON = strings.TrimSpace(out.CompanionConfigJSON)
	out.IsDefault = isDefault != 0
	return out, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(
		`SELECT id, name, project_key, root_path, kind, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, is_default, created_at, updated_at, last_opened_at
		 FROM projects
		 ORDER BY is_default DESC, last_opened_at DESC, lower(name) ASC, created_at ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, project)
	}
	return out, nil
}

func (s *Store) GetProject(id string) (Project, error) {
	return scanProject(s.db.QueryRow(
		`SELECT id, name, project_key, root_path, kind, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, is_default, created_at, updated_at, last_opened_at
		 FROM projects WHERE id = ?`,
		strings.TrimSpace(id),
	))
}

func (s *Store) GetProjectByProjectKey(projectKey string) (Project, error) {
	return scanProject(s.db.QueryRow(
		`SELECT id, name, project_key, root_path, kind, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, is_default, created_at, updated_at, last_opened_at
		 FROM projects WHERE project_key = ?`,
		strings.TrimSpace(projectKey),
	))
}

func (s *Store) GetProjectByRootPath(rootPath string) (Project, error) {
	return scanProject(s.db.QueryRow(
		`SELECT id, name, project_key, root_path, kind, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, is_default, created_at, updated_at, last_opened_at
		 FROM projects WHERE root_path = ?`,
		normalizeProjectPath(rootPath),
	))
}

func (s *Store) GetProjectByCanvasSession(canvasSessionID string) (Project, error) {
	return scanProject(s.db.QueryRow(
		`SELECT id, name, project_key, root_path, kind, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, companion_config_json, is_default, created_at, updated_at, last_opened_at
		 FROM projects WHERE canvas_session_id = ?`,
		strings.TrimSpace(canvasSessionID),
	))
}

func (s *Store) CreateProject(name, projectKey, rootPath, kind, mcpURL, canvasSessionID string, isDefault bool) (Project, error) {
	cleanName := normalizeProjectName(name)
	cleanKey := strings.TrimSpace(projectKey)
	cleanPath := normalizeProjectPath(rootPath)
	cleanKind := normalizeProjectKind(kind)
	cleanCanvasID := strings.TrimSpace(canvasSessionID)
	if cleanCanvasID == "" {
		cleanCanvasID = fmt.Sprintf("canvas-%s", randomHex(5))
	}
	if cleanName == "" {
		return Project{}, errors.New("project name is required")
	}
	if cleanKey == "" {
		return Project{}, errors.New("project key is required")
	}
	if cleanPath == "" {
		return Project{}, errors.New("project path is required")
	}
	now := time.Now().Unix()
	id := fmt.Sprintf("proj-%s", randomHex(6))
	tx, err := s.db.Begin()
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback()
	if isDefault {
		if _, err := tx.Exec(`UPDATE projects SET is_default = 0 WHERE is_default <> 0`); err != nil {
			return Project{}, err
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO projects (id, name, project_key, root_path, kind, mcp_url, canvas_session_id, is_default, created_at, updated_at, last_opened_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, cleanName, cleanKey, cleanPath, cleanKind, strings.TrimSpace(mcpURL), cleanCanvasID, boolToInt(isDefault), now, now, now,
	); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return Project{}, err
	}
	return s.GetProject(id)
}

func (s *Store) SetActiveProjectID(projectID string) error {
	id := strings.TrimSpace(projectID)
	if id == "" {
		return errors.New("project id is required")
	}
	return s.SetAppState("active_project_id", id)
}

func (s *Store) ActiveProjectID() (string, error) {
	return s.AppState("active_project_id")
}

func (s *Store) SetAppState(key, value string) error {
	cleanKey := strings.TrimSpace(key)
	if cleanKey == "" {
		return errors.New("app state key is required")
	}
	_, err := s.db.Exec(`INSERT INTO app_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, cleanKey, strings.TrimSpace(value))
	return err
}

func (s *Store) AppState(key string) (string, error) {
	cleanKey := strings.TrimSpace(key)
	if cleanKey == "" {
		return "", errors.New("app state key is required")
	}
	var value string
	err := s.db.QueryRow(`SELECT value FROM app_state WHERE key = ?`, cleanKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func (s *Store) TouchProject(projectID string) error {
	id := strings.TrimSpace(projectID)
	if id == "" {
		return errors.New("project id is required")
	}
	now := time.Now().Unix()
	_, err := s.db.Exec(`UPDATE projects SET last_opened_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
	return err
}

func (s *Store) UpdateProjectRuntime(id, mcpURL, canvasSessionID string) error {
	projectID := strings.TrimSpace(id)
	if projectID == "" {
		return errors.New("project id is required")
	}
	canvasID := strings.TrimSpace(canvasSessionID)
	if canvasID == "" {
		return errors.New("canvas session id is required")
	}
	_, err := s.db.Exec(
		`UPDATE projects SET mcp_url = ?, canvas_session_id = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(mcpURL),
		canvasID,
		time.Now().Unix(),
		projectID,
	)
	return err
}

func (s *Store) UpdateProjectChatModel(id, chatModel string) error {
	projectID := strings.TrimSpace(id)
	if projectID == "" {
		return errors.New("project id is required")
	}
	_, err := s.db.Exec(
		`UPDATE projects SET chat_model = ?, updated_at = ? WHERE id = ?`,
		normalizeProjectChatModel(chatModel),
		time.Now().Unix(),
		projectID,
	)
	return err
}

func (s *Store) UpdateProjectChatModelReasoningEffort(id, effort string) error {
	projectID := strings.TrimSpace(id)
	if projectID == "" {
		return errors.New("project id is required")
	}
	_, err := s.db.Exec(
		`UPDATE projects SET chat_model_reasoning_effort = ?, updated_at = ? WHERE id = ?`,
		normalizeProjectChatModelReasoningEffort(effort),
		time.Now().Unix(),
		projectID,
	)
	return err
}

func (s *Store) UpdateProjectKind(id, kind string) error {
	projectID := strings.TrimSpace(id)
	if projectID == "" {
		return errors.New("project id is required")
	}
	_, err := s.db.Exec(
		`UPDATE projects SET kind = ?, updated_at = ? WHERE id = ?`,
		normalizeProjectKind(kind),
		time.Now().Unix(),
		projectID,
	)
	return err
}

func (s *Store) DeleteProject(id string) error {
	projectID := strings.TrimSpace(id)
	if projectID == "" {
		return errors.New("project id is required")
	}
	project, err := s.GetProject(projectID)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	projectKey := strings.TrimSpace(project.ProjectKey)
	if _, err := tx.Exec(
		`DELETE FROM participant_room_state
		 WHERE session_id IN (SELECT id FROM participant_sessions WHERE project_key = ?)`,
		projectKey,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM participant_events
		 WHERE session_id IN (SELECT id FROM participant_sessions WHERE project_key = ?)`,
		projectKey,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM participant_segments
		 WHERE session_id IN (SELECT id FROM participant_sessions WHERE project_key = ?)`,
		projectKey,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM participant_sessions WHERE project_key = ?`, projectKey); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM chat_messages
		 WHERE session_id IN (SELECT id FROM chat_sessions WHERE project_key = ?)`,
		projectKey,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM chat_events
		 WHERE session_id IN (SELECT id FROM chat_sessions WHERE project_key = ?)`,
		projectKey,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM chat_sessions WHERE project_key = ?`, projectKey); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM projects WHERE id = ?`, projectID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM app_state WHERE key = 'active_project_id' AND value = ?`, projectID); err != nil {
		return err
	}

	return tx.Commit()
}
