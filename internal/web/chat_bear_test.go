package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/krystophny/tabura/internal/bear"
	"github.com/krystophny/tabura/internal/store"

	_ "modernc.org/sqlite"
)

func TestBearNoteRefURL(t *testing.T) {
	noteURL := bearNoteRefURL(bear.Note{ID: "note id/1"})
	if noteURL == nil {
		t.Fatal("bearNoteRefURL() = nil, want URL")
	}
	if *noteURL != "bear://x-callback-url/open-note?id=note+id%2F1" {
		t.Fatalf("bearNoteRefURL() = %q", *noteURL)
	}
	if bearNoteRefURL(bear.Note{}) != nil {
		t.Fatal("bearNoteRefURL() for empty id should be nil")
	}
}

func TestParseInlineBearIntent(t *testing.T) {
	action := parseInlineBearIntent("sync bear")
	if action == nil {
		t.Fatal("expected bear sync action")
	}
	if action.Action != "sync_bear" {
		t.Fatalf("action = %q, want sync_bear", action.Action)
	}
	checklist := parseInlineBearIntent("create items from this Bear note's checklist")
	if checklist == nil || checklist.Action != "promote_bear_checklist" {
		t.Fatalf("checklist action = %#v, want promote_bear_checklist", checklist)
	}
	if parseInlineBearIntent("sync evernote") != nil {
		t.Fatal("did not expect bear action for unrelated text")
	}
}

func TestClassifyAndExecuteSystemActionSyncBear(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	dbPath := seedBearTestDatabase(t, []bearSeedNote{{
		ID:       "note-1",
		Title:    "Reading queue",
		Markdown: "#Tabura\n- [ ] Review intro\n- [x] Fix refs",
		Created:  788918400,
		Modified: 788922000,
	}})
	account := createBearTestAccount(t, app, dbPath)
	workspace, err := app.store.CreateWorkspace("Research", filepath.Join(t.TempDir(), "research"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	targetProject, err := app.store.CreateProject("Tabura", "tabura", filepath.Join(t.TempDir(), "tabura"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderBear, "tag", "Tabura", &workspace.ID, &targetProject.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping() error: %v", err)
	}
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync bear")
	if !handled {
		t.Fatal("expected sync bear command to be handled")
	}
	if message != "Synced 1 Bear note(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "sync_bear" {
		t.Fatalf("payloads = %#v", payloads)
	}

	artifactBindings, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderBear, "note", "note-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(note) error: %v", err)
	}
	if artifactBindings.ArtifactID == nil {
		t.Fatal("expected bear note artifact binding")
	}
	artifact, err := app.store.GetArtifact(*artifactBindings.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if artifact.Kind != store.ArtifactKindMarkdown {
		t.Fatalf("artifact.Kind = %q, want markdown", artifact.Kind)
	}
	if artifact.RefURL == nil || *artifact.RefURL != "bear://x-callback-url/open-note?id=note-1" {
		t.Fatalf("artifact.RefURL = %v, want Bear open-note callback", artifact.RefURL)
	}
	var meta bearNoteMeta
	if artifact.MetaJSON == nil || json.Unmarshal([]byte(*artifact.MetaJSON), &meta) != nil {
		t.Fatalf("artifact.MetaJSON = %v, want valid bear note meta", artifact.MetaJSON)
	}
	if meta.NoteID != "note-1" {
		t.Fatalf("meta.NoteID = %q, want note-1", meta.NoteID)
	}
	if meta.Created != "2026-01-01T00:00:00Z" || meta.Modified != "2026-01-01T01:00:00Z" {
		t.Fatalf("meta timestamps = %#v", meta)
	}
	if len(meta.Tags) != 1 || meta.Tags[0] != "Tabura" {
		t.Fatalf("meta.Tags = %#v, want [Tabura]", meta.Tags)
	}
	linked, err := app.store.ListLinkedArtifacts(workspace.ID)
	if err != nil {
		t.Fatalf("ListLinkedArtifacts() error: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != artifact.ID {
		t.Fatalf("linked artifacts = %#v, want artifact %d", linked, artifact.ID)
	}
	items, err := app.store.ListItems()
	if err != nil {
		t.Fatalf("ListItems() error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("sync bear should not create checklist items automatically, got %d", len(items))
	}
}

func TestClassifyAndExecuteSystemActionSyncBearSkipsMissingDatabase(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	createBearTestAccount(t, app, filepath.Join(t.TempDir(), "missing.sqlite"))
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync bear")
	if !handled {
		t.Fatal("expected sync bear command to be handled")
	}
	if message != "Skipped Bear sync because no Bear database was found." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || intFromAny(payloads[0]["skipped_accounts"], 0) != 1 {
		t.Fatalf("payloads = %#v", payloads)
	}
}

func TestClassifyAndExecuteSystemActionPromoteBearChecklist(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	dbPath := seedBearTestDatabase(t, []bearSeedNote{{
		ID:       "note-1",
		Title:    "Reading queue",
		Markdown: "#Tabura\n- [ ] Review intro\n- [x] Fix refs",
		Created:  788918400,
		Modified: 788922000,
	}})
	account := createBearTestAccount(t, app, dbPath)
	workspace, err := app.store.CreateWorkspace("Research", filepath.Join(t.TempDir(), "research"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	targetProject, err := app.store.CreateProject("Tabura", "tabura", filepath.Join(t.TempDir(), "tabura"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderBear, "tag", "Tabura", &workspace.ID, &targetProject.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping() error: %v", err)
	}
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	if _, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync bear"); !handled {
		t.Fatal("expected sync bear command to be handled")
	}

	binding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderBear, "note", "note-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(note) error: %v", err)
	}
	artifact, err := app.store.GetArtifact(*binding.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: ideaNoteString(artifact.Title),
		artifactKind:  "text_artifact",
		artifactText:  "#Tabura\n- [ ] Review intro\n- [x] Fix refs",
	}
	server := mock.setupServer(t)
	t.Cleanup(server.Close)
	port := serverPort(t, server.Listener.Addr())
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create items from this Bear note's checklist")
	if !handled {
		t.Fatal("expected bear checklist command to be handled")
	}
	if message != "Created 2 item(s) from the Bear note checklist." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "bear_checklist_promoted" {
		t.Fatalf("payloads = %#v", payloads)
	}

	firstItem, err := app.store.GetItemBySource(store.ExternalProviderBear, "note:note-1:task:1")
	if err != nil {
		t.Fatalf("GetItemBySource(task1) error: %v", err)
	}
	if firstItem.State != store.ItemStateInbox {
		t.Fatalf("first item state = %q, want inbox", firstItem.State)
	}
	if firstItem.WorkspaceID == nil || *firstItem.WorkspaceID != workspace.ID {
		t.Fatalf("first item workspace_id = %v, want %d", firstItem.WorkspaceID, workspace.ID)
	}
	if firstItem.ProjectID == nil || *firstItem.ProjectID != targetProject.ID {
		t.Fatalf("first item project_id = %v, want %q", firstItem.ProjectID, targetProject.ID)
	}
	if firstItem.ArtifactID == nil || *firstItem.ArtifactID != artifact.ID {
		t.Fatalf("first item artifact_id = %v, want %d", firstItem.ArtifactID, artifact.ID)
	}

	secondItem, err := app.store.GetItemBySource(store.ExternalProviderBear, "note:note-1:task:2")
	if err != nil {
		t.Fatalf("GetItemBySource(task2) error: %v", err)
	}
	if secondItem.State != store.ItemStateDone {
		t.Fatalf("second item state = %q, want done", secondItem.State)
	}
	taskBinding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderBear, "task", "note:note-1:task:1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(task) error: %v", err)
	}
	if taskBinding.ItemID == nil || *taskBinding.ItemID != firstItem.ID {
		t.Fatalf("task binding item_id = %v, want %d", taskBinding.ItemID, firstItem.ID)
	}
}

type bearSeedNote struct {
	ID       string
	Title    string
	Markdown string
	Created  float64
	Modified float64
}

func seedBearTestDatabase(t *testing.T, notes []bearSeedNote) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "bear.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if _, err := db.Exec(`
CREATE TABLE ZSFNOTE (
  Z_PK INTEGER PRIMARY KEY,
  ZUNIQUEIDENTIFIER TEXT,
  ZTITLE TEXT,
  ZTEXT TEXT,
  ZCREATIONDATE REAL,
  ZMODIFICATIONDATE REAL,
  ZTRASHED INTEGER DEFAULT 0,
  ZARCHIVED INTEGER DEFAULT 0
);`); err != nil {
		t.Fatalf("create bear schema: %v", err)
	}
	for _, note := range notes {
		if _, err := db.Exec(`
INSERT INTO ZSFNOTE (ZUNIQUEIDENTIFIER, ZTITLE, ZTEXT, ZCREATIONDATE, ZMODIFICATIONDATE, ZTRASHED, ZARCHIVED)
VALUES (?, ?, ?, ?, ?, 0, 0)`,
			note.ID,
			note.Title,
			note.Markdown,
			note.Created,
			note.Modified,
		); err != nil {
			t.Fatalf("insert bear note %s: %v", note.ID, err)
		}
	}
	return dbPath
}

func createBearTestAccount(t *testing.T, app *App, dbPath string) store.ExternalAccount {
	t.Helper()
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderBear, "Reading Notes", map[string]any{
		"db_path": dbPath,
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	return account
}
