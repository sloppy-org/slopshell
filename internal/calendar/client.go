package calendar

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/krystophny/tabura/internal/googleauth"
	"github.com/krystophny/tabura/internal/providerdata"
	gcal "google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type Client struct {
	service *gcal.Service
	auth    *googleauth.Session
}

func New(ctx context.Context) (*Client, error) {
	return NewWithFiles(ctx, "", "")
}

func NewWithFiles(ctx context.Context, credentialsPath, tokenPath string) (*Client, error) {
	auth, err := googleauth.New(credentialsPath, tokenPath, googleauth.DefaultScopes)
	if err != nil {
		return nil, err
	}
	tokenSource, err := auth.TokenSource(ctx)
	if err != nil {
		return nil, err
	}
	service, err := gcal.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("create Google Calendar service: %w", err)
	}
	return &Client{service: service, auth: auth}, nil
}

func (c *Client) GetAuthURL() string {
	if c == nil || c.auth == nil {
		return ""
	}
	return c.auth.GetAuthURL()
}

func (c *Client) ExchangeCode(ctx context.Context, code string) error {
	if c == nil || c.auth == nil {
		return fmt.Errorf("google calendar auth is not configured")
	}
	return c.auth.ExchangeCode(ctx, code)
}

func (c *Client) ListCalendars(ctx context.Context) ([]providerdata.Calendar, error) {
	result, err := c.service.CalendarList.List().Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("list calendars: %w", err)
	}

	calendars := make([]providerdata.Calendar, 0, len(result.Items))
	for _, cal := range result.Items {
		calendars = append(calendars, providerdata.Calendar{
			ID:          cal.Id,
			Name:        cal.Summary,
			Description: cal.Description,
			TimeZone:    cal.TimeZone,
			Primary:     cal.Primary,
		})
	}
	return calendars, nil
}

type GetEventsOptions struct {
	CalendarID string
	TimeMin    time.Time
	TimeMax    time.Time
	MaxResults int64
	Query      string
}

func (c *Client) GetEvents(ctx context.Context, opts GetEventsOptions) ([]providerdata.Event, error) {
	if opts.CalendarID == "" {
		opts.CalendarID = "primary"
	}
	if opts.TimeMin.IsZero() {
		opts.TimeMin = time.Now()
	}
	if opts.TimeMax.IsZero() {
		opts.TimeMax = opts.TimeMin.Add(30 * 24 * time.Hour)
	}
	if opts.MaxResults == 0 {
		opts.MaxResults = 100
	}

	call := c.service.Events.List(opts.CalendarID).
		Context(ctx).
		TimeMin(opts.TimeMin.Format(time.RFC3339)).
		TimeMax(opts.TimeMax.Format(time.RFC3339)).
		MaxResults(opts.MaxResults).
		SingleEvents(true).
		OrderBy("startTime")
	if opts.Query != "" {
		call = call.Q(opts.Query)
	}

	result, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("get calendar events: %w", err)
	}

	events := make([]providerdata.Event, 0, len(result.Items))
	for _, item := range result.Items {
		event := providerdata.Event{
			ID:          item.Id,
			CalendarID:  opts.CalendarID,
			Summary:     item.Summary,
			Description: item.Description,
			Location:    item.Location,
			Status:      item.Status,
			Recurring:   item.RecurringEventId != "",
		}
		if event.Summary == "" {
			event.Summary = "(No title)"
		}
		if item.Organizer != nil {
			event.Organizer = item.Organizer.Email
		}
		for _, att := range item.Attendees {
			event.Attendees = append(event.Attendees, att.Email)
		}
		event.Start, event.AllDay = parseEventTime(item.Start)
		event.End, _ = parseEventTime(item.End)
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Start.Before(events[j].Start)
	})
	return events, nil
}

func parseEventTime(eventTime *gcal.EventDateTime) (time.Time, bool) {
	if eventTime == nil {
		return time.Time{}, false
	}
	if eventTime.DateTime != "" {
		t, err := time.Parse(time.RFC3339, eventTime.DateTime)
		if err == nil {
			return t, false
		}
	}
	if eventTime.Date != "" {
		t, err := time.Parse("2006-01-02", eventTime.Date)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
