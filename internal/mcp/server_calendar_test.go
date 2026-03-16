package mcp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	tabcalendar "github.com/krystophny/tabura/internal/calendar"
	"github.com/krystophny/tabura/internal/canvas"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type stubCalendarReader struct {
	calendars []providerdata.Calendar
	events    map[string][]providerdata.Event
}

func (s *stubCalendarReader) ListCalendars(context.Context) ([]providerdata.Calendar, error) {
	return append([]providerdata.Calendar(nil), s.calendars...), nil
}

func (s *stubCalendarReader) GetEvents(_ context.Context, opts tabcalendar.GetEventsOptions) ([]providerdata.Event, error) {
	return append([]providerdata.Event(nil), s.events[opts.CalendarID]...), nil
}

func TestCalendarListUsesGmailFallback(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "tabura.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Gmail", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(gmail): %v", err)
	}

	s := NewServerWithStore(canvas.NewAdapter(t.TempDir(), nil), st)
	s.newGoogleCalendarReader = func(context.Context) (googleCalendarReader, error) {
		return &stubCalendarReader{
			calendars: []providerdata.Calendar{{ID: "primary", Name: "Primary", Primary: true}},
		}, nil
	}
	got, err := s.callTool("calendar_list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("calendar_list failed: %v", err)
	}
	calendars, _ := got["calendars"].([]map[string]interface{})
	if len(calendars) != 1 {
		t.Fatalf("calendar count = %d, want 1", len(calendars))
	}
	if strFromAny(calendars[0]["sphere"]) != store.SpherePrivate {
		t.Fatalf("sphere = %q, want %q", strFromAny(calendars[0]["sphere"]), store.SpherePrivate)
	}
}

func TestCalendarEventsReturnsStructuredEvents(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "tabura.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGoogleCalendar, "Work", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}

	start := time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC)
	s := NewServerWithStore(canvas.NewAdapter(t.TempDir(), nil), st)
	s.newGoogleCalendarReader = func(context.Context) (googleCalendarReader, error) {
		return &stubCalendarReader{
			calendars: []providerdata.Calendar{{ID: "work", Name: "Work"}},
			events: map[string][]providerdata.Event{
				"work": {{
					ID:         "evt-1",
					CalendarID: "work",
					Summary:    "Standup",
					Start:      start,
					End:        start.Add(time.Hour),
					Organizer:  "alice@example.com",
				}},
			},
		}, nil
	}
	got, err := s.callTool("calendar_events", map[string]interface{}{
		"calendar_id": "work",
		"days":        7,
		"limit":       10,
	})
	if err != nil {
		t.Fatalf("calendar_events failed: %v", err)
	}
	events, _ := got["events"].([]map[string]interface{})
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if strFromAny(events[0]["summary"]) != "Standup" {
		t.Fatalf("summary = %q, want Standup", strFromAny(events[0]["summary"]))
	}
	if strFromAny(events[0]["provider"]) != store.ExternalProviderGoogleCalendar {
		t.Fatalf("provider = %q, want %q", strFromAny(events[0]["provider"]), store.ExternalProviderGoogleCalendar)
	}
	if strFromAny(events[0]["calendar_name"]) != "Work" {
		t.Fatalf("calendar_name = %q, want Work", strFromAny(events[0]["calendar_name"]))
	}
}
