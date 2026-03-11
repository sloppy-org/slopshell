package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func TestExtractMeetingItemsSupportsMixedSummaryFormats(t *testing.T) {
	app := newAuthedTestApp(t)

	summary := strings.Join([]string{
		"# Meeting Summary",
		"",
		"- ACTION: Alice will draft the revised agenda.",
		"1. TODO: review the budget appendix.",
		"Bob: send the follow-up email.",
		"Alice, can you prepare the budget by Friday?",
		"Discussion: the budget is tight.",
	}, "\n")

	proposed := app.extractMeetingItems(summary)
	if len(proposed) != 4 {
		t.Fatalf("extractMeetingItems() len = %d, want 4", len(proposed))
	}

	if proposed[0].Title != "Draft the revised agenda" || proposed[0].ActorName != "Alice" {
		t.Fatalf("first proposal = %#v", proposed[0])
	}
	if proposed[1].Title != "Review the budget appendix" || proposed[1].ActorName != "" {
		t.Fatalf("second proposal = %#v", proposed[1])
	}
	if proposed[2].Title != "Send the follow-up email" || proposed[2].ActorName != "Bob" {
		t.Fatalf("third proposal = %#v", proposed[2])
	}
	if proposed[3].Title != "Prepare the budget by Friday" || proposed[3].ActorName != "Alice" {
		t.Fatalf("fourth proposal = %#v", proposed[3])
	}

	if got := app.extractMeetingItems("The meeting covered current status only."); len(got) != 0 {
		t.Fatalf("extractMeetingItems(no actions) len = %d, want 0", len(got))
	}
}

func TestExtractMeetingDecisionsRecognizesDecisionLanguage(t *testing.T) {
	decisions := extractMeetingDecisions("OK, let us go with option B for the timeline. We decided to revisit staffing next week.")
	if len(decisions) != 2 {
		t.Fatalf("extractMeetingDecisions() len = %d, want 2", len(decisions))
	}
	if decisions[0] != "Go with option B for the timeline" {
		t.Fatalf("first decision = %q", decisions[0])
	}
	if decisions[1] != "Decided to revisit staffing next week" {
		t.Fatalf("second decision = %q", decisions[1])
	}
}

func TestProjectMeetingItemsAPIAndCreation(t *testing.T) {
	app := newAuthedTestApp(t)
	project, session := seedProjectCompanionSession(t, app)

	workspace, err := app.store.GetWorkspace(session.WorkspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace() error: %v", err)
	}
	if workspace.DirPath != project.RootPath {
		t.Fatalf("workspace dir_path = %q, want %q", workspace.DirPath, project.RootPath)
	}
	if err := app.store.UpsertParticipantRoomState(session.ID, strings.Join([]string{
		"Meeting summary",
		"",
		"- ACTION: Alice will draft the revised agenda.",
		"- TODO: review the budget appendix.",
	}, "\n"), `["Alice"]`, `[]`); err != nil {
		t.Fatalf("UpsertParticipantRoomState() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/meeting-items", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET meeting-items status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var proposals projectMeetingItemsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &proposals); err != nil {
		t.Fatalf("decode meeting-items response: %v", err)
	}
	if proposals.Session == nil || proposals.Session.ID != session.ID {
		t.Fatalf("selected session = %#v, want %q", proposals.Session, session.ID)
	}
	if len(proposals.ProposedItems) != 2 {
		t.Fatalf("proposed_items len = %d, want 2", len(proposals.ProposedItems))
	}
	if proposals.ProposedItems[0].ActorName != "Alice" {
		t.Fatalf("first proposed actor = %q, want Alice", proposals.ProposedItems[0].ActorName)
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspaces/"+itoa(workspace.ID)+"/meeting-items", map[string]any{
		"selected": []int{0, 1},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST meeting-items status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	var created createMeetingItemsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created response: %v", err)
	}
	if len(created.CreatedItems) != 2 {
		t.Fatalf("created_items len = %d, want 2", len(created.CreatedItems))
	}

	items, err := app.store.ListInboxItems(time.Now())
	if err != nil {
		t.Fatalf("ListInboxItems() error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ListInboxItems() len = %d, want 2", len(items))
	}

	var draftItem, reviewItem store.ItemSummary
	for _, item := range items {
		switch item.Title {
		case "Draft the revised agenda":
			draftItem = item
		case "Review the budget appendix":
			reviewItem = item
		}
	}
	if draftItem.ID == 0 || reviewItem.ID == 0 {
		t.Fatalf("created inbox items = %#v", items)
	}
	if draftItem.WorkspaceID == nil || *draftItem.WorkspaceID != workspace.ID {
		t.Fatalf("draft workspace_id = %v, want %d", draftItem.WorkspaceID, workspace.ID)
	}
	if reviewItem.WorkspaceID == nil || *reviewItem.WorkspaceID != workspace.ID {
		t.Fatalf("review workspace_id = %v, want %d", reviewItem.WorkspaceID, workspace.ID)
	}
	if draftItem.ActorName == nil || *draftItem.ActorName != "Alice" {
		t.Fatalf("draft actor_name = %v, want Alice", draftItem.ActorName)
	}
	if reviewItem.ActorName != nil && *reviewItem.ActorName != "" {
		t.Fatalf("review actor_name = %v, want nil/empty", reviewItem.ActorName)
	}
	if draftItem.ArtifactID == nil || reviewItem.ArtifactID == nil || *draftItem.ArtifactID != *reviewItem.ArtifactID {
		t.Fatalf("artifact ids = %v and %v, want shared summary artifact", draftItem.ArtifactID, reviewItem.ArtifactID)
	}

	artifact, err := app.store.GetArtifact(*draftItem.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if artifact.RefPath == nil || !strings.HasSuffix(*artifact.RefPath, "/summary.md") {
		t.Fatalf("artifact ref_path = %v, want summary.md", artifact.RefPath)
	}
	if artifact.MetaJSON == nil || !strings.Contains(*artifact.MetaJSON, `"source":"meeting_summary"`) {
		t.Fatalf("artifact meta_json = %v, want meeting_summary source", artifact.MetaJSON)
	}
	if artifact.MetaJSON == nil || !strings.Contains(*artifact.MetaJSON, `"summary":"Meeting summary`) {
		t.Fatalf("artifact meta_json = %v, want summary text", artifact.MetaJSON)
	}
}

func TestProjectMeetingItemsCreateRejectsEmptySelection(t *testing.T) {
	app := newAuthedTestApp(t)
	project, session := seedProjectCompanionSession(t, app)
	workspace := requireWorkspaceForProject(t, app, project)

	if err := app.store.UpsertParticipantRoomState(session.ID, "- ACTION: Alice will draft the revised agenda.", `["Alice"]`, `[]`); err != nil {
		t.Fatalf("UpsertParticipantRoomState() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspaces/"+itoa(workspace.ID)+"/meeting-items", map[string]any{
		"selected": []int{},
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("POST meeting-items empty selection status = %d, want 400", rr.Code)
	}
}
