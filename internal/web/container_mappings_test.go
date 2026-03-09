package web

import (
	"net/http"
	"testing"
)

func TestContainerMappingCRUDAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	workspace, err := app.store.CreateWorkspace("Work", t.TempDir())
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	project, err := app.store.CreateProject("Tabura", "tabura", t.TempDir(), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/container-mappings", map[string]any{
		"provider":       "todoist",
		"container_type": "project",
		"container_ref":  " Tabura ",
		"workspace_id":   workspace.ID,
		"project_id":     project.ID,
		"sphere":         "work",
	})
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create container mapping status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createPayload := decodeJSONDataResponse(t, rrCreate)
	mappingPayload, ok := createPayload["mapping"].(map[string]any)
	if !ok {
		t.Fatalf("create payload = %#v", createPayload)
	}
	mappingID := int64(mappingPayload["id"].(float64))
	if got := mappingPayload["container_ref"]; got != "Tabura" {
		t.Fatalf("container_ref = %#v, want %q", got, "Tabura")
	}
	if got := mappingPayload["project_id"]; got != project.ID {
		t.Fatalf("project_id = %#v, want %q", got, project.ID)
	}

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/container-mappings?provider=todoist", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("list container mappings status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	listPayload := decodeJSONDataResponse(t, rrList)
	mappings, ok := listPayload["mappings"].([]any)
	if !ok || len(mappings) != 1 {
		t.Fatalf("list payload = %#v", listPayload)
	}

	rrUpdate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/container-mappings", map[string]any{
		"provider":       "todoist",
		"container_type": "project",
		"container_ref":  "tabura",
		"sphere":         "private",
	})
	if rrUpdate.Code != http.StatusOK {
		t.Fatalf("update container mapping status = %d, want 200: %s", rrUpdate.Code, rrUpdate.Body.String())
	}
	updated := decodeJSONDataResponse(t, rrUpdate)
	updatedMapping, ok := updated["mapping"].(map[string]any)
	if !ok {
		t.Fatalf("update payload = %#v", updated)
	}
	if got := updatedMapping["id"]; got != float64(mappingID) {
		t.Fatalf("updated id = %#v, want %d", got, mappingID)
	}
	if _, ok := updatedMapping["workspace_id"]; ok {
		t.Fatalf("updated mapping should clear workspace_id: %#v", updatedMapping)
	}
	if _, ok := updatedMapping["project_id"]; ok {
		t.Fatalf("updated mapping should clear project_id: %#v", updatedMapping)
	}
	if got := updatedMapping["sphere"]; got != "private" {
		t.Fatalf("updated sphere = %#v, want private", got)
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/container-mappings/"+itoa(mappingID), nil)
	if rrDelete.Code != http.StatusNoContent {
		t.Fatalf("delete container mapping status = %d, want 204: %s", rrDelete.Code, rrDelete.Body.String())
	}
	if rrDelete.Body.Len() != 0 {
		t.Fatalf("delete container mapping body = %q, want empty", rrDelete.Body.String())
	}
	rrMissing := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/container-mappings/"+itoa(mappingID), nil)
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("missing delete status = %d, want 404: %s", rrMissing.Code, rrMissing.Body.String())
	}
}

func TestContainerMappingAPIRejectsInvalidInput(t *testing.T) {
	app := newAuthedTestApp(t)

	rrBadCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/container-mappings", map[string]any{
		"provider":       "todoist",
		"container_type": "board",
		"container_ref":  "Tabura",
		"sphere":         "work",
	})
	if rrBadCreate.Code != http.StatusBadRequest {
		t.Fatalf("bad create status = %d, want 400: %s", rrBadCreate.Code, rrBadCreate.Body.String())
	}
	if got := decodeJSONResponse(t, rrBadCreate)["error"]; got == nil {
		t.Fatalf("bad create payload = %#v, want error", decodeJSONResponse(t, rrBadCreate))
	}

	rrBadList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/container-mappings?provider=smtp", nil)
	if rrBadList.Code != http.StatusBadRequest {
		t.Fatalf("bad list status = %d, want 400: %s", rrBadList.Code, rrBadList.Body.String())
	}

	rrMissingDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/container-mappings/9999", nil)
	if rrMissingDelete.Code != http.StatusNotFound {
		t.Fatalf("missing delete status = %d, want 404: %s", rrMissingDelete.Code, rrMissingDelete.Body.String())
	}
}
