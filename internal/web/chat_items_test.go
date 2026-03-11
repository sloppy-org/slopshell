package web

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func mustFirstItemByState(t *testing.T, app *App, state string) store.Item {
	t.Helper()
	items, err := app.store.ListItemsByState(state)
	if err != nil {
		t.Fatalf("ListItemsByState(%s) error: %v", state, err)
	}
	if len(items) != 1 {
		t.Fatalf("ListItemsByState(%s) len = %d, want 1", state, len(items))
	}
	return items[0]
}

func TestParseInlineItemIntent(t *testing.T) {
	now := time.Date(2026, time.March, 8, 15, 4, 5, 0, time.UTC)

	cases := []struct {
		text       string
		wantAction string
		wantActor  string
		wantWhen   string
		wantCount  int
		wantKind   string
	}{
		{text: "idea: better swipe triage", wantAction: "capture_idea"},
		{text: "new idea: add a review inbox", wantAction: "capture_idea"},
		{text: "Idee: bessere Rückfragen für den Review-Modus", wantAction: "capture_idea"},
		{text: "expand this idea", wantAction: "refine_idea_note", wantKind: "expand"},
		{text: "Baue diese Idee aus", wantAction: "refine_idea_note", wantKind: "expand"},
		{text: "add pros and cons", wantAction: "refine_idea_note", wantKind: "pros_cons"},
		{text: "Ergänze Vor- und Nachteile", wantAction: "refine_idea_note", wantKind: "pros_cons"},
		{text: "compare alternatives", wantAction: "refine_idea_note", wantKind: "alternatives"},
		{text: "Vergleiche Alternativen", wantAction: "refine_idea_note", wantKind: "alternatives"},
		{text: "outline an implementation", wantAction: "refine_idea_note", wantKind: "implementation"},
		{text: "Skizziere eine Umsetzung", wantAction: "refine_idea_note", wantKind: "implementation"},
		{text: "make this an item", wantAction: "make_item"},
		{text: "Mach das zu einem Item", wantAction: "make_item"},
		{text: "delegate this to Codex", wantAction: "delegate_item", wantActor: "Codex"},
		{text: "Delegiere das an Codex", wantAction: "delegate_item", wantActor: "Codex"},
		{text: "remind me tomorrow", wantAction: "snooze_item", wantWhen: "2026-03-09T09:00:00Z"},
		{text: "Erinnere mich morgen", wantAction: "snooze_item", wantWhen: "2026-03-09T09:00:00Z"},
		{text: "Erinnere mich am Montag", wantAction: "snooze_item", wantWhen: "2026-03-09T09:00:00Z"},
		{text: "split this into three items", wantAction: "split_items", wantCount: 3},
		{text: "Teile das in drei Aufgaben", wantAction: "split_items", wantCount: 3},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			action := parseInlineItemIntent(tc.text, now)
			if action == nil {
				t.Fatal("expected inline item action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if tc.wantActor != "" && systemActionActorName(action.Params) != tc.wantActor {
				t.Fatalf("actor = %q, want %q", systemActionActorName(action.Params), tc.wantActor)
			}
			if tc.wantWhen != "" && systemActionVisibleAfter(action.Params) != tc.wantWhen {
				t.Fatalf("visible_after = %q, want %q", systemActionVisibleAfter(action.Params), tc.wantWhen)
			}
			if tc.wantCount != 0 && systemActionSplitCount(action.Params) != tc.wantCount {
				t.Fatalf("count = %d, want %d", systemActionSplitCount(action.Params), tc.wantCount)
			}
			if tc.wantKind != "" && systemActionStringParam(action.Params, "kind") != tc.wantKind {
				t.Fatalf("kind = %q, want %q", systemActionStringParam(action.Params, "kind"), tc.wantKind)
			}
		})
	}
}

func TestDeriveIdeaTitlePreservesGermanText(t *testing.T) {
	title := deriveIdeaTitle("größere Übersicht für Öl-Temperatur prüfen. Danach Details sammeln.")
	if title != "Größere Übersicht für Öl-Temperatur prüfen." {
		t.Fatalf("title = %q", title)
	}
}

func TestClassifyAndExecuteSystemActionMakeItemCreatesInboxItemFromAssistantContext(t *testing.T) {
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
	if _, err := app.store.AddChatMessage(session.ID, "assistant", "Refactor the parser pipeline\n\n- fold duplicate state handling", "Refactor the parser pipeline\n\n- fold duplicate state handling", "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "make this an item")
	if !handled {
		t.Fatal("expected item command to be handled")
	}
	if message != `Created inbox item "Refactor the parser pipeline".` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_created" {
		t.Fatalf("payloads = %#v", payloads)
	}

	item := mustFirstItemByState(t, app, store.ItemStateInbox)
	if item.Title != "Refactor the parser pipeline" {
		t.Fatalf("item title = %q", item.Title)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id = %v, want %d", item.WorkspaceID, workspace.ID)
	}
	if item.ArtifactID == nil {
		t.Fatal("expected artifact to be linked")
	}
	artifact, err := app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if artifact.Kind != store.ArtifactKindPlanNote {
		t.Fatalf("artifact kind = %q, want %q", artifact.Kind, store.ArtifactKindPlanNote)
	}
	if artifact.MetaJSON == nil || !containsAll(*artifact.MetaJSON, `"source":"assistant"`, `Refactor the parser pipeline`) {
		t.Fatalf("artifact meta_json = %v", artifact.MetaJSON)
	}
}

func TestClassifyAndExecuteSystemActionDelegateItemUsesActorAndCanvasArtifact(t *testing.T) {
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
	actor, err := app.store.CreateActor("Codex", store.ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "assistant", "Review the README cleanup plan", "Review the README cleanup plan", "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	readmePath := filepath.Join(project.RootPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
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

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "delegate this to Codex")
	if !handled {
		t.Fatal("expected delegate command to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")
	waitingItems, err := app.store.ListItemsByState(store.ItemStateWaiting)
	if err != nil {
		t.Fatalf("ListItemsByState(waiting) error: %v", err)
	}
	if len(waitingItems) != 0 {
		t.Fatalf("waiting item count = %d, want 0 before confirm", len(waitingItems))
	}

	message, payloads, handled = confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected delegate confirmation to be handled")
	}
	if message != `Created waiting item "Review the README cleanup plan" for Codex.` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_created" {
		t.Fatalf("payloads = %#v", payloads)
	}

	item := mustFirstItemByState(t, app, store.ItemStateWaiting)
	if item.ActorID == nil || *item.ActorID != actor.ID {
		t.Fatalf("actor_id = %v, want %d", item.ActorID, actor.ID)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id = %v, want %d", item.WorkspaceID, workspace.ID)
	}
	if item.ArtifactID == nil {
		t.Fatal("expected canvas artifact to be linked")
	}
	artifact, err := app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if artifact.Kind != store.ArtifactKindMarkdown {
		t.Fatalf("artifact kind = %q, want %q", artifact.Kind, store.ArtifactKindMarkdown)
	}
	if artifact.RefPath == nil || *artifact.RefPath != readmePath {
		t.Fatalf("artifact ref_path = %v, want %q", artifact.RefPath, readmePath)
	}
}

func TestClassifyAndExecuteSystemActionSnoozeItemCreatesWaitingItem(t *testing.T) {
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
	if _, err := app.store.AddChatMessage(session.ID, "assistant", "Follow up with the batching review", "Follow up with the batching review", "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	action := parseInlineItemIntent("remind me tomorrow", time.Date(2026, time.March, 8, 15, 4, 5, 0, time.UTC))
	if action == nil {
		t.Fatal("expected snooze action")
	}
	message, payload, err := app.executeSystemAction(session.ID, session, action)
	if err != nil {
		t.Fatalf("executeSystemAction() error: %v", err)
	}
	if message != `Created waiting item "Follow up with the batching review" for 2026-03-09T09:00:00Z.` {
		t.Fatalf("message = %q", message)
	}
	if strFromAny(payload["visible_after"]) != "2026-03-09T09:00:00Z" {
		t.Fatalf("payload = %#v", payload)
	}

	item := mustFirstItemByState(t, app, store.ItemStateWaiting)
	if item.VisibleAfter == nil || *item.VisibleAfter != "2026-03-09T09:00:00Z" {
		t.Fatalf("visible_after = %v", item.VisibleAfter)
	}
}

func TestClassifyAndExecuteSystemActionCaptureIdeaCreatesInboxItemFromUserInput(t *testing.T) {
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
	app.chatCaptureModes.set(session.ID, chatCaptureModeVoice)

	message, payloads, handled := app.classifyAndExecuteSystemAction(
		context.Background(),
		session.ID,
		session,
		"idea: better swipe triage for waiting items. Capture the blockers too.",
	)
	if !handled {
		t.Fatal("expected idea capture to be handled")
	}
	if message != "Captured idea: Better swipe triage for waiting items." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_created" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if strFromAny(payloads[0]["capture_mode"]) != chatCaptureModeVoice {
		t.Fatalf("capture_mode payload = %#v", payloads[0])
	}

	item := mustFirstItemByState(t, app, store.ItemStateInbox)
	if item.Title != "Better swipe triage for waiting items." {
		t.Fatalf("item title = %q", item.Title)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id = %v, want %d", item.WorkspaceID, workspace.ID)
	}
	if item.Source == nil || *item.Source != "idea" {
		t.Fatalf("item source = %v, want idea", item.Source)
	}
	if item.ArtifactID == nil {
		t.Fatal("expected idea artifact to be linked")
	}
	artifact, err := app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if artifact.Kind != store.ArtifactKindIdeaNote {
		t.Fatalf("artifact kind = %q, want %q", artifact.Kind, store.ArtifactKindIdeaNote)
	}
	if artifact.MetaJSON == nil {
		t.Fatalf("artifact meta_json = %v", artifact.MetaJSON)
	}
	meta := parseIdeaNoteMeta(artifact.MetaJSON, ideaNoteString(artifact.Title))
	if meta.CaptureMode != chatCaptureModeVoice {
		t.Fatalf("capture_mode = %q, want %q", meta.CaptureMode, chatCaptureModeVoice)
	}
	if meta.Transcript != "better swipe triage for waiting items. Capture the blockers too." {
		t.Fatalf("transcript = %q", meta.Transcript)
	}
	if meta.Title != "Better swipe triage for waiting items." {
		t.Fatalf("title = %q", meta.Title)
	}
	if meta.Workspace != "Default" {
		t.Fatalf("workspace = %q, want Default", meta.Workspace)
	}
	if len(meta.Notes) != 1 || meta.Notes[0] != meta.Transcript {
		t.Fatalf("notes = %#v", meta.Notes)
	}
}

func TestRunAssistantTurnCaptureIdeaPersistsAssistantConfirmation(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if _, err := app.store.CreateWorkspace("Default", project.RootPath); err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "idea: support batched inbox review", "idea: support batched inbox review", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	app.chatCaptureModes.set(session.ID, chatCaptureModeText)

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeVoice})

	messages, err := app.store.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages() error: %v", err)
	}
	foundAssistant := false
	for _, msg := range messages {
		if msg.Role == "assistant" && msg.ContentPlain == "Captured idea: Support batched inbox review." {
			foundAssistant = true
			break
		}
	}
	if !foundAssistant {
		t.Fatalf("expected assistant confirmation in chat history, got %#v", messages)
	}
}

func TestClassifyAndExecuteSystemActionRefineIdeaUpdatesArtifactAndCanvas(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if _, err := app.store.CreateWorkspace("Default", project.RootPath); err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	if _, _, handled := app.classifyAndExecuteSystemAction(
		context.Background(),
		session.ID,
		session,
		"idea: better swipe triage for waiting items. Capture the blockers too.",
	); !handled {
		t.Fatal("expected idea capture to be handled")
	}

	item := mustFirstItemByState(t, app, store.ItemStateInbox)
	if item.ArtifactID == nil {
		t.Fatal("expected captured idea artifact")
	}
	artifact, err := app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: ideaNoteString(artifact.Title),
		artifactKind:  "text_artifact",
		artifactText:  renderIdeaNoteMarkdown(parseIdeaNoteMeta(artifact.MetaJSON, ideaNoteString(artifact.Title))),
	}
	server := mock.setupServer(t)
	defer server.Close()
	port := serverPort(t, server.Listener.Addr())
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "add pros and cons")
	if !handled {
		t.Fatal("expected idea refinement to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")

	updatedArtifact, err := app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() before confirm error: %v", err)
	}
	meta := parseIdeaNoteMeta(updatedArtifact.MetaJSON, ideaNoteString(updatedArtifact.Title))
	if len(meta.Refinements) != 0 {
		t.Fatalf("refinements len = %d, want 0 before confirm", len(meta.Refinements))
	}

	message, payloads, handled = confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected idea refinement confirmation to be handled")
	}
	if message != "Updated idea note with Pros and Cons." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "artifact_updated" {
		t.Fatalf("payloads = %#v", payloads)
	}

	updatedArtifact, err = app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() updated error: %v", err)
	}
	meta = parseIdeaNoteMeta(updatedArtifact.MetaJSON, ideaNoteString(updatedArtifact.Title))
	if len(meta.Refinements) != 1 {
		t.Fatalf("refinements len = %d, want 1", len(meta.Refinements))
	}
	if meta.Refinements[0].Heading != "Pros and Cons" {
		t.Fatalf("heading = %q, want %q", meta.Refinements[0].Heading, "Pros and Cons")
	}
	if !strings.Contains(mock.lastShownContent, "## Pros and Cons") {
		t.Fatalf("canvas content = %q", mock.lastShownContent)
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "outline an implementation")
	if !handled {
		t.Fatal("expected second idea refinement to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")
	if _, _, handled := confirmNextAction(t, app, session); !handled {
		t.Fatal("expected second idea refinement confirmation to be handled")
	}
	updatedArtifact, err = app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() second update error: %v", err)
	}
	meta = parseIdeaNoteMeta(updatedArtifact.MetaJSON, ideaNoteString(updatedArtifact.Title))
	if len(meta.Refinements) != 2 {
		raw, _ := json.Marshal(meta)
		t.Fatalf("refinements len = %d, want 2: %s", len(meta.Refinements), raw)
	}
	if !strings.Contains(mock.lastShownContent, "## Implementation Outline") {
		t.Fatalf("canvas content = %q", mock.lastShownContent)
	}
}

func TestClassifyAndExecuteSystemActionSplitItemsCreatesMultipleItems(t *testing.T) {
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
	assistantText := "- Capture failing test case\n- Patch the parser fallback\n- Add regression coverage"
	if _, err := app.store.AddChatMessage(session.ID, "assistant", assistantText, assistantText, "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "split this into three items")
	if !handled {
		t.Fatal("expected split command to be handled")
	}
	if message != "Created 3 inbox items." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "items_created" {
		t.Fatalf("payloads = %#v", payloads)
	}
	items, err := app.store.ListItemsByState(store.ItemStateInbox)
	if err != nil {
		t.Fatalf("ListItemsByState(inbox) error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("inbox len = %d, want 3", len(items))
	}
}

func TestClassifyAndExecuteSystemActionDelegateItemSurfacesMissingActor(t *testing.T) {
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
	if _, err := app.store.AddChatMessage(session.ID, "assistant", "Prepare the handoff note", "Prepare the handoff note", "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "delegate this to Missing")
	if !handled {
		t.Fatal("expected missing-actor command to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")
	message, payloads, handled = confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected missing-actor confirmation to be handled")
	}
	if len(payloads) != 0 {
		t.Fatalf("payloads = %#v, want none", payloads)
	}
	requireConfirmationFailureMessage(t, message, `actor "Missing" not found`)
}

func TestClassifyAndExecuteSystemActionArtifactConfirmationCanBeCanceled(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if _, err := app.store.CreateActor("Codex", store.ActorKindAgent); err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "assistant", "Prepare the handoff note", "Prepare the handoff note", "markdown"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "delegate this to Codex")
	if !handled {
		t.Fatal("expected delegate command to be handled")
	}
	requireConfirmationRequired(t, message, payloads, "artifact")

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "cancel")
	if !handled {
		t.Fatal("expected cancel to be handled")
	}
	if message != "Canceled pending artifact action." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "confirmation_canceled" {
		t.Fatalf("payloads = %#v", payloads)
	}
	items, err := app.store.ListItemsByState(store.ItemStateWaiting)
	if err != nil {
		t.Fatalf("ListItemsByState(waiting) error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("waiting item count = %d, want 0", len(items))
	}
}

func serverPort(t *testing.T, addr net.Addr) int {
	t.Helper()
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener addr = %T, want *net.TCPAddr", addr)
	}
	return tcp.Port
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
