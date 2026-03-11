package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineBriefingIntent(t *testing.T) {
	now := time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC)
	cases := []string{
		"show my day",
		"show briefing",
		"update briefing",
		"was steht heute an?",
	}
	for _, text := range cases {
		action := parseInlineBriefingIntent(text, now)
		if action == nil {
			t.Fatalf("parseInlineBriefingIntent(%q) returned nil", text)
		}
		if action.Action != "show_briefing" {
			t.Fatalf("action = %q, want show_briefing", action.Action)
		}
		if got := strings.TrimSpace(systemActionStringParam(action.Params, "date")); got != "2026-03-09" {
			t.Fatalf("date = %q, want 2026-03-09", got)
		}
	}
}

func TestClassifyAndExecuteSystemActionShowBriefingRendersArtifact(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	now := time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC)
	app.calendarNow = func() time.Time { return now }
	app.newICSCalendarReader = func() (icsCalendarReader, error) { return stubICSCalendarReader{}, nil }

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	workWorkspace, err := app.store.CreateWorkspace("Work", project.RootPath, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(work): %v", err)
	}
	if err := app.store.SetActiveWorkspace(workWorkspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace(work): %v", err)
	}
	privateDir := filepath.Join(t.TempDir(), "private")
	if err := os.MkdirAll(privateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(private): %v", err)
	}
	privateWorkspace, err := app.store.CreateWorkspace("Home", privateDir, store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace(private): %v", err)
	}
	if _, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work Calendar", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(work calendar): %v", err)
	}
	if _, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Family", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(private calendar): %v", err)
	}

	workSphere := store.SphereWork
	privateSphere := store.SpherePrivate
	dueToday := now.Add(2 * time.Hour).Format(time.RFC3339)
	resurfaceToday := now.Add(-30 * time.Minute).Format(time.RFC3339)
	dueThisWeek := now.Add(48 * time.Hour).Format(time.RFC3339)
	if _, err := app.store.CreateItem("[P0] Ship fix", store.ItemOptions{
		WorkspaceID: &workWorkspace.ID,
		Sphere:      &workSphere,
	}); err != nil {
		t.Fatalf("CreateItem(urgent): %v", err)
	}
	if _, err := app.store.CreateItem("Prepare brief", store.ItemOptions{
		WorkspaceID: &workWorkspace.ID,
		Sphere:      &workSphere,
		FollowUpAt:  &dueToday,
	}); err != nil {
		t.Fatalf("CreateItem(due today): %v", err)
	}
	if _, err := app.store.CreateItem("Review backlog", store.ItemOptions{
		WorkspaceID:  &workWorkspace.ID,
		Sphere:       &workSphere,
		VisibleAfter: &resurfaceToday,
	}); err != nil {
		t.Fatalf("CreateItem(resurface today): %v", err)
	}
	if _, err := app.store.CreateItem("Reply to Carol", store.ItemOptions{
		WorkspaceID: &privateWorkspace.ID,
		Sphere:      &privateSphere,
		Source:      existingStringPtr(store.ExternalProviderGmail),
	}); err != nil {
		t.Fatalf("CreateItem(email): %v", err)
	}
	if _, err := app.store.CreateItem("Review PR 42", store.ItemOptions{
		State:       store.ItemStateWaiting,
		WorkspaceID: &workWorkspace.ID,
		Sphere:      &workSphere,
		Source:      existingStringPtr("github"),
		SourceRef:   existingStringPtr("owner/repo#PR-42"),
	}); err != nil {
		t.Fatalf("CreateItem(pr review): %v", err)
	}
	if _, err := app.store.CreateItem("Draft budget", store.ItemOptions{
		WorkspaceID: &workWorkspace.ID,
		Sphere:      &workSphere,
		FollowUpAt:  &dueThisWeek,
	}); err != nil {
		t.Fatalf("CreateItem(due this week): %v", err)
	}

	app.newGoogleCalendarReader = func(context.Context) (googleCalendarReader, error) {
		return &stubGoogleCalendarReader{
			calendars: []providerdata.Calendar{
				{ID: "work", Name: "Work Calendar"},
				{ID: "family", Name: "Family"},
			},
			events: map[string][]providerdata.Event{
				"work": {
					{
						CalendarID: "work",
						Summary:    "Work sync",
						Location:   "Lab",
						Attendees:  []string{"alice@example.com"},
						Start:      now.Add(1 * time.Hour),
						End:        now.Add(2 * time.Hour),
					},
					{
						CalendarID: "work",
						Summary:    "Design sync",
						Start:      now.Add(26 * time.Hour),
						End:        now.Add(27 * time.Hour),
					},
				},
				"family": {
					{
						CalendarID: "family",
						Summary:    "Family holiday",
						Start:      now.Add(12 * time.Hour),
						End:        now.Add(36 * time.Hour),
						AllDay:     true,
					},
				},
			},
		}, nil
	}

	var (
		showCalls int
		observed  map[string]interface{}
	)
	canvasServer := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer canvasServer.Close()
	port, err := extractPort(canvasServer.URL)
	if err != nil {
		t.Fatalf("extractPort(canvas): %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show my day")
	if !handled {
		t.Fatal("expected show my day to be handled")
	}
	if !strings.Contains(message, "Opened Daily Briefing 2026-03-09 on canvas.") {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	if got := strFromAny(payloads[0]["type"]); got != "show_briefing" {
		t.Fatalf("payload type = %q, want show_briefing", got)
	}
	if showCalls != 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want 1", showCalls)
	}
	path := strFromAny(payloads[0]["path"])
	if path != ".tabura/artifacts/briefing/2026-03-09.md" {
		t.Fatalf("payload path = %q", path)
	}
	rendered, err := os.ReadFile(filepath.Join(project.RootPath, path))
	if err != nil {
		t.Fatalf("ReadFile(rendered): %v", err)
	}
	content := string(rendered)
	for _, snippet := range []string{
		"# Daily Briefing",
		"## Schedule",
		"### Work",
		"### Private",
		"Work sync @ Lab with alice@example.com [Work Calendar]",
		"All day Family holiday [Family]",
		"## Attention Needed",
		"Inbox items: work 4, private 1",
		"P0/urgent items: 1",
		"Unread emails requiring action: 1",
		"Prepare brief [Work] (Work)",
		"Review backlog [Work] (Work)",
		"Reply to Carol [Home] (Private)",
		"## Active Work",
		"Watched workspaces: 0",
		"Orchestrator items in flight: 1",
		"PRs awaiting review: 1",
		"Review PR 42 [Work] (Work)",
		"## Upcoming",
		"Tomorrow's key events",
		"Design sync [Work Calendar]",
		"Items due this week",
		"Draft budget [Work] (Work)",
		"Deadlines approaching",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("briefing artifact missing %q:\n%s", snippet, content)
		}
	}
	if got := strFromAny(observed["title"]); got != path {
		t.Fatalf("canvas title = %q, want %q", got, path)
	}
	artifacts, err := app.store.ListArtifactsByKind(briefingArtifactKind)
	if err != nil {
		t.Fatalf("ListArtifactsByKind(briefing): %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("briefing artifacts = %d, want 1", len(artifacts))
	}
}

func existingStringPtr(value string) *string {
	return &value
}
