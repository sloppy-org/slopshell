package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *Store) migrateDomainTables() error {
	schema := `
CREATE TABLE IF NOT EXISTS workspaces (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  dir_path TEXT NOT NULL UNIQUE,
  is_active INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS actors (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('human', 'agent')),
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS artifacts (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,
  ref_path TEXT,
  ref_url TEXT,
  title TEXT,
  meta_json TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS items (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'inbox' CHECK (state IN ('inbox', 'waiting', 'done')),
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  artifact_id INTEGER REFERENCES artifacts(id) ON DELETE SET NULL,
  actor_id INTEGER REFERENCES actors(id) ON DELETE SET NULL,
  visible_after TEXT,
  follow_up_at TEXT,
  source TEXT,
  source_ref TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
`
	_, err := s.db.Exec(schema)
	return err
}

func normalizeWorkspaceName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeWorkspacePath(path string) string {
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

func normalizeActorName(name string) string {
	return strings.TrimSpace(name)
}

func normalizeActorKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case ActorKindHuman:
		return ActorKindHuman
	case ActorKindAgent:
		return ActorKindAgent
	default:
		return ""
	}
}

func normalizeOptionalString(v *string) any {
	if v == nil {
		return nil
	}
	clean := strings.TrimSpace(*v)
	if clean == "" {
		return nil
	}
	return clean
}

func normalizeArtifactKind(kind ArtifactKind) ArtifactKind {
	return ArtifactKind(strings.TrimSpace(string(kind)))
}

func normalizeItemState(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "", ItemStateInbox:
		return ItemStateInbox
	case ItemStateWaiting:
		return ItemStateWaiting
	case ItemStateDone:
		return ItemStateDone
	default:
		return ""
	}
}

func validateItemTransition(current, next string) error {
	if next == "" {
		return errors.New("item state is required")
	}
	if current == ItemStateDone && next != ItemStateDone {
		return fmt.Errorf("cannot transition item from %s to %s", current, next)
	}
	return nil
}

func scanWorkspace(
	row interface {
		Scan(dest ...any) error
	},
) (Workspace, error) {
	var out Workspace
	var isActive int
	err := row.Scan(&out.ID, &out.Name, &out.DirPath, &isActive, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return Workspace{}, err
	}
	out.Name = normalizeWorkspaceName(out.Name)
	out.DirPath = normalizeWorkspacePath(out.DirPath)
	out.IsActive = isActive != 0
	return out, nil
}

func scanActor(
	row interface {
		Scan(dest ...any) error
	},
) (Actor, error) {
	var out Actor
	err := row.Scan(&out.ID, &out.Name, &out.Kind, &out.CreatedAt)
	if err != nil {
		return Actor{}, err
	}
	out.Name = normalizeActorName(out.Name)
	out.Kind = normalizeActorKind(out.Kind)
	return out, nil
}

func scanArtifact(
	row interface {
		Scan(dest ...any) error
	},
) (Artifact, error) {
	var (
		out                              Artifact
		refPath, refURL, title, metaJSON sql.NullString
	)
	err := row.Scan(&out.ID, &out.Kind, &refPath, &refURL, &title, &metaJSON, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return Artifact{}, err
	}
	out.Kind = normalizeArtifactKind(out.Kind)
	out.RefPath = nullStringPointer(refPath)
	out.RefURL = nullStringPointer(refURL)
	out.Title = nullStringPointer(title)
	out.MetaJSON = nullStringPointer(metaJSON)
	return out, nil
}

func scanItem(
	row interface {
		Scan(dest ...any) error
	},
) (Item, error) {
	var (
		out                                         Item
		workspaceID, artifactID, actorID            sql.NullInt64
		visibleAfter, followUpAt, source, sourceRef sql.NullString
	)
	err := row.Scan(
		&out.ID,
		&out.Title,
		&out.State,
		&workspaceID,
		&artifactID,
		&actorID,
		&visibleAfter,
		&followUpAt,
		&source,
		&sourceRef,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		return Item{}, err
	}
	out.Title = strings.TrimSpace(out.Title)
	out.State = normalizeItemState(out.State)
	out.WorkspaceID = nullInt64Pointer(workspaceID)
	out.ArtifactID = nullInt64Pointer(artifactID)
	out.ActorID = nullInt64Pointer(actorID)
	out.VisibleAfter = nullStringPointer(visibleAfter)
	out.FollowUpAt = nullStringPointer(followUpAt)
	out.Source = nullStringPointer(source)
	out.SourceRef = nullStringPointer(sourceRef)
	return out, nil
}

func nullStringPointer(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	value := strings.TrimSpace(v.String)
	return &value
}

func nullInt64Pointer(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}

func (s *Store) CreateWorkspace(name, dirPath string) (Workspace, error) {
	cleanName := normalizeWorkspaceName(name)
	cleanPath := normalizeWorkspacePath(dirPath)
	if cleanName == "" {
		return Workspace{}, errors.New("workspace name is required")
	}
	if cleanPath == "" {
		return Workspace{}, errors.New("workspace path is required")
	}
	res, err := s.db.Exec(`INSERT INTO workspaces (name, dir_path) VALUES (?, ?)`, cleanName, cleanPath)
	if err != nil {
		return Workspace{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Workspace{}, err
	}
	return s.GetWorkspace(id)
}

func (s *Store) GetWorkspace(id int64) (Workspace, error) {
	return scanWorkspace(s.db.QueryRow(
		`SELECT id, name, dir_path, is_active, created_at, updated_at
		 FROM workspaces
		 WHERE id = ?`,
		id,
	))
}

func (s *Store) GetWorkspaceByPath(dirPath string) (Workspace, error) {
	return scanWorkspace(s.db.QueryRow(
		`SELECT id, name, dir_path, is_active, created_at, updated_at
		 FROM workspaces
		 WHERE dir_path = ?`,
		normalizeWorkspacePath(dirPath),
	))
}

func (s *Store) ListWorkspaces() ([]Workspace, error) {
	rows, err := s.db.Query(
		`SELECT id, name, dir_path, is_active, created_at, updated_at
		 FROM workspaces`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		workspace, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, workspace)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsActive != out[j].IsActive {
			return out[i].IsActive
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *Store) SetActiveWorkspace(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE workspaces SET is_active = 0, updated_at = datetime('now') WHERE is_active <> 0`); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE workspaces SET is_active = 1, updated_at = datetime('now') WHERE id = ?`, id)
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
	return tx.Commit()
}

func (s *Store) DeleteWorkspace(id int64) error {
	res, err := s.db.Exec(`DELETE FROM workspaces WHERE id = ?`, id)
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

func (s *Store) CreateActor(name, kind string) (Actor, error) {
	cleanName := normalizeActorName(name)
	cleanKind := normalizeActorKind(kind)
	if cleanName == "" {
		return Actor{}, errors.New("actor name is required")
	}
	if cleanKind == "" {
		return Actor{}, errors.New("actor kind must be human or agent")
	}
	res, err := s.db.Exec(`INSERT INTO actors (name, kind) VALUES (?, ?)`, cleanName, cleanKind)
	if err != nil {
		return Actor{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Actor{}, err
	}
	return s.GetActor(id)
}

func (s *Store) GetActor(id int64) (Actor, error) {
	return scanActor(s.db.QueryRow(
		`SELECT id, name, kind, created_at
		 FROM actors
		 WHERE id = ?`,
		id,
	))
}

func (s *Store) ListActors() ([]Actor, error) {
	rows, err := s.db.Query(`SELECT id, name, kind, created_at FROM actors`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Actor
	for rows.Next() {
		actor, err := scanActor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, actor)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func (s *Store) DeleteActor(id int64) error {
	res, err := s.db.Exec(`DELETE FROM actors WHERE id = ?`, id)
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

func (s *Store) CreateArtifact(kind ArtifactKind, refPath, refURL, title, metaJSON *string) (Artifact, error) {
	cleanKind := normalizeArtifactKind(kind)
	if cleanKind == "" {
		return Artifact{}, errors.New("artifact kind is required")
	}
	res, err := s.db.Exec(
		`INSERT INTO artifacts (kind, ref_path, ref_url, title, meta_json)
		 VALUES (?, ?, ?, ?, ?)`,
		cleanKind,
		normalizeOptionalString(refPath),
		normalizeOptionalString(refURL),
		normalizeOptionalString(title),
		normalizeOptionalString(metaJSON),
	)
	if err != nil {
		return Artifact{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Artifact{}, err
	}
	return s.GetArtifact(id)
}

func (s *Store) GetArtifact(id int64) (Artifact, error) {
	return scanArtifact(s.db.QueryRow(
		`SELECT id, kind, ref_path, ref_url, title, meta_json, created_at, updated_at
		 FROM artifacts
		 WHERE id = ?`,
		id,
	))
}

func (s *Store) ListArtifactsByKind(kind ArtifactKind) ([]Artifact, error) {
	cleanKind := normalizeArtifactKind(kind)
	if cleanKind == "" {
		return nil, errors.New("artifact kind is required")
	}
	rows, err := s.db.Query(
		`SELECT id, kind, ref_path, ref_url, title, meta_json, created_at, updated_at
		 FROM artifacts
		 WHERE kind = ?`,
		cleanKind,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		artifact, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}

func (s *Store) UpdateArtifact(id int64, updates ArtifactUpdate) error {
	parts := []string{}
	args := []any{}
	if updates.Kind != nil {
		cleanKind := normalizeArtifactKind(*updates.Kind)
		if cleanKind == "" {
			return errors.New("artifact kind is required")
		}
		parts = append(parts, "kind = ?")
		args = append(args, cleanKind)
	}
	if updates.RefPath != nil {
		parts = append(parts, "ref_path = ?")
		args = append(args, normalizeOptionalString(updates.RefPath))
	}
	if updates.RefURL != nil {
		parts = append(parts, "ref_url = ?")
		args = append(args, normalizeOptionalString(updates.RefURL))
	}
	if updates.Title != nil {
		parts = append(parts, "title = ?")
		args = append(args, normalizeOptionalString(updates.Title))
	}
	if updates.MetaJSON != nil {
		parts = append(parts, "meta_json = ?")
		args = append(args, normalizeOptionalString(updates.MetaJSON))
	}
	if len(parts) == 0 {
		_, err := s.GetArtifact(id)
		return err
	}
	parts = append(parts, "updated_at = datetime('now')")
	args = append(args, id)
	res, err := s.db.Exec(`UPDATE artifacts SET `+stringsJoin(parts, ", ")+` WHERE id = ?`, args...)
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

func (s *Store) DeleteArtifact(id int64) error {
	res, err := s.db.Exec(`DELETE FROM artifacts WHERE id = ?`, id)
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

func (s *Store) CreateItem(title string, opts ItemOptions) (Item, error) {
	cleanTitle := strings.TrimSpace(title)
	cleanState := normalizeItemState(opts.State)
	if cleanTitle == "" {
		return Item{}, errors.New("item title is required")
	}
	if cleanState == "" {
		return Item{}, errors.New("invalid item state")
	}
	res, err := s.db.Exec(
		`INSERT INTO items (
			title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cleanTitle,
		cleanState,
		opts.WorkspaceID,
		opts.ArtifactID,
		opts.ActorID,
		normalizeOptionalString(opts.VisibleAfter),
		normalizeOptionalString(opts.FollowUpAt),
		normalizeOptionalString(opts.Source),
		normalizeOptionalString(opts.SourceRef),
	)
	if err != nil {
		return Item{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Item{}, err
	}
	return s.GetItem(id)
}

func (s *Store) GetItem(id int64) (Item, error) {
	return scanItem(s.db.QueryRow(
		`SELECT id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, created_at, updated_at
		 FROM items
		 WHERE id = ?`,
		id,
	))
}

func (s *Store) UpdateItemState(id int64, state string) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	next := normalizeItemState(state)
	if err := validateItemTransition(item.State, next); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE items SET state = ?, updated_at = datetime('now') WHERE id = ?`,
		next,
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

func (s *Store) AssignItem(id, actorID int64) error {
	if _, err := s.GetActor(actorID); err != nil {
		return err
	}
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	if item.State == ItemStateDone {
		return fmt.Errorf("cannot assign item in %s state", item.State)
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET actor_id = ?, state = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		actorID,
		ItemStateWaiting,
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

func (s *Store) UnassignItem(id int64) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	if item.State == ItemStateDone {
		return fmt.Errorf("cannot unassign item in %s state", item.State)
	}
	if item.ActorID == nil {
		return errors.New("item is not assigned")
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET actor_id = NULL, state = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		ItemStateInbox,
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

func (s *Store) CompleteItemByActor(id, actorID int64) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	if item.State == ItemStateDone {
		return fmt.Errorf("cannot complete item in %s state", item.State)
	}
	if item.ActorID == nil {
		return errors.New("item is not assigned")
	}
	if *item.ActorID != actorID {
		return errors.New("item actor does not match")
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET state = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		ItemStateDone,
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

func (s *Store) ReturnItemToInbox(id int64) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}
	if item.State == ItemStateDone {
		return fmt.Errorf("cannot return item from %s state", item.State)
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET state = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		ItemStateInbox,
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

func (s *Store) CompleteItemBySource(source, sourceRef string) error {
	cleanSource := strings.TrimSpace(source)
	cleanSourceRef := strings.TrimSpace(sourceRef)
	if cleanSource == "" || cleanSourceRef == "" {
		return errors.New("item source and source_ref are required")
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET state = ?, updated_at = datetime('now')
		 WHERE source = ? AND source_ref = ? AND state != ?`,
		ItemStateDone,
		cleanSource,
		cleanSourceRef,
		ItemStateDone,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	var existingCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM items WHERE source = ? AND source_ref = ?`,
		cleanSource,
		cleanSourceRef,
	).Scan(&existingCount); err != nil {
		return err
	}
	if existingCount == 0 {
		return sql.ErrNoRows
	}
	return fmt.Errorf("cannot complete item from source %s:%s", cleanSource, cleanSourceRef)
}

func (s *Store) UpdateItemTimes(id int64, visibleAfter, followUpAt *string) error {
	res, err := s.db.Exec(
		`UPDATE items
		 SET visible_after = ?, follow_up_at = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		normalizeOptionalString(visibleAfter),
		normalizeOptionalString(followUpAt),
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

func (s *Store) ResurfaceDueItems(now time.Time) (int, error) {
	cutoff := now.UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE items
		 SET state = ?, updated_at = datetime('now')
		 WHERE state = ?
		   AND (
		     (visible_after IS NOT NULL AND trim(visible_after) <> '' AND datetime(visible_after) <= datetime(?))
		     OR
		     (follow_up_at IS NOT NULL AND trim(follow_up_at) <> '' AND datetime(follow_up_at) <= datetime(?))
		   )`,
		ItemStateInbox,
		ItemStateWaiting,
		cutoff,
		cutoff,
	)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

func (s *Store) DeleteItem(id int64) error {
	res, err := s.db.Exec(`DELETE FROM items WHERE id = ?`, id)
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

func (s *Store) ListItemsByState(state string) ([]Item, error) {
	cleanState := normalizeItemState(state)
	if cleanState == "" {
		return nil, errors.New("invalid item state")
	}
	rows, err := s.db.Query(
		`SELECT id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, created_at, updated_at
		 FROM items
		 WHERE state = ?`,
		cleanState,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out, nil
}
