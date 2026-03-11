package web

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestWorkspaceProjectAssignmentPreservesProjectScopedItemQueries(t *testing.T) {
	app := newAuthedTestApp(t)

	project, _, err := app.createProject(projectCreateRequest{Name: "EUROfusion"})
	if err != nil {
		t.Fatalf("createProject() error: %v", err)
	}

	workspacePath := filepath.Join(t.TempDir(), "workspace-alpha")
	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspaces", map[string]any{
		"name":     "Alpha",
		"dir_path": workspacePath,
		"sphere":   store.SphereWork,
	})
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	workspacePayload, ok := decodeJSONDataResponse(t, rrCreate)["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("create workspace payload = %#v", rrCreate.Body.String())
	}
	workspaceID := int64(workspacePayload["id"].(float64))

	if _, err := app.store.SetWorkspaceProject(workspaceID, &project.ID); err != nil {
		t.Fatalf("SetWorkspaceProject() error: %v", err)
	}

	item, err := app.store.CreateItem("Prepare agenda", store.ItemOptions{WorkspaceID: &workspaceID})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if item.ProjectID == nil || *item.ProjectID != project.ID {
		t.Fatalf("item project_id = %v, want %q", item.ProjectID, project.ID)
	}

	rrProjectItems := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items?project_id="+project.ID, nil)
	if rrProjectItems.Code != http.StatusOK {
		t.Fatalf("project filtered items status = %d, want 200: %s", rrProjectItems.Code, rrProjectItems.Body.String())
	}
	projectItems, ok := decodeJSONDataResponse(t, rrProjectItems)["items"].([]any)
	if !ok || len(projectItems) != 1 {
		t.Fatalf("project filtered items payload = %#v", rrProjectItems.Body.String())
	}

	rrInbox := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/inbox?project_id="+project.ID, nil)
	if rrInbox.Code != http.StatusOK {
		t.Fatalf("project inbox status = %d, want 200: %s", rrInbox.Code, rrInbox.Body.String())
	}
	inboxItems, ok := decodeJSONDataResponse(t, rrInbox)["items"].([]any)
	if !ok || len(inboxItems) != 1 {
		t.Fatalf("project inbox payload = %#v", rrInbox.Body.String())
	}
}

func TestLegacyWorkspaceProjectAssignmentRouteRemoved(t *testing.T) {
	app := newAuthedTestApp(t)

	project, _, err := app.createProject(projectCreateRequest{Name: "EUROfusion"})
	if err != nil {
		t.Fatalf("createProject() error: %v", err)
	}

	workspace, err := app.store.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "workspace-alpha"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}

	rrAssign := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/workspaces/"+itoa(workspace.ID)+"/project", map[string]any{
		"project_id": project.ID,
	})
	if rrAssign.Code != http.StatusNotFound {
		t.Fatalf("legacy workspace project route status = %d, want 404: %s", rrAssign.Code, rrAssign.Body.String())
	}
}

func TestWorkspaceCreateInfersProjectFromPathAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	project, _, err := app.createProject(projectCreateRequest{Name: "EUROfusion"})
	if err != nil {
		t.Fatalf("createProject() error: %v", err)
	}

	workspacePath := filepath.Join(t.TempDir(), "write", "eurofusion-proposal")
	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspaces", map[string]any{
		"name":     "Proposal",
		"dir_path": workspacePath,
		"sphere":   store.SphereWork,
	})
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	workspacePayload, ok := decodeJSONDataResponse(t, rrCreate)["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("create workspace payload = %#v", rrCreate.Body.String())
	}
	if got := strFromAny(workspacePayload["project_id"]); got != project.ID {
		t.Fatalf("workspace project_id = %q, want %q", got, project.ID)
	}
}
