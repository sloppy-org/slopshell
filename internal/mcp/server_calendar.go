package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tabcalendar "github.com/krystophny/tabura/internal/calendar"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

func (s *Server) calendarList(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	accounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return map[string]interface{}{
			"provider":  store.ExternalProviderGoogleCalendar,
			"calendars": []map[string]interface{}{},
			"count":     0,
		}, nil
	}
	if s.newGoogleCalendarReader == nil {
		return nil, fmt.Errorf("google calendar reader is unavailable")
	}
	reader, err := s.newGoogleCalendarReader(context.Background())
	if err != nil {
		return nil, err
	}
	calendars, err := reader.ListCalendars(context.Background())
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(calendars))
	for _, cal := range calendars {
		items = append(items, map[string]interface{}{
			"id":          cal.ID,
			"name":        cal.Name,
			"description": cal.Description,
			"time_zone":   cal.TimeZone,
			"primary":     cal.Primary,
			"provider":    store.ExternalProviderGoogleCalendar,
			"sphere":      tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, cal.ID, cal.Name, "", accounts),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(strFromAny(items[i]["name"])) < strings.ToLower(strFromAny(items[j]["name"]))
	})
	return map[string]interface{}{
		"provider":  store.ExternalProviderGoogleCalendar,
		"calendars": items,
		"count":     len(items),
	}, nil
}

func (s *Server) calendarEvents(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	accounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return map[string]interface{}{
			"provider": store.ExternalProviderGoogleCalendar,
			"events":   []map[string]interface{}{},
			"count":    0,
		}, nil
	}
	if s.newGoogleCalendarReader == nil {
		return nil, fmt.Errorf("google calendar reader is unavailable")
	}
	reader, err := s.newGoogleCalendarReader(context.Background())
	if err != nil {
		return nil, err
	}
	calendars, err := reader.ListCalendars(context.Background())
	if err != nil {
		return nil, err
	}
	calendarID := strings.TrimSpace(strArg(args, "calendar_id"))
	query := strings.TrimSpace(strArg(args, "query"))
	days := intArg(args, "days", 30)
	if days <= 0 {
		days = 30
	}
	limit := intArg(args, "limit", 100)
	if limit <= 0 {
		limit = 100
	}
	now := time.Now()
	timeMin := now
	timeMax := now.Add(time.Duration(days) * 24 * time.Hour)
	calendarNames := make(map[string]string, len(calendars))
	events := make([]map[string]interface{}, 0, limit)
	for _, cal := range calendars {
		if calendarID != "" && !strings.EqualFold(strings.TrimSpace(cal.ID), calendarID) {
			continue
		}
		calendarNames[cal.ID] = cal.Name
		providerSphere := tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, cal.ID, cal.Name, "", accounts)
		items, err := reader.GetEvents(context.Background(), tabcalendar.GetEventsOptions{
			CalendarID: cal.ID,
			TimeMin:    timeMin,
			TimeMax:    timeMax,
			MaxResults: int64(limit),
			Query:      query,
		})
		if err != nil {
			return nil, fmt.Errorf("list events for %q: %w", cal.Name, err)
		}
		for _, event := range items {
			events = append(events, eventPayload(event, cal.Name, providerSphere))
		}
	}
	sort.Slice(events, func(i, j int) bool {
		iStart, _ := time.Parse(time.RFC3339, strFromAny(events[i]["start"]))
		jStart, _ := time.Parse(time.RFC3339, strFromAny(events[j]["start"]))
		if iStart.Equal(jStart) {
			return strings.ToLower(strFromAny(events[i]["summary"])) < strings.ToLower(strFromAny(events[j]["summary"]))
		}
		return iStart.Before(jStart)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return map[string]interface{}{
		"provider":      store.ExternalProviderGoogleCalendar,
		"calendar_id":   calendarID,
		"calendar_name": strings.TrimSpace(calendarNames[calendarID]),
		"days":          days,
		"query":         query,
		"events":        events,
		"count":         len(events),
	}, nil
}

func eventPayload(event providerdata.Event, calendarName, sphere string) map[string]interface{} {
	return map[string]interface{}{
		"id":            event.ID,
		"calendar_id":   event.CalendarID,
		"calendar_name": calendarName,
		"provider":      store.ExternalProviderGoogleCalendar,
		"sphere":        sphere,
		"summary":       event.Summary,
		"description":   event.Description,
		"location":      event.Location,
		"start":         event.Start.Format(time.RFC3339),
		"end":           event.End.Format(time.RFC3339),
		"all_day":       event.AllDay,
		"status":        event.Status,
		"organizer":     event.Organizer,
		"attendees":     append([]string(nil), event.Attendees...),
		"recurring":     event.Recurring,
	}
}

func strFromAny(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
