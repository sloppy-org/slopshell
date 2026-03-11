package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineItemReassignmentIntent(t *testing.T) {
	cases := []struct {
		text       string
		wantAction string
		wantTarget string
	}{
		{text: "move this to beta workspace", wantAction: "reassign_workspace", wantTarget: "beta"},
		{text: "assign to ~/write/paper-x", wantAction: "reassign_workspace", wantTarget: "~/write/paper-x"},
		{text: "assign to EUROfusion project", wantAction: "reassign_project", wantTarget: "EUROfusion"},
		{text: "this belongs to tabura", wantAction: "reassign_project", wantTarget: "tabura"},
		{text: "remove workspace from this item", wantAction: "clear_workspace"},
		{text: "remove project from this item", wantAction: "clear_project"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			action := parseInlineItemReassignmentIntent(tc.text)
			if action == nil {
				t.Fatal("expected item reassignment action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if tc.wantTarget != "" && systemActionAssignmentTarget(action.Params) != tc.wantTarget {
				t.Fatalf("target = %q, want %q", systemActionAssignmentTarget(action.Params), tc.wantTarget)
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionItemReassignment(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	alpha, err := app.store.CreateWorkspace("Alpha", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha) error: %v", err)
	}
	beta, err := app.store.CreateWorkspace("Beta", filepath.Join(t.TempDir(), "beta"))
	if err != nil {
		t.Fatalf("CreateWorkspace(beta) error: %v", err)
	}
	taburaProject, err := app.store.CreateProject("Tabura", "tabura", filepath.Join(t.TempDir(), "tabura-project"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	readmePath := filepath.Join(project.RootPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	title := "README.md"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &readmePath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Review the README cleanup plan", store.ItemOptions{
		WorkspaceID: &alpha.ID,
		ArtifactID:  &artifact.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: "README.md",
		artifactKind:  "text_artifact",
		artifactText:  "# notes",
	}
	server := mock.setupServer(t)
	defer server.Close()
	port := serverPort(t, server.Listener.Addr())
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "move this to Beta workspace")
	if !handled {
		t.Fatal("expected workspace reassignment command to be handled")
	}
	if message == "" || payloads == nil {
		t.Fatalf("unexpected workspace reassignment result: %q %#v", message, payloads)
	}
	updatedItem, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(reassigned workspace) error: %v", err)
	}
	if updatedItem.WorkspaceID == nil || *updatedItem.WorkspaceID != beta.ID {
		t.Fatalf("workspace_id = %v, want %d", updatedItem.WorkspaceID, beta.ID)
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "this belongs to Tabura")
	if !handled {
		t.Fatal("expected project reassignment command to be handled")
	}
	if message == "" || payloads == nil {
		t.Fatalf("unexpected project reassignment result: %q %#v", message, payloads)
	}
	updatedItem, err = app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(reassigned project) error: %v", err)
	}
	if updatedItem.ProjectID == nil || *updatedItem.ProjectID != taburaProject.ID {
		t.Fatalf("project_id = %v, want %q", updatedItem.ProjectID, taburaProject.ID)
	}

	message, _, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "remove workspace from this item")
	if !handled {
		t.Fatal("expected clear workspace command to be handled")
	}
	if message == "" {
		t.Fatal("expected clear workspace confirmation message")
	}
	updatedItem, err = app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(cleared workspace) error: %v", err)
	}
	if updatedItem.WorkspaceID != nil {
		t.Fatalf("workspace_id = %v, want nil", updatedItem.WorkspaceID)
	}
}
