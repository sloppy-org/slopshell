package web

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineGitHubIssueActions(t *testing.T) {
	t.Run("create issue with labels and assignees", func(t *testing.T) {
		actions := parseInlineGitHubIssueActions("Create a GitHub issue from this and label it bug, parser and assign it to @octocat and hubot.")
		if len(actions) != 1 {
			t.Fatalf("action count = %d, want 1", len(actions))
		}
		if actions[0].Action != "create_github_issue" {
			t.Fatalf("action = %q, want create_github_issue", actions[0].Action)
		}
		if got := systemActionStringListParam(actions[0].Params, "labels"); strings.Join(got, ",") != "bug,parser" {
			t.Fatalf("labels = %v, want [bug parser]", got)
		}
		if got := systemActionStringListParam(actions[0].Params, "assignees"); strings.Join(got, ",") != "octocat,hubot" {
			t.Fatalf("assignees = %v, want [octocat hubot]", got)
		}
	})

	t.Run("split into local items and github issue", func(t *testing.T) {
		actions := parseInlineGitHubIssueActions("Split this into local items and a GitHub issue.")
		if len(actions) != 2 {
			t.Fatalf("action count = %d, want 2", len(actions))
		}
		if actions[0].Action != "split_items" {
			t.Fatalf("first action = %q, want split_items", actions[0].Action)
		}
		if got := systemActionSplitCount(actions[0].Params); got != 0 {
			t.Fatalf("split count = %d, want 0", got)
		}
		if actions[1].Action != "create_github_issue" {
			t.Fatalf("second action = %q, want create_github_issue", actions[1].Action)
		}
	})
}

func TestClassifyAndExecuteSystemActionCreateGitHubIssuePromotesExistingItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	initGitHubWorkspaceRepo(t, project.RootPath, "https://github.com/owner/tabula.git")
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	assistantText := "Parser panic when config file is missing\n\n1. Reproduce with an empty config.\n2. Observe the nil pointer panic."
	if _, err := app.store.AddChatMessage(session.ID, "assistant", assistantText, assistantText, "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	_, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "make this an item")
	if !handled {
		t.Fatal("expected local item creation to be handled")
	}
	item := mustFirstItemByState(t, app, store.ItemStateInbox)
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id = %v, want %d", item.WorkspaceID, workspace.ID)
	}

	var calls [][]string
	app.ghCommandRunner = func(_ context.Context, cwd string, args ...string) (string, error) {
		calls = append(calls, append([]string{cwd}, args...))
		if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
			return "https://github.com/owner/tabula/issues/77\n", nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "view" {
			return `{"number":77,"title":"Parser panic when config file is missing","url":"https://github.com/owner/tabula/issues/77","state":"OPEN","labels":[{"name":"bug"},{"name":"parser"}],"assignees":[{"login":"octocat"}]}`, nil
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "Create a GitHub issue from this and label it bug, parser and assign it to @octocat.")
	if !handled {
		t.Fatal("expected GitHub issue action to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")
	if len(calls) != 0 {
		t.Fatalf("gh call count before confirm = %d, want 0", len(calls))
	}

	message, payloads, handled = confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected GitHub issue confirmation to be handled")
	}
	if message != "Created GitHub issue #77: https://github.com/owner/tabula/issues/77" {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "github_issue_created" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strings.TrimSpace(strFromAny(payloads[0]["source_ref"])); got != "owner/tabula#77" {
		t.Fatalf("source_ref payload = %q, want owner/tabula#77", got)
	}

	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updated.Source == nil || *updated.Source != "github" {
		t.Fatalf("updated.Source = %v, want github", updated.Source)
	}
	if updated.SourceRef == nil || *updated.SourceRef != "owner/tabula#77" {
		t.Fatalf("updated.SourceRef = %v, want owner/tabula#77", updated.SourceRef)
	}
	if updated.ArtifactID == nil {
		t.Fatal("expected linked artifact")
	}
	artifact, err := app.store.GetArtifact(*updated.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if artifact.Kind != store.ArtifactKindGitHubIssue {
		t.Fatalf("artifact kind = %q, want %q", artifact.Kind, store.ArtifactKindGitHubIssue)
	}
	if artifact.RefURL == nil || *artifact.RefURL != "https://github.com/owner/tabula/issues/77" {
		t.Fatalf("artifact.RefURL = %v, want issue URL", artifact.RefURL)
	}
	if artifact.MetaJSON == nil {
		t.Fatal("expected artifact meta_json")
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(*artifact.MetaJSON), &meta); err != nil {
		t.Fatalf("unmarshal meta_json: %v", err)
	}
	if meta["owner_repo"] != "owner/tabula" {
		t.Fatalf("owner_repo = %v, want owner/tabula", meta["owner_repo"])
	}

	if len(calls) != 2 {
		t.Fatalf("gh call count = %d, want 2", len(calls))
	}
	createCall := strings.Join(calls[0][1:], " ")
	if calls[0][0] != project.RootPath {
		t.Fatalf("gh create cwd = %q, want %q", calls[0][0], project.RootPath)
	}
	for _, needle := range []string{
		"issue create",
		"--title Parser panic when config file is missing",
		"--label bug",
		"--label parser",
		"--assignee octocat",
	} {
		if !strings.Contains(createCall, needle) {
			t.Fatalf("create call = %q, missing %q", createCall, needle)
		}
	}
	if !strings.Contains(createCall, "Reproduce with an empty config.") {
		t.Fatalf("create call body = %q, want assistant text", createCall)
	}
}

func TestClassifyAndExecuteSystemActionCreateGitHubIssueCreatesLinkedItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	initGitHubWorkspaceRepo(t, project.RootPath, "https://github.com/owner/tabula.git")
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	assistantText := "Document the workspace reassignment flow"
	if _, err := app.store.AddChatMessage(session.ID, "assistant", assistantText, assistantText, "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	app.ghCommandRunner = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
			return "https://github.com/owner/tabula/issues/91\n", nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "view" {
			return `{"number":91,"title":"Document the workspace reassignment flow","url":"https://github.com/owner/tabula/issues/91","state":"OPEN","labels":[],"assignees":[]}`, nil
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "file this as an issue")
	if !handled {
		t.Fatal("expected GitHub issue creation to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")
	message, payloads, handled = confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected GitHub issue confirmation to be handled")
	}
	if message != "Created GitHub issue #91: https://github.com/owner/tabula/issues/91" {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "github_issue_created" {
		t.Fatalf("payloads = %#v", payloads)
	}

	item := mustFirstItemByState(t, app, store.ItemStateInbox)
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id = %v, want %d", item.WorkspaceID, workspace.ID)
	}
	if item.Source == nil || *item.Source != "github" {
		t.Fatalf("item.Source = %v, want github", item.Source)
	}
	if item.SourceRef == nil || *item.SourceRef != "owner/tabula#91" {
		t.Fatalf("item.SourceRef = %v, want owner/tabula#91", item.SourceRef)
	}
}

func TestClassifyAndExecuteSystemActionCreateGitHubIssueRejectsMissingWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	assistantText := "Follow up on the MCP control plane"
	if _, err := app.store.AddChatMessage(session.ID, "assistant", assistantText, assistantText, "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create a GitHub issue from this")
	if !handled {
		t.Fatal("expected missing-workspace command to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")
	message, payloads, handled = confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected missing-workspace confirmation to be handled")
	}
	if len(payloads) != 0 {
		t.Fatalf("payloads = %#v, want none", payloads)
	}
	requireConfirmationFailureMessage(t, message, "workspace has no GitHub origin remote")
}

func TestClassifyAndExecuteSystemActionCreateGitHubIssueRejectsMissingRemote(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	assistantText := "Write down the iPhone HTTPS capture steps"
	if _, err := app.store.AddChatMessage(session.ID, "assistant", assistantText, assistantText, "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create a GitHub issue from this")
	if !handled {
		t.Fatal("expected missing-remote command to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")
	message, payloads, handled = confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected missing-remote confirmation to be handled")
	}
	if len(payloads) != 0 {
		t.Fatalf("payloads = %#v, want none", payloads)
	}
	requireConfirmationFailureMessage(t, message, "workspace has no GitHub origin remote")
	items, err := app.store.ListItemsByState(store.ItemStateInbox)
	if err != nil {
		t.Fatalf("ListItemsByState(inbox) error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("inbox item count = %d, want 0", len(items))
	}
	if workspace.ID == 0 {
		t.Fatal("expected workspace ID")
	}
}

func TestClassifyAndExecuteSystemActionCreateGitHubIssueSurfacesCreateFailure(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	initGitHubWorkspaceRepo(t, project.RootPath, "https://github.com/owner/tabula.git")
	if _, err := app.store.CreateWorkspace("Default", project.RootPath); err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	assistantText := "Ship the local intent classifier defaults"
	if _, err := app.store.AddChatMessage(session.ID, "assistant", assistantText, assistantText, "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}
	app.ghCommandRunner = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
			return "", errors.New("gh issue create failed: permission denied")
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create a GitHub issue from this")
	if !handled {
		t.Fatal("expected create failure to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")
	message, payloads, handled = confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected create-failure confirmation to be handled")
	}
	if len(payloads) != 0 {
		t.Fatalf("payloads = %#v, want none", payloads)
	}
	requireConfirmationFailureMessage(t, message, "gh issue create failed: permission denied")
}

func TestClassifyAndExecuteSystemActionCreateGitHubIssueRejectsDuplicateLinkedItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	initGitHubWorkspaceRepo(t, project.RootPath, "https://github.com/owner/tabula.git")
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	assistantText := "Parser panic when config file is missing\n\n1. Reproduce with an empty config.\n2. Observe the nil pointer panic."
	if _, err := app.store.AddChatMessage(session.ID, "assistant", assistantText, assistantText, "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	source := "github"
	sourceRef := "owner/tabula#77"
	linkedItem, err := app.store.CreateItem("Parser panic when config file is missing", store.ItemOptions{
		WorkspaceID: &workspace.ID,
		Source:      &source,
		SourceRef:   &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(linked) error: %v", err)
	}
	duplicateItem, err := app.store.CreateItem("Parser panic when config file is missing", store.ItemOptions{
		WorkspaceID: &workspace.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem(duplicate) error: %v", err)
	}

	ghCalls := 0
	app.ghCommandRunner = func(_ context.Context, _ string, args ...string) (string, error) {
		ghCalls++
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create a GitHub issue from this")
	if !handled {
		t.Fatal("expected duplicate command to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")
	if ghCalls != 0 {
		t.Fatalf("gh call count before confirm = %d, want 0", ghCalls)
	}
	message, payloads, handled = confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected duplicate confirmation to be handled")
	}
	if ghCalls != 0 {
		t.Fatalf("gh call count = %d, want 0", ghCalls)
	}
	if len(payloads) != 0 {
		t.Fatalf("payloads = %#v, want none", payloads)
	}
	requireConfirmationFailureMessage(t, message, "item is already linked to github owner/tabula#77")

	gotLinked, err := app.store.GetItem(linkedItem.ID)
	if err != nil {
		t.Fatalf("GetItem(linked) error: %v", err)
	}
	if gotLinked.SourceRef == nil || *gotLinked.SourceRef != sourceRef {
		t.Fatalf("linked source_ref = %v, want %q", gotLinked.SourceRef, sourceRef)
	}
	gotDuplicate, err := app.store.GetItem(duplicateItem.ID)
	if err != nil {
		t.Fatalf("GetItem(duplicate) error: %v", err)
	}
	if gotDuplicate.SourceRef != nil {
		t.Fatalf("duplicate source_ref = %v, want nil", gotDuplicate.SourceRef)
	}
	items, err := app.store.ListItemsByState(store.ItemStateInbox)
	if err != nil {
		t.Fatalf("ListItemsByState(inbox) error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListItemsByState(inbox) len = %d, want 2", len(items))
	}
}
