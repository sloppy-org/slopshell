package web

import (
	"net/http"
	"path/filepath"
	"testing"
)

func TestWorkspaceCRUDAPI(t *testing.T) {
	app := newAuthedTestApp(t)
	workspacePath := filepath.Join(t.TempDir(), "workspace-one")

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspaces", map[string]any{
		"name":      " Workspace One ",
		"dir_path":  workspacePath,
		"sphere":    "work",
		"is_active": true,
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("create workspace status = %d, want 200: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createdPayload := decodeJSONResponse(t, rrCreate)
	workspacePayload, ok := createdPayload["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("create workspace payload = %#v", createdPayload)
	}
	workspaceID := int64(workspacePayload["id"].(float64))
	if workspacePayload["name"] != "Workspace One" {
		t.Fatalf("workspace name = %#v, want %q", workspacePayload["name"], "Workspace One")
	}
	if workspacePayload["sphere"] != "work" {
		t.Fatalf("workspace sphere = %#v, want %q", workspacePayload["sphere"], "work")
	}
	if isActive, _ := workspacePayload["is_active"].(bool); !isActive {
		t.Fatalf("workspace payload = %#v, want active workspace", workspacePayload)
	}

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("list workspaces status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	listPayload := decodeJSONResponse(t, rrList)
	workspaces, ok := listPayload["workspaces"].([]any)
	if !ok || len(workspaces) != 1 {
		t.Fatalf("list workspaces payload = %#v", listPayload)
	}

	rrGet := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspaceID), nil)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("get workspace status = %d, want 200: %s", rrGet.Code, rrGet.Body.String())
	}

	rrUpdate := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/workspaces/"+itoa(workspaceID), map[string]any{
		"name":      "Renamed Workspace",
		"sphere":    "private",
		"is_active": true,
	})
	if rrUpdate.Code != http.StatusOK {
		t.Fatalf("update workspace status = %d, want 200: %s", rrUpdate.Code, rrUpdate.Body.String())
	}
	updatePayload := decodeJSONResponse(t, rrUpdate)
	updatedWorkspace, ok := updatePayload["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("update workspace payload = %#v", updatePayload)
	}
	if updatedWorkspace["name"] != "Renamed Workspace" {
		t.Fatalf("updated workspace name = %#v, want %q", updatedWorkspace["name"], "Renamed Workspace")
	}
	if updatedWorkspace["sphere"] != "private" {
		t.Fatalf("updated workspace sphere = %#v, want %q", updatedWorkspace["sphere"], "private")
	}
	if isActive, _ := updatedWorkspace["is_active"].(bool); !isActive {
		t.Fatalf("updated workspace payload = %#v, want active workspace", updatedWorkspace)
	}

	rrDuplicate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspaces", map[string]any{
		"name":     "Duplicate",
		"dir_path": workspacePath,
		"sphere":   "work",
	})
	if rrDuplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate workspace status = %d, want 409: %s", rrDuplicate.Code, rrDuplicate.Body.String())
	}

	rrMissingSphere := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspaces", map[string]any{
		"name":     "No Sphere",
		"dir_path": filepath.Join(t.TempDir(), "workspace-two"),
	})
	if rrMissingSphere.Code != http.StatusBadRequest {
		t.Fatalf("missing sphere status = %d, want 400: %s", rrMissingSphere.Code, rrMissingSphere.Body.String())
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/workspaces/"+itoa(workspaceID), nil)
	if rrDelete.Code != http.StatusOK {
		t.Fatalf("delete workspace status = %d, want 200: %s", rrDelete.Code, rrDelete.Body.String())
	}

	rrMissing := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspaceID), nil)
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("deleted workspace status = %d, want 404: %s", rrMissing.Code, rrMissing.Body.String())
	}
}
