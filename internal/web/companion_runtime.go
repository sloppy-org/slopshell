package web

import (
	"strings"
	"sync"
	"time"
)

const (
	companionRuntimeStateThinking = "thinking"
	companionRuntimeStateTalking  = "talking"
	companionRuntimeStateError    = "error"

	companionEventState             = "companion_state"
	companionEventTranscriptPartial = "companion_transcript_partial"
	companionEventTranscriptFinal   = "companion_transcript_final"
)

type companionRuntimeSnapshot struct {
	State                string `json:"state"`
	Reason               string `json:"reason,omitempty"`
	Error                string `json:"error,omitempty"`
	ProjectKey           string `json:"project_key,omitempty"`
	ChatSessionID        string `json:"chat_session_id,omitempty"`
	ParticipantSessionID string `json:"participant_session_id,omitempty"`
	ParticipantSegmentID int64  `json:"participant_segment_id,omitempty"`
	TurnID               string `json:"turn_id,omitempty"`
	OutputMode           string `json:"output_mode,omitempty"`
	UpdatedAt            int64  `json:"updated_at"`
}

type companionRuntimeTracker struct {
	mu     sync.Mutex
	states map[string]companionRuntimeSnapshot
}

func newCompanionRuntimeTracker() *companionRuntimeTracker {
	return &companionRuntimeTracker{
		states: map[string]companionRuntimeSnapshot{},
	}
}

func (t *companionRuntimeTracker) set(projectKey string, snapshot companionRuntimeSnapshot) companionRuntimeSnapshot {
	cleanProjectKey := strings.TrimSpace(projectKey)
	if cleanProjectKey == "" {
		return companionRuntimeSnapshot{}
	}
	snapshot.ProjectKey = cleanProjectKey
	snapshot.State = normalizeCompanionRuntimeState(snapshot.State)
	snapshot.Reason = strings.TrimSpace(snapshot.Reason)
	snapshot.Error = strings.TrimSpace(snapshot.Error)
	snapshot.ChatSessionID = strings.TrimSpace(snapshot.ChatSessionID)
	snapshot.ParticipantSessionID = strings.TrimSpace(snapshot.ParticipantSessionID)
	snapshot.TurnID = strings.TrimSpace(snapshot.TurnID)
	snapshot.OutputMode = normalizeTurnOutputMode(snapshot.OutputMode)
	if snapshot.UpdatedAt == 0 {
		snapshot.UpdatedAt = time.Now().Unix()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.states[cleanProjectKey] = snapshot
	return snapshot
}

func (t *companionRuntimeTracker) get(projectKey string) (companionRuntimeSnapshot, bool) {
	cleanProjectKey := strings.TrimSpace(projectKey)
	if cleanProjectKey == "" {
		return companionRuntimeSnapshot{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	snapshot, ok := t.states[cleanProjectKey]
	return snapshot, ok
}

func normalizeCompanionRuntimeState(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case companionRuntimeStateListening:
		return companionRuntimeStateListening
	case companionRuntimeStateThinking:
		return companionRuntimeStateThinking
	case companionRuntimeStateTalking:
		return companionRuntimeStateTalking
	case companionRuntimeStateError:
		return companionRuntimeStateError
	default:
		return companionRuntimeStateIdle
	}
}

func (s companionRuntimeSnapshot) payload(eventType string) map[string]interface{} {
	payload := map[string]interface{}{
		"type":        strings.TrimSpace(eventType),
		"state":       s.State,
		"project_key": s.ProjectKey,
		"updated_at":  s.UpdatedAt,
	}
	if s.Reason != "" {
		payload["reason"] = s.Reason
	}
	if s.Error != "" {
		payload["error"] = s.Error
	}
	if s.ChatSessionID != "" {
		payload["chat_session_id"] = s.ChatSessionID
	}
	if s.ParticipantSessionID != "" {
		payload["participant_session_id"] = s.ParticipantSessionID
	}
	if s.ParticipantSegmentID != 0 {
		payload["participant_segment_id"] = s.ParticipantSegmentID
	}
	if s.TurnID != "" {
		payload["turn_id"] = s.TurnID
	}
	if s.OutputMode != "" {
		payload["output_mode"] = s.OutputMode
	}
	return payload
}

func (a *App) chatSessionIDForProjectKey(projectKey string) (string, bool) {
	if a == nil || a.store == nil {
		return "", false
	}
	session, err := a.chatSessionForProjectKey(strings.TrimSpace(projectKey))
	if err != nil {
		return "", false
	}
	return session.ID, true
}

func (a *App) currentCompanionRuntimeState(projectKey string, cfg companionConfig) companionRuntimeSnapshot {
	projectKey = strings.TrimSpace(projectKey)
	if projectKey == "" {
		return companionRuntimeSnapshot{State: companionRuntimeStateIdle}
	}
	if a != nil && a.companionRuntime != nil {
		if snapshot, ok := a.companionRuntime.get(projectKey); ok {
			return snapshot
		}
	}
	return a.companionSteadyState(projectKey, cfg, "")
}

func (a *App) companionSteadyState(projectKey string, cfg companionConfig, reason string) companionRuntimeSnapshot {
	state := companionRuntimeStateIdle
	if cfg.CompanionEnabled {
		if activeSessions := a.activeCompanionSessionCount(projectKey); activeSessions > 0 {
			state = companionRuntimeStateListening
			if strings.TrimSpace(reason) == "" {
				reason = "participant_capture_active"
			}
		}
	}
	if strings.TrimSpace(reason) == "" {
		if state == companionRuntimeStateIdle {
			reason = "idle"
		} else {
			reason = "listening"
		}
	}
	return companionRuntimeSnapshot{
		State:      state,
		Reason:     strings.TrimSpace(reason),
		ProjectKey: strings.TrimSpace(projectKey),
	}
}

func (a *App) activeCompanionSessionCount(projectKey string) int {
	if a == nil || a.store == nil {
		return 0
	}
	sessions, err := a.store.ListParticipantSessions(strings.TrimSpace(projectKey))
	if err != nil {
		return 0
	}
	activeSessions := 0
	for _, session := range sessions {
		if session.EndedAt == 0 {
			activeSessions++
		}
	}
	return activeSessions
}

func (a *App) setCompanionRuntimeState(projectKey string, snapshot companionRuntimeSnapshot) companionRuntimeSnapshot {
	if a == nil || a.companionRuntime == nil {
		return companionRuntimeSnapshot{}
	}
	return a.companionRuntime.set(projectKey, snapshot)
}

func (a *App) companionPendingTurnForChatSession(chatSessionID string) (companionPendingTurn, bool) {
	if a == nil || a.companionTurns == nil {
		return companionPendingTurn{}, false
	}
	return a.companionTurns.get(chatSessionID)
}

func (a *App) broadcastCompanionRuntimeState(projectKey string, snapshot companionRuntimeSnapshot) {
	projectKey = strings.TrimSpace(projectKey)
	if projectKey == "" {
		return
	}
	sessionID, ok := a.chatSessionIDForProjectKey(projectKey)
	if !ok {
		return
	}
	snapshot.ChatSessionID = sessionID
	snapshot = a.setCompanionRuntimeState(projectKey, snapshot)
	a.broadcastChatEvent(sessionID, snapshot.payload(companionEventState))
}

func (a *App) settleCompanionRuntimeState(projectKey string, cfg companionConfig, reason string) {
	a.broadcastCompanionRuntimeState(projectKey, a.companionSteadyState(projectKey, cfg, reason))
}

func (a *App) broadcastCompanionTranscriptEvent(projectKey string, payload map[string]interface{}) {
	projectKey = strings.TrimSpace(projectKey)
	if projectKey == "" {
		return
	}
	sessionID, ok := a.chatSessionIDForProjectKey(projectKey)
	if !ok {
		return
	}
	payload["project_key"] = projectKey
	a.broadcastChatEvent(sessionID, payload)
}

func (a *App) markCompanionThinking(sessionID, projectKey, turnID, outputMode, reason string) {
	pending, ok := a.companionPendingTurnForChatSession(sessionID)
	if !ok {
		return
	}
	a.broadcastCompanionRuntimeState(projectKey, companionRuntimeSnapshot{
		State:                companionRuntimeStateThinking,
		Reason:               strings.TrimSpace(reason),
		ProjectKey:           strings.TrimSpace(projectKey),
		ParticipantSessionID: pending.participantSessionID,
		ParticipantSegmentID: pending.segmentID,
		TurnID:               strings.TrimSpace(turnID),
		OutputMode:           outputMode,
	})
}
