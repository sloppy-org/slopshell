package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
	"github.com/krystophny/tabura/internal/todoist"
)

func TestParseInlineTodoistIntent(t *testing.T) {
	cases := []struct {
		text           string
		wantAction     string
		wantProject    string
		wantWorkspace  string
		wantTargetProj string
		wantTaskText   string
	}{
		{text: "sync todoist", wantAction: "sync_todoist"},
		{text: "map todoist project Admin to workspace ~/admin", wantAction: "map_todoist_project", wantProject: "Admin", wantWorkspace: "~/admin"},
		{text: "map todoist project Admin to project Tabura", wantAction: "map_todoist_project", wantProject: "Admin", wantTargetProj: "Tabura"},
		{text: "create todoist task: review proposal by Friday", wantAction: "create_todoist_task", wantTaskText: "review proposal by Friday"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			action := parseInlineTodoistIntent(tc.text)
			if action == nil {
				t.Fatal("expected todoist action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if got := strFromAny(action.Params["project"]); got != tc.wantProject {
				t.Fatalf("project = %q, want %q", got, tc.wantProject)
			}
			if got := systemActionWorkspaceRef(action.Params); got != tc.wantWorkspace {
				t.Fatalf("workspace = %q, want %q", got, tc.wantWorkspace)
			}
			if got := strFromAny(action.Params["target_project"]); got != tc.wantTargetProj {
				t.Fatalf("target_project = %q, want %q", got, tc.wantTargetProj)
			}
			if got := strFromAny(action.Params["text"]); got != tc.wantTaskText {
				t.Fatalf("text = %q, want %q", got, tc.wantTaskText)
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionMapTodoistProject(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Admin", filepath.Join(t.TempDir(), "admin"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "map todoist project Admin to workspace Admin")
	if !handled {
		t.Fatal("expected mapping command to be handled")
	}
	if message != "Mapped Todoist project Admin to workspace Admin." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "todoist_container_mapping" {
		t.Fatalf("payloads = %#v", payloads)
	}
	mapping, err := app.store.GetContainerMapping(store.ExternalProviderTodoist, "project", "Admin")
	if err != nil {
		t.Fatalf("GetContainerMapping() error: %v", err)
	}
	if mapping.WorkspaceID == nil || *mapping.WorkspaceID != workspace.ID {
		t.Fatalf("mapping workspace_id = %#v, want %d", mapping.WorkspaceID, workspace.ID)
	}

	linkedProject, err := app.store.CreateProject("Tabura", "tabura", filepath.Join(t.TempDir(), "tabura"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "map todoist project Tabura to project Tabura")
	if !handled {
		t.Fatal("expected project mapping command to be handled")
	}
	if message != "Mapped Todoist project Tabura to project Tabura." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "todoist_container_mapping" {
		t.Fatalf("payloads = %#v", payloads)
	}
	projectMapping, err := app.store.GetContainerMapping(store.ExternalProviderTodoist, "project", "Tabura")
	if err != nil {
		t.Fatalf("GetContainerMapping(project) error: %v", err)
	}
	if projectMapping.ProjectID == nil || *projectMapping.ProjectID != linkedProject.ID {
		t.Fatalf("mapping project_id = %#v, want %q", projectMapping.ProjectID, linkedProject.ID)
	}
}

func TestClassifyAndExecuteSystemActionSyncTodoist(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects":
			writeTodoistJSON(t, w, []map[string]any{{"id": "proj-1", "name": "Admin"}})
		case r.Method == http.MethodGet && r.URL.Path == "/tasks":
			writeTodoistJSON(t, w, []map[string]any{{
				"id":         "task-1",
				"content":    "Review proposal",
				"project_id": "proj-1",
				"url":        "https://todoist.test/task-1",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	account := createTodoistTestAccount(t, app, "Personal Todoist", server.URL)
	workspace, err := app.store.CreateWorkspace("Admin", filepath.Join(t.TempDir(), "admin"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderTodoist, "project", "Admin", &workspace.ID, nil, nil); err != nil {
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

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync todoist")
	if !handled {
		t.Fatal("expected sync command to be handled")
	}
	if message != "Synced 1 Todoist task(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "sync_todoist" {
		t.Fatalf("payloads = %#v", payloads)
	}
	item, err := app.store.GetItemBySource(store.ExternalProviderTodoist, "task:task-1")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("item workspace_id = %#v, want %d", item.WorkspaceID, workspace.ID)
	}
	if item.State != store.ItemStateInbox {
		t.Fatalf("item state = %q, want %q", item.State, store.ItemStateInbox)
	}
	if item.Sphere != store.SpherePrivate {
		t.Fatalf("item sphere = %q, want %q", item.Sphere, store.SpherePrivate)
	}
	if item.Title != "Review proposal" {
		t.Fatalf("item title = %q, want Review proposal", item.Title)
	}
	if item.ArtifactID == nil {
		t.Fatal("expected synced todoist artifact")
	}
	artifact, err := app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if artifact.Kind != store.ArtifactKindExternalTask {
		t.Fatalf("artifact kind = %q, want %q", artifact.Kind, store.ArtifactKindExternalTask)
	}
	if artifact.RefURL == nil || *artifact.RefURL != "https://todoist.test/task-1" {
		t.Fatalf("artifact ref_url = %v, want task URL", artifact.RefURL)
	}
	binding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderTodoist, "task", "task-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote() error: %v", err)
	}
	if binding.ItemID == nil || *binding.ItemID != item.ID {
		t.Fatalf("binding item_id = %#v, want %d", binding.ItemID, item.ID)
	}
}

func TestClassifyAndExecuteSystemActionSyncTodoistPersistsCommentMetadata(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	var detailCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects":
			writeTodoistJSON(t, w, []map[string]any{{"id": "proj-1", "name": "Admin"}})
		case r.Method == http.MethodGet && r.URL.Path == "/tasks":
			writeTodoistJSON(t, w, []map[string]any{{
				"id":            "task-1",
				"content":       "Review proposal",
				"project_id":    "proj-1",
				"comment_count": 1,
				"url":           "https://todoist.test/task-1",
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/tasks/task-1":
			detailCalls++
			writeTodoistJSON(t, w, map[string]any{
				"id":            "task-1",
				"content":       "Review proposal",
				"project_id":    "proj-1",
				"comment_count": 1,
				"url":           "https://todoist.test/task-1",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/comments":
			if got := r.URL.Query().Get("task_id"); got != "task-1" {
				t.Fatalf("task_id = %q, want task-1", got)
			}
			writeTodoistJSON(t, w, []map[string]any{{
				"id":        "comment-1",
				"task_id":   "task-1",
				"posted_at": "2026-03-10T09:00:00Z",
				"content":   "Remember appendix",
				"attachment": map[string]any{
					"file_name":     "appendix.pdf",
					"file_type":     "application/pdf",
					"file_url":      "https://files.todoist.test/appendix.pdf",
					"resource_type": "file",
				},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	createTodoistTestAccount(t, app, "Personal Todoist", server.URL)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	if _, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync todoist"); !handled {
		t.Fatal("expected sync command to be handled")
	}
	if detailCalls != 1 {
		t.Fatalf("detailCalls = %d, want 1", detailCalls)
	}

	item, err := app.store.GetItemBySource(store.ExternalProviderTodoist, "task:task-1")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if item.ArtifactID == nil {
		t.Fatal("expected synced todoist artifact")
	}
	artifact, err := app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}

	var meta map[string]any
	if artifact.MetaJSON == nil || json.Unmarshal([]byte(*artifact.MetaJSON), &meta) != nil {
		t.Fatalf("artifact meta_json = %v, want valid JSON", artifact.MetaJSON)
	}
	if got := intFromAny(meta["comment_count"], 0); got != 1 {
		t.Fatalf("comment_count = %d, want 1", got)
	}
	comments, _ := meta["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("comments len = %d, want 1", len(comments))
	}
	comment, _ := comments[0].(map[string]any)
	if got := strFromAny(comment["content"]); got != "Remember appendix" {
		t.Fatalf("comment content = %q, want Remember appendix", got)
	}
	attachment, _ := comment["attachment"].(map[string]any)
	if got := strFromAny(attachment["file_name"]); got != "appendix.pdf" {
		t.Fatalf("attachment file_name = %q, want appendix.pdf", got)
	}
}

func TestClassifyAndExecuteSystemActionSyncTodoistRemapUpdatesExistingItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects":
			writeTodoistJSON(t, w, []map[string]any{{"id": "proj-1", "name": "Admin"}})
		case r.Method == http.MethodGet && r.URL.Path == "/tasks":
			writeTodoistJSON(t, w, []map[string]any{{
				"id":         "task-1",
				"content":    "Review proposal",
				"project_id": "proj-1",
				"url":        "https://todoist.test/task-1",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	createTodoistTestAccount(t, app, "Personal Todoist", server.URL)
	workspace, err := app.store.CreateWorkspace("Admin", filepath.Join(t.TempDir(), "admin"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	targetProject, err := app.store.CreateProject("Tabura", "tabura", filepath.Join(t.TempDir(), "tabura"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderTodoist, "project", "Admin", &workspace.ID, nil, nil); err != nil {
		t.Fatalf("SetContainerMapping(workspace) error: %v", err)
	}
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	if _, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync todoist"); !handled {
		t.Fatal("expected initial sync command to be handled")
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderTodoist, "project", "Admin", nil, &targetProject.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping(project) error: %v", err)
	}
	if _, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync todoist"); !handled {
		t.Fatal("expected remap sync command to be handled")
	}

	item, err := app.store.GetItemBySource(store.ExternalProviderTodoist, "task:task-1")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if item.WorkspaceID != nil {
		t.Fatalf("item workspace_id = %#v, want nil after remap", item.WorkspaceID)
	}
	if item.ProjectID == nil || *item.ProjectID != targetProject.ID {
		t.Fatalf("item project_id = %#v, want %q", item.ProjectID, targetProject.ID)
	}
}

func TestClassifyAndExecuteSystemActionSyncTodoistUsesAccountSphereAsSourceOfTruth(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects":
			writeTodoistJSON(t, w, []map[string]any{{"id": "proj-1", "name": "Admin"}})
		case r.Method == http.MethodGet && r.URL.Path == "/tasks":
			writeTodoistJSON(t, w, []map[string]any{{
				"id":         "task-1",
				"content":    "Review proposal",
				"project_id": "proj-1",
				"url":        "https://todoist.test/task-1",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	account := createTodoistTestAccountInSphere(t, app, store.SphereWork, "Work Todoist", server.URL)
	privateWorkspace, err := app.store.CreateWorkspace("Admin", filepath.Join(t.TempDir(), "admin"), store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderTodoist, "project", "Admin", &privateWorkspace.ID, nil, nil); err != nil {
		t.Fatalf("SetContainerMapping() error: %v", err)
	}
	source := store.ExternalProviderTodoist
	sourceRef := todoistTaskSourceRef("task-1")
	existing, err := app.store.CreateItem("Old proposal title", store.ItemOptions{
		WorkspaceID: &privateWorkspace.ID,
		Source:      &source,
		SourceRef:   &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(existing) error: %v", err)
	}

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	if _, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync todoist"); !handled {
		t.Fatal("expected sync command to be handled")
	}

	item, err := app.store.GetItem(existing.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if item.WorkspaceID != nil {
		t.Fatalf("item workspace_id = %#v, want nil after cross-sphere sync", item.WorkspaceID)
	}
	if item.Sphere != store.SphereWork {
		t.Fatalf("item sphere = %q, want %q", item.Sphere, store.SphereWork)
	}
	if item.Title != "Review proposal" {
		t.Fatalf("item title = %q, want Review proposal", item.Title)
	}
	binding, err := app.store.GetBindingByRemote(account.ID, store.ExternalProviderTodoist, "task", "task-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote() error: %v", err)
	}
	if binding.ItemID == nil || *binding.ItemID != item.ID {
		t.Fatalf("binding item_id = %#v, want %d", binding.ItemID, item.ID)
	}
}

func TestClassifyAndExecuteSystemActionCreateTodoistTask(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects":
			writeTodoistJSON(t, w, []map[string]any{{"id": "proj-1", "name": "Admin"}})
		case r.Method == http.MethodPost && r.URL.Path == "/tasks":
			defer r.Body.Close()
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read create body: %v", err)
			}
			if err := json.Unmarshal(body, &createBody); err != nil {
				t.Fatalf("unmarshal create body: %v", err)
			}
			writeTodoistJSON(t, w, map[string]any{
				"id":         "task-99",
				"content":    createBody["content"],
				"project_id": createBody["project_id"],
				"url":        "https://todoist.test/task-99",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	createTodoistTestAccount(t, app, "Personal Todoist", server.URL)
	workspace, err := app.store.CreateWorkspace("Admin", filepath.Join(t.TempDir(), "admin"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderTodoist, "project", "Admin", &workspace.ID, nil, nil); err != nil {
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

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create todoist task: review proposal by Friday")
	if !handled {
		t.Fatal("expected create task command to be handled")
	}
	if message != `Created Todoist task "review proposal".` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "todoist_task_created" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(createBody["content"]); got != "review proposal" {
		t.Fatalf("content = %q, want review proposal", got)
	}
	if got := strFromAny(createBody["due_string"]); got != "Friday" {
		t.Fatalf("due_string = %q, want Friday", got)
	}
	if got := strFromAny(createBody["project_id"]); got != "proj-1" {
		t.Fatalf("project_id = %q, want proj-1", got)
	}
	item, err := app.store.GetItemBySource(store.ExternalProviderTodoist, "task:task-99")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("item workspace_id = %#v, want %d", item.WorkspaceID, workspace.ID)
	}
	if item.Title != "review proposal" {
		t.Fatalf("item title = %q, want review proposal", item.Title)
	}
}

func createTodoistTestAccount(t *testing.T, app *App, label, baseURL string) store.ExternalAccount {
	return createTodoistTestAccountInSphere(t, app, store.SpherePrivate, label, baseURL)
}

func createTodoistTestAccountInSphere(t *testing.T, app *App, sphere, label, baseURL string) store.ExternalAccount {
	t.Helper()
	t.Setenv(todoist.TokenEnvVar(label), "todo-token")
	account, err := app.store.CreateExternalAccount(sphere, store.ExternalProviderTodoist, label, map[string]any{
		"base_url": strings.TrimSpace(baseURL),
	})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	return account
}

func writeTodoistJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode todoist payload: %v", err)
	}
}
