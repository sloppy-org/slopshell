package web

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineProjectIntent(t *testing.T) {
	cases := []struct {
		text        string
		wantAction  string
		wantProject string
	}{
		{text: "assign this workspace to EUROfusion", wantAction: "assign_workspace_project", wantProject: "EUROfusion"},
		{text: "what project is this?", wantAction: "show_workspace_project"},
		{text: "create project EUROfusion", wantAction: "create_project", wantProject: "EUROfusion"},
		{text: "list EUROfusion workspaces", wantAction: "list_project_workspaces", wantProject: "EUROfusion"},
		{text: "sync EUROfusion", wantAction: "sync_project", wantProject: "EUROfusion"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			action := parseInlineProjectIntent(tc.text)
			if action == nil {
				t.Fatal("expected project action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if got := systemActionProjectRef(action.Params); got != tc.wantProject {
				t.Fatalf("project ref = %q, want %q", got, tc.wantProject)
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionAssignWorkspaceProject(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, _, err := app.createProject(projectCreateRequest{Name: "EUROfusion"})
	if err != nil {
		t.Fatalf("createProject() error: %v", err)
	}
	defaultProject, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord() error: %v", err)
	}
	session, err := app.chatSessionForProject(defaultProject)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Proposal", filepath.Join(t.TempDir(), "proposal"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "assign this workspace to EUROfusion")
	if !handled {
		t.Fatal("expected command to be handled")
	}
	if message != "Assigned workspace Proposal to project EUROfusion." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "workspace_project_updated" {
		t.Fatalf("payloads = %#v", payloads)
	}
	updated, err := app.store.GetWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspace() error: %v", err)
	}
	if updated.ProjectID == nil || *updated.ProjectID != project.ID {
		t.Fatalf("workspace project_id = %v, want %q", updated.ProjectID, project.ID)
	}
}

func TestClassifyAndExecuteSystemActionShowProjectItemsByName(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, _, err := app.createProject(projectCreateRequest{Name: "EUROfusion"})
	if err != nil {
		t.Fatalf("createProject() error: %v", err)
	}
	defaultProject, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord() error: %v", err)
	}
	session, err := app.chatSessionForProject(defaultProject)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Proposal", filepath.Join(t.TempDir(), "proposal"), store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetWorkspaceProject(workspace.ID, &project.ID); err != nil {
		t.Fatalf("SetWorkspaceProject() error: %v", err)
	}
	if _, err := app.store.CreateItem("Draft agenda", store.ItemOptions{WorkspaceID: &workspace.ID}); err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show all EUROfusion items")
	if !handled {
		t.Fatal("expected command to be handled")
	}
	if message == "" {
		t.Fatal("expected message")
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "show_item_sidebar_view" {
		t.Fatalf("payloads = %#v", payloads)
	}
	filters, ok := payloads[0]["filters"].(map[string]interface{})
	if !ok {
		t.Fatalf("filters payload = %#v", payloads[0]["filters"])
	}
	if got := strFromAny(filters["project_id"]); got != project.ID {
		t.Fatalf("project filter = %q, want %q", got, project.ID)
	}
	if got := intFromAny(payloads[0]["count"], 0); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
}

func TestClassifyAndExecuteSystemActionSyncProjectSkipsNonGitWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, _, err := app.createProject(projectCreateRequest{Name: "EUROfusion"})
	if err != nil {
		t.Fatalf("createProject() error: %v", err)
	}
	defaultProject, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord() error: %v", err)
	}
	session, err := app.chatSessionForProject(defaultProject)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	workspaceDir := filepath.Join(t.TempDir(), "proposal")
	workspace, err := app.store.CreateWorkspace("Proposal", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetWorkspaceProject(workspace.ID, &project.ID); err != nil {
		t.Fatalf("SetWorkspaceProject() error: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync EUROfusion")
	if !handled {
		t.Fatal("expected command to be handled")
	}
	if message != "Synced 0 of 1 workspace(s) for project EUROfusion." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "sync_project" {
		t.Fatalf("payloads = %#v", payloads)
	}
	results, ok := payloads[0]["results"].([]projectWorkspaceSyncResult)
	if !ok || len(results) != 1 {
		t.Fatalf("results payload = %#v", payloads[0]["results"])
	}
	if results[0].Status != "skipped" {
		t.Fatalf("sync status = %q, want skipped", results[0].Status)
	}
}

func TestClassifyAndExecuteSystemActionSyncProjectPullsGitWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, _, err := app.createProject(projectCreateRequest{Name: "EUROfusion"})
	if err != nil {
		t.Fatalf("createProject() error: %v", err)
	}
	defaultProject, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord() error: %v", err)
	}
	session, err := app.chatSessionForProject(defaultProject)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}

	sourceRepo := initGitTestRepo(t, "sync-source")
	cloneDir := filepath.Join(t.TempDir(), "sync-clone")
	if err := exec.Command("git", "clone", "file://"+filepath.ToSlash(sourceRepo), cloneDir).Run(); err != nil {
		t.Fatalf("git clone %s: %v", cloneDir, err)
	}
	workspace, err := app.store.CreateWorkspace("Proposal", cloneDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if _, err := app.store.SetWorkspaceProject(workspace.ID, &project.ID); err != nil {
		t.Fatalf("SetWorkspaceProject() error: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync EUROfusion")
	if !handled {
		t.Fatal("expected command to be handled")
	}
	if message != "Synced 1 of 1 workspace(s) for project EUROfusion." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "sync_project" {
		t.Fatalf("payloads = %#v", payloads)
	}
	results, ok := payloads[0]["results"].([]projectWorkspaceSyncResult)
	if !ok || len(results) != 1 {
		t.Fatalf("results payload = %#v", payloads[0]["results"])
	}
	if results[0].Status != "synced" {
		t.Fatalf("sync status = %q, want synced", results[0].Status)
	}
}
