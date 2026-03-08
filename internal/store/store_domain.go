package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const itemsTableSchema = `CREATE TABLE IF NOT EXISTS items (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'inbox' CHECK (state IN ('inbox', 'waiting', 'someday', 'done')),
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  artifact_id INTEGER REFERENCES artifacts(id) ON DELETE SET NULL,
  actor_id INTEGER REFERENCES actors(id) ON DELETE SET NULL,
  visible_after TEXT,
  follow_up_at TEXT,
  source TEXT,
  source_ref TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);`

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
CREATE TABLE IF NOT EXISTS external_accounts (
  id INTEGER PRIMARY KEY,
  sphere TEXT NOT NULL CHECK (sphere IN ('work', 'private')),
  provider TEXT NOT NULL,
  label TEXT NOT NULL,
  config_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_external_accounts_identity
  ON external_accounts(lower(sphere), lower(provider), lower(label));
`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}
	if _, err := s.db.Exec(itemsTableSchema); err != nil {
		return err
	}
	return s.migrateItemTableStateSupport()
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
	case ItemStateSomeday:
		return ItemStateSomeday
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

func (s *Store) migrateItemTableStateSupport() error {
	var schema sql.NullString
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'items'`).Scan(&schema); err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(schema.String), "'someday'") {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`ALTER TABLE items RENAME TO items_legacy`); err != nil {
		return err
	}
	if _, err := tx.Exec(strings.Replace(itemsTableSchema, "IF NOT EXISTS ", "", 1)); err != nil {
		return err
	}
	if _, err := tx.Exec(`
INSERT INTO items (
	id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, created_at, updated_at
)
SELECT
	id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, created_at, updated_at
FROM items_legacy
`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE items_legacy`); err != nil {
		return err
	}
	return tx.Commit()
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

func (s *Store) FindWorkspaceContainingPath(filePath string) (*int64, error) {
	targetPath := normalizeWorkspacePath(filePath)
	if targetPath == "" {
		return nil, nil
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	var best *Workspace
	for i := range workspaces {
		rel, err := filepath.Rel(workspaces[i].DirPath, targetPath)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if best == nil || len(workspaces[i].DirPath) > len(best.DirPath) {
			best = &workspaces[i]
		}
	}
	if best == nil {
		return nil, nil
	}
	return &best.ID, nil
}

func normalizeGitHubOwnerRepo(raw string) string {
	clean := strings.TrimSpace(strings.ToLower(raw))
	if clean == "" {
		return ""
	}
	clean = strings.TrimSuffix(clean, ".git")
	if idx := strings.Index(clean, "#"); idx >= 0 {
		clean = clean[:idx]
	}
	clean = strings.Trim(clean, "/")
	switch {
	case strings.HasPrefix(clean, "git@github.com:"):
		clean = strings.TrimPrefix(clean, "git@github.com:")
	case strings.HasPrefix(clean, "ssh://git@github.com/"):
		clean = strings.TrimPrefix(clean, "ssh://git@github.com/")
	case strings.HasPrefix(clean, "https://github.com/"):
		clean = strings.TrimPrefix(clean, "https://github.com/")
	case strings.HasPrefix(clean, "http://github.com/"):
		clean = strings.TrimPrefix(clean, "http://github.com/")
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func githubOwnerRepoFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if !strings.EqualFold(parsed.Host, "github.com") {
		return ""
	}
	return normalizeGitHubOwnerRepo(parsed.Path)
}

func githubOwnerRepoFromMeta(metaJSON string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(metaJSON), &payload); err != nil {
		return ""
	}
	for _, key := range []string{"owner_repo", "repo", "source_ref", "url", "html_url"} {
		value, _ := payload[key].(string)
		if repo := normalizeGitHubOwnerRepo(value); repo != "" {
			return repo
		}
		if repo := githubOwnerRepoFromURL(value); repo != "" {
			return repo
		}
	}
	return ""
}

func workspaceGitRemoteOwnerRepo(dirPath string) (string, error) {
	cmd := exec.Command("git", "-C", dirPath, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", nil
		}
		return "", err
	}
	return normalizeGitHubOwnerRepo(string(output)), nil
}

func (s *Store) FindWorkspaceByGitRemote(ownerRepo string) (*int64, error) {
	target := normalizeGitHubOwnerRepo(ownerRepo)
	if target == "" {
		return nil, nil
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	var matches []int64
	for _, workspace := range workspaces {
		repo, err := workspaceGitRemoteOwnerRepo(workspace.DirPath)
		if err != nil {
			return nil, err
		}
		if repo == target {
			matches = append(matches, workspace.ID)
		}
	}
	if len(matches) != 1 {
		return nil, nil
	}
	return &matches[0], nil
}

func (s *Store) GitHubRepoForWorkspace(id int64) (string, error) {
	workspace, err := s.GetWorkspace(id)
	if err != nil {
		return "", err
	}
	return workspaceGitRemoteOwnerRepo(workspace.DirPath)
}

func (s *Store) InferWorkspaceForArtifact(artifact Artifact) (*int64, error) {
	switch artifact.Kind {
	case ArtifactKindDocument, ArtifactKindMarkdown, ArtifactKindPDF:
		if artifact.RefPath == nil {
			return nil, nil
		}
		return s.FindWorkspaceContainingPath(*artifact.RefPath)
	case ArtifactKindGitHubIssue, ArtifactKindGitHubPR:
		var ownerRepo string
		if artifact.RefURL != nil {
			ownerRepo = githubOwnerRepoFromURL(*artifact.RefURL)
		}
		if ownerRepo == "" && artifact.MetaJSON != nil {
			ownerRepo = githubOwnerRepoFromMeta(*artifact.MetaJSON)
		}
		if ownerRepo == "" {
			return nil, nil
		}
		return s.FindWorkspaceByGitRemote(ownerRepo)
	default:
		return nil, nil
	}
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

func (s *Store) ListArtifacts() ([]Artifact, error) {
	rows, err := s.db.Query(
		`SELECT id, kind, ref_path, ref_url, title, meta_json, created_at, updated_at
		 FROM artifacts`,
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
	if opts.WorkspaceID == nil && opts.ArtifactID != nil {
		artifact, err := s.GetArtifact(*opts.ArtifactID)
		if err != nil {
			return Item{}, err
		}
		inferredWorkspaceID, err := s.InferWorkspaceForArtifact(artifact)
		if err != nil {
			return Item{}, err
		}
		opts.WorkspaceID = inferredWorkspaceID
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

func (s *Store) GetItemBySource(source, sourceRef string) (Item, error) {
	cleanSource := strings.TrimSpace(source)
	cleanSourceRef := strings.TrimSpace(sourceRef)
	if cleanSource == "" || cleanSourceRef == "" {
		return Item{}, errors.New("item source and source_ref are required")
	}
	return scanItem(s.db.QueryRow(
		`SELECT id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, created_at, updated_at
		 FROM items
		 WHERE source = ? AND source_ref = ?`,
		cleanSource,
		cleanSourceRef,
	))
}

func (s *Store) UpsertItemFromSource(source, sourceRef, title string, workspaceID *int64) (Item, error) {
	cleanSource := strings.TrimSpace(source)
	cleanSourceRef := strings.TrimSpace(sourceRef)
	cleanTitle := strings.TrimSpace(title)
	if cleanSource == "" || cleanSourceRef == "" {
		return Item{}, errors.New("item source and source_ref are required")
	}
	if cleanTitle == "" {
		return Item{}, errors.New("item title is required")
	}

	existing, err := s.GetItemBySource(cleanSource, cleanSourceRef)
	switch {
	case err == nil:
		res, err := s.db.Exec(
			`UPDATE items
			 SET title = ?, workspace_id = ?, updated_at = datetime('now')
			 WHERE id = ?`,
			cleanTitle,
			workspaceID,
			existing.ID,
		)
		if err != nil {
			return Item{}, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return Item{}, err
		}
		if affected == 0 {
			return Item{}, sql.ErrNoRows
		}
		return s.GetItem(existing.ID)
	case !errors.Is(err, sql.ErrNoRows):
		return Item{}, err
	}

	return s.CreateItem(cleanTitle, ItemOptions{
		WorkspaceID: workspaceID,
		Source:      &cleanSource,
		SourceRef:   &cleanSourceRef,
	})
}

func (s *Store) UpdateItemArtifact(id int64, artifactID *int64) error {
	res, err := s.db.Exec(
		`UPDATE items
		 SET artifact_id = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		artifactID,
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

func (s *Store) UpdateItemSource(id int64, source, sourceRef string) error {
	cleanSource := strings.TrimSpace(source)
	cleanSourceRef := strings.TrimSpace(sourceRef)
	if cleanSource == "" || cleanSourceRef == "" {
		return errors.New("item source and source_ref are required")
	}
	existing, err := s.GetItemBySource(cleanSource, cleanSourceRef)
	switch {
	case err == nil && existing.ID != id:
		return fmt.Errorf("item source %s:%s is already linked to item %d", cleanSource, cleanSourceRef, existing.ID)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return err
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET source = ?, source_ref = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		cleanSource,
		cleanSourceRef,
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

func (s *Store) UpdateItem(id int64, updates ItemUpdate) error {
	item, err := s.GetItem(id)
	if err != nil {
		return err
	}

	parts := []string{}
	args := []any{}

	if updates.Title != nil {
		title := strings.TrimSpace(*updates.Title)
		if title == "" {
			return errors.New("item title is required")
		}
		parts = append(parts, "title = ?")
		args = append(args, title)
	}
	if updates.State != nil {
		next := normalizeItemState(*updates.State)
		if err := validateItemTransition(item.State, next); err != nil {
			return err
		}
		parts = append(parts, "state = ?")
		args = append(args, next)
	}
	if updates.WorkspaceID != nil {
		parts = append(parts, "workspace_id = ?")
		args = append(args, nullablePositiveID(*updates.WorkspaceID))
	}
	if updates.ArtifactID != nil {
		parts = append(parts, "artifact_id = ?")
		args = append(args, nullablePositiveID(*updates.ArtifactID))
	}
	if updates.ActorID != nil {
		parts = append(parts, "actor_id = ?")
		args = append(args, nullablePositiveID(*updates.ActorID))
	}
	if updates.VisibleAfter != nil {
		value, err := normalizeOptionalRFC3339String(updates.VisibleAfter)
		if err != nil {
			return err
		}
		parts = append(parts, "visible_after = ?")
		args = append(args, value)
	}
	if updates.FollowUpAt != nil {
		value, err := normalizeOptionalRFC3339String(updates.FollowUpAt)
		if err != nil {
			return err
		}
		parts = append(parts, "follow_up_at = ?")
		args = append(args, value)
	}
	if updates.Source != nil {
		sourceValue := strings.TrimSpace(*updates.Source)
		sourceRefValue := strings.TrimSpace(nullStringValue(updates.SourceRef))
		switch {
		case sourceValue == "" && sourceRefValue != "":
			return errors.New("item source and source_ref are required")
		case sourceValue != "" && sourceRefValue == "":
			return errors.New("item source and source_ref are required")
		case sourceValue != "" && sourceRefValue != "":
			if err := s.UpdateItemSource(id, sourceValue, sourceRefValue); err != nil {
				return err
			}
		case sourceValue == "" && sourceRefValue == "":
			parts = append(parts, "source = ?", "source_ref = ?")
			args = append(args, nil, nil)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	parts = append(parts, "updated_at = datetime('now')")
	args = append(args, id)
	res, err := s.db.Exec(`UPDATE items SET `+stringsJoin(parts, ", ")+` WHERE id = ?`, args...)
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

func (s *Store) SyncItemStateBySource(source, sourceRef, state string) error {
	cleanSource := strings.TrimSpace(source)
	cleanSourceRef := strings.TrimSpace(sourceRef)
	cleanState := normalizeItemState(state)
	if cleanSource == "" || cleanSourceRef == "" {
		return errors.New("item source and source_ref are required")
	}
	if cleanState == "" {
		return errors.New("invalid item state")
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET state = ?, updated_at = datetime('now')
		 WHERE source = ? AND source_ref = ?`,
		cleanState,
		cleanSource,
		cleanSourceRef,
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

func (s *Store) triageableItem(id int64) (Item, error) {
	item, err := s.GetItem(id)
	if err != nil {
		return Item{}, err
	}
	if item.State == ItemStateDone {
		return Item{}, fmt.Errorf("cannot triage item in %s state", item.State)
	}
	return item, nil
}

func normalizeRFC3339String(value string) (string, error) {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}

func (s *Store) TriageItemDone(id int64) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
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

func (s *Store) TriageItemLater(id int64, visibleAfter string) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
	}
	normalized, err := normalizeRFC3339String(visibleAfter)
	if err != nil {
		return errors.New("visible_after must be a valid RFC3339 timestamp")
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET state = ?, visible_after = ?, updated_at = datetime('now')
		 WHERE id = ?`,
		ItemStateWaiting,
		normalized,
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

func (s *Store) TriageItemDelegate(id, actorID int64) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
	}
	if _, err := s.GetActor(actorID); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET actor_id = ?, state = ?, visible_after = NULL, updated_at = datetime('now')
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

func (s *Store) TriageItemDelete(id int64) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
	}
	return s.DeleteItem(id)
}

func (s *Store) TriageItemSomeday(id int64) error {
	if _, err := s.triageableItem(id); err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE items
		 SET state = ?, visible_after = NULL, follow_up_at = NULL, updated_at = datetime('now')
		 WHERE id = ?`,
		ItemStateSomeday,
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

func (s *Store) ListItems() ([]Item, error) {
	rows, err := s.db.Query(
		`SELECT id, title, state, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref, created_at, updated_at
		 FROM items`,
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

func nullablePositiveID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

func nullStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func normalizeOptionalRFC3339String(value *string) (any, error) {
	if value == nil {
		return nil, nil
	}
	clean := strings.TrimSpace(*value)
	if clean == "" {
		return nil, nil
	}
	normalized, err := normalizeRFC3339String(clean)
	if err != nil {
		return nil, errors.New("timestamps must be valid RFC3339")
	}
	return normalized, nil
}
