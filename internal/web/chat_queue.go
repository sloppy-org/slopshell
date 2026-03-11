package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/krystophny/tabura/internal/appserver"
)

type chatTurnTracker struct {
	mu         sync.Mutex
	active     map[string]activeChatTurn
	queue      map[string]int
	outputMode map[string][]string
	localOnly  map[string][]bool
	messageID  map[string][]int64
	capture    map[string][]string
	cursor     map[string][]*chatCursorContext
	worker     map[string]bool
}

type activeChatTurn struct {
	runID  string
	cancel context.CancelFunc
}

func newChatTurnTracker() *chatTurnTracker {
	return &chatTurnTracker{
		active:     map[string]activeChatTurn{},
		queue:      map[string]int{},
		outputMode: map[string][]string{},
		localOnly:  map[string][]bool{},
		messageID:  map[string][]int64{},
		capture:    map[string][]string{},
		cursor:     map[string][]*chatCursorContext{},
		worker:     map[string]bool{},
	}
}

func (t *chatTurnTracker) register(sessionID, runID string, cancelFn context.CancelFunc) {
	t.mu.Lock()
	previous, hadPrevious := t.active[sessionID]
	t.active[sessionID] = activeChatTurn{
		runID:  runID,
		cancel: cancelFn,
	}
	t.mu.Unlock()
	if hadPrevious && previous.runID != runID && previous.cancel != nil {
		previous.cancel()
	}
}

func (t *chatTurnTracker) unregister(sessionID, runID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	current, ok := t.active[sessionID]
	if !ok {
		return
	}
	if current.runID == runID {
		delete(t.active, sessionID)
	}
}

func (t *chatTurnTracker) cancelActive(sessionID string) int {
	t.mu.Lock()
	current, ok := t.active[sessionID]
	if !ok {
		t.mu.Unlock()
		return 0
	}
	delete(t.active, sessionID)
	t.mu.Unlock()
	if current.cancel != nil {
		current.cancel()
	}
	return 1
}

func (t *chatTurnTracker) cancelAll() {
	t.mu.Lock()
	all := t.active
	t.active = map[string]activeChatTurn{}
	t.mu.Unlock()
	for _, run := range all {
		if run.cancel != nil {
			run.cancel()
		}
	}
}

func (t *chatTurnTracker) clearQueued(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	queued := t.queue[sessionID]
	delete(t.queue, sessionID)
	delete(t.outputMode, sessionID)
	delete(t.localOnly, sessionID)
	delete(t.messageID, sessionID)
	delete(t.capture, sessionID)
	delete(t.cursor, sessionID)
	return queued
}

func (t *chatTurnTracker) activeCount(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.active[sessionID]; ok {
		return 1
	}
	return 0
}

func (t *chatTurnTracker) activeRunID(sessionID string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active[sessionID].runID
}

func (t *chatTurnTracker) queuedCount(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.queue[sessionID]
}

func (t *chatTurnTracker) enqueue(sessionID, outputMode string, localOnlyFlag bool, messageID int64, captureMode string, cursor *chatCursorContext) (queued int, startWorker bool) {
	mode := normalizeTurnOutputMode(outputMode)
	t.mu.Lock()
	t.outputMode[sessionID] = append(t.outputMode[sessionID], mode)
	t.localOnly[sessionID] = append(t.localOnly[sessionID], localOnlyFlag)
	t.messageID[sessionID] = append(t.messageID[sessionID], messageID)
	t.capture[sessionID] = append(t.capture[sessionID], normalizeChatCaptureMode(captureMode))
	t.cursor[sessionID] = append(t.cursor[sessionID], normalizeChatCursorContext(cursor))
	t.queue[sessionID] = t.queue[sessionID] + 1
	queued = t.queue[sessionID]
	workerRunning := t.worker[sessionID]
	if !workerRunning {
		t.worker[sessionID] = true
	}
	t.mu.Unlock()
	startWorker = !workerRunning
	return
}

func (t *chatTurnTracker) dequeue(sessionID string) (dequeuedTurn, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	queued := t.queue[sessionID]
	if queued <= 0 {
		return dequeuedTurn{}, false
	}
	modes := t.outputMode[sessionID]
	mode := turnOutputModeVoice
	if len(modes) > 0 {
		mode = normalizeTurnOutputMode(modes[0])
		modes = modes[1:]
		if len(modes) == 0 {
			delete(t.outputMode, sessionID)
		} else {
			t.outputMode[sessionID] = modes
		}
	}
	localFlags := t.localOnly[sessionID]
	localOnlyFlag := false
	if len(localFlags) > 0 {
		localOnlyFlag = localFlags[0]
		localFlags = localFlags[1:]
		if len(localFlags) == 0 {
			delete(t.localOnly, sessionID)
		} else {
			t.localOnly[sessionID] = localFlags
		}
	}
	messageIDs := t.messageID[sessionID]
	messageID := int64(0)
	if len(messageIDs) > 0 {
		messageID = messageIDs[0]
		messageIDs = messageIDs[1:]
		if len(messageIDs) == 0 {
			delete(t.messageID, sessionID)
		} else {
			t.messageID[sessionID] = messageIDs
		}
	}
	captureModes := t.capture[sessionID]
	captureMode := chatCaptureModeText
	if len(captureModes) > 0 {
		captureMode = normalizeChatCaptureMode(captureModes[0])
		captureModes = captureModes[1:]
		if len(captureModes) == 0 {
			delete(t.capture, sessionID)
		} else {
			t.capture[sessionID] = captureModes
		}
	}
	cursors := t.cursor[sessionID]
	var cursor *chatCursorContext
	if len(cursors) > 0 {
		cursor = cursors[0]
		cursors = cursors[1:]
		if len(cursors) == 0 {
			delete(t.cursor, sessionID)
		} else {
			t.cursor[sessionID] = cursors
		}
	}
	queued--
	if queued <= 0 {
		delete(t.queue, sessionID)
		delete(t.outputMode, sessionID)
		delete(t.localOnly, sessionID)
		delete(t.messageID, sessionID)
		delete(t.capture, sessionID)
		delete(t.cursor, sessionID)
		return dequeuedTurn{
			outputMode:  mode,
			localOnly:   localOnlyFlag,
			messageID:   messageID,
			captureMode: captureMode,
			cursor:      cursor,
		}, true
	}
	t.queue[sessionID] = queued
	return dequeuedTurn{
		outputMode:  mode,
		localOnly:   localOnlyFlag,
		messageID:   messageID,
		captureMode: captureMode,
		cursor:      cursor,
	}, true
}

func (t *chatTurnTracker) markIdleIfEmpty(sessionID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.queue[sessionID] > 0 {
		return false
	}
	delete(t.worker, sessionID)
	return true
}

type dequeuedTurn struct {
	outputMode  string
	localOnly   bool
	messageID   int64
	captureMode string
	cursor      *chatCursorContext
}

type projectRunState struct {
	ActiveTurns  int    `json:"active_turns"`
	QueuedTurns  int    `json:"queued_turns"`
	IsWorking    bool   `json:"is_working"`
	Status       string `json:"status"`
	ActiveTurnID string `json:"active_turn_id,omitempty"`
}

func newProjectRunState(activeTurns, queuedTurns int, activeTurnID string) projectRunState {
	state := projectRunState{
		ActiveTurns: activeTurns,
		QueuedTurns: queuedTurns,
		IsWorking:   activeTurns > 0 || queuedTurns > 0,
		Status:      "idle",
	}
	if activeTurns > 0 {
		state.Status = "running"
	}
	if activeTurns == 0 && queuedTurns > 0 {
		state.Status = "queued"
	}
	if activeTurns > 0 {
		state.ActiveTurnID = strings.TrimSpace(activeTurnID)
	}
	return state
}

func (a *App) registerActiveChatTurn(sessionID, runID string, cancel context.CancelFunc) {
	a.turns.register(sessionID, runID, cancel)
	a.broadcastWorkspaceBusyChanged()
}

func (a *App) unregisterActiveChatTurn(sessionID, runID string) {
	a.turns.unregister(sessionID, runID)
	a.broadcastWorkspaceBusyChanged()
}

func (a *App) cancelActiveChatTurns(sessionID string) int {
	count := a.turns.cancelActive(sessionID)
	if count > 0 {
		a.broadcastWorkspaceBusyChanged()
	}
	return count
}

func (a *App) clearQueuedChatTurns(sessionID string) int {
	count := a.turns.clearQueued(sessionID)
	if count > 0 {
		a.broadcastWorkspaceBusyChanged()
	}
	return count
}

func (a *App) cancelChatWork(sessionID string) (int, int) {
	activeCanceled := a.cancelActiveChatTurns(sessionID)
	queuedCanceled := a.clearQueuedChatTurns(sessionID)
	if queuedCanceled > 0 {
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":  "turn_queue_cleared",
			"count": queuedCanceled,
		})
	}
	return activeCanceled, queuedCanceled
}

type clearAllReport struct {
	ActiveCanceled   int
	QueuedCanceled   int
	SessionsClosed   int
	TempFilesCleared int
}

func (a *App) clearCanvasForProject(projectKey string) {
	canvasSessionID := strings.TrimSpace(a.resolveCanvasSessionID(projectKey))
	if canvasSessionID == "" {
		return
	}
	port, ok := a.tunnels.getPort(canvasSessionID)
	if !ok {
		return
	}
	_, _ = a.mcpToolsCall(port, "canvas_clear", map[string]interface{}{
		"session_id": canvasSessionID,
		"reason":     "context reset",
	})
}

func (a *App) clearProjectTempCanvasFiles(projectKey string) int {
	cwd := strings.TrimSpace(a.cwdForProjectKey(projectKey))
	if cwd == "" {
		return 0
	}
	tmpDir := filepath.Join(cwd, ".tabura", "artifacts", "tmp")
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		return 0
	}
	cleared := 0
	for _, entry := range entries {
		target := filepath.Join(tmpDir, entry.Name())
		if err := os.RemoveAll(target); err == nil {
			cleared++
		}
	}
	return cleared
}

func (a *App) clearAllAgentsAndContexts(currentSessionID string) (clearAllReport, error) {
	report := clearAllReport{}
	sessions, err := a.store.ListChatSessions()
	if err != nil {
		return report, err
	}
	for _, session := range sessions {
		activeCanceled, queuedCanceled := a.cancelChatWork(session.ID)
		report.ActiveCanceled += activeCanceled
		report.QueuedCanceled += queuedCanceled
		report.TempFilesCleared += a.clearProjectTempCanvasFiles(session.ProjectKey)
		a.clearCanvasForProject(session.ProjectKey)
		a.broadcastChatEvent(session.ID, map[string]interface{}{
			"type": "chat_cleared",
		})
	}
	closed := 0
	a.mu.Lock()
	appSessions := a.chatAppSessions
	a.chatAppSessions = map[string]*appserver.Session{}
	a.mu.Unlock()
	for _, s := range appSessions {
		if s == nil {
			continue
		}
		_ = s.Close()
		closed++
	}
	report.SessionsClosed = closed
	if err := a.store.ClearAllChatMessages(); err != nil {
		return report, err
	}
	if err := a.store.ClearAllChatEvents(); err != nil {
		return report, err
	}
	if err := a.store.ResetAllChatSessionThreads(); err != nil {
		return report, err
	}
	if strings.TrimSpace(currentSessionID) != "" {
		a.closeAppSession(currentSessionID)
	}
	return report, nil
}

func (a *App) activeChatTurnCount(sessionID string) int {
	return a.turns.activeCount(sessionID)
}

func (a *App) activeChatTurnID(sessionID string) string {
	return a.turns.activeRunID(sessionID)
}

func (a *App) queuedChatTurnCount(sessionID string) int {
	return a.turns.queuedCount(sessionID)
}

func (a *App) projectRunStateForSession(sessionID string) projectRunState {
	activeTurns := a.activeChatTurnCount(sessionID)
	queuedTurns := a.queuedChatTurnCount(sessionID)
	return newProjectRunState(activeTurns, queuedTurns, a.activeChatTurnID(sessionID))
}

func (a *App) enqueueAssistantTurn(sessionID, outputMode string, opts ...chatTurnOptions) int {
	options := chatTurnOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}
	queued, startWorker := a.turns.enqueue(
		sessionID,
		outputMode,
		options.localOnly,
		options.messageID,
		options.captureMode,
		options.cursor,
	)
	if startWorker {
		a.startAssistantTurnWorker(sessionID)
	}
	a.broadcastWorkspaceBusyChanged()
	return queued
}

func (a *App) dequeueAssistantTurn(sessionID string) (dequeuedTurn, bool) {
	turn, ok := a.turns.dequeue(sessionID)
	if ok {
		a.broadcastWorkspaceBusyChanged()
	}
	return turn, ok
}

func (a *App) markAssistantWorkerIdleIfQueueEmpty(sessionID string) bool {
	return a.turns.markIdleIfEmpty(sessionID)
}

func (a *App) startAssistantTurnWorker(sessionID string) {
	a.workerWG.Add(1)
	go func() {
		defer a.workerWG.Done()
		a.runAssistantTurnQueue(sessionID)
	}()
}

func (a *App) shutdownRequested() bool {
	if a == nil || a.shutdownCtx == nil {
		return false
	}
	select {
	case <-a.shutdownCtx.Done():
		return true
	default:
		return false
	}
}

func (a *App) runAssistantTurnQueue(sessionID string) {
	for {
		if a.shutdownRequested() {
			return
		}
		turn, ok := a.dequeueAssistantTurn(sessionID)
		if !ok {
			if a.markAssistantWorkerIdleIfQueueEmpty(sessionID) {
				return
			}
			continue
		}
		if a.shutdownRequested() {
			return
		}
		a.runAssistantTurn(sessionID, turn)
	}
}

type chatTurnOptions struct {
	localOnly   bool
	messageID   int64
	captureMode string
	cursor      *chatCursorContext
}

func (a *App) getOrCreateAppSession(sessionID string, cwd string, profile appServerModelProfile) (*appserver.Session, string, bool, error) {
	bindingSession, workspace, err := a.appSessionBindingForChatSessionID(sessionID)
	if err != nil {
		return nil, "", false, err
	}
	cwd = strings.TrimSpace(workspace.DirPath)
	appSessionKey := bindingSession.ID
	a.mu.Lock()
	s := a.chatAppSessions[appSessionKey]
	a.mu.Unlock()
	if s != nil && s.IsOpen() && s.MatchesConfig(cwd, profile.Model, profile.ThreadParams) {
		return s, appSessionKey, true, nil
	}
	if s != nil {
		_ = s.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	existingThreadID := strings.TrimSpace(bindingSession.AppThreadID)
	var newSess *appserver.Session
	var resumed bool
	if existingThreadID != "" {
		rs, ok, err := a.appServerClient.ResumeSessionWithParams(ctx, cwd, profile.Model, profile.ThreadParams, existingThreadID)
		if err != nil {
			return nil, "", false, err
		}
		newSess = rs
		resumed = ok
	} else {
		rs, err := a.appServerClient.OpenSessionWithParams(ctx, cwd, profile.Model, profile.ThreadParams)
		if err != nil {
			return nil, "", false, err
		}
		newSess = rs
	}
	a.mu.Lock()
	if old := a.chatAppSessions[appSessionKey]; old != nil {
		_ = old.Close()
	}
	a.chatAppSessions[appSessionKey] = newSess
	a.mu.Unlock()
	return newSess, appSessionKey, resumed, nil
}

func (a *App) closeAppSession(sessionID string) {
	if bindingSession, _, err := a.appSessionBindingForChatSessionID(sessionID); err == nil {
		sessionID = bindingSession.ID
	}
	a.mu.Lock()
	s := a.chatAppSessions[sessionID]
	delete(a.chatAppSessions, sessionID)
	a.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
}

func (a *App) closeAllAppSessions() {
	a.mu.Lock()
	appSessions := a.chatAppSessions
	a.chatAppSessions = map[string]*appserver.Session{}
	a.mu.Unlock()
	for _, s := range appSessions {
		if s != nil {
			_ = s.Close()
		}
	}
}

func (a *App) waitForAssistantWorkers(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	done := make(chan struct{})
	go func() {
		a.workerWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
