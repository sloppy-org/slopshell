package web

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func waitForCondition(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestWorkspaceWatchAPIProcessesItemsSequentially(t *testing.T) {
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Watch", filepath.Join(t.TempDir(), "watch"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	first, err := app.store.CreateItem("First task", store.ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(first) error: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	second, err := app.store.CreateItem("Second task", store.ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(second) error: %v", err)
	}

	orderCh := make(chan int64, 2)
	app.workspaceWatchProcessor = func(_ context.Context, _ store.Workspace, item store.ItemSummary) error {
		orderCh <- item.ID
		return nil
	}

	rrStart := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspaces/"+itoa(workspace.ID)+"/watch", map[string]any{
		"poll_interval_seconds": 1,
		"config":                map[string]any{"worker": "codex"},
	})
	if rrStart.Code != http.StatusOK {
		t.Fatalf("watch start status = %d, want 200: %s", rrStart.Code, rrStart.Body.String())
	}

	waitForCondition(t, 2*time.Second, func() bool {
		gotFirst, err := app.store.GetItem(first.ID)
		if err != nil || gotFirst.State != store.ItemStateDone {
			return false
		}
		gotSecond, err := app.store.GetItem(second.ID)
		return err == nil && gotSecond.State == store.ItemStateDone
	})

	gotOrder := []int64{<-orderCh, <-orderCh}
	if gotOrder[0] != first.ID || gotOrder[1] != second.ID {
		t.Fatalf("processor order = %v, want [%d %d]", gotOrder, first.ID, second.ID)
	}

	rrStatus := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/workspaces/"+itoa(workspace.ID)+"/watch", nil)
	if rrStatus.Code != http.StatusOK {
		t.Fatalf("watch status code = %d, want 200: %s", rrStatus.Code, rrStatus.Body.String())
	}
	statusData := decodeJSONDataResponse(t, rrStatus)
	statusPayload, ok := statusData["status"].(map[string]any)
	if !ok {
		t.Fatalf("status payload = %#v", statusData)
	}
	if got := int(statusPayload["processed_count"].(float64)); got < 2 {
		t.Fatalf("processed_count = %d, want at least 2", got)
	}

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/watches", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("watch list code = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	listData := decodeJSONDataResponse(t, rrList)
	watches, ok := listData["watches"].([]any)
	if !ok || len(watches) != 1 {
		t.Fatalf("watch list payload = %#v", listData)
	}

	rrStop := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/workspaces/"+itoa(workspace.ID)+"/watch", nil)
	if rrStop.Code != http.StatusOK {
		t.Fatalf("watch stop code = %d, want 200: %s", rrStop.Code, rrStop.Body.String())
	}

	waitForCondition(t, 2*time.Second, func() bool {
		watch, err := app.store.GetWorkspaceWatch(workspace.ID)
		return err == nil && !watch.Enabled
	})
}

func TestWorkspaceWatchFailureMovesItemToWaiting(t *testing.T) {
	app := newAuthedTestApp(t)
	workspace, err := app.store.CreateWorkspace("Watch", filepath.Join(t.TempDir(), "watch"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	item, err := app.store.CreateItem("Retry task", store.ItemOptions{WorkspaceID: &workspace.ID})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	app.workspaceWatchProcessor = func(_ context.Context, _ store.Workspace, _ store.ItemSummary) error {
		return errors.New("processor failed")
	}

	rrStart := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspaces/"+itoa(workspace.ID)+"/watch", map[string]any{
		"poll_interval_seconds": 1,
	})
	if rrStart.Code != http.StatusOK {
		t.Fatalf("watch start status = %d, want 200: %s", rrStart.Code, rrStart.Body.String())
	}

	waitForCondition(t, 2*time.Second, func() bool {
		got, err := app.store.GetItem(item.ID)
		return err == nil && got.State == store.ItemStateWaiting && got.VisibleAfter != nil
	})
}
