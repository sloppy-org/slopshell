package store

import (
	"database/sql"
	"errors"
	"strings"
)

func (s *Store) CreateContext(name string, parentID *int64) (Context, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return Context{}, errors.New("context name is required")
	}
	if parentID != nil && *parentID <= 0 {
		return Context{}, errors.New("parent_id must be a positive integer")
	}
	var existingID int64
	err := s.db.QueryRow(
		`SELECT id
		 FROM contexts
		 WHERE lower(name) = lower(?)
		   AND (
		     (parent_id IS NULL AND ? IS NULL)
		     OR parent_id = ?
		   )`,
		cleanName,
		parentID,
		parentID,
	).Scan(&existingID)
	switch {
	case err == nil:
		return s.GetContext(existingID)
	case !errors.Is(err, sql.ErrNoRows):
		return Context{}, err
	}
	res, err := s.db.Exec(
		`INSERT INTO contexts (name, parent_id) VALUES (?, ?)`,
		cleanName,
		parentID,
	)
	if err != nil {
		return Context{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Context{}, err
	}
	return s.GetContext(id)
}

func (s *Store) GetContext(id int64) (Context, error) {
	row := s.db.QueryRow(
		`SELECT id, name, color, parent_id, created_at
		 FROM contexts
		 WHERE id = ?`,
		id,
	)
	var (
		ctx      Context
		parentID sql.NullInt64
	)
	if err := row.Scan(&ctx.ID, &ctx.Name, &ctx.Color, &parentID, &ctx.CreatedAt); err != nil {
		return Context{}, err
	}
	ctx.ParentID = nullInt64Pointer(parentID)
	return ctx, nil
}

func (s *Store) LinkContextToWorkspace(contextID, workspaceID int64) error {
	if contextID <= 0 || workspaceID <= 0 {
		return errors.New("context_id and workspace_id must be positive integers")
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO context_workspaces (context_id, workspace_id) VALUES (?, ?)`,
		contextID,
		workspaceID,
	)
	return err
}

func (s *Store) LinkContextToItem(contextID, itemID int64) error {
	if contextID <= 0 || itemID <= 0 {
		return errors.New("context_id and item_id must be positive integers")
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO context_items (context_id, item_id) VALUES (?, ?)`,
		contextID,
		itemID,
	)
	return err
}

func (s *Store) ListContexts() ([]Context, error) {
	rows, err := s.db.Query(
		`SELECT id, name, color, parent_id, created_at
		 FROM contexts
		 ORDER BY CASE WHEN parent_id IS NULL THEN 0 ELSE 1 END, lower(name), id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Context
	for rows.Next() {
		var (
			ctx      Context
			parentID sql.NullInt64
		)
		if err := rows.Scan(&ctx.ID, &ctx.Name, &ctx.Color, &parentID, &ctx.CreatedAt); err != nil {
			return nil, err
		}
		ctx.ParentID = nullInt64Pointer(parentID)
		out = append(out, ctx)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
