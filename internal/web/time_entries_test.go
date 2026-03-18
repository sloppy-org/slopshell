package web

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func TestTimeEntriesTrackProjectWorkspaceAndSphereSwitches(t *testing.T) {
	app := newAuthedTestApp(t)

	project, err := app.store.CreateProject("Tabura", "tabura", filepath.Join(t.TempDir(), "project"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Workspace", filepath.Join(t.TempDir(), "workspace"), store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}

	rrProject := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/runtime/workspaces/"+projectIDString(project.ID)+"/activate", nil)
	if rrProject.Code != http.StatusOK {
		t.Fatalf("project activate status = %d, want 200: %s", rrProject.Code, rrProject.Body.String())
	}

	rrWorkspace := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/workspaces/"+itoa(workspace.ID), map[string]any{
		"is_active": true,
	})
	if rrWorkspace.Code != http.StatusOK {
		t.Fatalf("workspace activate status = %d, want 200: %s", rrWorkspace.Code, rrWorkspace.Body.String())
	}

	rrSphere := doAuthedJSONRequest(t, app.Router(), http.MethodPatch, "/api/runtime/preferences", map[string]any{
		"active_sphere": store.SphereWork,
	})
	if rrSphere.Code != http.StatusOK {
		t.Fatalf("sphere update status = %d, want 200: %s", rrSphere.Code, rrSphere.Body.String())
	}

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/time-entries", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("time entry list status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	payload := decodeJSONDataResponse(t, rrList)
	entries, ok := payload["time_entries"].([]any)
	if !ok || len(entries) != 3 {
		t.Fatalf("time entries payload = %#v, want 3 entries", payload)
	}
	last, ok := entries[len(entries)-1].(map[string]any)
	if !ok {
		t.Fatalf("last time entry = %#v", entries[len(entries)-1])
	}
	if got := strFromAny(last["activity"]); got != "sphere_switch" {
		t.Fatalf("last activity = %q, want %q", got, "sphere_switch")
	}
	if got := strFromAny(last["sphere"]); got != store.SphereWork {
		t.Fatalf("last sphere = %q, want %q", got, store.SphereWork)
	}
	if got := int64(last["workspace_id"].(float64)); got != workspace.ID {
		t.Fatalf("last workspace_id = %d, want %d", got, workspace.ID)
	}
	if _, ok := last["ended_at"]; ok {
		t.Fatalf("last ended_at = %#v, want active entry", last["ended_at"])
	}
}

func TestTimeEntrySummaryCSVAndManualStampAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	project, err := app.store.CreateProject("Tabura", "tabura", filepath.Join(t.TempDir(), "project"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Workspace", filepath.Join(t.TempDir(), "workspace"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}

	start := time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)
	middle := start.Add(90 * time.Minute)
	end := middle.Add(30 * time.Minute)
	if _, _, err := app.store.SwitchActiveTimeEntry(start, &workspace.ID, store.SphereWork, "workspace_switch", nil); err != nil {
		t.Fatalf("SwitchActiveTimeEntry(first) error: %v", err)
	}
	if _, _, err := app.store.SwitchActiveTimeEntry(middle, nil, store.SphereWork, "workspace_switch", nil); err != nil {
		t.Fatalf("SwitchActiveTimeEntry(second) error: %v", err)
	}
	if _, err := app.store.StopActiveTimeEntries(end); err != nil {
		t.Fatalf("StopActiveTimeEntries() error: %v", err)
	}

	from := start.Format(time.RFC3339)
	to := end.Format(time.RFC3339)
	rrSummary := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/time-entries/summary?from="+from+"&to="+to+"&group_by=project", nil)
	if rrSummary.Code != http.StatusOK {
		t.Fatalf("summary status = %d, want 200: %s", rrSummary.Code, rrSummary.Body.String())
	}
	summaryPayload := decodeJSONDataResponse(t, rrSummary)
	rows, ok := summaryPayload["summary"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("summary payload = %#v", summaryPayload)
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("summary row = %#v", rows[0])
	}
	if got := strFromAny(row["label"]); got != project.Name {
		t.Fatalf("summary label = %q, want %q", got, project.Name)
	}
	if got := int64(row["seconds"].(float64)); got != 2*60*60 {
		t.Fatalf("summary seconds = %d, want %d", got, 2*60*60)
	}

	rrCSV := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/time-entries/summary?from="+from+"&to="+to+"&group_by=project&format=csv", nil)
	if rrCSV.Code != http.StatusOK {
		t.Fatalf("summary csv status = %d, want 200: %s", rrCSV.Code, rrCSV.Body.String())
	}
	if got := rrCSV.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("summary csv content type = %q, want text/csv", got)
	}
	if !strings.Contains(rrCSV.Body.String(), project.Name+",7200,2h,2") {
		t.Fatalf("summary csv body = %q, want project duration row", rrCSV.Body.String())
	}

	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveSphere(store.SphereWork); err != nil {
		t.Fatalf("SetActiveSphere() error: %v", err)
	}

	rrStampIn := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/time-entries/stamp-in", map[string]any{})
	if rrStampIn.Code != http.StatusOK {
		t.Fatalf("stamp-in status = %d, want 200: %s", rrStampIn.Code, rrStampIn.Body.String())
	}
	stampInPayload := decodeJSONDataResponse(t, rrStampIn)
	if started, _ := stampInPayload["started"].(bool); !started {
		t.Fatalf("stamp-in payload = %#v, want started=true", stampInPayload)
	}

	rrStampOut := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/time-entries/stamp-out", map[string]any{})
	if rrStampOut.Code != http.StatusOK {
		t.Fatalf("stamp-out status = %d, want 200: %s", rrStampOut.Code, rrStampOut.Body.String())
	}
	stampOutPayload := decodeJSONDataResponse(t, rrStampOut)
	if got := int64(stampOutPayload["stopped_count"].(float64)); got != 1 {
		t.Fatalf("stamp-out stopped_count = %d, want 1", got)
	}

	entries, err := app.store.ListTimeEntries(store.TimeEntryListFilter{ActiveOnly: true})
	if err != nil {
		t.Fatalf("ListTimeEntries(active) error: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("active time entries = %#v, want none", entries)
	}
}
