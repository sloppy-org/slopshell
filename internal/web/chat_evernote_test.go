package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineEvernoteIntent(t *testing.T) {
	action := parseInlineEvernoteIntent("sync evernote")
	if action == nil {
		t.Fatal("expected evernote action")
	}
	if action.Action != "sync_evernote" {
		t.Fatalf("action = %q, want sync_evernote", action.Action)
	}
	if parseInlineEvernoteIntent("sync todoist") != nil {
		t.Fatal("did not expect evernote action for unrelated text")
	}
}

func TestClassifyAndExecuteSystemActionSyncEvernote(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/notebooks":
			writeEvernoteJSON(t, w, map[string]any{
				"notebooks": []map[string]any{{
					"id":   "nb-1",
					"name": "Research",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/notes":
			if got := r.URL.Query().Get("updated_after"); got != "" {
				t.Fatalf("updated_after = %q, want empty on first sync", got)
			}
			writeEvernoteJSON(t, w, map[string]any{
				"notes": []map[string]any{{
					"id":          "note-1",
					"notebook_id": "nb-1",
					"title":       "Reading queue",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/notes/note-1":
			writeEvernoteJSON(t, w, map[string]any{
				"note": map[string]any{
					"id":          "note-1",
					"notebook_id": "nb-1",
					"title":       "Reading queue",
					"created_at":  "2026-03-08T08:00:00Z",
					"updated_at":  "2026-03-09T09:30:00Z",
					"tag_names":   []string{"Tabura"},
					"url":         "https://evernote.test/note-1",
					"content_enml": `<en-note>` +
						`<div><en-todo/>Review section 2</div>` +
						`<div><en-todo checked="true"/>Check bibliography</div>` +
						`</en-note>`,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	account := createEvernoteTestAccount(t, app, "Research Notes", server.URL)
	workspace, err := app.store.CreateWorkspace("Research", filepath.Join(t.TempDir(), "research"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	targetProject, err := app.store.CreateProject("Tabura", "tabura", filepath.Join(t.TempDir(), "tabura"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderEvernote, "notebook", "Research", &workspace.ID, nil, nil); err != nil {
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

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync evernote")
	if !handled {
		t.Fatal("expected sync command to be handled")
	}
	if message != "Synced 1 Evernote note(s) and 2 task item(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "sync_evernote" {
		t.Fatalf("payloads = %#v", payloads)
	}

	firstItem, err := app.store.GetItemBySource(store.ExternalProviderEvernote, "note:note-1:task:1")
	if err != nil {
		t.Fatalf("GetItemBySource(task1) error: %v", err)
	}
	if firstItem.WorkspaceID == nil || *firstItem.WorkspaceID != workspace.ID {
		t.Fatalf("first item workspace_id = %v, want %d", firstItem.WorkspaceID, workspace.ID)
	}
	if firstItem.ProjectID == nil || *firstItem.ProjectID != targetProject.ID {
		t.Fatalf("first item project_id = %v, want %q", firstItem.ProjectID, targetProject.ID)
	}
	if firstItem.State != store.ItemStateInbox {
		t.Fatalf("first item state = %q, want inbox", firstItem.State)
	}
	if firstItem.ArtifactID == nil {
		t.Fatal("expected first item artifact")
	}
	secondItem, err := app.store.GetItemBySource(store.ExternalProviderEvernote, "note:note-1:task:2")
	if err != nil {
		t.Fatalf("GetItemBySource(task2) error: %v", err)
	}
	if secondItem.State != store.ItemStateDone {
		t.Fatalf("second item state = %q, want done", secondItem.State)
	}
	artifact, err := app.store.GetArtifact(*firstItem.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if artifact.Kind != store.ArtifactKindExternalNote {
		t.Fatalf("artifact kind = %q, want %q", artifact.Kind, store.ArtifactKindExternalNote)
	}
	if artifact.RefURL == nil || *artifact.RefURL != "https://evernote.test/note-1" {
		t.Fatalf("artifact ref_url = %v, want note URL", artifact.RefURL)
	}
	var meta map[string]any
	if artifact.MetaJSON == nil || json.Unmarshal([]byte(*artifact.MetaJSON), &meta) != nil {
		t.Fatalf("artifact meta_json = %v, want valid JSON", artifact.MetaJSON)
	}
	if got := strFromAny(meta["notebook"]); got != "Research" {
		t.Fatalf("artifact notebook = %q, want Research", got)
	}
	linked, err := app.store.ListLinkedArtifacts(workspace.ID)
	if err != nil {
		t.Fatalf("ListLinkedArtifacts() error: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != artifact.ID {
		t.Fatalf("linked artifacts = %#v, want artifact %d", linked, artifact.ID)
	}
	binding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderEvernote, "note", "note-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(note) error: %v", err)
	}
	if binding.ArtifactID == nil || *binding.ArtifactID != artifact.ID {
		t.Fatalf("note binding artifact_id = %v, want %d", binding.ArtifactID, artifact.ID)
	}
	taskBinding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderEvernote, "task", "note:note-1:task:1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(task) error: %v", err)
	}
	if taskBinding.ItemID == nil || *taskBinding.ItemID != firstItem.ID {
		t.Fatalf("task binding item_id = %v, want %d", taskBinding.ItemID, firstItem.ID)
	}
}

func TestClassifyAndExecuteSystemActionSyncEvernoteLinksNoteArtifactsWithoutTasks(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/notebooks":
			writeEvernoteJSON(t, w, map[string]any{
				"notebooks": []map[string]any{{
					"id":   "nb-1",
					"name": "Research",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/notes":
			writeEvernoteJSON(t, w, map[string]any{
				"notes": []map[string]any{{
					"id":          "note-1",
					"notebook_id": "nb-1",
					"title":       "Reading queue",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/notes/note-1":
			writeEvernoteJSON(t, w, map[string]any{
				"note": map[string]any{
					"id":               "note-1",
					"notebook_id":      "nb-1",
					"title":            "Reading queue",
					"updated_at":       "2026-03-09T09:30:00Z",
					"content_text":     "Reference notes only",
					"content_markdown": "Reference notes only",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	account := createEvernoteTestAccount(t, app, "Research Notes", server.URL)
	workspace, err := app.store.CreateWorkspace("Research", filepath.Join(t.TempDir(), "research"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderEvernote, "notebook", "Research", &workspace.ID, nil, nil); err != nil {
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

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync evernote")
	if !handled {
		t.Fatal("expected sync command to be handled")
	}
	if message != "Synced 1 Evernote note(s) and 0 task item(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "sync_evernote" {
		t.Fatalf("payloads = %#v", payloads)
	}

	binding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderEvernote, "note", "note-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote(note) error: %v", err)
	}
	if binding.ArtifactID == nil {
		t.Fatal("expected note artifact binding")
	}
	linked, err := app.store.ListLinkedArtifacts(workspace.ID)
	if err != nil {
		t.Fatalf("ListLinkedArtifacts() error: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != *binding.ArtifactID {
		t.Fatalf("linked artifacts = %#v, want artifact %d", linked, *binding.ArtifactID)
	}
	items, err := app.store.ListItems()
	if err != nil {
		t.Fatalf("ListItems() error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("sync evernote without tasks should not create items, got %d", len(items))
	}
}

func TestClassifyAndExecuteSystemActionSyncEvernoteUsesUpdatedAfterAndRemapsExistingItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	var updatedAfterSeen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/notebooks":
			writeEvernoteJSON(t, w, map[string]any{
				"notebooks": []map[string]any{{
					"id":   "nb-1",
					"name": "Research",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/notes":
			updatedAfterSeen = r.URL.Query().Get("updated_after")
			writeEvernoteJSON(t, w, map[string]any{
				"notes": []map[string]any{{
					"id":          "note-1",
					"notebook_id": "nb-1",
					"title":       "Reading queue",
				}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/notes/note-1":
			writeEvernoteJSON(t, w, map[string]any{
				"note": map[string]any{
					"id":          "note-1",
					"notebook_id": "nb-1",
					"title":       "Reading queue updated",
					"updated_at":  "2026-03-09T10:30:00Z",
					"tag_names":   []string{"Tabura"},
					"content_enml": `<en-note>` +
						`<div><en-todo checked="true"/>Review section 2</div>` +
						`</en-note>`,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	account := createEvernoteTestAccount(t, app, "Research Notes", server.URL)
	source := store.ExternalProviderEvernote
	artifactTitle := "Old note"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindExternalNote, nil, nil, &artifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Old task", store.ItemOptions{
		Source:     &source,
		SourceRef:  optionalStringPointer("note:note-1:task:1"),
		ArtifactID: &artifact.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	remoteUpdatedAt := "2026-03-09T09:30:00Z"
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        store.ExternalProviderEvernote,
		ObjectType:      "note",
		RemoteID:        "note-1",
		ArtifactID:      &artifact.ID,
		RemoteUpdatedAt: &remoteUpdatedAt,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding(note) error: %v", err)
	}
	if _, err := app.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        store.ExternalProviderEvernote,
		ObjectType:      "task",
		RemoteID:        "note:note-1:task:1",
		ItemID:          &item.ID,
		ArtifactID:      &artifact.ID,
		RemoteUpdatedAt: &remoteUpdatedAt,
	}); err != nil {
		t.Fatalf("UpsertExternalBinding(task) error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Research", filepath.Join(t.TempDir(), "research"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderEvernote, "notebook", "Research", &workspace.ID, nil, nil); err != nil {
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

	message, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync evernote")
	if !handled {
		t.Fatal("expected sync command to be handled")
	}
	if message != "Synced 1 Evernote note(s) and 1 task item(s)." {
		t.Fatalf("message = %q", message)
	}
	if updatedAfterSeen != remoteUpdatedAt {
		t.Fatalf("updated_after = %q, want %q", updatedAfterSeen, remoteUpdatedAt)
	}
	updatedItem, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(updated) error: %v", err)
	}
	if updatedItem.Title != "Review section 2" {
		t.Fatalf("updated title = %q, want Review section 2", updatedItem.Title)
	}
	if updatedItem.State != store.ItemStateDone {
		t.Fatalf("updated state = %q, want done", updatedItem.State)
	}
	if updatedItem.WorkspaceID == nil || *updatedItem.WorkspaceID != workspace.ID {
		t.Fatalf("updated workspace_id = %v, want %d", updatedItem.WorkspaceID, workspace.ID)
	}
	updatedArtifact, err := app.store.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(updated) error: %v", err)
	}
	if updatedArtifact.Title == nil || *updatedArtifact.Title != "Reading queue updated" {
		t.Fatalf("updated artifact title = %v, want Reading queue updated", updatedArtifact.Title)
	}
}

func createEvernoteTestAccount(t *testing.T, app *App, label, baseURL string) store.ExternalAccount {
	t.Helper()
	t.Setenv("TABURA_EVERNOTE_TOKEN_RESEARCH_NOTES", "token-1")
	account, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderEvernote, label, map[string]any{
		"base_url": baseURL,
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	return account
}

func writeEvernoteJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode evernote payload: %v", err)
	}
}
