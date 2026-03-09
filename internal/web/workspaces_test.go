package web

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/krystophny/tabura/internal/store"
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
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create workspace status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createdData := decodeJSONDataResponse(t, rrCreate)
	workspacePayload, ok := createdData["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("create workspace payload = %#v", createdData)
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
	listPayload := decodeJSONDataResponse(t, rrList)
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
	updatePayload := decodeJSONDataResponse(t, rrUpdate)
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
	if got := decodeJSONResponse(t, rrMissingSphere)["error"]; got != "sphere is required" {
		t.Fatalf("missing sphere error = %#v, want %q", got, "sphere is required")
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/workspaces/"+itoa(workspaceID), nil)
	if rrDelete.Code != http.StatusNoContent {
		t.Fatalf("delete workspace status = %d, want 204: %s", rrDelete.Code, rrDelete.Body.String())
	}
	if rrDelete.Body.Len() != 0 {
		t.Fatalf("delete workspace body = %q, want empty", rrDelete.Body.String())
	}

	rrMissing := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspaceID), nil)
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("deleted workspace status = %d, want 404: %s", rrMissing.Code, rrMissing.Body.String())
	}
}

func TestWorkspaceListFiltersBySphere(t *testing.T) {
	app := newAuthedTestApp(t)

	privateWorkspace, err := app.store.CreateWorkspace("Private", filepath.Join(t.TempDir(), "private"), store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace(private) error: %v", err)
	}
	workWorkspace, err := app.store.CreateWorkspace("Work", filepath.Join(t.TempDir(), "work"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(work) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces?sphere=work", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("filter workspaces status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONDataResponse(t, rr)
	workspaces, ok := payload["workspaces"].([]any)
	if !ok || len(workspaces) != 1 {
		t.Fatalf("filtered workspaces payload = %#v", payload)
	}
	workspace, ok := workspaces[0].(map[string]any)
	if !ok {
		t.Fatalf("workspace payload = %#v", workspaces[0])
	}
	if got := int64(workspace["id"].(float64)); got != workWorkspace.ID {
		t.Fatalf("workspace id = %d, want %d", got, workWorkspace.ID)
	}
	if got := workspace["sphere"]; got != store.SphereWork {
		t.Fatalf("workspace sphere = %#v, want %q", got, store.SphereWork)
	}

	rrBad := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces?sphere=office", nil)
	if rrBad.Code != http.StatusBadRequest {
		t.Fatalf("invalid sphere status = %d, want 400: %s", rrBad.Code, rrBad.Body.String())
	}
	if got := decodeJSONResponse(t, rrBad)["error"]; got == nil {
		t.Fatalf("invalid sphere payload = %#v, want error field", decodeJSONResponse(t, rrBad))
	}

	if _, err := app.store.GetWorkspace(privateWorkspace.ID); err != nil {
		t.Fatalf("GetWorkspace(private) error: %v", err)
	}
}
