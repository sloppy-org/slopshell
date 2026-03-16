package web

import (
	"context"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

func TestCollectGoogleCalendarEventsFallsBackToGmailAccount(t *testing.T) {
	app := newAuthedTestApp(t)
	if _, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Gmail", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(gmail): %v", err)
	}
	start := time.Date(2026, time.March, 16, 9, 0, 0, 0, time.UTC)
	app.newGoogleCalendarReader = func(context.Context) (googleCalendarReader, error) {
		return &stubGoogleCalendarReader{
			calendars: []providerdata.Calendar{{ID: "primary", Name: "Primary"}},
			events: map[string][]providerdata.Event{
				"primary": {{
					ID:         "evt-1",
					CalendarID: "primary",
					Summary:    "Weekly sync",
					Start:      start,
					End:        start.Add(time.Hour),
				}},
			},
		}, nil
	}

	events, warnings, err := app.collectGoogleCalendarEvents(context.Background(), calendarActionRequest{View: calendarViewDay}, store.SpherePrivate, start.Add(-time.Hour), start.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("collectGoogleCalendarEvents() error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Sphere != store.SphereWork {
		t.Fatalf("sphere = %q, want %q", events[0].Sphere, store.SphereWork)
	}
}
