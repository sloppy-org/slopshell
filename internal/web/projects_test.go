package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

type projectsListResponse struct {
	OK               bool   `json:"ok"`
	DefaultProjectID string `json:"default_project_id"`
	ActiveProjectID  string `json:"active_project_id"`
	Projects         []struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Kind            string `json:"kind"`
		ProjectKey      string `json:"project_key"`
		ChatSessionID   string `json:"chat_session_id"`
		ChatModel       string `json:"chat_model"`
		ReasoningEffort string `json:"chat_model_reasoning_effort"`
		CanvasSessionID string `json:"canvas_session_id"`
		RunState        struct {
			ActiveTurns  int    `json:"active_turns"`
			QueuedTurns  int    `json:"queued_turns"`
			IsWorking    bool   `json:"is_working"`
			Status       string `json:"status"`
			ActiveTurnID string `json:"active_turn_id"`
		} `json:"run_state"`
	} `json:"projects"`
}

type projectsActivityResponse struct {
	OK       bool `json:"ok"`
	Projects []struct {
		ProjectID     string `json:"project_id"`
		ChatSessionID string `json:"chat_session_id"`
		RunState      struct {
			ActiveTurns int    `json:"active_turns"`
			QueuedTurns int    `json:"queued_turns"`
			IsWorking   bool   `json:"is_working"`
			Status      string `json:"status"`
		} `json:"run_state"`
	} `json:"projects"`
}

type projectFilesListResponse struct {
	OK        bool   `json:"ok"`
	ProjectID string `json:"project_id"`
	Path      string `json:"path"`
	IsRoot    bool   `json:"is_root"`
	Entries   []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
	} `json:"entries"`
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
	if first.ChatModel == "" {
		t.Fatalf("expected project chat model")
	}
	if first.RunState.Status == "" {
		t.Fatalf("expected project run state status")
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

func TestProjectChatModelUpdate(t *testing.T) {
	app := newAuthedTestApp(t)

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects", map[string]any{})
	if rrList.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", rrList.Code, rrList.Body.String())
	}
	var listPayload projectsListResponse
	if err := json.Unmarshal(rrList.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	if len(listPayload.Projects) == 0 {
		t.Fatalf("expected at least one project")
	}
	projectID := listPayload.Projects[0].ID
	if projectID == "" {
		t.Fatalf("expected project id")
	}

	rrUpdate := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/projects/"+projectID+"/chat-model",
		map[string]any{"model": "gpt"},
	)
	if rrUpdate.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", rrUpdate.Code, rrUpdate.Body.String())
	}
	var updatePayload struct {
		OK      bool `json:"ok"`
		Project struct {
			ID                       string `json:"id"`
			ChatModel                string `json:"chat_model"`
			ChatModelReasoningEffort string `json:"chat_model_reasoning_effort"`
		} `json:"project"`
	}
	if err := json.Unmarshal(rrUpdate.Body.Bytes(), &updatePayload); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if !updatePayload.OK {
		t.Fatalf("expected update ok=true")
	}
	if updatePayload.Project.ID != projectID {
		t.Fatalf("expected updated project id %q, got %q", projectID, updatePayload.Project.ID)
	}
	if updatePayload.Project.ChatModel != "gpt" {
		t.Fatalf("expected chat model gpt, got %q", updatePayload.Project.ChatModel)
	}
	if updatePayload.Project.ChatModelReasoningEffort != "high" {
		t.Fatalf("expected gpt reasoning effort high, got %q", updatePayload.Project.ChatModelReasoningEffort)
	}

	rrEffortUpdate := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/projects/"+projectID+"/chat-model",
		map[string]any{
			"model":            "gpt",
			"reasoning_effort": "extra_high",
		},
	)
	if rrEffortUpdate.Code != http.StatusOK {
		t.Fatalf("expected effort update 200, got %d: %s", rrEffortUpdate.Code, rrEffortUpdate.Body.String())
	}
	var effortPayload struct {
		OK      bool `json:"ok"`
		Project struct {
			ID                       string `json:"id"`
			ChatModelReasoningEffort string `json:"chat_model_reasoning_effort"`
		} `json:"project"`
	}
	if err := json.Unmarshal(rrEffortUpdate.Body.Bytes(), &effortPayload); err != nil {
		t.Fatalf("decode effort update response: %v", err)
	}
	if !effortPayload.OK {
		t.Fatalf("expected effort update ok=true")
	}
	if effortPayload.Project.ChatModelReasoningEffort != "xhigh" {
		t.Fatalf("expected effort xhigh, got %q", effortPayload.Project.ChatModelReasoningEffort)
	}

	rrInvalid := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/projects/"+projectID+"/chat-model",
		map[string]any{"model": "invalid"},
	)
	if rrInvalid.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid model 400, got %d: %s", rrInvalid.Code, rrInvalid.Body.String())
	}
}

func TestHubProjectCreatedWithFixedSparkModel(t *testing.T) {
	app := newAuthedTestApp(t)

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects", map[string]any{})
	if rrList.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", rrList.Code, rrList.Body.String())
	}
	var payload projectsListResponse
	if err := json.Unmarshal(rrList.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}

	foundHub := false
	for _, project := range payload.Projects {
		if project.ProjectKey != HubProjectKey {
			continue
		}
		foundHub = true
		if project.Kind != HubProjectKind {
			t.Fatalf("hub project kind = %q, want %q", project.Kind, HubProjectKind)
		}
		if project.ChatModel != "spark" {
			t.Fatalf("hub chat model = %q, want spark", project.ChatModel)
		}
		if project.ReasoningEffort != "low" {
			t.Fatalf("hub reasoning effort = %q, want low", project.ReasoningEffort)
		}
		break
	}
	if !foundHub {
		t.Fatalf("expected hub project in projects list")
	}
}

func TestProjectsListIncludesRunState(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	app.registerActiveChatTurn(session.ID, "run-projects", func() {})
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 2
	app.turns.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload projectsListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, item := range payload.Projects {
		if item.ID != project.ID {
			continue
		}
		if item.RunState.ActiveTurns != 1 {
			t.Fatalf("active_turns = %d, want 1", item.RunState.ActiveTurns)
		}
		if item.RunState.QueuedTurns != 2 {
			t.Fatalf("queued_turns = %d, want 2", item.RunState.QueuedTurns)
		}
		if !item.RunState.IsWorking {
			t.Fatalf("expected project to be working")
		}
		if item.RunState.Status != "running" {
			t.Fatalf("status = %q, want running", item.RunState.Status)
		}
		if item.RunState.ActiveTurnID != "run-projects" {
			t.Fatalf("active_turn_id = %q, want run-projects", item.RunState.ActiveTurnID)
		}
		return
	}
	t.Fatalf("expected project %q in list response", project.ID)
}

func TestProjectsActivityListsPerProjectRunState(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	app.turns.mu.Lock()
	app.turns.queue[session.ID] = 3
	app.turns.mu.Unlock()

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects/activity", map[string]any{})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected activity 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload projectsActivityResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode activity response: %v", err)
	}
	for _, item := range payload.Projects {
		if item.ProjectID != project.ID {
			continue
		}
		if item.ChatSessionID != session.ID {
			t.Fatalf("chat_session_id = %q, want %q", item.ChatSessionID, session.ID)
		}
		if item.RunState.ActiveTurns != 0 {
			t.Fatalf("active_turns = %d, want 0", item.RunState.ActiveTurns)
		}
		if item.RunState.QueuedTurns != 3 {
			t.Fatalf("queued_turns = %d, want 3", item.RunState.QueuedTurns)
		}
		if !item.RunState.IsWorking {
			t.Fatalf("expected project to be working")
		}
		if item.RunState.Status != "queued" {
			t.Fatalf("status = %q, want queued", item.RunState.Status)
		}
		return
	}
	t.Fatalf("expected project %q in activity response", project.ID)
}

func TestHubProjectRejectsModelUpdates(t *testing.T) {
	app := newAuthedTestApp(t)
	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}

	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/projects/"+hub.ID+"/chat-model",
		map[string]any{"model": "gpt"},
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProjectFilesListReturnsOneLevelAndSupportsSubfolders(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	root := filepath.Clean(project.RootPath)
	dirName := "zz_test_dir"
	fileName := "zz_test_file.txt"
	dirPath := filepath.Join(root, dirName)
	filePath := filepath.Join(root, fileName)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir test dir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("root"), 0o644); err != nil {
		t.Fatalf("write root test file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "child.md"), []byte("child"), 0o644); err != nil {
		t.Fatalf("write child test file: %v", err)
	}

	rrRoot := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodGet,
		"/api/projects/"+project.ID+"/files",
		nil,
	)
	if rrRoot.Code != http.StatusOK {
		t.Fatalf("expected root list 200, got %d: %s", rrRoot.Code, rrRoot.Body.String())
	}
	var rootPayload projectFilesListResponse
	if err := json.Unmarshal(rrRoot.Body.Bytes(), &rootPayload); err != nil {
		t.Fatalf("decode root payload: %v", err)
	}
	if !rootPayload.OK {
		t.Fatalf("expected ok=true")
	}
	if !rootPayload.IsRoot || rootPayload.Path != "" {
		t.Fatalf("expected root listing, got is_root=%v path=%q", rootPayload.IsRoot, rootPayload.Path)
	}
	dirIndex := -1
	fileIndex := -1
	for i, entry := range rootPayload.Entries {
		if entry.Path == dirName {
			if !entry.IsDir {
				t.Fatalf("expected %q to be a directory", dirName)
			}
			dirIndex = i
		}
		if entry.Path == fileName {
			if entry.IsDir {
				t.Fatalf("expected %q to be a file", fileName)
			}
			fileIndex = i
		}
	}
	if dirIndex < 0 || fileIndex < 0 {
		t.Fatalf("expected seeded entries in root listing")
	}
	if dirIndex > fileIndex {
		t.Fatalf("expected directories before files in listing")
	}

	rrSub := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodGet,
		"/api/projects/"+project.ID+"/files?path="+dirName,
		nil,
	)
	if rrSub.Code != http.StatusOK {
		t.Fatalf("expected subdirectory list 200, got %d: %s", rrSub.Code, rrSub.Body.String())
	}
	var subPayload projectFilesListResponse
	if err := json.Unmarshal(rrSub.Body.Bytes(), &subPayload); err != nil {
		t.Fatalf("decode sub payload: %v", err)
	}
	if subPayload.IsRoot || subPayload.Path != dirName {
		t.Fatalf("expected subdirectory payload for %q, got is_root=%v path=%q", dirName, subPayload.IsRoot, subPayload.Path)
	}
	if len(subPayload.Entries) == 0 || subPayload.Entries[0].Path != dirName+"/child.md" {
		t.Fatalf("expected child file path %q in subdirectory listing", dirName+"/child.md")
	}
}

func TestProjectFilesListRejectsTraversal(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	rr := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodGet,
		"/api/projects/"+project.ID+"/files?path=../secret",
		nil,
	)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected traversal request 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProjectWelcomeListsDocsAndRecentFiles(t *testing.T) {
	app := newAuthedTestApp(t)

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects", map[string]any{})
	if rrList.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", rrList.Code, rrList.Body.String())
	}
	var listPayload projectsListResponse
	if err := json.Unmarshal(rrList.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	if len(listPayload.Projects) == 0 {
		t.Fatalf("expected at least one project")
	}
	projectID := ""
	for _, project := range listPayload.Projects {
		if project.Kind != "hub" {
			projectID = project.ID
			break
		}
	}
	if projectID == "" {
		t.Fatalf("expected a non-hub project")
	}
	project, err := app.store.GetProject(projectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("# hello"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "notes.txt"), []byte("recent"), 0o644); err != nil {
		t.Fatalf("write notes.txt: %v", err)
	}

	rrWelcome := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects/"+projectID+"/welcome", nil)
	if rrWelcome.Code != http.StatusOK {
		t.Fatalf("expected welcome 200, got %d: %s", rrWelcome.Code, rrWelcome.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rrWelcome.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode welcome response: %v", err)
	}
	if got := strFromAny(payload["scope"]); got != "project" {
		t.Fatalf("scope = %q, want %q", got, "project")
	}
	sections, ok := payload["sections"].([]any)
	if !ok || len(sections) == 0 {
		t.Fatalf("expected welcome sections, got %v", payload["sections"])
	}
	body := rrWelcome.Body.String()
	if !strings.Contains(body, "README.md") {
		t.Fatalf("welcome body missing README.md: %s", body)
	}
	if !strings.Contains(body, "notes.txt") {
		t.Fatalf("welcome body missing notes.txt: %s", body)
	}
}

func TestHubWelcomeListsProjects(t *testing.T) {
	app := newAuthedTestApp(t)

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects", map[string]any{})
	if rrList.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d: %s", rrList.Code, rrList.Body.String())
	}
	var listPayload projectsListResponse
	if err := json.Unmarshal(rrList.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	hubID := ""
	projectName := ""
	for _, project := range listPayload.Projects {
		if project.Kind == "hub" {
			hubID = project.ID
			continue
		}
		if projectName == "" {
			projectName = project.Name
		}
	}
	if hubID == "" {
		t.Fatalf("expected hub project id")
	}

	rrWelcome := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects/"+hubID+"/welcome", nil)
	if rrWelcome.Code != http.StatusOK {
		t.Fatalf("expected hub welcome 200, got %d: %s", rrWelcome.Code, rrWelcome.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rrWelcome.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode hub welcome response: %v", err)
	}
	if got := strFromAny(payload["scope"]); got != "hub" {
		t.Fatalf("scope = %q, want %q", got, "hub")
	}
	if projectName != "" && !strings.Contains(rrWelcome.Body.String(), projectName) {
		t.Fatalf("hub welcome missing project name %q: %s", projectName, rrWelcome.Body.String())
	}
}

func TestTemporaryProjectCreationCopiesSourceSettingsAndPersist(t *testing.T) {
	app := newAuthedTestApp(t)
	source, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	if err := app.store.UpdateProjectChatModel(source.ID, "gpt"); err != nil {
		t.Fatalf("UpdateProjectChatModel() error: %v", err)
	}
	if err := app.store.UpdateProjectChatModelReasoningEffort(source.ID, "xhigh"); err != nil {
		t.Fatalf("UpdateProjectChatModelReasoningEffort() error: %v", err)
	}
	if err := app.store.UpdateProjectCompanionConfig(source.ID, `{"companion_enabled":true,"idle_surface":"black"}`); err != nil {
		t.Fatalf("UpdateProjectCompanionConfig() error: %v", err)
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/projects", map[string]any{
		"kind":              "meeting",
		"source_project_id": source.ID,
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("expected create 200, got %d: %s", rrCreate.Code, rrCreate.Body.String())
	}
	var createPayload struct {
		OK      bool `json:"ok"`
		Project struct {
			ID        string `json:"id"`
			Kind      string `json:"kind"`
			RootPath  string `json:"root_path"`
			ChatModel string `json:"chat_model"`
		} `json:"project"`
	}
	if err := json.Unmarshal(rrCreate.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if !createPayload.OK || createPayload.Project.ID == "" {
		t.Fatalf("expected created project payload")
	}
	if createPayload.Project.Kind != "meeting" {
		t.Fatalf("created kind = %q, want meeting", createPayload.Project.Kind)
	}
	if createPayload.Project.ChatModel != "gpt" {
		t.Fatalf("created chat model = %q, want gpt", createPayload.Project.ChatModel)
	}
	if createPayload.Project.RootPath == source.RootPath {
		t.Fatalf("temporary project root should differ from source root")
	}
	if !strings.Contains(filepath.ToSlash(createPayload.Project.RootPath), "/projects/temporary/meeting/") {
		t.Fatalf("temporary root = %q, want temporary meeting path", createPayload.Project.RootPath)
	}

	created, err := app.store.GetProject(createPayload.Project.ID)
	if err != nil {
		t.Fatalf("GetProject(created) error: %v", err)
	}
	if created.ChatModel != "gpt" {
		t.Fatalf("stored chat model = %q, want gpt", created.ChatModel)
	}
	if created.ChatModelReasoningEffort != "xhigh" {
		t.Fatalf("stored reasoning effort = %q, want xhigh", created.ChatModelReasoningEffort)
	}
	if got := strings.TrimSpace(created.CompanionConfigJSON); got != `{"companion_enabled":true,"idle_surface":"black"}` {
		t.Fatalf("stored companion config = %q", got)
	}

	rrPersist := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/projects/"+createPayload.Project.ID+"/persist",
		map[string]any{},
	)
	if rrPersist.Code != http.StatusOK {
		t.Fatalf("expected persist 200, got %d: %s", rrPersist.Code, rrPersist.Body.String())
	}
	var persistPayload struct {
		OK      bool `json:"ok"`
		Project struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"project"`
	}
	if err := json.Unmarshal(rrPersist.Body.Bytes(), &persistPayload); err != nil {
		t.Fatalf("decode persist response: %v", err)
	}
	if !persistPayload.OK {
		t.Fatalf("expected persist ok=true")
	}
	if persistPayload.Project.Kind != "managed" {
		t.Fatalf("persisted kind = %q, want managed", persistPayload.Project.Kind)
	}
}

func TestTemporaryProjectDiscardRemovesProjectDataAndFallsBackToHub(t *testing.T) {
	app := newAuthedTestApp(t)
	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub: %v", err)
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/projects", map[string]any{
		"kind": "task",
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("expected create 200, got %d: %s", rrCreate.Code, rrCreate.Body.String())
	}
	var createPayload struct {
		Project struct {
			ID         string `json:"id"`
			ProjectKey string `json:"project_key"`
			RootPath   string `json:"root_path"`
		} `json:"project"`
	}
	if err := json.Unmarshal(rrCreate.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createPayload.Project.ID == "" {
		t.Fatalf("expected created task project id")
	}
	if err := os.WriteFile(filepath.Join(createPayload.Project.RootPath, "run-output.md"), []byte("saved output"), 0o644); err != nil {
		t.Fatalf("WriteFile(run-output.md) error: %v", err)
	}
	chatSession, err := app.store.GetOrCreateChatSession(createPayload.Project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	if _, err := app.store.AddChatMessage(chatSession.ID, "assistant", "saved output", "saved output", "markdown"); err != nil {
		t.Fatalf("AddChatMessage() error: %v", err)
	}
	participantSession, err := app.store.AddParticipantSession(createPayload.Project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession() error: %v", err)
	}
	if _, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID: participantSession.ID,
		StartTS:   100,
		EndTS:     101,
		Text:      "transcript only",
		Status:    "final",
	}); err != nil {
		t.Fatalf("AddParticipantSegment() error: %v", err)
	}
	if err := app.store.UpsertParticipantRoomState(participantSession.ID, "summary", `["Acme"]`, `["Decision"]`); err != nil {
		t.Fatalf("UpsertParticipantRoomState() error: %v", err)
	}

	rrDiscard := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/projects/"+createPayload.Project.ID+"/discard",
		map[string]any{},
	)
	if rrDiscard.Code != http.StatusOK {
		t.Fatalf("expected discard 200, got %d: %s", rrDiscard.Code, rrDiscard.Body.String())
	}
	var discardPayload struct {
		OK              bool   `json:"ok"`
		ActiveProjectID string `json:"active_project_id"`
		ActiveProject   struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"active_project"`
	}
	if err := json.Unmarshal(rrDiscard.Body.Bytes(), &discardPayload); err != nil {
		t.Fatalf("decode discard response: %v", err)
	}
	if !discardPayload.OK {
		t.Fatalf("expected discard ok=true")
	}
	if discardPayload.ActiveProjectID != hub.ID {
		t.Fatalf("active_project_id = %q, want %q", discardPayload.ActiveProjectID, hub.ID)
	}
	if discardPayload.ActiveProject.Kind != "hub" {
		t.Fatalf("active project kind = %q, want hub", discardPayload.ActiveProject.Kind)
	}
	if _, err := app.store.GetProject(createPayload.Project.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetProject(discarded) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := app.store.GetChatSession(chatSession.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetChatSession(discarded) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := app.store.GetParticipantSession(participantSession.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetParticipantSession(discarded) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := os.Stat(createPayload.Project.RootPath); !os.IsNotExist(err) {
		t.Fatalf("temporary project root still exists: %v", err)
	}
}
