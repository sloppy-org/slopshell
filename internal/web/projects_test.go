package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
)

type projectsListResponse struct {
	OK               bool   `json:"ok"`
	DefaultProjectID string `json:"default_project_id"`
	ActiveProjectID  string `json:"active_project_id"`
	Projects         []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		ChatSessionID   string `json:"chat_session_id"`
		CanvasSessionID string `json:"canvas_session_id"`
	} `json:"projects"`
}

func TestProjectsListIncludesActiveAndSessions(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload projectsListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.OK {
		t.Fatalf("expected ok=true")
	}
	if payload.DefaultProjectID == "" {
		t.Fatalf("expected default project id")
	}
	if payload.ActiveProjectID == "" {
		t.Fatalf("expected active project id")
	}
	if len(payload.Projects) == 0 {
		t.Fatalf("expected at least one project")
	}
	first := payload.Projects[0]
	if first.ChatSessionID == "" {
		t.Fatalf("expected project chat session id")
	}
	if first.CanvasSessionID == "" {
		t.Fatalf("expected project canvas session id")
	}
}

func TestCreateActivateProjectAffectsChatSessionCreation(t *testing.T) {
	app := newAuthedTestApp(t)

	linkedDir := t.TempDir()
	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/projects", map[string]any{
		"name":     "linked-repo",
		"kind":     "linked",
		"path":     filepath.Clean(linkedDir),
		"activate": false,
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("expected create 200, got %d: %s", rrCreate.Code, rrCreate.Body.String())
	}
	var createPayload struct {
		OK      bool `json:"ok"`
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal(rrCreate.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !createPayload.OK || createPayload.Project.ID == "" {
		t.Fatalf("expected created project id")
	}

	rrActivate := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/projects/"+createPayload.Project.ID+"/activate",
		map[string]any{},
	)
	if rrActivate.Code != http.StatusOK {
		t.Fatalf("expected activate 200, got %d: %s", rrActivate.Code, rrActivate.Body.String())
	}
	var activatePayload struct {
		OK              bool   `json:"ok"`
		ActiveProjectID string `json:"active_project_id"`
		Project         struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal(rrActivate.Body.Bytes(), &activatePayload); err != nil {
		t.Fatalf("decode activate response: %v", err)
	}
	if !activatePayload.OK {
		t.Fatalf("expected activate ok=true")
	}
	if activatePayload.ActiveProjectID != createPayload.Project.ID {
		t.Fatalf("expected active project %q, got %q", createPayload.Project.ID, activatePayload.ActiveProjectID)
	}

	rrSession := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/chat/sessions", map[string]any{})
	if rrSession.Code != http.StatusOK {
		t.Fatalf("expected chat session create 200, got %d: %s", rrSession.Code, rrSession.Body.String())
	}
	var sessionPayload struct {
		OK        bool   `json:"ok"`
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(rrSession.Body.Bytes(), &sessionPayload); err != nil {
		t.Fatalf("decode chat session response: %v", err)
	}
	if !sessionPayload.OK {
		t.Fatalf("expected chat session create ok=true")
	}
	if sessionPayload.ProjectID != createPayload.Project.ID {
		t.Fatalf("expected chat session project %q, got %q", createPayload.Project.ID, sessionPayload.ProjectID)
	}
}
