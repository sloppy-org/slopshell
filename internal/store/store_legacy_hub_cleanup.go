package store

import (
	"database/sql"
	"path/filepath"
	"strings"
)

type legacyHubProjectRow struct {
	ID         string
	Name       string
	ProjectKey string
	RootPath   string
	Kind       string
}

func normalizeLegacyHubPath(path string) string {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return ""
	}
	abs, err := filepath.Abs(clean)
	if err == nil {
		clean = abs
	}
	return filepath.Clean(clean)
}

func isLegacyHubProjectRow(row legacyHubProjectRow) bool {
	projectKey := strings.TrimSpace(row.ProjectKey)
	if strings.EqualFold(projectKey, "__hub__") {
		return true
	}
	kind := strings.ToLower(strings.TrimSpace(row.Kind))
	if kind == "hub" {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(row.Name))
	rootPath := strings.ToLower(normalizeLegacyHubPath(row.RootPath))
	if name != "hub" {
		return false
	}
	return rootPath == "" || strings.HasSuffix(rootPath, string(filepath.Separator)+"projects"+string(filepath.Separator)+"hub")
}

func isLegacyHubWorkspace(workspace Workspace, hubProjectIDs map[string]struct{}) bool {
	if workspace.ProjectID != nil {
		if _, ok := hubProjectIDs[strings.TrimSpace(*workspace.ProjectID)]; ok {
			return true
		}
	}
	name := strings.TrimSpace(workspace.Name)
	if !strings.EqualFold(name, "hub") {
		return false
	}
	rootPath := strings.ToLower(normalizeLegacyHubPath(workspace.DirPath))
	return rootPath == "" || strings.HasSuffix(rootPath, string(filepath.Separator)+"projects"+string(filepath.Separator)+"hub")
}

func contextHasAnyLinks(tx *sql.Tx, contextID int64) (bool, error) {
	for _, check := range []struct {
		table string
		query string
	}{
		{table: "context_items", query: `SELECT 1 FROM context_items WHERE context_id = ? LIMIT 1`},
		{table: "context_artifacts", query: `SELECT 1 FROM context_artifacts WHERE context_id = ? LIMIT 1`},
		{table: "context_workspaces", query: `SELECT 1 FROM context_workspaces WHERE context_id = ? LIMIT 1`},
		{table: "context_external_accounts", query: `SELECT 1 FROM context_external_accounts WHERE context_id = ? LIMIT 1`},
		{table: "context_external_container_mappings", query: `SELECT 1 FROM context_external_container_mappings WHERE context_id = ? LIMIT 1`},
		{table: "context_time_entries", query: `SELECT 1 FROM context_time_entries WHERE context_id = ? LIMIT 1`},
	} {
		present, err := tableExists(tx, check.table)
		if err != nil {
			return false, err
		}
		if !present {
			continue
		}
		var sentinel int
		err = tx.QueryRow(check.query, contextID).Scan(&sentinel)
		switch {
		case err == nil:
			return true, nil
		case err == sql.ErrNoRows:
			continue
		default:
			return false, err
		}
	}
	return false, nil
}

func tableExists(tx *sql.Tx, table string) (bool, error) {
	var name string
	err := tx.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
		strings.TrimSpace(table),
	).Scan(&name)
	switch {
	case err == nil:
		return true, nil
	case err == sql.ErrNoRows:
		return false, nil
	default:
		return false, err
	}
}

func (s *Store) purgeLegacyHubData() error {
	rows, err := s.db.Query(`SELECT id, name, project_key, root_path, kind FROM projects`)
	if err != nil {
		return err
	}
	defer rows.Close()

	hubProjects := make([]legacyHubProjectRow, 0, 2)
	for rows.Next() {
		var row legacyHubProjectRow
		if err := rows.Scan(&row.ID, &row.Name, &row.ProjectKey, &row.RootPath, &row.Kind); err != nil {
			return err
		}
		if isLegacyHubProjectRow(row) {
			hubProjects = append(hubProjects, row)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(hubProjects) == 0 {
		return nil
	}

	hubProjectIDs := make(map[string]struct{}, len(hubProjects))
	for _, row := range hubProjects {
		hubProjectIDs[strings.TrimSpace(row.ID)] = struct{}{}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	workspaceRows, err := tx.Query(`SELECT id, name, dir_path, project_id FROM workspaces`)
	if err != nil {
		return err
	}
	var hubWorkspaceIDs []int64
	for workspaceRows.Next() {
		var (
			workspaceID int64
			name        string
			dirPath     string
			projectID   sql.NullString
		)
		if err := workspaceRows.Scan(&workspaceID, &name, &dirPath, &projectID); err != nil {
			workspaceRows.Close()
			return err
		}
		workspace := Workspace{
			ID:      workspaceID,
			Name:    name,
			DirPath: dirPath,
		}
		if projectID.Valid {
			clean := strings.TrimSpace(projectID.String)
			if clean != "" {
				workspace.ProjectID = &clean
			}
		}
		if isLegacyHubWorkspace(workspace, hubProjectIDs) {
			hubWorkspaceIDs = append(hubWorkspaceIDs, workspace.ID)
		}
	}
	if err := workspaceRows.Close(); err != nil {
		return err
	}
	if len(hubWorkspaceIDs) > 0 {
		for _, workspaceID := range hubWorkspaceIDs {
			if _, err := tx.Exec(`DELETE FROM workspaces WHERE id = ?`, workspaceID); err != nil {
				return err
			}
		}
	}

	for projectID := range hubProjectIDs {
		if _, err := tx.Exec(`DELETE FROM app_state WHERE key = 'active_project_id' AND value = ?`, projectID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM projects WHERE id = ?`, projectID); err != nil {
			return err
		}
	}

	contextsPresent, err := tableExists(tx, "contexts")
	if err != nil {
		return err
	}
	if !contextsPresent {
		return tx.Commit()
	}

	contextRows, err := tx.Query(`SELECT id, name FROM contexts WHERE lower(name) = 'hub'`)
	if err != nil {
		return err
	}
	var contextIDs []int64
	for contextRows.Next() {
		var contextID int64
		var name string
		if err := contextRows.Scan(&contextID, &name); err != nil {
			contextRows.Close()
			return err
		}
		contextIDs = append(contextIDs, contextID)
	}
	if err := contextRows.Close(); err != nil {
		return err
	}
	for _, contextID := range contextIDs {
		hasLinks, err := contextHasAnyLinks(tx, contextID)
		if err != nil {
			return err
		}
		if hasLinks {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM contexts WHERE id = ?`, contextID); err != nil {
			return err
		}
	}

	return tx.Commit()
}
