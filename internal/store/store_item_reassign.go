package store

import (
	"database/sql"
	"strings"
)

func normalizeOptionalProjectID(value *string) any {
	if value == nil {
		return nil
	}
	clean := strings.TrimSpace(*value)
	if clean == "" {
		return nil
	}
	return clean
}

func (s *Store) SetItemWorkspace(id int64, workspaceID *int64) error {
	res, err := s.db.Exec(
		`UPDATE items
		 SET workspace_id = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		nullablePositiveID(valueOrZeroInt64(workspaceID)),
		id,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SetItemProject(id int64, projectID *string) error {
	res, err := s.db.Exec(
		`UPDATE items
		 SET project_id = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		normalizeOptionalProjectID(projectID),
		id,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func valueOrZeroInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
