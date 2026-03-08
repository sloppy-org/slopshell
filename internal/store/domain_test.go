package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStoreMigratesDomainTablesOnFreshDatabase(t *testing.T) {
	s := newTestStore(t)

	var foreignKeys int
	if err := s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("read PRAGMA foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", foreignKeys)
	}

	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	for table, want := range map[string][]string{
		"workspaces": {"id", "name", "dir_path", "is_active", "created_at", "updated_at"},
		"actors":     {"id", "name", "kind", "created_at"},
		"artifacts":  {"id", "kind", "ref_path", "ref_url", "title", "meta_json", "created_at", "updated_at"},
		"items":      {"id", "title", "state", "workspace_id", "artifact_id", "actor_id", "visible_after", "follow_up_at", "source", "source_ref", "created_at", "updated_at"},
	} {
		got := make(map[string]bool, len(columns[table]))
		for _, name := range columns[table] {
			got[name] = true
		}
		for _, name := range want {
			if !got[name] {
				t.Fatalf("table %s missing column %s: %#v", table, name, columns[table])
			}
		}
	}

	targets := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA foreign_key_list(items)`)
	if err != nil {
		t.Fatalf("PRAGMA foreign_key_list(items) error: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id, seq                                    int
			table, from, to, onUpdate, onDelete, match string
		)
		if err := rows.Scan(&id, &seq, &table, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			t.Fatalf("scan foreign key: %v", err)
		}
		targets[table] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign keys: %v", err)
	}
	for _, table := range []string{"workspaces", "artifacts", "actors"} {
		if !targets[table] {
			t.Fatalf("items missing foreign key to %s", table)
		}
	}
}

func TestStoreMigratesDomainTablesOnExistingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tabura.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
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
CREATE TABLE chat_messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  role TEXT NOT NULL,
  content_markdown TEXT NOT NULL DEFAULT '',
  content_plain TEXT NOT NULL DEFAULT '',
  render_format TEXT NOT NULL DEFAULT 'markdown',
  created_at INTEGER NOT NULL
);
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
		t.Fatalf("store.New() on legacy db error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	for _, table := range []string{"workspaces", "actors", "artifacts", "items"} {
		if _, ok := columns[table]; !ok {
			t.Fatalf("expected migrated table %s to exist", table)
		}
	}
}

func TestItemSchemaAllowsNilOptionalFields(t *testing.T) {
	s := newTestStore(t)

	res, err := s.db.Exec(`INSERT INTO items (title) VALUES ('triage me')`)
	if err != nil {
		t.Fatalf("insert item without optional fields: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId() error: %v", err)
	}

	var (
		title                                       string
		workspaceID, artifactID, actorID            sql.NullInt64
		visibleAfter, followUpAt, source, sourceRef sql.NullString
	)
	err = s.db.QueryRow(`
SELECT title, workspace_id, artifact_id, actor_id, visible_after, follow_up_at, source, source_ref
FROM items
WHERE id = ?
`, id).Scan(&title, &workspaceID, &artifactID, &actorID, &visibleAfter, &followUpAt, &source, &sourceRef)
	if err != nil {
		t.Fatalf("query item: %v", err)
	}
	if title != "triage me" {
		t.Fatalf("title = %q, want triage me", title)
	}
	if workspaceID.Valid || artifactID.Valid || actorID.Valid || visibleAfter.Valid || followUpAt.Valid || source.Valid || sourceRef.Valid {
		t.Fatalf("expected optional fields to remain NULL, got workspace=%v artifact=%v actor=%v visible_after=%v follow_up_at=%v source=%v source_ref=%v",
			workspaceID, artifactID, actorID, visibleAfter, followUpAt, source, sourceRef)
	}
}

func TestItemSchemaEnforcesForeignKeys(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.db.Exec(`INSERT INTO items (title, workspace_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing workspace")
	}
	if _, err := s.db.Exec(`INSERT INTO items (title, artifact_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing artifact")
	}
	if _, err := s.db.Exec(`INSERT INTO items (title, actor_id) VALUES ('invalid', 999)`); err == nil {
		t.Fatal("expected foreign key violation for missing actor")
	}
}

func TestDomainTypesExposeJSONTags(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{name: "Workspace", typ: reflect.TypeOf(Workspace{})},
		{name: "Actor", typ: reflect.TypeOf(Actor{})},
		{name: "Artifact", typ: reflect.TypeOf(Artifact{})},
		{name: "Item", typ: reflect.TypeOf(Item{})},
	} {
		for i := 0; i < tc.typ.NumField(); i++ {
			field := tc.typ.Field(i)
			if field.PkgPath != "" {
				continue
			}
			if tag := field.Tag.Get("json"); tag == "" || tag == "-" {
				t.Fatalf("%s.%s missing json tag", tc.name, field.Name)
			}
		}
	}
}

func TestDomainCRUDRoundTrip(t *testing.T) {
	s := newTestStore(t)

	workspaceAPath := filepath.Join(t.TempDir(), "workspace-a")
	workspaceBPath := filepath.Join(t.TempDir(), "workspace-b")

	workspaceA, err := s.CreateWorkspace("Workspace A", workspaceAPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(workspace-a) error: %v", err)
	}
	workspaceB, err := s.CreateWorkspace(" Workspace B ", workspaceBPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(workspace-b) error: %v", err)
	}
	gotByPath, err := s.GetWorkspaceByPath(workspaceBPath)
	if err != nil {
		t.Fatalf("GetWorkspaceByPath() error: %v", err)
	}
	if gotByPath.ID != workspaceB.ID {
		t.Fatalf("GetWorkspaceByPath() ID = %d, want %d", gotByPath.ID, workspaceB.ID)
	}
	if _, err := s.CreateWorkspace("Duplicate", workspaceAPath); err == nil {
		t.Fatal("expected duplicate workspace path error")
	}
	if err := s.SetActiveWorkspace(workspaceB.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("ListWorkspaces() len = %d, want 2", len(workspaces))
	}
	if !workspaces[0].IsActive || workspaces[0].ID != workspaceB.ID {
		t.Fatalf("ListWorkspaces() active workspace mismatch: %+v", workspaces)
	}
	workspaceA, err = s.GetWorkspace(workspaceA.ID)
	if err != nil {
		t.Fatalf("GetWorkspace(workspace-a) error: %v", err)
	}
	if workspaceA.IsActive {
		t.Fatal("expected inactive workspace after SetActiveWorkspace")
	}

	human, err := s.CreateActor("Alice", ActorKindHuman)
	if err != nil {
		t.Fatalf("CreateActor(Alice) error: %v", err)
	}
	agent, err := s.CreateActor("Codex", ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor(Codex) error: %v", err)
	}
	if _, err := s.CreateActor("Nobody", "robot"); err == nil {
		t.Fatal("expected invalid actor kind error")
	}
	actors, err := s.ListActors()
	if err != nil {
		t.Fatalf("ListActors() error: %v", err)
	}
	if len(actors) != 2 {
		t.Fatalf("ListActors() len = %d, want 2", len(actors))
	}
	if actors[0].Name != "Alice" || actors[1].Name != "Codex" {
		t.Fatalf("ListActors() names = %#v, want Alice/Codex", []string{actors[0].Name, actors[1].Name})
	}
	gotActor, err := s.GetActor(agent.ID)
	if err != nil {
		t.Fatalf("GetActor() error: %v", err)
	}
	if gotActor.Kind != ActorKindAgent {
		t.Fatalf("GetActor().Kind = %q, want %q", gotActor.Kind, ActorKindAgent)
	}

	refPath := filepath.Join(t.TempDir(), "artifact.md")
	refURL := "https://example.invalid/item/1"
	title := "Plan draft"
	metaJSON := `{"source":"unit"}`
	artifact, err := s.CreateArtifact(ArtifactKindMarkdown, &refPath, &refURL, &title, &metaJSON)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	gotArtifact, err := s.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if gotArtifact.Kind != ArtifactKindMarkdown || gotArtifact.Title == nil || *gotArtifact.Title != title {
		t.Fatalf("GetArtifact() = %+v", gotArtifact)
	}
	updatedTitle := "Plan draft v2"
	clearRefURL := ""
	updatedKind := ArtifactKindDocument
	if err := s.UpdateArtifact(artifact.ID, ArtifactUpdate{
		Kind:   &updatedKind,
		Title:  &updatedTitle,
		RefURL: &clearRefURL,
	}); err != nil {
		t.Fatalf("UpdateArtifact() error: %v", err)
	}
	gotArtifact, err = s.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(updated) error: %v", err)
	}
	if gotArtifact.Kind != ArtifactKindDocument {
		t.Fatalf("GetArtifact(updated).Kind = %q, want %q", gotArtifact.Kind, ArtifactKindDocument)
	}
	if gotArtifact.RefURL != nil {
		t.Fatalf("GetArtifact(updated).RefURL = %v, want nil", *gotArtifact.RefURL)
	}
	if gotArtifact.Title == nil || *gotArtifact.Title != updatedTitle {
		t.Fatalf("GetArtifact(updated).Title = %v, want %q", gotArtifact.Title, updatedTitle)
	}
	artifacts, err := s.ListArtifactsByKind(ArtifactKindDocument)
	if err != nil {
		t.Fatalf("ListArtifactsByKind() error: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].ID != artifact.ID {
		t.Fatalf("ListArtifactsByKind() = %+v, want artifact %d", artifacts, artifact.ID)
	}

	source := "github"
	sourceRef := "issue-174"
	visibleAfter := "2026-03-09T10:00:00Z"
	followUpAt := "2026-03-10T11:00:00Z"

	inboxItem, err := s.CreateItem("Inbox item", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(inbox) error: %v", err)
	}
	artifactItem, err := s.CreateItem("Artifact item", ItemOptions{
		ArtifactID: &artifact.ID,
		Source:     &source,
		SourceRef:  &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(artifact) error: %v", err)
	}
	workspaceItem, err := s.CreateItem("Workspace item", ItemOptions{
		WorkspaceID: &workspaceA.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem(workspace) error: %v", err)
	}
	assignedItem, err := s.CreateItem("Assigned item", ItemOptions{
		State:        ItemStateWaiting,
		WorkspaceID:  &workspaceB.ID,
		ArtifactID:   &artifact.ID,
		ActorID:      &human.ID,
		VisibleAfter: &visibleAfter,
		FollowUpAt:   &followUpAt,
	})
	if err != nil {
		t.Fatalf("CreateItem(assigned) error: %v", err)
	}
	if assignedItem.WorkspaceID == nil || *assignedItem.WorkspaceID != workspaceB.ID {
		t.Fatalf("CreateItem(assigned).WorkspaceID = %v, want %d", assignedItem.WorkspaceID, workspaceB.ID)
	}
	if assignedItem.ArtifactID == nil || *assignedItem.ArtifactID != artifact.ID {
		t.Fatalf("CreateItem(assigned).ArtifactID = %v, want %d", assignedItem.ArtifactID, artifact.ID)
	}
	if assignedItem.ActorID == nil || *assignedItem.ActorID != human.ID {
		t.Fatalf("CreateItem(assigned).ActorID = %v, want %d", assignedItem.ActorID, human.ID)
	}
	sourceCompleteRef := "issue-183"
	sourceItem, err := s.CreateItem("Source completion item", ItemOptions{
		State:     ItemStateWaiting,
		Source:    &source,
		SourceRef: &sourceCompleteRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(source completion) error: %v", err)
	}

	if err := s.AssignItem(artifactItem.ID, agent.ID); err != nil {
		t.Fatalf("AssignItem() error: %v", err)
	}
	gotItem, err := s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact assigned) error: %v", err)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != agent.ID {
		t.Fatalf("GetItem(artifact assigned).ActorID = %v, want %d", gotItem.ActorID, agent.ID)
	}
	if err := s.AssignItem(artifactItem.ID, 9999); err == nil {
		t.Fatal("expected assign to nonexistent actor error")
	}
	if err := s.AssignItem(artifactItem.ID, human.ID); err != nil {
		t.Fatalf("AssignItem(reassign) error: %v", err)
	}
	gotItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact reassigned) error: %v", err)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != human.ID {
		t.Fatalf("GetItem(artifact reassigned).ActorID = %v, want %d", gotItem.ActorID, human.ID)
	}
	if gotItem.State != ItemStateWaiting {
		t.Fatalf("GetItem(artifact reassigned).State = %q, want %q", gotItem.State, ItemStateWaiting)
	}
	if err := s.UnassignItem(artifactItem.ID); err != nil {
		t.Fatalf("UnassignItem() error: %v", err)
	}
	gotItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact unassigned) error: %v", err)
	}
	if gotItem.ActorID != nil {
		t.Fatalf("GetItem(artifact unassigned).ActorID = %v, want nil", gotItem.ActorID)
	}
	if gotItem.State != ItemStateInbox {
		t.Fatalf("GetItem(artifact unassigned).State = %q, want %q", gotItem.State, ItemStateInbox)
	}
	if err := s.UnassignItem(artifactItem.ID); err == nil {
		t.Fatal("expected unassign on unassigned item error")
	}
	if err := s.AssignItem(artifactItem.ID, agent.ID); err != nil {
		t.Fatalf("AssignItem(reassign to agent) error: %v", err)
	}
	if err := s.CompleteItemByActor(artifactItem.ID, human.ID); err == nil {
		t.Fatal("expected complete with wrong actor error")
	}
	if err := s.CompleteItemByActor(artifactItem.ID, agent.ID); err != nil {
		t.Fatalf("CompleteItemByActor() error: %v", err)
	}
	gotItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact completed) error: %v", err)
	}
	if gotItem.State != ItemStateDone {
		t.Fatalf("GetItem(artifact completed).State = %q, want %q", gotItem.State, ItemStateDone)
	}
	if err := s.CompleteItemByActor(artifactItem.ID, agent.ID); err == nil {
		t.Fatal("expected double complete error")
	}
	if err := s.AssignItem(artifactItem.ID, human.ID); err == nil {
		t.Fatal("expected assign on done item error")
	}
	if err := s.ReturnItemToInbox(assignedItem.ID); err != nil {
		t.Fatalf("ReturnItemToInbox() error: %v", err)
	}
	gotItem, err = s.GetItem(assignedItem.ID)
	if err != nil {
		t.Fatalf("GetItem(returned to inbox) error: %v", err)
	}
	if gotItem.State != ItemStateInbox {
		t.Fatalf("GetItem(returned to inbox).State = %q, want %q", gotItem.State, ItemStateInbox)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != human.ID {
		t.Fatalf("GetItem(returned to inbox).ActorID = %v, want %d", gotItem.ActorID, human.ID)
	}
	if err := s.ReturnItemToInbox(artifactItem.ID); err == nil {
		t.Fatal("expected return on done item error")
	}
	if err := s.CompleteItemBySource(source, sourceCompleteRef); err != nil {
		t.Fatalf("CompleteItemBySource() error: %v", err)
	}
	gotItem, err = s.GetItem(sourceItem.ID)
	if err != nil {
		t.Fatalf("GetItem(source completed) error: %v", err)
	}
	if gotItem.State != ItemStateDone {
		t.Fatalf("GetItem(source completed).State = %q, want %q", gotItem.State, ItemStateDone)
	}
	if err := s.CompleteItemBySource(source, sourceCompleteRef); err == nil {
		t.Fatal("expected double source complete error")
	}
	if err := s.CompleteItemBySource("github", "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("CompleteItemBySource(missing) error = %v, want sql.ErrNoRows", err)
	}

	if err := s.UpdateItemTimes(inboxItem.ID, &visibleAfter, &followUpAt); err != nil {
		t.Fatalf("UpdateItemTimes() error: %v", err)
	}
	gotItem, err = s.GetItem(inboxItem.ID)
	if err != nil {
		t.Fatalf("GetItem(updated times) error: %v", err)
	}
	if gotItem.VisibleAfter == nil || *gotItem.VisibleAfter != visibleAfter {
		t.Fatalf("VisibleAfter = %v, want %q", gotItem.VisibleAfter, visibleAfter)
	}
	if gotItem.FollowUpAt == nil || *gotItem.FollowUpAt != followUpAt {
		t.Fatalf("FollowUpAt = %v, want %q", gotItem.FollowUpAt, followUpAt)
	}

	if err := s.UpdateItemState(inboxItem.ID, ItemStateWaiting); err != nil {
		t.Fatalf("UpdateItemState(waiting) error: %v", err)
	}
	if err := s.UpdateItemState(inboxItem.ID, ItemStateDone); err != nil {
		t.Fatalf("UpdateItemState(done) error: %v", err)
	}
	if err := s.UpdateItemState(inboxItem.ID, ItemStateInbox); err == nil {
		t.Fatal("expected invalid done -> inbox transition error")
	}
	if err := s.UpdateItemState(inboxItem.ID, "paused"); err == nil {
		t.Fatal("expected invalid item state error")
	}

	waitingItems, err := s.ListItemsByState(ItemStateWaiting)
	if err != nil {
		t.Fatalf("ListItemsByState(waiting) error: %v", err)
	}
	if len(waitingItems) != 0 {
		t.Fatalf("ListItemsByState(waiting) len = %d, want 0", len(waitingItems))
	}
	doneItems, err := s.ListItemsByState(ItemStateDone)
	if err != nil {
		t.Fatalf("ListItemsByState(done) error: %v", err)
	}
	if len(doneItems) != 3 {
		t.Fatalf("ListItemsByState(done) len = %d, want 3", len(doneItems))
	}
	doneIDs := map[int64]bool{}
	for _, item := range doneItems {
		doneIDs[item.ID] = true
	}
	for _, id := range []int64{artifactItem.ID, sourceItem.ID, inboxItem.ID} {
		if !doneIDs[id] {
			t.Fatalf("ListItemsByState(done) missing item %d: %+v", id, doneItems)
		}
	}
	if _, err := s.ListItemsByState("paused"); err == nil {
		t.Fatal("expected invalid ListItemsByState error")
	}

	if err := s.DeleteWorkspace(workspaceA.ID); err != nil {
		t.Fatalf("DeleteWorkspace() error: %v", err)
	}
	workspaceItem, err = s.GetItem(workspaceItem.ID)
	if err != nil {
		t.Fatalf("GetItem(workspace item after workspace delete) error: %v", err)
	}
	if workspaceItem.WorkspaceID != nil {
		t.Fatalf("workspace item WorkspaceID = %v, want nil", *workspaceItem.WorkspaceID)
	}
	if err := s.DeleteArtifact(artifact.ID); err != nil {
		t.Fatalf("DeleteArtifact() error: %v", err)
	}
	artifactItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact item after artifact delete) error: %v", err)
	}
	if artifactItem.ArtifactID != nil {
		t.Fatalf("artifact item ArtifactID = %v, want nil", *artifactItem.ArtifactID)
	}
	if err := s.DeleteActor(agent.ID); err != nil {
		t.Fatalf("DeleteActor() error: %v", err)
	}
	artifactItem, err = s.GetItem(artifactItem.ID)
	if err != nil {
		t.Fatalf("GetItem(artifact item after actor delete) error: %v", err)
	}
	if artifactItem.ActorID != nil {
		t.Fatalf("artifact item ActorID = %v, want nil", *artifactItem.ActorID)
	}

	if err := s.DeleteItem(assignedItem.ID); err != nil {
		t.Fatalf("DeleteItem() error: %v", err)
	}
	if _, err := s.GetItem(assignedItem.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetItem(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestDomainConcurrentWorkspaceCreates(t *testing.T) {
	s := newTestStore(t)

	const count = 12
	baseDir := t.TempDir()
	errCh := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateWorkspace(
				"Workspace",
				filepath.Join(baseDir, fmt.Sprintf("workspace-%02d", i)),
			)
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("CreateWorkspace() concurrent error: %v", err)
		}
	}
	workspaces, err := s.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces() error: %v", err)
	}
	if len(workspaces) != count {
		t.Fatalf("ListWorkspaces() len = %d, want %d", len(workspaces), count)
	}
}

func TestResurfaceDueItems(t *testing.T) {
	s := newTestStore(t)

	now := time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)
	past := now.Add(-30 * time.Minute).Format(time.RFC3339)
	future := now.Add(30 * time.Minute).Format(time.RFC3339)

	pastVisible, err := s.CreateItem("past visible_after", ItemOptions{
		State:        ItemStateWaiting,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(past visible_after) error: %v", err)
	}
	pastFollowUp, err := s.CreateItem("past follow_up_at", ItemOptions{
		State:      ItemStateWaiting,
		FollowUpAt: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(past follow_up_at) error: %v", err)
	}
	futureWaiting, err := s.CreateItem("future waiting", ItemOptions{
		State:        ItemStateWaiting,
		VisibleAfter: &future,
		FollowUpAt:   &future,
	})
	if err != nil {
		t.Fatalf("CreateItem(future waiting) error: %v", err)
	}
	bothTimes, err := s.CreateItem("both timestamps", ItemOptions{
		State:        ItemStateWaiting,
		VisibleAfter: &future,
		FollowUpAt:   &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(both timestamps) error: %v", err)
	}
	inboxItem, err := s.CreateItem("already inbox", ItemOptions{
		State:        ItemStateInbox,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(already inbox) error: %v", err)
	}
	doneItem, err := s.CreateItem("already done", ItemOptions{
		State:        ItemStateDone,
		VisibleAfter: &past,
	})
	if err != nil {
		t.Fatalf("CreateItem(already done) error: %v", err)
	}

	count, err := s.ResurfaceDueItems(now)
	if err != nil {
		t.Fatalf("ResurfaceDueItems() error: %v", err)
	}
	if count != 3 {
		t.Fatalf("ResurfaceDueItems() count = %d, want 3", count)
	}

	for _, tc := range []struct {
		name string
		id   int64
		want string
	}{
		{name: "past visible_after", id: pastVisible.ID, want: ItemStateInbox},
		{name: "past follow_up_at", id: pastFollowUp.ID, want: ItemStateInbox},
		{name: "both timestamps", id: bothTimes.ID, want: ItemStateInbox},
		{name: "future waiting", id: futureWaiting.ID, want: ItemStateWaiting},
		{name: "already inbox", id: inboxItem.ID, want: ItemStateInbox},
		{name: "already done", id: doneItem.ID, want: ItemStateDone},
	} {
		item, err := s.GetItem(tc.id)
		if err != nil {
			t.Fatalf("GetItem(%s) error: %v", tc.name, err)
		}
		if item.State != tc.want {
			t.Fatalf("%s state = %q, want %q", tc.name, item.State, tc.want)
		}
	}
}
