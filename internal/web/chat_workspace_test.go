package web

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineWorkspaceIntent(t *testing.T) {
	cases := []struct {
		text          string
		wantAction    string
		wantWorkspace string
	}{
		{text: "open workspace Alpha", wantAction: "switch_workspace", wantWorkspace: "Alpha"},
		{text: "switch to workspace Beta", wantAction: "switch_workspace", wantWorkspace: "Beta"},
		{text: "switch to ./repo", wantAction: "switch_workspace", wantWorkspace: "./repo"},
		{text: "show items here", wantAction: "list_workspace_items"},
		{text: "what's open", wantAction: "list_workspace_items"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			action := parseInlineWorkspaceIntent(tc.text)
			if action == nil {
				t.Fatal("expected workspace action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if got := systemActionWorkspaceRef(action.Params); got != tc.wantWorkspace {
				t.Fatalf("workspace ref = %q, want %q", got, tc.wantWorkspace)
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionSwitchWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	alphaPath := filepath.Join(t.TempDir(), "alpha")
	betaPath := filepath.Join(t.TempDir(), "beta")
	alpha, err := app.store.CreateWorkspace("Alpha", alphaPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha) error: %v", err)
	}
	beta, err := app.store.CreateWorkspace("Beta", betaPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(beta) error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(alpha.ID); err != nil {
		t.Fatalf("SetActiveWorkspace(alpha) error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "switch to "+betaPath)
	if !handled {
		t.Fatal("expected switch workspace command to be handled")
	}
	if message != "Switched to workspace Beta." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "switch_workspace" {
		t.Fatalf("payloads = %#v", payloads)
	}

	updated, err := app.store.GetWorkspace(beta.ID)
	if err != nil {
		t.Fatalf("GetWorkspace(beta) error: %v", err)
	}
	if !updated.IsActive {
		t.Fatal("expected beta workspace to be active")
	}
}

func TestClassifyAndExecuteSystemActionListWorkspaceItemsUsesActiveWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	alpha, err := app.store.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"))
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha) error: %v", err)
	}
	beta, err := app.store.CreateWorkspace("Beta", filepath.Join(t.TempDir(), "beta"))
	if err != nil {
		t.Fatalf("CreateWorkspace(beta) error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(beta.ID); err != nil {
		t.Fatalf("SetActiveWorkspace(beta) error: %v", err)
	}
	if _, err := app.store.CreateItem("Review parser plan", store.ItemOptions{WorkspaceID: &beta.ID}); err != nil {
		t.Fatalf("CreateItem(beta inbox) error: %v", err)
	}
	if _, err := app.store.CreateItem("Follow up on review", store.ItemOptions{
		WorkspaceID: &beta.ID,
		State:       store.ItemStateWaiting,
	}); err != nil {
		t.Fatalf("CreateItem(beta waiting) error: %v", err)
	}
	if _, err := app.store.CreateItem("Closed beta item", store.ItemOptions{
		WorkspaceID: &beta.ID,
		State:       store.ItemStateDone,
	}); err != nil {
		t.Fatalf("CreateItem(beta done) error: %v", err)
	}
	if _, err := app.store.CreateItem("Alpha stray item", store.ItemOptions{WorkspaceID: &alpha.ID}); err != nil {
		t.Fatalf("CreateItem(alpha) error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show items here")
	if !handled {
		t.Fatal("expected list workspace items command to be handled")
	}
	if !strings.Contains(message, "Open items for workspace Beta:") {
		t.Fatalf("message = %q", message)
	}
	if !strings.Contains(message, "Review parser plan [inbox]") {
		t.Fatalf("message missing inbox item: %q", message)
	}
	if !strings.Contains(message, "Follow up on review [waiting]") {
		t.Fatalf("message missing waiting item: %q", message)
	}
	if strings.Contains(message, "Closed beta item") || strings.Contains(message, "Alpha stray item") {
		t.Fatalf("message should exclude non-open or other-workspace items: %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "list_workspace_items" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := intFromAny(payloads[0]["item_count"], 0); got != 2 {
		t.Fatalf("item_count = %d, want 2", got)
	}
}

func TestApplyWorkspacePromptContextIncludesActiveWorkspaceSummary(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	if _, err := app.store.CreateItem("Visible item", store.ItemOptions{WorkspaceID: &workspace.ID}); err != nil {
		t.Fatalf("CreateItem(visible) error: %v", err)
	}
	if _, err := app.store.CreateItem("Done item", store.ItemOptions{
		WorkspaceID: &workspace.ID,
		State:       store.ItemStateDone,
	}); err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}

	prompt := app.applyWorkspacePromptContext(project.ProjectKey, "Conversation transcript:\nUSER:\nhello")
	if !strings.Contains(prompt, "## Workspace Context") {
		t.Fatalf("prompt missing workspace section: %q", prompt)
	}
	if !strings.Contains(prompt, "Active workspace: Default ("+project.RootPath+")") {
		t.Fatalf("prompt missing active workspace line: %q", prompt)
	}
	if !strings.Contains(prompt, "Open items in this workspace: 1") {
		t.Fatalf("prompt missing open item count: %q", prompt)
	}
	if !strings.Contains(prompt, "Conversation transcript:\nUSER:\nhello") {
		t.Fatalf("prompt missing original content: %q", prompt)
	}
}
