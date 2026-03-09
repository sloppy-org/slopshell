package web

import (
	"context"
	"os"
	"os/exec"
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
		wantRepoURL   string
		wantTarget    string
		wantNewName   string
		wantScratch   bool
		wantAll       bool
	}{
		{text: "open workspace Alpha", wantAction: "switch_workspace", wantWorkspace: "Alpha"},
		{text: "switch to workspace Beta", wantAction: "switch_workspace", wantWorkspace: "Beta"},
		{text: "switch to ./repo", wantAction: "switch_workspace", wantWorkspace: "./repo"},
		{text: "show items here", wantAction: "list_workspace_items"},
		{text: "what's open", wantAction: "list_workspace_items"},
		{text: "list workspaces", wantAction: "list_workspaces"},
		{text: "show all workspaces", wantAction: "list_workspaces", wantAll: true},
		{text: "watch this workspace", wantAction: "workspace_watch_start"},
		{text: "stop watching", wantAction: "workspace_watch_stop"},
		{text: "watch status", wantAction: "workspace_watch_status"},
		{text: "create workspace ./notes", wantAction: "create_workspace", wantWorkspace: "./notes"},
		{text: "create scratch workspace", wantAction: "create_workspace", wantScratch: true},
		{text: "rename workspace Alpha to Beta", wantAction: "rename_workspace", wantWorkspace: "Alpha", wantNewName: "Beta"},
		{text: "delete workspace Alpha", wantAction: "delete_workspace", wantWorkspace: "Alpha"},
		{text: "show workspace details for Alpha", wantAction: "show_workspace_details", wantWorkspace: "Alpha"},
		{text: "create workspace from git@github.com:user/repo.git", wantAction: "create_workspace_from_git", wantRepoURL: "git@github.com:user/repo.git"},
		{text: "create workspace from https://gitlab.example.com/user/data-repo.git to ~/write/proposal", wantAction: "create_workspace_from_git", wantRepoURL: "https://gitlab.example.com/user/data-repo.git", wantTarget: "~/write/proposal"},
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
			if got := systemActionWorkspaceNewName(action.Params); got != tc.wantNewName {
				t.Fatalf("new_name = %q, want %q", got, tc.wantNewName)
			}
			if got := systemActionGitRepoURL(action.Params); got != tc.wantRepoURL {
				t.Fatalf("repo_url = %q, want %q", got, tc.wantRepoURL)
			}
			if got := systemActionGitTargetPath(action.Params); tc.wantAction == "create_workspace_from_git" && got != tc.wantTarget {
				t.Fatalf("target_path = %q, want %q", got, tc.wantTarget)
			}
			if got := systemActionBoolParam(action.Params, "scratch"); got != tc.wantScratch {
				t.Fatalf("scratch = %v, want %v", got, tc.wantScratch)
			}
			if got := systemActionTruthyParam(action.Params, "all_spheres"); got != tc.wantAll {
				t.Fatalf("all_spheres = %v, want %v", got, tc.wantAll)
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

func TestClassifyAndExecuteSystemActionCreateWorkspaceFromGit(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	cloneRoot := filepath.Join(t.TempDir(), "code")
	t.Setenv("TABURA_WORKSPACE_CLONE_ROOT", cloneRoot)

	sourceRepo := initGitTestRepo(t, "example-workspace")
	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	command := "create workspace from file://" + filepath.ToSlash(sourceRepo)
	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, command)
	if !handled {
		t.Fatal("expected create workspace from git command to be handled")
	}
	targetDir := filepath.Join(cloneRoot, "example-workspace")
	if message != "Created workspace example-workspace at "+targetDir+"." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "create_workspace_from_git" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["dir_path"]); got != targetDir {
		t.Fatalf("dir_path = %q, want %q", got, targetDir)
	}

	workspace, err := app.store.GetWorkspace(int64FromAny(payloads[0]["workspace_id"]))
	if err != nil {
		t.Fatalf("GetWorkspace() error: %v", err)
	}
	if workspace.Name != "example-workspace" {
		t.Fatalf("workspace name = %q, want %q", workspace.Name, "example-workspace")
	}
	if !workspace.IsActive {
		t.Fatal("expected cloned workspace to be active")
	}
	if workspace.DirPath != targetDir {
		t.Fatalf("workspace dir_path = %q, want %q", workspace.DirPath, targetDir)
	}
	if _, err := os.Stat(filepath.Join(targetDir, ".git")); err != nil {
		t.Fatalf("clone missing .git directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, ".tabura", "codex-mcp.toml")); err != nil {
		t.Fatalf("bootstrap missing codex-mcp.toml: %v", err)
	}
	gitignoreBody, err := os.ReadFile(filepath.Join(targetDir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(gitignoreBody), ".tabura/artifacts/") {
		t.Fatalf(".gitignore = %q, want .tabura/artifacts entry", string(gitignoreBody))
	}
	readmeBody, err := os.ReadFile(filepath.Join(targetDir, "README.md"))
	if err != nil {
		t.Fatalf("read cloned README: %v", err)
	}
	if strings.TrimSpace(string(readmeBody)) != "# example-workspace" {
		t.Fatalf("README = %q", string(readmeBody))
	}
}

func TestClassifyAndExecuteSystemActionListWorkspacesUsesActiveSphereByDefault(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	privateWorkspace, err := app.store.CreateWorkspace("Private", filepath.Join(t.TempDir(), "private"), store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace(private) error: %v", err)
	}
	workWorkspace, err := app.store.CreateWorkspace("Work", filepath.Join(t.TempDir(), "work"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(work) error: %v", err)
	}
	if err := app.store.SetActiveSphere(store.SpherePrivate); err != nil {
		t.Fatalf("SetActiveSphere() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "list workspaces")
	if !handled {
		t.Fatal("expected list workspaces command to be handled")
	}
	if !strings.Contains(message, "Workspaces in private sphere:") {
		t.Fatalf("message = %q", message)
	}
	if !strings.Contains(message, "Private —") || strings.Contains(message, "Work —") {
		t.Fatalf("message should stay in active sphere: %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "list_workspaces" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["sphere"]); got != store.SpherePrivate {
		t.Fatalf("payload sphere = %q, want %q", got, store.SpherePrivate)
	}
	workspaces, ok := payloads[0]["workspaces"].([]map[string]interface{})
	if !ok {
		t.Fatalf("workspaces payload = %#v", payloads[0]["workspaces"])
	}
	if len(workspaces) != 1 || int64FromAny(workspaces[0]["workspace_id"]) != privateWorkspace.ID {
		t.Fatalf("workspaces payload = %#v, want only private workspace", workspaces)
	}
	if int64FromAny(payloads[0]["workspace_count"]) != 1 {
		t.Fatalf("workspace_count = %v, want 1", payloads[0]["workspace_count"])
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show all workspaces")
	if !handled {
		t.Fatal("expected all-spheres workspace command to be handled")
	}
	if !strings.Contains(message, "Workspaces:") {
		t.Fatalf("message = %q", message)
	}
	if !strings.Contains(message, "Private") || !strings.Contains(message, "Work") {
		t.Fatalf("message should include both spheres: %q", message)
	}
	if len(payloads) != 1 || !boolFromAny(payloads[0]["all_spheres"]) {
		t.Fatalf("payloads = %#v", payloads)
	}
	workspacesAny, ok := payloads[0]["workspaces"].([]map[string]interface{})
	if !ok {
		t.Fatalf("workspaces payload = %#v", payloads[0]["workspaces"])
	}
	if len(workspacesAny) != 2 {
		t.Fatalf("workspaces payload len = %d, want 2", len(workspacesAny))
	}
	if int64FromAny(workspacesAny[1]["workspace_id"]) != workWorkspace.ID && int64FromAny(workspacesAny[0]["workspace_id"]) != workWorkspace.ID {
		t.Fatalf("workspaces payload missing work workspace: %#v", workspacesAny)
	}
}

func TestClassifyAndExecuteSystemActionWorkspaceManagement(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create workspace ./notes")
	if !handled {
		t.Fatal("expected create workspace command to be handled")
	}
	expectedDir := filepath.Join(hub.RootPath, "notes")
	if message != "Created workspace notes at "+expectedDir+"." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "create_workspace" {
		t.Fatalf("payloads = %#v", payloads)
	}
	workspaceID := int64FromAny(payloads[0]["workspace_id"])
	workspace, err := app.store.GetWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace(notes) error: %v", err)
	}
	if workspace.Name != "notes" {
		t.Fatalf("workspace.Name = %q, want %q", workspace.Name, "notes")
	}
	if workspace.DirPath != expectedDir {
		t.Fatalf("workspace.DirPath = %q, want %q", workspace.DirPath, expectedDir)
	}
	if !workspace.IsActive {
		t.Fatal("expected created workspace to be active")
	}
	if _, err := os.Stat(expectedDir); err != nil {
		t.Fatalf("expected workspace directory to exist: %v", err)
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create scratch workspace research lab")
	if !handled {
		t.Fatal("expected scratch workspace command to be handled")
	}
	if !strings.Contains(message, "Created scratch workspace research lab at ") {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "create_scratch_workspace" {
		t.Fatalf("scratch payloads = %#v", payloads)
	}
	scratchDir := strFromAny(payloads[0]["dir_path"])
	if !strings.Contains(filepath.ToSlash(scratchDir), "/.tabura/artifacts/tmp/") {
		t.Fatalf("scratch dir = %q, want .tabura/artifacts/tmp path", scratchDir)
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "list workspaces")
	if !handled {
		t.Fatal("expected list workspaces command to be handled")
	}
	if !strings.Contains(message, "Workspaces in private sphere:") || !strings.Contains(message, "research lab") || !strings.Contains(message, "notes") {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "list_workspaces" {
		t.Fatalf("list payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["sphere"]); got != store.SpherePrivate {
		t.Fatalf("payload sphere = %q, want %q", got, store.SpherePrivate)
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show workspace details for notes")
	if !handled {
		t.Fatal("expected show workspace details command to be handled")
	}
	if !strings.Contains(message, "Workspace notes") || !strings.Contains(message, "- Path: "+expectedDir) {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "show_workspace_details" {
		t.Fatalf("details payloads = %#v", payloads)
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "rename workspace notes to Research Notes")
	if !handled {
		t.Fatal("expected rename workspace command to be handled")
	}
	if message != "Renamed workspace notes to Research Notes." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "rename_workspace" {
		t.Fatalf("rename payloads = %#v", payloads)
	}
	renamed, err := app.store.GetWorkspace(workspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace(renamed) error: %v", err)
	}
	if renamed.Name != "Research Notes" {
		t.Fatalf("renamed.Name = %q, want %q", renamed.Name, "Research Notes")
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "delete workspace Research Notes")
	if !handled {
		t.Fatal("expected delete workspace command to be handled")
	}
	if message != "Deleted workspace Research Notes." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "delete_workspace" {
		t.Fatalf("delete payloads = %#v", payloads)
	}
	if _, err := app.store.GetWorkspace(workspaceID); err == nil {
		t.Fatal("expected deleted workspace to be gone")
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
	if err := app.store.SetActiveSphere(store.SphereWork); err != nil {
		t.Fatalf("SetActiveSphere() error: %v", err)
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
	if !strings.Contains(prompt, "Active sphere: work") {
		t.Fatalf("prompt missing active sphere line: %q", prompt)
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

func initGitTestRepo(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
		}
	}
	runGit("init", root)
	runGit("-C", root, "add", "README.md")
	runGit("-C", root, "-c", "user.name=Tabura Test", "-c", "user.email=test@example.com", "commit", "-m", "init")
	return root
}

func int64FromAny(v any) int64 {
	switch value := v.(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	default:
		return 0
	}
}
