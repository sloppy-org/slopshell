package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func seedProjectCompanionSession(t *testing.T, app *App) (store.Project, store.ParticipantSession) {
	t.Helper()
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession: %v", err)
	}
	return project, session
}

func TestProjectCompanionTranscriptAPIAndExports(t *testing.T) {
	app := newAuthedTestApp(t)
	project, session := seedProjectCompanionSession(t, app)
	workspace := requireWorkspaceForProject(t, app, project)

	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID: session.ID,
		StartTS:   100,
		EndTS:     110,
		Speaker:   "Alice",
		Text:      "alpha note",
		Status:    "final",
	})
	_, _ = app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID: session.ID,
		StartTS:   200,
		EndTS:     210,
		Speaker:   "Bob",
		Text:      "beta note",
		Status:    "final",
	})

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/transcript?q=beta", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET transcript status = %d, want 200", rr.Code)
	}
	var payload companionTranscriptResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode transcript payload: %v", err)
	}
	if payload.ProjectID != project.ID {
		t.Fatalf("project_id = %q, want %q", payload.ProjectID, project.ID)
	}
	if payload.Session == nil || payload.Session.ID != session.ID {
		t.Fatalf("selected session = %#v, want %q", payload.Session, session.ID)
	}
	if len(payload.Segments) != 1 {
		t.Fatalf("segments = %d, want 1", len(payload.Segments))
	}
	if payload.Segments[0].Text != "beta note" {
		t.Fatalf("segment text = %q, want beta note", payload.Segments[0].Text)
	}
	body := rr.Body.String()
	if strings.Contains(strings.ToLower(body), "audio") {
		t.Fatalf("transcript payload must remain text-only: %s", body)
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/transcript?format=md", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET transcript markdown status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "# Meeting Transcript") {
		t.Fatalf("transcript markdown missing header: %q", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "alpha note") || !strings.Contains(rr.Body.String(), "beta note") {
		t.Fatalf("transcript markdown missing segment text: %q", rr.Body.String())
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/transcript?format=txt", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET transcript text status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Alice: alpha note") || !strings.Contains(rr.Body.String(), "Bob: beta note") {
		t.Fatalf("transcript text missing export content: %q", rr.Body.String())
	}
}

func TestProjectCompanionSummaryAndReferencesAPIAndExports(t *testing.T) {
	app := newAuthedTestApp(t)
	project, session := seedProjectCompanionSession(t, app)
	workspace := requireWorkspaceForProject(t, app, project)
	if err := app.store.UpsertParticipantRoomState(session.ID, "Decision summary", `["Acme","Budget"]`, `[{"topic":"Status"},{"topic":"Risks"}]`); err != nil {
		t.Fatalf("UpsertParticipantRoomState: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/summary", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET summary status = %d, want 200", rr.Code)
	}
	var summary companionSummaryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary payload: %v", err)
	}
	if summary.Session == nil || summary.Session.ID != session.ID {
		t.Fatalf("summary session = %#v, want %q", summary.Session, session.ID)
	}
	if summary.SummaryText != "Decision summary" {
		t.Fatalf("summary_text = %q, want Decision summary", summary.SummaryText)
	}
	if strings.Contains(strings.ToLower(rr.Body.String()), "audio") {
		t.Fatalf("summary payload must remain text-only: %s", rr.Body.String())
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/summary?format=md", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET summary markdown status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "# Meeting Summary") || !strings.Contains(rr.Body.String(), "Decision summary") {
		t.Fatalf("summary markdown missing expected content: %q", rr.Body.String())
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/references", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET references status = %d, want 200", rr.Code)
	}
	var refs companionReferencesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &refs); err != nil {
		t.Fatalf("decode references payload: %v", err)
	}
	if len(refs.Entities) != 2 {
		t.Fatalf("entities = %d, want 2", len(refs.Entities))
	}
	if len(refs.TopicTimeline) != 2 {
		t.Fatalf("topic_timeline = %d, want 2", len(refs.TopicTimeline))
	}
	if strings.Contains(strings.ToLower(rr.Body.String()), "audio") {
		t.Fatalf("references payload must remain text-only: %s", rr.Body.String())
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/references?format=md", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET references markdown status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "# Meeting References") {
		t.Fatalf("references markdown missing header: %q", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Acme") || !strings.Contains(rr.Body.String(), "Status") {
		t.Fatalf("references markdown missing captured metadata: %q", rr.Body.String())
	}

	artifactDir := filepath.Join(project.RootPath, ".tabura", "artifacts", "companion", session.ID)
	transcriptPath := filepath.Join(artifactDir, "transcript.md")
	transcriptBody, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("read transcript artifact: %v", err)
	}
	if !strings.Contains(string(transcriptBody), "# Meeting Transcript") {
		t.Fatalf("transcript artifact missing header: %q", string(transcriptBody))
	}

	summaryPath := filepath.Join(artifactDir, "summary.md")
	summaryBody, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("read summary artifact: %v", err)
	}
	if !strings.Contains(string(summaryBody), "Decision summary") {
		t.Fatalf("summary artifact missing room memory text: %q", string(summaryBody))
	}

	referencesPath := filepath.Join(artifactDir, "references.md")
	referencesBody, err := os.ReadFile(referencesPath)
	if err != nil {
		t.Fatalf("read references artifact: %v", err)
	}
	if !strings.Contains(string(referencesBody), "Acme") || !strings.Contains(string(referencesBody), "Status") {
		t.Fatalf("references artifact missing metadata: %q", string(referencesBody))
	}
}

func TestProjectCompanionRoomMemoryDerivesFromTranscriptAndEvents(t *testing.T) {
	app := newAuthedTestApp(t)
	project, session := seedProjectCompanionSession(t, app)
	workspace := requireWorkspaceForProject(t, app, project)

	if err := app.store.AddParticipantEvent(session.ID, 0, "session_started", `{"reason":"manual"}`); err != nil {
		t.Fatalf("AddParticipantEvent session_started: %v", err)
	}
	seg1, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   session.ID,
		StartTS:     100,
		EndTS:       101,
		Speaker:     "Alice",
		Text:        "Review the Acme Cloud budget before Friday.",
		CommittedAt: 102,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("AddParticipantSegment seg1: %v", err)
	}
	seg2, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   session.ID,
		StartTS:     120,
		EndTS:       121,
		Speaker:     "Bob",
		Text:        "Bob will send Contoso follow-up notes after the meeting.",
		CommittedAt: 122,
		Status:      "final",
	})
	if err != nil {
		t.Fatalf("AddParticipantSegment seg2: %v", err)
	}
	if err := app.store.AddParticipantEvent(session.ID, seg1.ID, "segment_committed", `{"text":"Review the Acme Cloud budget before Friday."}`); err != nil {
		t.Fatalf("AddParticipantEvent segment_committed seg1: %v", err)
	}
	if err := app.store.AddParticipantEvent(session.ID, seg2.ID, "segment_committed", `{"text":"Bob will send Contoso follow-up notes after the meeting."}`); err != nil {
		t.Fatalf("AddParticipantEvent segment_committed seg2: %v", err)
	}
	if err := app.store.AddParticipantEvent(session.ID, seg2.ID, "assistant_triggered", `{"chat_session_id":"chat-1"}`); err != nil {
		t.Fatalf("AddParticipantEvent assistant_triggered: %v", err)
	}
	if err := app.store.AddParticipantEvent(session.ID, seg2.ID, "assistant_turn_completed", `{"chat_session_id":"chat-1"}`); err != nil {
		t.Fatalf("AddParticipantEvent assistant_turn_completed: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/summary", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET derived summary status = %d, want 200", rr.Code)
	}
	var summary companionSummaryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode derived summary payload: %v", err)
	}
	if summary.Session == nil || summary.Session.ID != session.ID {
		t.Fatalf("summary session = %#v, want %q", summary.Session, session.ID)
	}
	if !strings.Contains(summary.SummaryText, "Assistant response completed") {
		t.Fatalf("summary_text = %q, want assistant completion topic", summary.SummaryText)
	}
	if !strings.Contains(summary.SummaryText, "Acme Cloud") {
		t.Fatalf("summary_text = %q, want derived entity", summary.SummaryText)
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/references", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET derived references status = %d, want 200", rr.Code)
	}
	var refs companionReferencesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &refs); err != nil {
		t.Fatalf("decode derived references payload: %v", err)
	}
	for _, want := range []string{"Alice", "Bob", "Acme Cloud", "Contoso"} {
		if !containsString(refs.Entities, want) {
			t.Fatalf("entities = %#v, want %q", refs.Entities, want)
		}
	}
	if len(refs.TopicTimeline) != 5 {
		t.Fatalf("topic_timeline = %d, want 5", len(refs.TopicTimeline))
	}
	if !topicTimelineContains(refs.TopicTimeline, "Session started") {
		t.Fatalf("topic_timeline = %#v, want Session started entry", refs.TopicTimeline)
	}
	last, ok := refs.TopicTimeline[len(refs.TopicTimeline)-1].(map[string]any)
	if !ok {
		t.Fatalf("last topic timeline entry type = %T, want map[string]any", refs.TopicTimeline[len(refs.TopicTimeline)-1])
	}
	if got := strings.TrimSpace(last["topic"].(string)); got != "Assistant response completed" {
		t.Fatalf("last topic = %q, want Assistant response completed", got)
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/references?format=md", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET derived references markdown status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Alice: Review the Acme Cloud budget before Friday") {
		t.Fatalf("references markdown missing derived segment timeline: %q", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Assistant response completed") {
		t.Fatalf("references markdown missing derived assistant event: %q", rr.Body.String())
	}
}

func TestProjectCompanionRoomMemoryIsProjectScoped(t *testing.T) {
	app := newAuthedTestApp(t)
	project, session := seedProjectCompanionSession(t, app)
	workspace := requireWorkspaceForProject(t, app, project)

	otherProject, err := app.store.CreateProject("Meeting Temp", "meeting-temp", t.TempDir(), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject other: %v", err)
	}
	otherSession, err := app.store.AddParticipantSession(otherProject.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession other: %v", err)
	}

	if _, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   session.ID,
		StartTS:     100,
		EndTS:       101,
		Speaker:     "Alice",
		Text:        "Discuss the Acme Cloud budget.",
		CommittedAt: 102,
		Status:      "final",
	}); err != nil {
		t.Fatalf("AddParticipantSegment primary: %v", err)
	}
	if _, err := app.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   otherSession.ID,
		StartTS:     200,
		EndTS:       201,
		Speaker:     "Mallory",
		Text:        "Discuss the Zeus acquisition.",
		CommittedAt: 202,
		Status:      "final",
	}); err != nil {
		t.Fatalf("AddParticipantSegment other: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/references", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET scoped references status = %d, want 200", rr.Code)
	}
	var refs companionReferencesResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &refs); err != nil {
		t.Fatalf("decode scoped references payload: %v", err)
	}
	if !containsString(refs.Entities, "Acme Cloud") {
		t.Fatalf("entities = %#v, want Acme Cloud", refs.Entities)
	}
	if containsString(refs.Entities, "Zeus") || containsString(refs.Entities, "Mallory") {
		t.Fatalf("entities leaked across projects: %#v", refs.Entities)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func topicTimelineContains(items []any, want string) bool {
	for _, item := range items {
		typed, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(typed["topic"].(string)) == want {
			return true
		}
	}
	return false
}
