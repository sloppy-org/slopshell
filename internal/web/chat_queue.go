package web

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/krystophny/tabura/internal/appserver"
)

type chatTurnTracker struct {
	mu         sync.Mutex
	cancel     map[string]map[string]context.CancelFunc
	queue      map[string]int
	outputMode map[string][]string
	localOnly  map[string][]bool
	worker     map[string]bool
}

func newChatTurnTracker() *chatTurnTracker {
	return &chatTurnTracker{
		cancel:     map[string]map[string]context.CancelFunc{},
		queue:      map[string]int{},
		outputMode: map[string][]string{},
		localOnly:  map[string][]bool{},
		worker:     map[string]bool{},
	}
}

func (t *chatTurnTracker) register(sessionID, runID string, cancelFn context.CancelFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel[sessionID] == nil {
		t.cancel[sessionID] = map[string]context.CancelFunc{}
	}
	t.cancel[sessionID][runID] = cancelFn
}

func (t *chatTurnTracker) unregister(sessionID, runID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	runs := t.cancel[sessionID]
	if runs == nil {
		return
	}
	delete(runs, runID)
	if len(runs) == 0 {
		delete(t.cancel, sessionID)
	}
}

func (t *chatTurnTracker) cancelActive(sessionID string) int {
	t.mu.Lock()
	runs := t.cancel[sessionID]
	if len(runs) == 0 {
		t.mu.Unlock()
		return 0
	}
	cancels := make([]context.CancelFunc, 0, len(runs))
	for _, fn := range runs {
		cancels = append(cancels, fn)
	}
	delete(t.cancel, sessionID)
	t.mu.Unlock()
	for _, fn := range cancels {
		fn()
	}
	return len(cancels)
}

func (t *chatTurnTracker) cancelAll() {
	t.mu.Lock()
	all := t.cancel
	t.cancel = map[string]map[string]context.CancelFunc{}
	t.mu.Unlock()
	for _, runs := range all {
		for _, fn := range runs {
			fn()
		}
	}
}

func (t *chatTurnTracker) clearQueued(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	queued := t.queue[sessionID]
	delete(t.queue, sessionID)
	delete(t.outputMode, sessionID)
	return queued
}

func (t *chatTurnTracker) activeCount(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.cancel[sessionID])
}

func (t *chatTurnTracker) queuedCount(sessionID string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.queue[sessionID]
}

func (t *chatTurnTracker) enqueue(sessionID, outputMode string, localOnlyFlag bool) (queued int, startWorker bool) {
	mode := normalizeTurnOutputMode(outputMode)
	t.mu.Lock()
	t.outputMode[sessionID] = append(t.outputMode[sessionID], mode)
	t.localOnly[sessionID] = append(t.localOnly[sessionID], localOnlyFlag)
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
	queued--
	if queued <= 0 {
		delete(t.queue, sessionID)
		delete(t.outputMode, sessionID)
		delete(t.localOnly, sessionID)
		return dequeuedTurn{outputMode: mode, localOnly: localOnlyFlag}, true
	}
	t.queue[sessionID] = queued
	return dequeuedTurn{outputMode: mode, localOnly: localOnlyFlag}, true
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
	outputMode string
	localOnly  bool
}

func (a *App) registerActiveChatTurn(sessionID, runID string, cancel context.CancelFunc) {
	a.turns.register(sessionID, runID, cancel)
}

func (a *App) unregisterActiveChatTurn(sessionID, runID string) {
	a.turns.unregister(sessionID, runID)
}

func (a *App) cancelActiveChatTurns(sessionID string) int {
	return a.turns.cancelActive(sessionID)
}

func (a *App) clearQueuedChatTurns(sessionID string) int {
	return a.turns.clearQueued(sessionID)
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
	DelegateCanceled int
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
		report.DelegateCanceled += a.cancelDelegatedJobsForProject(session.ProjectKey)
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

func (a *App) delegateActiveJobsForProject(projectKey string) int {
	cwd := strings.TrimSpace(a.cwdForProjectKey(projectKey))
	if cwd == "" {
		return 0
	}
	canvasSessionID := strings.TrimSpace(a.resolveCanvasSessionID(projectKey))
	if canvasSessionID == "" {
		return 0
	}
	port, ok := a.tunnels.getPort(canvasSessionID)
	if !ok {
		return 0
	}
	status, err := a.mcpToolsCall(port, "delegate_to_model_active_count", map[string]interface{}{"cwd_prefix": cwd})
	if err != nil {
		log.Printf("delegate activity probe failed for project=%q cwd=%q: %v", projectKey, cwd, err)
		return 0
	}
	return intFromAny(status["active"], 0)
}

func (a *App) cancelDelegatedJobsForProject(projectKey string) int {
	cwd := strings.TrimSpace(a.cwdForProjectKey(projectKey))
	if cwd == "" {
		return 0
	}
	canvasSessionID := strings.TrimSpace(a.resolveCanvasSessionID(projectKey))
	if canvasSessionID == "" {
		return 0
	}
	port, ok := a.tunnels.getPort(canvasSessionID)
	if !ok {
		return 0
	}
	status, err := a.mcpToolsCall(port, "delegate_to_model_cancel_all", map[string]interface{}{"cwd_prefix": cwd})
	if err != nil {
		log.Printf("delegate cancel-all failed for project=%q cwd=%q: %v", projectKey, cwd, err)
		return 0
	}
	return intFromAny(status["canceled"], 0)
}

func (a *App) activeChatTurnCount(sessionID string) int {
	return a.turns.activeCount(sessionID)
}

func (a *App) queuedChatTurnCount(sessionID string) int {
	return a.turns.queuedCount(sessionID)
}

func (a *App) enqueueAssistantTurn(sessionID, outputMode string, opts ...bool) int {
	localOnlyFlag := len(opts) > 0 && opts[0]
	queued, startWorker := a.turns.enqueue(sessionID, outputMode, localOnlyFlag)
	if startWorker {
		go a.runAssistantTurnQueue(sessionID)
	}
	return queued
}

func (a *App) dequeueAssistantTurn(sessionID string) (dequeuedTurn, bool) {
	return a.turns.dequeue(sessionID)
}

func (a *App) markAssistantWorkerIdleIfQueueEmpty(sessionID string) bool {
	return a.turns.markIdleIfEmpty(sessionID)
}

func (a *App) runAssistantTurnQueue(sessionID string) {
	for {
		turn, ok := a.dequeueAssistantTurn(sessionID)
		if !ok {
			if a.markAssistantWorkerIdleIfQueueEmpty(sessionID) {
				return
			}
			continue
		}
		a.runAssistantTurn(sessionID, turn.outputMode, turn.localOnly)
	}
}

func (a *App) getOrCreateAppSession(sessionID string, cwd string, profile appServerModelProfile) (*appserver.Session, bool, error) {
	a.mu.Lock()
	s := a.chatAppSessions[sessionID]
	a.mu.Unlock()
	if s != nil && s.IsOpen() {
		return s, true, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var existingThreadID string
	if sess, err := a.store.GetChatSession(sessionID); err == nil {
		existingThreadID = strings.TrimSpace(sess.AppThreadID)
	}
	var newSess *appserver.Session
	var resumed bool
	if existingThreadID != "" {
		rs, ok, err := a.appServerClient.ResumeSessionWithParams(ctx, cwd, profile.Model, profile.ThreadParams, existingThreadID)
		if err != nil {
			return nil, false, err
		}
		newSess = rs
		resumed = ok
	} else {
		rs, err := a.appServerClient.OpenSessionWithParams(ctx, cwd, profile.Model, profile.ThreadParams)
		if err != nil {
			return nil, false, err
		}
		newSess = rs
	}
	a.mu.Lock()
	if old := a.chatAppSessions[sessionID]; old != nil {
		_ = old.Close()
	}
	a.chatAppSessions[sessionID] = newSess
	a.mu.Unlock()
	return newSess, resumed, nil
}

func (a *App) closeAppSession(sessionID string) {
	a.mu.Lock()
	s := a.chatAppSessions[sessionID]
	delete(a.chatAppSessions, sessionID)
	a.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
}
