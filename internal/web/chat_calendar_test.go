package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/calendar"
	"github.com/krystophny/tabura/internal/ics"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type stubGoogleCalendarReader struct {
	calendars []providerdata.Calendar
	events    map[string][]providerdata.Event
}

func (s *stubGoogleCalendarReader) ListCalendars(context.Context) ([]providerdata.Calendar, error) {
	return append([]providerdata.Calendar(nil), s.calendars...), nil
}

func (s *stubGoogleCalendarReader) GetEvents(_ context.Context, opts calendar.GetEventsOptions) ([]providerdata.Event, error) {
	return append([]providerdata.Event(nil), s.events[opts.CalendarID]...), nil
}

type stubICSCalendarReader struct{}

func (stubICSCalendarReader) ListCalendars() []providerdata.Calendar {
	return nil
}

func (stubICSCalendarReader) GetEvents(string, time.Time, time.Time) ([]ics.ICSEvent, error) {
	return nil, nil
}

func TestParseInlineCalendarIntent(t *testing.T) {
	now := time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC)
	cases := []struct {
		text      string
		wantView  string
		wantDate  string
		wantQuery string
	}{
		{text: "show calendar", wantView: calendarViewDay},
		{text: "show my schedule", wantView: calendarViewDay},
		{text: "what's today?", wantView: calendarViewAgenda, wantDate: "2026-03-09"},
		{text: "what's this week?", wantView: calendarViewWeek, wantDate: "2026-03-09"},
		{text: "when am I free tomorrow?", wantView: calendarViewAvailability, wantDate: "2026-03-10"},
		{text: "show calendar for EUROfusion", wantView: calendarViewDay, wantQuery: "EUROfusion"},
	}
	for _, tc := range cases {
		action := parseInlineCalendarIntent(tc.text, now)
		if action == nil {
			t.Fatalf("parseInlineCalendarIntent(%q) returned nil", tc.text)
		}
		if action.Action != "show_calendar" {
			t.Fatalf("action = %q, want show_calendar", action.Action)
		}
		if got := strings.TrimSpace(systemActionStringParam(action.Params, "view")); got != tc.wantView {
			t.Fatalf("view = %q, want %q", got, tc.wantView)
		}
		if tc.wantDate != "" {
			if got := strings.TrimSpace(systemActionStringParam(action.Params, "date")); got != tc.wantDate {
				t.Fatalf("date = %q, want %q", got, tc.wantDate)
			}
		}
		if tc.wantQuery != "" {
			if got := strings.TrimSpace(systemActionStringParam(action.Params, "query")); got != tc.wantQuery {
				t.Fatalf("query = %q, want %q", got, tc.wantQuery)
			}
		}
	}
}

func TestClassifyAndExecuteSystemActionShowCalendarRendersSphereAwareArtifact(t *testing.T) {
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
	if err := app.store.SetActiveSphere(store.SphereWork); err != nil {
		t.Fatalf("SetActiveSphere(work): %v", err)
	}
	if _, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work Calendar", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(work calendar): %v", err)
	}
	if _, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Family", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(private calendar): %v", err)
	}

	workSphere := store.SphereWork
	privateSphere := store.SpherePrivate
	workDue := now.Add(8 * time.Hour).Format(time.RFC3339)
	privateDue := now.Add(9 * time.Hour).Format(time.RFC3339)
	workVisible := now.Add(2 * time.Hour).Format(time.RFC3339)
	if _, err := app.store.CreateItem("Prepare brief", store.ItemOptions{
		WorkspaceID: &workWorkspace.ID,
		Sphere:      &workSphere,
		FollowUpAt:  &workDue,
	}); err != nil {
		t.Fatalf("CreateItem(work due): %v", err)
	}
	if _, err := app.store.CreateItem("Review backlog", store.ItemOptions{
		WorkspaceID:  &workWorkspace.ID,
		Sphere:       &workSphere,
		VisibleAfter: &workVisible,
	}); err != nil {
		t.Fatalf("CreateItem(work resurface): %v", err)
	}
	if _, err := app.store.CreateItem("Buy flowers", store.ItemOptions{
		WorkspaceID: &privateWorkspace.ID,
		Sphere:      &privateSphere,
		FollowUpAt:  &privateDue,
	}); err != nil {
		t.Fatalf("CreateItem(private due): %v", err)
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
				},
				"family": {
					{
						CalendarID: "family",
						Summary:    "Design review",
						Start:      now.Add(4 * time.Hour),
						End:        now.Add(5 * time.Hour),
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

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show calendar")
	if !handled {
		t.Fatal("expected show calendar to be handled")
	}
	if !strings.Contains(message, "Opened Calendar 2026-03-09 on canvas.") {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	if got := strFromAny(payloads[0]["type"]); got != "show_calendar" {
		t.Fatalf("payload type = %q, want show_calendar", got)
	}
	if got := strFromAny(payloads[0]["view"]); got != calendarViewDay {
		t.Fatalf("payload view = %q, want %q", got, calendarViewDay)
	}
	if showCalls != 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want 1", showCalls)
	}
	path := strFromAny(payloads[0]["path"])
	if !strings.HasPrefix(path, ".tabura/artifacts/calendar/2026-03-09-day") {
		t.Fatalf("payload path = %q", path)
	}
	rendered, err := os.ReadFile(filepath.Join(project.RootPath, path))
	if err != nil {
		t.Fatalf("ReadFile(rendered): %v", err)
	}
	content := string(rendered)
	for _, snippet := range []string{"Work sync", "Busy (other sphere)", "Prepare brief", "Review backlog"} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("calendar artifact missing %q:\n%s", snippet, content)
		}
	}
	for _, hidden := range []string{"Design review", "Buy flowers"} {
		if strings.Contains(content, hidden) {
			t.Fatalf("calendar artifact leaked %q:\n%s", hidden, content)
		}
	}
	if got := strFromAny(observed["title"]); got != path {
		t.Fatalf("canvas title = %q, want %q", got, path)
	}
	artifacts, err := app.store.ListArtifactsByKind(store.ArtifactKind("calendar_view"))
	if err != nil {
		t.Fatalf("ListArtifactsByKind(calendar_view): %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("calendar_view artifacts = %d, want 1", len(artifacts))
	}
}

func TestClassifyAndExecuteSystemActionCalendarAvailabilityUsesAllBusyBlocks(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	now := time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC)
	app.calendarNow = func() time.Time { return now }
	app.newICSCalendarReader = func() (icsCalendarReader, error) { return stubICSCalendarReader{}, nil }
	if err := app.store.SetActiveSphere(store.SphereWork); err != nil {
		t.Fatalf("SetActiveSphere(work): %v", err)
	}
	if _, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work Calendar", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(work calendar): %v", err)
	}
	if _, err := app.store.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Family", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(private calendar): %v", err)
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
						Summary:    "Standup",
						Start:      time.Date(2026, time.March, 10, 9, 0, 0, 0, time.Local),
						End:        time.Date(2026, time.March, 10, 10, 0, 0, 0, time.Local),
					},
				},
				"family": {
					{
						CalendarID: "family",
						Summary:    "Dentist",
						Start:      time.Date(2026, time.March, 10, 11, 0, 0, 0, time.Local),
						End:        time.Date(2026, time.March, 10, 12, 0, 0, 0, time.Local),
					},
				},
			},
		}, nil
	}

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	var showCalls int
	canvasServer := setupMockCanvasShowServer(t, &showCalls, nil)
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

	_, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "when am I free tomorrow?")
	if !handled {
		t.Fatal("expected availability query to be handled")
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	rendered, err := os.ReadFile(filepath.Join(project.RootPath, strFromAny(payloads[0]["path"])))
	if err != nil {
		t.Fatalf("ReadFile(rendered): %v", err)
	}
	content := string(rendered)
	for _, snippet := range []string{"08:00 to 09:00", "10:00 to 11:00", "12:00 to 18:00", "Standup", "Busy (other sphere)"} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("availability artifact missing %q:\n%s", snippet, content)
		}
	}
	if strings.Contains(content, "Dentist") {
		t.Fatalf("availability artifact leaked private title:\n%s", content)
	}
	if showCalls != 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want 1", showCalls)
	}
}
