package web

import (
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

type timeTrackingContext struct {
	WorkspaceID *int64
	ProjectID   *string
	Sphere      string
}

func parseTimeEntryQueryTime(raw string) (*time.Time, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, nil
	}
	if len(clean) == len("2006-01-02") {
		value, err := time.ParseInLocation("2006-01-02", clean, time.UTC)
		if err != nil {
			return nil, err
		}
		return &value, nil
	}
	value, err := time.Parse(time.RFC3339, clean)
	if err != nil {
		return nil, err
	}
	value = value.UTC()
	return &value, nil
}

func timeEntryListFilterFromRequest(r *http.Request) (store.TimeEntryListFilter, error) {
	query := r.URL.Query()
	dateRaw := strings.TrimSpace(query.Get("date"))
	fromRaw := strings.TrimSpace(query.Get("from"))
	toRaw := strings.TrimSpace(query.Get("to"))
	from, err := parseTimeEntryQueryTime(fromRaw)
	if err != nil {
		return store.TimeEntryListFilter{}, errors.New("from must be YYYY-MM-DD or RFC3339")
	}
	to, err := parseTimeEntryQueryTime(toRaw)
	if err != nil {
		return store.TimeEntryListFilter{}, errors.New("to must be YYYY-MM-DD or RFC3339")
	}
	if to != nil && len(toRaw) == len("2006-01-02") {
		next := to.Add(24 * time.Hour)
		to = &next
	}
	if dateRaw != "" {
		if from != nil || to != nil {
			return store.TimeEntryListFilter{}, errors.New("date cannot be combined with from/to")
		}
		date, err := parseTimeEntryQueryTime(dateRaw)
		if err != nil {
			return store.TimeEntryListFilter{}, errors.New("date must be YYYY-MM-DD")
		}
		from = date
		next := date.Add(24 * time.Hour)
		to = &next
	}
	return store.TimeEntryListFilter{
		Sphere: strings.TrimSpace(query.Get("sphere")),
		From:   from,
		To:     to,
	}, nil
}

func csvSummaryRows(rows []store.TimeEntrySummary) [][]string {
	out := [][]string{{"key", "label", "seconds", "duration", "entry_count", "sphere", "workspace_id", "project_id"}}
	for _, row := range rows {
		workspaceID := ""
		if row.WorkspaceID != nil {
			workspaceID = strconv.FormatInt(*row.WorkspaceID, 10)
		}
		projectID := ""
		if row.ProjectID != nil {
			projectID = *row.ProjectID
		}
		out = append(out, []string{
			row.Key,
			row.Label,
			strconv.FormatInt(row.Seconds, 10),
			row.Duration,
			strconv.Itoa(row.EntryCount),
			row.Sphere,
			workspaceID,
			projectID,
		})
	}
	return out
}

func (a *App) currentTimeTrackingContext() (timeTrackingContext, error) {
	ctx := timeTrackingContext{Sphere: a.runtimeActiveSphere()}
	workspace, err := a.store.ActiveWorkspace()
	if err != nil && !isNoRows(err) {
		return timeTrackingContext{}, err
	}
	if err == nil {
		ctx.WorkspaceID = &workspace.ID
	}
	projectID, err := a.store.ActiveProjectID()
	if err != nil {
		return timeTrackingContext{}, err
	}
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		ctx.ProjectID = &projectID
	}
	return ctx, nil
}

func (a *App) syncTimeTrackingContext(activity string) (store.TimeEntry, bool, error) {
	ctx, err := a.currentTimeTrackingContext()
	if err != nil {
		return store.TimeEntry{}, false, err
	}
	return a.store.SwitchActiveTimeEntry(time.Now().UTC(), ctx.WorkspaceID, ctx.ProjectID, ctx.Sphere, activity, nil)
}

func (a *App) startTimeTrackingEntry(activity string) (store.TimeEntry, bool, error) {
	ctx, err := a.currentTimeTrackingContext()
	if err != nil {
		return store.TimeEntry{}, false, err
	}
	active, err := a.store.ActiveTimeEntry()
	if err != nil {
		return store.TimeEntry{}, false, err
	}
	if active != nil && strings.TrimSpace(activity) == "stamp_in" && timeEntryContextMatches(*active, ctx) {
		return *active, false, nil
	}
	return a.store.SwitchActiveTimeEntry(time.Now().UTC(), ctx.WorkspaceID, ctx.ProjectID, ctx.Sphere, activity, nil)
}

func (a *App) setActiveWorkspaceTracked(id int64, activity string) error {
	if err := a.store.SetActiveWorkspace(id); err != nil {
		return err
	}
	_, _, err := a.syncTimeTrackingContext(activity)
	if err == nil {
		a.broadcastWorkspaceBusyChanged()
	}
	return err
}

func (a *App) setActiveSphereTracked(sphere string, activity string) error {
	cleanSphere := normalizeRuntimeActiveSphere(sphere)
	if cleanSphere == "" {
		return errors.New("active sphere must be work or private")
	}
	if err := a.store.SetActiveSphere(cleanSphere); err != nil {
		return err
	}
	_, _, err := a.syncTimeTrackingContext(activity)
	return err
}

func (a *App) handleTimeEntryList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	filter, err := timeEntryListFilterFromRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := a.store.ListTimeEntries(filter)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"time_entries": entries,
	})
}

func (a *App) handleTimeEntrySummary(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	filter, err := timeEntryListFilterFromRequest(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	groupBy := strings.TrimSpace(r.URL.Query().Get("group_by"))
	if groupBy == "" {
		groupBy = "project"
	}
	summary, err := a.store.SummarizeTimeEntries(filter, groupBy, time.Now().UTC())
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "csv") {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		writer := csv.NewWriter(w)
		writer.WriteAll(csvSummaryRows(summary))
		if err := writer.Error(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"group_by": groupBy,
		"summary":  summary,
	})
}

func (a *App) handleTimeEntryStampIn(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	entry, started, err := a.startTimeTrackingEntry("stamp_in")
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"started":    started,
		"time_entry": entry,
	})
}

func (a *App) handleTimeEntryStampOut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	stopped, err := a.store.StopActiveTimeEntries(time.Now().UTC())
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"stopped_count": stopped,
	})
}

func timeEntryContextMatches(entry store.TimeEntry, ctx timeTrackingContext) bool {
	if entry.Sphere != ctx.Sphere {
		return false
	}
	switch {
	case entry.WorkspaceID == nil && ctx.WorkspaceID != nil:
		return false
	case entry.WorkspaceID != nil && ctx.WorkspaceID == nil:
		return false
	case entry.WorkspaceID != nil && ctx.WorkspaceID != nil && *entry.WorkspaceID != *ctx.WorkspaceID:
		return false
	}
	switch {
	case entry.ProjectID == nil && ctx.ProjectID != nil:
		return false
	case entry.ProjectID != nil && ctx.ProjectID == nil:
		return false
	case entry.ProjectID != nil && ctx.ProjectID != nil && *entry.ProjectID != *ctx.ProjectID:
		return false
	}
	return true
}
