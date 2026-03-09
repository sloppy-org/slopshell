package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/krystophny/tabura/internal/store"

	_ "modernc.org/sqlite"
)

func TestParseInlineZoteroIntent(t *testing.T) {
	action := parseInlineZoteroIntent("sync zotero")
	if action == nil {
		t.Fatal("expected zotero sync action")
	}
	if action.Action != "sync_zotero" {
		t.Fatalf("action = %q, want sync_zotero", action.Action)
	}
	if parseInlineZoteroIntent("sync bear") != nil {
		t.Fatal("did not expect zotero action for unrelated text")
	}
}

func TestClassifyAndExecuteSystemActionSyncZotero(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	dbPath := seedZoteroTestLibrary(t)
	account := createZoteroTestAccount(t, app, dbPath)
	workspace, err := app.store.CreateWorkspace("Research", filepath.Join(t.TempDir(), "research"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	targetProject, err := app.store.CreateProject("Tabura", "tabura", filepath.Join(t.TempDir(), "tabura"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderZotero, "collection", "Papers", &workspace.ID, nil, nil); err != nil {
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

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync zotero")
	if !handled {
		t.Fatal("expected sync zotero command to be handled")
	}
	if message != "Synced 1 Zotero reference(s), 1 PDF attachment(s), 1 annotation(s), and 1 reading item(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "sync_zotero" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if intFromAny(payloads[0]["reference_count"], 0) != 1 || intFromAny(payloads[0]["reading_items"], 0) != 1 {
		t.Fatalf("payload counts = %#v", payloads[0])
	}

	referenceBinding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderZotero, "reference", "ITEM-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(reference) error: %v", err)
	}
	if referenceBinding.ArtifactID == nil {
		t.Fatal("expected reference artifact binding")
	}
	referenceArtifact, err := app.store.GetArtifact(*referenceBinding.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact(reference) error: %v", err)
	}
	if referenceArtifact.Kind != store.ArtifactKindReference {
		t.Fatalf("reference artifact kind = %q, want %q", referenceArtifact.Kind, store.ArtifactKindReference)
	}
	var referenceMeta map[string]any
	if referenceArtifact.MetaJSON == nil || json.Unmarshal([]byte(*referenceArtifact.MetaJSON), &referenceMeta) != nil {
		t.Fatalf("reference meta_json = %v, want valid JSON", referenceArtifact.MetaJSON)
	}
	if got := strFromAny(referenceMeta["citation_key"]); got != "lovelace2026" {
		t.Fatalf("reference citation_key = %q, want lovelace2026", got)
	}
	if got := strFromAny(referenceMeta["journal"]); got != "Journal of Tests" {
		t.Fatalf("reference journal = %q, want Journal of Tests", got)
	}

	readingItem, err := app.store.GetItemBySource(store.ExternalProviderZotero, "reference:ITEM-1")
	if err != nil {
		t.Fatalf("GetItemBySource(reading) error: %v", err)
	}
	if readingItem.State != store.ItemStateInbox {
		t.Fatalf("reading item state = %q, want inbox", readingItem.State)
	}
	if readingItem.WorkspaceID == nil || *readingItem.WorkspaceID != workspace.ID {
		t.Fatalf("reading item workspace_id = %v, want %d", readingItem.WorkspaceID, workspace.ID)
	}
	if readingItem.ProjectID == nil || *readingItem.ProjectID != targetProject.ID {
		t.Fatalf("reading item project_id = %v, want %q", readingItem.ProjectID, targetProject.ID)
	}
	if readingItem.ArtifactID == nil || *readingItem.ArtifactID != referenceArtifact.ID {
		t.Fatalf("reading item artifact_id = %v, want %d", readingItem.ArtifactID, referenceArtifact.ID)
	}

	artifacts, err := app.store.ListItemArtifacts(readingItem.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts() error: %v", err)
	}
	if len(artifacts) != 3 {
		t.Fatalf("ListItemArtifacts() len = %d, want 3", len(artifacts))
	}

	attachmentBinding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderZotero, "attachment", "ATT-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(attachment) error: %v", err)
	}
	if attachmentBinding.ArtifactID == nil {
		t.Fatal("expected attachment artifact binding")
	}
	attachmentArtifact, err := app.store.GetArtifact(*attachmentBinding.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact(attachment) error: %v", err)
	}
	if attachmentArtifact.Kind != store.ArtifactKindPDF {
		t.Fatalf("attachment artifact kind = %q, want pdf", attachmentArtifact.Kind)
	}
	expectedURL := "file://" + filepath.ToSlash(filepath.Join(filepath.Dir(dbPath), "storage", "ATT-1", "paper.pdf"))
	if attachmentArtifact.RefURL == nil || *attachmentArtifact.RefURL != expectedURL {
		t.Fatalf("attachment ref_url = %v, want %q", attachmentArtifact.RefURL, expectedURL)
	}

	annotationBinding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderZotero, "annotation", "ANN-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(annotation) error: %v", err)
	}
	if annotationBinding.ArtifactID == nil {
		t.Fatal("expected annotation artifact binding")
	}
	annotationArtifact, err := app.store.GetArtifact(*annotationBinding.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact(annotation) error: %v", err)
	}
	if annotationArtifact.Kind != store.ArtifactKindAnnotation {
		t.Fatalf("annotation artifact kind = %q, want %q", annotationArtifact.Kind, store.ArtifactKindAnnotation)
	}
	var annotationMeta map[string]any
	if annotationArtifact.MetaJSON == nil || json.Unmarshal([]byte(*annotationArtifact.MetaJSON), &annotationMeta) != nil {
		t.Fatalf("annotation meta_json = %v, want valid JSON", annotationArtifact.MetaJSON)
	}
	if intFromAny(annotationMeta["reference_artifact_id"], 0) != int(referenceArtifact.ID) {
		t.Fatalf("annotation reference_artifact_id = %#v, want %d", annotationMeta["reference_artifact_id"], referenceArtifact.ID)
	}
}

func TestClassifyAndExecuteSystemActionSyncZoteroSkipsMissingDatabase(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	createZoteroTestAccount(t, app, filepath.Join(t.TempDir(), "missing.sqlite"))
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync zotero")
	if !handled {
		t.Fatal("expected sync zotero command to be handled")
	}
	if message != "Skipped Zotero sync because no Zotero database was found." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || intFromAny(payloads[0]["skipped_accounts"], 0) != 1 {
		t.Fatalf("payloads = %#v", payloads)
	}
}

func createZoteroTestAccount(t *testing.T, app *App, dbPath string) store.ExternalAccount {
	t.Helper()
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderZotero, "Research Library", map[string]any{
		"db_path": dbPath,
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	return account
}

func seedZoteroTestLibrary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "zotero.sqlite")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		t.Fatalf("sql.Open(): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	statements := []string{
		`CREATE TABLE itemTypes (itemTypeID INTEGER PRIMARY KEY, typeName TEXT NOT NULL);`,
		`CREATE TABLE items (itemID INTEGER PRIMARY KEY, itemTypeID INTEGER NOT NULL, key TEXT NOT NULL, dateAdded TEXT NOT NULL DEFAULT '', dateModified TEXT NOT NULL DEFAULT '');`,
		`CREATE TABLE deletedItems (itemID INTEGER PRIMARY KEY);`,
		`CREATE TABLE fields (fieldID INTEGER PRIMARY KEY, fieldName TEXT NOT NULL);`,
		`CREATE TABLE itemDataValues (valueID INTEGER PRIMARY KEY, value TEXT NOT NULL);`,
		`CREATE TABLE itemData (itemID INTEGER NOT NULL, fieldID INTEGER NOT NULL, valueID INTEGER NOT NULL);`,
		`CREATE TABLE creatorTypes (creatorTypeID INTEGER PRIMARY KEY, creatorType TEXT NOT NULL);`,
		`CREATE TABLE creatorData (creatorDataID INTEGER PRIMARY KEY, firstName TEXT NOT NULL DEFAULT '', lastName TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '');`,
		`CREATE TABLE creators (creatorID INTEGER PRIMARY KEY, creatorDataID INTEGER NOT NULL);`,
		`CREATE TABLE itemCreators (itemID INTEGER NOT NULL, creatorID INTEGER NOT NULL, creatorTypeID INTEGER NOT NULL, orderIndex INTEGER NOT NULL);`,
		`CREATE TABLE collections (collectionID INTEGER PRIMARY KEY, key TEXT NOT NULL, collectionName TEXT NOT NULL, parentCollectionID INTEGER);`,
		`CREATE TABLE collectionItems (collectionID INTEGER NOT NULL, itemID INTEGER NOT NULL);`,
		`CREATE TABLE tags (tagID INTEGER PRIMARY KEY, name TEXT NOT NULL);`,
		`CREATE TABLE itemTags (itemID INTEGER NOT NULL, tagID INTEGER NOT NULL, type INTEGER NOT NULL DEFAULT 0);`,
		`CREATE TABLE itemAttachments (itemID INTEGER PRIMARY KEY, parentItemID INTEGER, contentType TEXT NOT NULL DEFAULT '', path TEXT NOT NULL DEFAULT '', linkMode INTEGER NOT NULL DEFAULT 0);`,
		`CREATE TABLE itemAnnotations (itemID INTEGER PRIMARY KEY, parentItemID INTEGER, type TEXT NOT NULL DEFAULT '', authorName TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '', color TEXT NOT NULL DEFAULT '', pageLabel TEXT NOT NULL DEFAULT '', sortIndex TEXT NOT NULL DEFAULT '', position TEXT NOT NULL DEFAULT '');`,
		`INSERT INTO itemTypes (itemTypeID, typeName) VALUES (1, 'journalArticle'), (2, 'attachment'), (3, 'annotation');`,
		`INSERT INTO items (itemID, itemTypeID, key, dateAdded, dateModified) VALUES (1, 1, 'ITEM-1', '2026-03-08T08:00:00Z', '2026-03-09T09:00:00Z'), (2, 2, 'ATT-1', '2026-03-08T08:30:00Z', '2026-03-09T09:15:00Z'), (3, 3, 'ANN-1', '2026-03-08T08:45:00Z', '2026-03-09T09:20:00Z');`,
		`INSERT INTO fields (fieldID, fieldName) VALUES (1, 'title'), (2, 'DOI'), (3, 'abstractNote'), (4, 'date'), (5, 'publicationTitle');`,
		`INSERT INTO itemDataValues (valueID, value) VALUES (1, 'Pragmatic Testing'), (2, '10.1000/example'), (3, 'Short abstract.'), (4, '2026'), (5, 'Paper PDF'), (6, 'Journal of Tests');`,
		`INSERT INTO itemData (itemID, fieldID, valueID) VALUES (1, 1, 1), (1, 2, 2), (1, 3, 3), (1, 4, 4), (1, 5, 6), (2, 1, 5);`,
		`INSERT INTO creatorTypes (creatorTypeID, creatorType) VALUES (1, 'author');`,
		`INSERT INTO creatorData (creatorDataID, firstName, lastName, name) VALUES (1, 'Ada', 'Lovelace', '');`,
		`INSERT INTO creators (creatorID, creatorDataID) VALUES (1, 1);`,
		`INSERT INTO itemCreators (itemID, creatorID, creatorTypeID, orderIndex) VALUES (1, 1, 1, 0);`,
		`INSERT INTO collections (collectionID, key, collectionName, parentCollectionID) VALUES (1, 'COLL-1', 'Papers', NULL);`,
		`INSERT INTO collectionItems (collectionID, itemID) VALUES (1, 1);`,
		`INSERT INTO tags (tagID, name) VALUES (1, 'Tabura'), (2, 'unread');`,
		`INSERT INTO itemTags (itemID, tagID, type) VALUES (1, 1, 0), (1, 2, 0);`,
		`INSERT INTO itemAttachments (itemID, parentItemID, contentType, path, linkMode) VALUES (2, 1, 'application/pdf', 'storage:paper.pdf', 1);`,
		`INSERT INTO itemAnnotations (itemID, parentItemID, type, authorName, text, comment, color, pageLabel, sortIndex, position) VALUES (3, 2, 'highlight', 'Ada', 'Important result', 'Revisit this proof', '#ffd400', '4', '00001|00001|00001', '{\"pageIndex\":3}');`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("db.Exec(%q): %v", stmt, err)
		}
	}
	storagePath := filepath.Join(root, "storage", "ATT-1")
	if err := os.MkdirAll(storagePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(storage): %v", err)
	}
	if err := os.WriteFile(filepath.Join(storagePath, "paper.pdf"), []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("WriteFile(paper.pdf): %v", err)
	}
	exportPath := filepath.Join(root, "library.json")
	exportJSON := `{"items":[{"itemKey":"ITEM-1","citationKey":"lovelace2026","DOI":"10.1000/example","title":"Pragmatic Testing"}]}`
	if err := os.WriteFile(exportPath, []byte(exportJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(library.json): %v", err)
	}
	return dbPath
}
