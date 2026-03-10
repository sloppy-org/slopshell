package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestStoreMigratesLegacyProjectsIntoWorkspaces(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tabura.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}

	alphaPath := filepath.Join(t.TempDir(), "alpha")
	betaPath := filepath.Join(t.TempDir(), "beta")
	legacySchema := `
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  project_key TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL DEFAULT 'managed',
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT INTO projects (id, name, project_key, root_path, kind, is_default, created_at, updated_at) VALUES
  ('proj-alpha', 'Alpha', 'alpha-key', ?, 'managed', 1, 100, 100),
  ('proj-beta', 'Beta', 'beta-key', ?, 'linked', 0, 101, 101);
INSERT INTO app_state (key, value) VALUES ('active_project_id', 'proj-beta');
`
	if _, err := db.Exec(legacySchema, alphaPath, betaPath); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("ListWorkspaces() len = %d, want 2", len(workspaces))
	}

	alphaWorkspace, err := s.GetWorkspaceByPath(alphaPath)
	if err != nil {
		t.Fatalf("GetWorkspaceByPath(alpha) error: %v", err)
	}
	if alphaWorkspace.ProjectID == nil || *alphaWorkspace.ProjectID != "proj-alpha" {
		t.Fatalf("alpha workspace project_id = %v, want proj-alpha", alphaWorkspace.ProjectID)
	}
	if alphaWorkspace.Name != "Alpha" {
		t.Fatalf("alpha workspace name = %q, want Alpha", alphaWorkspace.Name)
	}
	if alphaWorkspace.IsActive {
		t.Fatal("alpha workspace is_active = true, want false")
	}

	betaWorkspace, err := s.GetWorkspaceByPath(betaPath)
	if err != nil {
		t.Fatalf("GetWorkspaceByPath(beta) error: %v", err)
	}
	if betaWorkspace.ProjectID == nil || *betaWorkspace.ProjectID != "proj-beta" {
		t.Fatalf("beta workspace project_id = %v, want proj-beta", betaWorkspace.ProjectID)
	}
	if !betaWorkspace.IsActive {
		t.Fatal("beta workspace is_active = false, want true")
	}

	active, err := s.ActiveWorkspace()
	if err != nil {
		t.Fatalf("ActiveWorkspace() error: %v", err)
	}
	if active.ID != betaWorkspace.ID {
		t.Fatalf("ActiveWorkspace() = %d, want %d", active.ID, betaWorkspace.ID)
	}
}

func TestStoreMigrationReusesExistingWorkspaceForProjectPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tabura.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}

	projectPath := filepath.Join(t.TempDir(), "existing")
	legacySchema := `
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  project_key TEXT NOT NULL UNIQUE,
  root_path TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL DEFAULT 'managed',
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE workspaces (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  dir_path TEXT NOT NULL UNIQUE,
  project_id TEXT,
  sphere TEXT NOT NULL DEFAULT 'private',
  is_active INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO projects (id, name, project_key, root_path, kind, is_default, created_at, updated_at) VALUES
  ('proj-existing', '', 'existing-key', ?, 'managed', 0, 100, 100);
INSERT INTO app_state (key, value) VALUES ('active_project_id', 'proj-existing');
INSERT INTO workspaces (id, name, dir_path, project_id, sphere, is_active) VALUES
  (7, 'Existing Workspace', ?, NULL, 'work', 0);
`
	if _, err := db.Exec(legacySchema, projectPath, projectPath); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("ListWorkspaces() len = %d, want 1", len(workspaces))
	}

	workspace, err := s.GetWorkspaceByPath(projectPath)
	if err != nil {
		t.Fatalf("GetWorkspaceByPath() error: %v", err)
	}
	if workspace.ID != 7 {
		t.Fatalf("workspace ID = %d, want 7", workspace.ID)
	}
	if workspace.ProjectID == nil || *workspace.ProjectID != "proj-existing" {
		t.Fatalf("workspace project_id = %v, want proj-existing", workspace.ProjectID)
	}
	if workspace.Name != "Existing Workspace" {
		t.Fatalf("workspace name = %q, want Existing Workspace", workspace.Name)
	}
	if !workspace.IsActive {
		t.Fatal("workspace is_active = false, want true")
	}
}

func TestStoreMigrationToleratesProjectsWithoutWorkspaceFields(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tabura.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}

	legacySchema := `
CREATE TABLE projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL
);
INSERT INTO projects (id, name) VALUES ('proj-legacy', 'Legacy Project');
`
	if _, err := db.Exec(legacySchema); err != nil {
		_ = db.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	project, err := s.GetProject("proj-legacy")
	if err != nil {
		t.Fatalf("GetProject() error: %v", err)
	}
	if project.ProjectKey != "proj-legacy" {
		t.Fatalf("project_key = %q, want %q", project.ProjectKey, "proj-legacy")
	}
	if project.RootPath != "" {
		t.Fatalf("root_path = %q, want empty", project.RootPath)
	}

	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("ListWorkspaces() len = %d, want 0", len(workspaces))
	}
}
