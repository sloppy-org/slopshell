package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/store"
)

const (
	workspaceWatchReconcileInterval = 2 * time.Second
	workspaceWatchProcessTimeout    = 30 * time.Minute
)

type workspaceWatchProcessorFunc func(context.Context, store.Workspace, store.ItemSummary) error

type workspaceWatchStartRequest struct {
	Config              map[string]any `json:"config"`
	PollIntervalSeconds int            `json:"poll_interval_seconds"`
}

type workspaceWatchStatus struct {
	WorkspaceID    int64   `json:"workspace_id"`
	Active         bool    `json:"active"`
	PendingStop    bool    `json:"pending_stop"`
	CurrentItemID  *int64  `json:"current_item_id,omitempty"`
	CurrentBatchID *int64  `json:"current_batch_id,omitempty"`
	ProcessedCount int     `json:"processed_count"`
	StartedAt      string  `json:"started_at,omitempty"`
	LastError      *string `json:"last_error,omitempty"`
}

type workspaceWatchHandle struct {
	status        workspaceWatchStatus
	stopRequested bool
	wake          chan struct{}
}

type workspaceWatchTracker struct {
	mu      sync.Mutex
	handles map[int64]*workspaceWatchHandle
}

func newWorkspaceWatchTracker() *workspaceWatchTracker {
	return &workspaceWatchTracker{handles: make(map[int64]*workspaceWatchHandle)}
}

func (t *workspaceWatchTracker) list() []workspaceWatchStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]workspaceWatchStatus, 0, len(t.handles))
	for _, handle := range t.handles {
		out = append(out, handle.status)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkspaceID < out[j].WorkspaceID })
	return out
}

func (t *workspaceWatchTracker) snapshot(workspaceID int64) (workspaceWatchStatus, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	handle, ok := t.handles[workspaceID]
	if !ok {
		return workspaceWatchStatus{}, false
	}
	return handle.status, true
}

func (t *workspaceWatchTracker) ensureRunning(workspaceID int64, batchID *int64, startedAt string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.handles[workspaceID]; exists {
		return false
	}
	t.handles[workspaceID] = &workspaceWatchHandle{
		status: workspaceWatchStatus{
			WorkspaceID:    workspaceID,
			Active:         true,
			CurrentBatchID: copyInt64Pointer(batchID),
			StartedAt:      strings.TrimSpace(startedAt),
		},
		wake: make(chan struct{}, 1),
	}
	return true
}

func (t *workspaceWatchTracker) update(workspaceID int64, fn func(*workspaceWatchStatus)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	handle := t.handles[workspaceID]
	if handle == nil {
		return
	}
	fn(&handle.status)
}

func (t *workspaceWatchTracker) requestStop(workspaceID int64) {
	t.mu.Lock()
	handle := t.handles[workspaceID]
	if handle == nil {
		t.mu.Unlock()
		return
	}
	handle.stopRequested = true
	handle.status.PendingStop = true
	wake := handle.wake
	t.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (t *workspaceWatchTracker) stopRequested(workspaceID int64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	handle := t.handles[workspaceID]
	return handle != nil && handle.stopRequested
}

func (t *workspaceWatchTracker) wake(workspaceID int64) {
	t.mu.Lock()
	handle := t.handles[workspaceID]
	if handle == nil {
		t.mu.Unlock()
		return
	}
	wake := handle.wake
	t.mu.Unlock()
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (t *workspaceWatchTracker) wakeChannel(workspaceID int64) <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	handle := t.handles[workspaceID]
	if handle == nil {
		return nil
	}
	return handle.wake
}

func (t *workspaceWatchTracker) finish(workspaceID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.handles, workspaceID)
}

func (a *App) resumeWorkspaceWatches() {
	a.reconcileWorkspaceWatches()
	a.startWorkspaceWatchReconciler()
}

func (a *App) startWorkspaceWatchReconciler() {
	if a == nil || a.shutdownCtx == nil || a.store == nil {
		return
	}
	a.workerWG.Add(1)
	go func() {
		defer a.workerWG.Done()
		ticker := time.NewTicker(workspaceWatchReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-a.shutdownCtx.Done():
				return
			case <-ticker.C:
				a.reconcileWorkspaceWatches()
			}
		}
	}()
}

func (a *App) reconcileWorkspaceWatches() {
	if a == nil || a.store == nil || a.workspaceWatches == nil {
		return
	}
	watches, err := a.store.ListWorkspaceWatches(false)
	if err != nil {
		return
	}
	desired := make(map[int64]store.WorkspaceWatch, len(watches))
	for _, watch := range watches {
		desired[watch.WorkspaceID] = watch
		if !watch.Enabled {
			a.workspaceWatches.requestStop(watch.WorkspaceID)
			continue
		}
		a.workspaceWatches.wake(watch.WorkspaceID)
		if _, active := a.workspaceWatches.snapshot(watch.WorkspaceID); active {
			continue
		}
		a.launchWorkspaceWatch(watch)
	}
	for _, status := range a.workspaceWatches.list() {
		watch, ok := desired[status.WorkspaceID]
		if !ok || !watch.Enabled {
			a.workspaceWatches.requestStop(status.WorkspaceID)
		}
	}
}

func (a *App) launchWorkspaceWatch(watch store.WorkspaceWatch) {
	if a == nil || a.store == nil || a.workspaceWatches == nil {
		return
	}
	if !watch.Enabled {
		return
	}
	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if !a.workspaceWatches.ensureRunning(watch.WorkspaceID, watch.CurrentBatchID, startedAt) {
		return
	}
	a.broadcastWorkspaceWatchStatus(watch.WorkspaceID)
	a.workerWG.Add(1)
	go func() {
		defer a.workerWG.Done()
		a.runWorkspaceWatchLoop(watch.WorkspaceID)
	}()
}

func (a *App) runWorkspaceWatchLoop(workspaceID int64) {
	defer func() {
		a.workspaceWatches.finish(workspaceID)
		a.broadcastWorkspaceWatchStatus(workspaceID)
	}()

	for {
		watch, err := a.store.GetWorkspaceWatch(workspaceID)
		if err != nil {
			return
		}
		if !watch.Enabled && !a.workspaceWatches.stopRequested(workspaceID) {
			return
		}
		workspace, err := a.store.GetWorkspace(workspaceID)
		if err != nil {
			a.setWorkspaceWatchError(workspaceID, err)
			return
		}
		watch, err = a.ensureWorkspaceWatchBatch(workspace, watch)
		if err != nil {
			a.setWorkspaceWatchError(workspaceID, err)
			if !a.waitWorkspaceWatch(workspaceID, watch.PollIntervalSeconds) {
				return
			}
			continue
		}
		if err := a.workspaceWatchSyncSources(); err != nil {
			a.setWorkspaceWatchError(workspaceID, err)
		}
		item, ok, err := a.nextWorkspaceWatchItem(workspace.ID)
		if err != nil {
			a.setWorkspaceWatchError(workspaceID, err)
			if !a.waitWorkspaceWatch(workspaceID, watch.PollIntervalSeconds) {
				return
			}
			continue
		}
		if !ok {
			if a.workspaceWatches.stopRequested(workspaceID) || !watch.Enabled {
				a.finishWorkspaceWatchBatch(watch.CurrentBatchID)
				return
			}
			if !a.waitWorkspaceWatch(workspaceID, watch.PollIntervalSeconds) {
				a.finishWorkspaceWatchBatch(watch.CurrentBatchID)
				return
			}
			continue
		}

		a.workspaceWatches.update(workspaceID, func(status *workspaceWatchStatus) {
			status.CurrentItemID = copyInt64Pointer(&item.ID)
			status.CurrentBatchID = copyInt64Pointer(watch.CurrentBatchID)
			status.LastError = nil
		})
		a.broadcastWorkspaceWatchStatus(workspaceID)

		err = a.processWorkspaceWatchIteration(workspace, watch, item)
		a.workspaceWatches.update(workspaceID, func(status *workspaceWatchStatus) {
			status.CurrentItemID = nil
			status.CurrentBatchID = copyInt64Pointer(watch.CurrentBatchID)
			status.ProcessedCount++
			if err != nil {
				msg := err.Error()
				status.LastError = &msg
			} else {
				status.LastError = nil
			}
		})
		a.broadcastWorkspaceWatchStatus(workspaceID)

		if a.workspaceWatches.stopRequested(workspaceID) || !watch.Enabled {
			a.finishWorkspaceWatchBatch(watch.CurrentBatchID)
			return
		}
	}
}

func (a *App) workspaceWatchSyncSources() error {
	if a == nil || a.sourceSync == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceSyncCommandTimeout)
	defer cancel()
	_, err := a.syncSourcesNow(ctx)
	return err
}

func (a *App) ensureWorkspaceWatchBatch(workspace store.Workspace, watch store.WorkspaceWatch) (store.WorkspaceWatch, error) {
	if watch.CurrentBatchID != nil && *watch.CurrentBatchID > 0 {
		run, err := a.store.GetBatchRun(*watch.CurrentBatchID)
		if err == nil && run.FinishedAt == nil {
			return watch, nil
		}
	}
	run, err := a.store.CreateBatchRun(workspace.ID, watch.ConfigJSON, "running")
	if err != nil {
		return store.WorkspaceWatch{}, err
	}
	return a.store.UpsertWorkspaceWatch(workspace.ID, watch.ConfigJSON, watch.PollIntervalSeconds, watch.Enabled, &run.ID)
}

func (a *App) finishWorkspaceWatchBatch(batchID *int64) {
	if a == nil || batchID == nil || *batchID <= 0 {
		return
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339Nano)
	_, _ = a.recordBatchRunStatus(*batchID, "completed", &finishedAt)
}

func (a *App) waitWorkspaceWatch(workspaceID int64, pollIntervalSeconds int) bool {
	delay := time.Duration(normalizeWorkspaceWatchPollIntervalSeconds(pollIntervalSeconds)) * time.Second
	if delay <= 0 {
		delay = 5 * time.Minute
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	wake := a.workspaceWatches.wakeChannel(workspaceID)
	select {
	case <-a.shutdownCtx.Done():
		return false
	case <-timer.C:
		return true
	case <-wake:
		return true
	}
}

func normalizeWorkspaceWatchPollIntervalSeconds(raw int) int {
	if raw <= 0 {
		return 300
	}
	return raw
}

func (a *App) nextWorkspaceWatchItem(workspaceID int64) (store.ItemSummary, bool, error) {
	filter := store.ItemListFilter{WorkspaceID: &workspaceID}
	items, err := a.store.ListInboxItemsFiltered(time.Now().UTC(), filter)
	if err != nil {
		return store.ItemSummary{}, false, err
	}
	if len(items) == 0 {
		return store.ItemSummary{}, false, nil
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt == items[j].CreatedAt {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt < items[j].CreatedAt
	})
	return items[0], true, nil
}

func (a *App) processWorkspaceWatchIteration(workspace store.Workspace, watch store.WorkspaceWatch, item store.ItemSummary) error {
	if watch.CurrentBatchID != nil {
		_, _ = a.recordBatchRunItemUpdate(*watch.CurrentBatchID, item.ID, store.BatchRunItemUpdate{Status: "running"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), workspaceWatchProcessTimeout)
	defer cancel()
	processor := a.workspaceWatchProcessor
	if processor == nil {
		processor = a.processWorkspaceWatchItem
	}
	err := processor(ctx, workspace, item)
	if err != nil {
		if watch.CurrentBatchID != nil {
			msg := err.Error()
			_, _ = a.recordBatchRunItemUpdate(*watch.CurrentBatchID, item.ID, store.BatchRunItemUpdate{
				Status:   "failed",
				ErrorMsg: &msg,
			})
		}
		retryAt := time.Now().UTC().Add(time.Duration(watch.PollIntervalSeconds) * time.Second).Format(time.RFC3339)
		if retryAt == "" {
			retryAt = time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
		}
		if triageErr := a.store.TriageItemLater(item.ID, retryAt); triageErr != nil {
			return fmt.Errorf("%w (triage failed: %v)", err, triageErr)
		}
		return err
	}
	if err := a.store.TriageItemDone(item.ID); err != nil {
		return err
	}
	if watch.CurrentBatchID != nil {
		_, _ = a.recordBatchRunItemUpdate(*watch.CurrentBatchID, item.ID, store.BatchRunItemUpdate{Status: "completed"})
	}
	return nil
}

func (a *App) processWorkspaceWatchItem(ctx context.Context, workspace store.Workspace, item store.ItemSummary) error {
	if a == nil || a.appServerClient == nil {
		return errors.New("workspace watch requires app-server")
	}
	prompt := strings.TrimSpace(fmt.Sprintf(
		"You are Tabura workspace watch mode. Process this workspace item end-to-end in the current working directory and stop when the item is handled.\n\nWorkspace: %s\nDirectory: %s\nItem #%d: %s\n",
		workspace.Name,
		workspace.DirPath,
		item.ID,
		item.Title,
	))
	resp, err := a.appServerClient.SendPrompt(ctx, appserver.PromptRequest{
		CWD:     workspace.DirPath,
		Prompt:  prompt,
		Timeout: workspaceWatchProcessTimeout,
	})
	if err != nil {
		return err
	}
	if resp == nil || strings.TrimSpace(resp.Message) == "" {
		return nil
	}
	return nil
}

func (a *App) setWorkspaceWatchError(workspaceID int64, err error) {
	if a == nil || err == nil {
		return
	}
	msg := err.Error()
	a.workspaceWatches.update(workspaceID, func(status *workspaceWatchStatus) {
		status.LastError = &msg
	})
	a.broadcastWorkspaceWatchStatus(workspaceID)
}

func (a *App) workspaceWatchSnapshot(workspaceID int64) (*store.WorkspaceWatch, workspaceWatchStatus, error) {
	watch, err := a.store.GetWorkspaceWatch(workspaceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			status, ok := a.workspaceWatches.snapshot(workspaceID)
			if !ok {
				return nil, workspaceWatchStatus{}, err
			}
			return nil, status, nil
		}
		return nil, workspaceWatchStatus{}, err
	}
	status, _ := a.workspaceWatches.snapshot(workspaceID)
	if status.WorkspaceID == 0 {
		status.WorkspaceID = workspaceID
		status.CurrentBatchID = copyInt64Pointer(watch.CurrentBatchID)
	}
	return &watch, status, nil
}

func encodeWorkspaceWatchConfig(config map[string]any) (string, error) {
	if len(config) == 0 {
		return "{}", nil
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *App) broadcastWorkspaceWatchStatus(workspaceID int64) {
	if a == nil {
		return
	}
	watch, status, err := a.workspaceWatchSnapshot(workspaceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return
	}
	payload := map[string]any{
		"type":   "workspace_watch",
		"status": status,
	}
	if watch != nil {
		payload["watch"] = watch
	}
	a.hub.forEachChatConn(func(conn *chatWSConn) {
		_ = conn.writeJSON(payload)
	})
}

func (a *App) handleWorkspaceWatchStart(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaceID, err := parseURLInt64Param(r, "workspace_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req workspaceWatchStartRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
	}
	configJSON, err := encodeWorkspaceWatchConfig(req.Config)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "config must be valid JSON")
		return
	}
	watch, err := a.store.UpsertWorkspaceWatch(workspaceID, configJSON, req.PollIntervalSeconds, true, nil)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.reconcileWorkspaceWatches()
	updatedWatch, status, err := a.workspaceWatchSnapshot(workspaceID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if updatedWatch == nil {
		updatedWatch = &watch
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"watch":  updatedWatch,
		"status": status,
	})
}

func (a *App) handleWorkspaceWatchGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaceID, err := parseURLInt64Param(r, "workspace_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	watch, status, err := a.workspaceWatchSnapshot(workspaceID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"watch":  watch,
		"status": status,
	})
}

func (a *App) handleWorkspaceWatchStop(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaceID, err := parseURLInt64Param(r, "workspace_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	watch, err := a.store.SetWorkspaceWatchEnabled(workspaceID, false)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.workspaceWatches.requestStop(workspaceID)
	updatedWatch, status, err := a.workspaceWatchSnapshot(workspaceID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if updatedWatch == nil {
		updatedWatch = &watch
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"watch":  updatedWatch,
		"status": status,
	})
}

func (a *App) handleWorkspaceWatchList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	watches, err := a.store.ListWorkspaceWatches(false)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	statusByWorkspace := make(map[int64]workspaceWatchStatus)
	for _, status := range a.workspaceWatches.list() {
		statusByWorkspace[status.WorkspaceID] = status
	}
	entries := make([]map[string]any, 0, len(watches))
	for _, watch := range watches {
		entries = append(entries, map[string]any{
			"watch":  watch,
			"status": statusByWorkspace[watch.WorkspaceID],
		})
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"watches": entries,
	})
}

func (a *App) executeWorkspaceWatchAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	workspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionWorkspaceRef(action.Params))
	if err != nil {
		return "", nil, err
	}
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "workspace_watch_start":
		watch, err := a.store.UpsertWorkspaceWatch(workspace.ID, "{}", 300, true, nil)
		if err != nil {
			return "", nil, err
		}
		a.reconcileWorkspaceWatches()
		updatedWatch, status, err := a.workspaceWatchSnapshot(workspace.ID)
		if err != nil {
			return "", nil, err
		}
		if updatedWatch == nil {
			updatedWatch = &watch
		}
		return fmt.Sprintf("Watching workspace %s.", workspace.Name), map[string]any{
			"type":   "workspace_watch",
			"watch":  updatedWatch,
			"status": status,
		}, nil
	case "workspace_watch_stop":
		watch, err := a.store.SetWorkspaceWatchEnabled(workspace.ID, false)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Sprintf("Workspace %s is not being watched.", workspace.Name), map[string]any{
					"type":         "workspace_watch",
					"workspace_id": workspace.ID,
				}, nil
			}
			return "", nil, err
		}
		a.workspaceWatches.requestStop(workspace.ID)
		updatedWatch, status, err := a.workspaceWatchSnapshot(workspace.ID)
		if err != nil {
			return "", nil, err
		}
		if updatedWatch == nil {
			updatedWatch = &watch
		}
		return fmt.Sprintf("Stopping workspace watch for %s after the current item.", workspace.Name), map[string]any{
			"type":   "workspace_watch",
			"watch":  updatedWatch,
			"status": status,
		}, nil
	case "workspace_watch_status":
		watch, status, err := a.workspaceWatchSnapshot(workspace.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Sprintf("Workspace %s is not being watched.", workspace.Name), map[string]any{
					"type":         "workspace_watch",
					"workspace_id": workspace.ID,
				}, nil
			}
			return "", nil, err
		}
		message := fmt.Sprintf("Workspace %s watch is idle.", workspace.Name)
		if status.Active {
			message = fmt.Sprintf("Workspace %s watch is active.", workspace.Name)
			if status.CurrentItemID != nil {
				message = fmt.Sprintf("Workspace %s is processing item %d.", workspace.Name, *status.CurrentItemID)
			}
		}
		return message, map[string]any{
			"type":   "workspace_watch",
			"watch":  watch,
			"status": status,
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported workspace watch action: %s", action.Action)
	}
}
