package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func contextIDByNameForTest(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	var contextID int64
	if err := s.db.QueryRow(`SELECT id FROM contexts WHERE lower(name) = lower(?)`, name).Scan(&contextID); err != nil {
		t.Fatalf("context lookup %q: %v", name, err)
	}
	return contextID
}

func assertContextLinkCount(t *testing.T, s *Store, table string, contextID int64, want int) {
	t.Helper()
	var count int
	query := "SELECT COUNT(*) FROM " + table + " WHERE context_id = ?"
	if err := s.db.QueryRow(query, contextID).Scan(&count); err != nil {
		t.Fatalf("%s count: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s count = %d, want %d", table, count, want)
	}
}

func TestStoreMigratesLegacyProjectsIntoWorkspacesAndContexts(t *testing.T) {
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
  mcp_url TEXT NOT NULL DEFAULT '',
  canvas_session_id TEXT NOT NULL DEFAULT '',
  chat_model TEXT NOT NULL DEFAULT '',
  chat_model_reasoning_effort TEXT NOT NULL DEFAULT '',
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE TABLE app_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
INSERT INTO projects (id, name, project_key, root_path, kind, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, is_default, created_at, updated_at) VALUES
  ('proj-alpha', 'Alpha', 'alpha-key', ?, 'managed', 'http://127.0.0.1:9001', 'canvas-alpha', 'spark', 'medium', 1, 100, 100),
  ('proj-beta', 'Beta', 'beta-key', ?, 'linked', 'http://127.0.0.1:9002', 'canvas-beta', 'qwen3.5-9b', 'high', 0, 101, 101);
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
	if alphaWorkspace.ChatModel != "spark" {
		t.Fatalf("alpha workspace chat_model = %q, want spark", alphaWorkspace.ChatModel)
	}
	if alphaWorkspace.CanvasSessionID != "canvas-alpha" {
		t.Fatalf("alpha workspace canvas_session_id = %q, want canvas-alpha", alphaWorkspace.CanvasSessionID)
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
	if betaWorkspace.ChatModelReasoningEffort != "high" {
		t.Fatalf("beta workspace chat_model_reasoning_effort = %q, want high", betaWorkspace.ChatModelReasoningEffort)
	}
	if betaWorkspace.MCPURL != "http://127.0.0.1:9002" {
		t.Fatalf("beta workspace mcp_url = %q, want copied project config", betaWorkspace.MCPURL)
	}
	if !betaWorkspace.IsActive {
		t.Fatal("beta workspace is_active = false, want true")
	}

	alphaContextID := contextIDByNameForTest(t, s, "Alpha")
	betaContextID := contextIDByNameForTest(t, s, "Beta")
	assertContextLinkCount(t, s, "context_workspaces", alphaContextID, 1)
	assertContextLinkCount(t, s, "context_workspaces", betaContextID, 1)
}

func TestStoreMigrationReusesExistingWorkspaceAndSeedsContexts(t *testing.T) {
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
  mcp_url TEXT NOT NULL DEFAULT '',
  canvas_session_id TEXT NOT NULL DEFAULT '',
  chat_model TEXT NOT NULL DEFAULT '',
  chat_model_reasoning_effort TEXT NOT NULL DEFAULT '',
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
INSERT INTO projects (id, name, project_key, root_path, kind, mcp_url, canvas_session_id, chat_model, chat_model_reasoning_effort, is_default, created_at, updated_at) VALUES
  ('proj-existing', 'Existing', 'existing-key', ?, 'managed', 'http://127.0.0.1:9003', 'canvas-existing', 'spark', 'low', 0, 100, 100);
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
	if workspace.CanvasSessionID != "canvas-existing" {
		t.Fatalf("workspace canvas_session_id = %q, want copied project value", workspace.CanvasSessionID)
	}
	if !workspace.IsActive {
		t.Fatal("workspace is_active = false, want true")
	}

	contextID := contextIDByNameForTest(t, s, "Existing")
	assertContextLinkCount(t, s, "context_workspaces", contextID, 1)
}

func TestStoreMigrationCreatesContextForProjectWithoutWorkspacePath(t *testing.T) {
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
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != 0 {
		t.Fatalf("ListWorkspaces() len = %d, want 0", len(workspaces))
	}

	contextID := contextIDByNameForTest(t, s, "Legacy Project")
	if contextID <= 0 {
		t.Fatalf("context ID = %d, want positive", contextID)
	}
}
