package web

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func seedActiveIdeaNote(
	t *testing.T,
	app *App,
	notes []string,
	refinements []ideaNoteRefinement,
	withWorkspace bool,
) (store.Project, store.ChatSession, store.Item, store.Artifact, *canvasMCPMock) {
	t.Helper()

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	var workspaceID *int64
	workspaceName := ""
	if withWorkspace {
		workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
		if err != nil {
			t.Fatalf("CreateWorkspace() error: %v", err)
		}
		workspaceID = &workspace.ID
		workspaceName = workspace.Name
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	title := "Parser cleanup plan"
	metaJSON, err := encodeIdeaNoteMeta(ideaNoteMeta{
		Title:       title,
		Transcript:  strings.Join(notes, " "),
		CaptureMode: chatCaptureModeText,
		CapturedAt:  "2026-03-08T09:40:00Z",
		Workspace:   workspaceName,
		Notes:       notes,
		Refinements: refinements,
	})
	if err != nil {
		t.Fatalf("encodeIdeaNoteMeta() error: %v", err)
	}
	artifact, err := app.store.CreateArtifact(store.ArtifactKindIdeaNote, nil, nil, &title, metaJSON)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	source := "idea"
	item, err := app.store.CreateItem(title, store.ItemOptions{
		WorkspaceID: workspaceID,
		ArtifactID:  &artifact.ID,
		Source:      &source,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: title,
		artifactKind:  "text_artifact",
		artifactText:  renderIdeaNoteMarkdown(parseIdeaNoteMeta(artifact.MetaJSON, title)),
	}
	server := mock.setupServer(t)
	t.Cleanup(server.Close)
	port := serverPort(t, server.Listener.Addr())
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	return project, session, item, artifact, mock
}

func TestParseInlineItemIntentIdeaPromotion(t *testing.T) {
	now := timeStub()
	cases := []struct {
		text            string
		wantAction      string
		wantTarget      string
		wantDisposition string
		wantSelected    []int
	}{
		{text: "make this actionable", wantAction: canonicalActionDispatchExecute, wantTarget: ideaPromotionTargetTask},
		{text: "Mach diese Idee umsetzbar", wantAction: canonicalActionDispatchExecute, wantTarget: ideaPromotionTargetTask},
		{text: "turn this idea into tasks", wantAction: canonicalActionDispatchExecute, wantTarget: ideaPromotionTargetItems},
		{text: "Wandle diese Idee in Aufgaben um", wantAction: canonicalActionDispatchExecute, wantTarget: ideaPromotionTargetItems},
		{text: "create this idea GitHub issue", wantAction: canonicalActionDispatchExecute, wantTarget: ideaPromotionTargetGitHub, wantDisposition: ideaPromotionDispositionKeep},
		{text: "Erstelle diese Idee GitHub Issue", wantAction: canonicalActionDispatchExecute, wantTarget: ideaPromotionTargetGitHub, wantDisposition: ideaPromotionDispositionKeep},
		{text: "create selected idea items 1, 3 and mark this idea done", wantAction: canonicalActionDispatchExecute, wantTarget: ideaPromotionTargetItems, wantDisposition: ideaPromotionDispositionDone, wantSelected: []int{1, 3}},
		{text: "Erstelle ausgewählte Idee Items 1 und 3 und markiere diese Idee als erledigt", wantAction: canonicalActionDispatchExecute, wantTarget: ideaPromotionTargetItems, wantDisposition: ideaPromotionDispositionDone, wantSelected: []int{1, 3}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			action := parseInlineItemIntent(tc.text, now)
			if action == nil {
				t.Fatal("expected inline idea promotion action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if got := normalizeIdeaPromotionTarget(systemActionStringParam(action.Params, "promotion_target")); got != tc.wantTarget {
				t.Fatalf("target = %q, want %q", got, tc.wantTarget)
			}
			if tc.wantDisposition != "" {
				if got := normalizeIdeaPromotionDisposition(systemActionStringParam(action.Params, "disposition")); got != tc.wantDisposition {
					t.Fatalf("disposition = %q, want %q", got, tc.wantDisposition)
				}
			}
			if len(tc.wantSelected) > 0 {
				got := ideaPromotionSelectionFromParams(action.Params)
				if len(got) != len(tc.wantSelected) {
					t.Fatalf("selected len = %d, want %d (%v)", len(got), len(tc.wantSelected), got)
				}
				for i, want := range tc.wantSelected {
					if got[i] != want {
						t.Fatalf("selected[%d] = %d, want %d", i, got[i], want)
					}
				}
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionPreviewIdeaPromotionRendersReview(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	_, session, _, artifact, mock := seedActiveIdeaNote(t, app, []string{
		"Draft the rollout checklist",
		"Add regression coverage",
		"File the cleanup follow-up",
	}, nil, true)

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "turn this idea into tasks")
	if !handled {
		t.Fatal("expected idea promotion preview to be handled")
	}
	if message != `Drafted idea item proposals on canvas. Say "create these idea items" or "create selected idea items 1,2" to confirm.` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "idea_promotion_preview" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := intFromAny(payloads[0]["candidate_count"], 0); got != 3 {
		t.Fatalf("candidate_count = %d, want 3", got)
	}

	updatedArtifact, err := app.store.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	meta := parseIdeaNoteMeta(updatedArtifact.MetaJSON, ideaNoteString(updatedArtifact.Title))
	if meta.PromotionPreview == nil {
		t.Fatal("expected promotion preview in idea note meta")
	}
	if meta.PromotionPreview.Target != ideaPromotionTargetItems {
		t.Fatalf("preview target = %q, want %q", meta.PromotionPreview.Target, ideaPromotionTargetItems)
	}
	if len(meta.PromotionPreview.Candidates) != 3 {
		t.Fatalf("candidate len = %d, want 3", len(meta.PromotionPreview.Candidates))
	}
	if !containsAll(mock.lastShownContent, "## Promotion Review", "### 1. Draft the rollout checklist", "### 2. Add regression coverage") {
		t.Fatalf("canvas content = %q", mock.lastShownContent)
	}
}

func TestClassifyAndExecuteSystemActionApplyIdeaPromotionCreatesItemsAndMarksDone(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	_, session, ideaItem, artifact, _ := seedActiveIdeaNote(t, app, []string{
		"Draft the rollout checklist",
		"Add regression coverage",
		"File the cleanup follow-up",
	}, nil, true)

	if _, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "turn this idea into tasks"); !handled {
		t.Fatal("expected preview command to be handled")
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create selected idea items 1, 2 and mark this idea done")
	if !handled {
		t.Fatal("expected promotion apply command to be handled")
	}
	if message != "Created 2 items from the idea." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "idea_promotion_applied" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["idea_state"]); got != store.ItemStateDone {
		t.Fatalf("idea_state = %q, want done", got)
	}

	inboxItems, err := app.store.ListItemsByState(store.ItemStateInbox)
	if err != nil {
		t.Fatalf("ListItemsByState(inbox) error: %v", err)
	}
	if len(inboxItems) != 2 {
		t.Fatalf("inbox item count = %d, want 2", len(inboxItems))
	}
	for _, item := range inboxItems {
		if item.ArtifactID == nil || *item.ArtifactID != artifact.ID {
			t.Fatalf("artifact_id = %v, want %d", item.ArtifactID, artifact.ID)
		}
		if item.Source == nil || *item.Source != "idea" {
			t.Fatalf("item.Source = %v, want idea", item.Source)
		}
		if item.SourceRef == nil || !strings.HasPrefix(*item.SourceRef, ideaPromotionSourceRef(ideaItem.ID, 1)[:len(fmt.Sprintf("idea-%d:", ideaItem.ID))]) {
			t.Fatalf("item.SourceRef = %v, want idea source ref prefix", item.SourceRef)
		}
	}

	updatedIdea, err := app.store.GetItem(ideaItem.ID)
	if err != nil {
		t.Fatalf("GetItem(idea) error: %v", err)
	}
	if updatedIdea.State != store.ItemStateDone {
		t.Fatalf("idea state = %q, want done", updatedIdea.State)
	}

	updatedArtifact, err := app.store.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	meta := parseIdeaNoteMeta(updatedArtifact.MetaJSON, ideaNoteString(updatedArtifact.Title))
	if meta.PromotionPreview != nil {
		t.Fatalf("promotion preview = %#v, want nil", meta.PromotionPreview)
	}
	if len(meta.Promotions) != 1 {
		t.Fatalf("promotion history len = %d, want 1", len(meta.Promotions))
	}
	if meta.Promotions[0].Target != ideaPromotionTargetItems {
		t.Fatalf("promotion target = %q, want items", meta.Promotions[0].Target)
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "make this actionable")
	if !handled {
		t.Fatal("expected already-promoted idea to be handled")
	}
	if len(payloads) != 0 {
		t.Fatalf("payloads = %#v, want none", payloads)
	}
	if message != "I couldn't prepare the idea promotion: idea has already been promoted" {
		t.Fatalf("message = %q", message)
	}
}

func TestClassifyAndExecuteSystemActionApplyIdeaPromotionCreatesGitHubIssue(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, session, ideaItem, _, _ := seedActiveIdeaNote(t, app, []string{
		"Draft the rollout checklist",
		"Add regression coverage",
	}, []ideaNoteRefinement{{
		Kind:    "implementation",
		Heading: "Implementation Outline",
		Body:    "1. Build the parser patch.\n2. Add coverage for the missing case.",
	}}, true)
	initGitHubWorkspaceRepo(t, project.RootPath, "https://github.com/owner/tabula.git")

	var calls [][]string
	app.ghCommandRunner = func(_ context.Context, _ string, args ...string) (string, error) {
		calls = append(calls, append([]string{}, args...))
		if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
			return "https://github.com/owner/tabula/issues/88\n", nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "view" {
			return `{"number":88,"title":"Parser cleanup plan","url":"https://github.com/owner/tabula/issues/88","state":"OPEN","labels":[],"assignees":[]}`, nil
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	if _, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create a GitHub issue from this idea"); !handled {
		t.Fatal("expected GitHub preview to be handled")
	}
	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "create this idea GitHub issue and keep this idea")
	if !handled {
		t.Fatal("expected GitHub apply to be handled")
	}
	if message != "Created GitHub issue #88 from the idea: https://github.com/owner/tabula/issues/88" {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "idea_promotion_applied" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["issue_url"]); got != "https://github.com/owner/tabula/issues/88" {
		t.Fatalf("issue_url = %q", got)
	}
	if got := strFromAny(payloads[0]["idea_state"]); got != store.ItemStateInbox {
		t.Fatalf("idea_state = %q, want inbox", got)
	}
	updatedIdea, err := app.store.GetItem(ideaItem.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updatedIdea.State != store.ItemStateInbox {
		t.Fatalf("updated idea state = %q, want inbox", updatedIdea.State)
	}
	if len(calls) != 2 {
		t.Fatalf("gh call count = %d, want 2", len(calls))
	}
	createCall := strings.Join(calls[0], " ")
	if !containsAll(createCall, "issue create", "--title Parser cleanup plan", "## Notes", "## Implementation Outline", "Add regression coverage") {
		t.Fatalf("gh create args = %q", createCall)
	}
}

func timeStub() time.Time {
	return time.Date(2026, time.March, 8, 15, 4, 5, 0, time.UTC)
}
