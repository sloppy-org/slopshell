package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/krystophny/tabura/internal/store"
)

const (
	companionIdleSurfaceRobot      = "robot"
	companionIdleSurfaceBlack      = "black"
	companionAudioPersistenceNone  = "none"
	companionCaptureSourceMic      = "microphone"
	companionRuntimeStateIdle      = "idle"
	companionRuntimeStateListening = "listening"
)

type companionConfig struct {
	CompanionEnabled          bool   `json:"companion_enabled"`
	DirectedSpeechGateEnabled bool   `json:"directed_speech_gate_enabled"`
	Language                  string `json:"language"`
	MaxSegmentDurationMS      int    `json:"max_segment_duration_ms"`
	SessionRAMCapMB           int    `json:"session_ram_cap_mb"`
	STTModel                  string `json:"stt_model"`
	IdleSurface               string `json:"idle_surface"`
	AudioPersistence          string `json:"audio_persistence"`
	CaptureSource             string `json:"capture_source"`
}

type participantConfig = companionConfig

type companionConfigPatch struct {
	CompanionEnabled          *bool   `json:"companion_enabled"`
	DirectedSpeechGateEnabled *bool   `json:"directed_speech_gate_enabled"`
	Language                  *string `json:"language"`
	MaxSegmentDurationMS      *int    `json:"max_segment_duration_ms"`
	SessionRAMCapMB           *int    `json:"session_ram_cap_mb"`
	STTModel                  *string `json:"stt_model"`
	IdleSurface               *string `json:"idle_surface"`
	AudioPersistence          *string `json:"audio_persistence"`
	CaptureSource             *string `json:"capture_source"`
}

type companionStateResponse struct {
	OK                 bool                            `json:"ok"`
	ProjectID          string                          `json:"project_id"`
	ProjectKey         string                          `json:"project_key"`
	State              string                          `json:"state"`
	Runtime            companionRuntimeSnapshot        `json:"runtime"`
	CompanionEnabled   bool                            `json:"companion_enabled"`
	IdleSurface        string                          `json:"idle_surface"`
	AudioPersistence   string                          `json:"audio_persistence"`
	CaptureSource      string                          `json:"capture_source"`
	ActiveSessions     int                             `json:"active_sessions"`
	ActiveSessionID    string                          `json:"active_session_id,omitempty"`
	LatestSession      *store.ParticipantSession       `json:"latest_session,omitempty"`
	DirectedSpeechGate companionDirectedSpeechGate     `json:"directed_speech_gate"`
	InteractionPolicy  companionInteractionPolicyState `json:"interaction_policy"`
	Config             companionConfig                 `json:"config"`
}

func defaultCompanionConfig() companionConfig {
	return companionConfig{
		CompanionEnabled:          false,
		DirectedSpeechGateEnabled: false,
		Language:                  "en",
		MaxSegmentDurationMS:      30000,
		SessionRAMCapMB:           64,
		STTModel:                  "whisper-1",
		IdleSurface:               companionIdleSurfaceRobot,
		AudioPersistence:          companionAudioPersistenceNone,
		CaptureSource:             companionCaptureSourceMic,
	}
}

func normalizeCompanionIdleSurface(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case companionIdleSurfaceBlack:
		return companionIdleSurfaceBlack
	default:
		return companionIdleSurfaceRobot
	}
}

func normalizeCompanionConfig(cfg companionConfig) companionConfig {
	normalized := defaultCompanionConfig()
	normalized.CompanionEnabled = cfg.CompanionEnabled
	normalized.DirectedSpeechGateEnabled = cfg.DirectedSpeechGateEnabled
	if language := strings.TrimSpace(cfg.Language); language != "" {
		normalized.Language = language
	}
	if cfg.MaxSegmentDurationMS > 0 {
		normalized.MaxSegmentDurationMS = cfg.MaxSegmentDurationMS
	}
	if cfg.SessionRAMCapMB > 0 {
		normalized.SessionRAMCapMB = cfg.SessionRAMCapMB
	}
	if model := strings.TrimSpace(cfg.STTModel); model != "" {
		normalized.STTModel = model
	}
	normalized.IdleSurface = normalizeCompanionIdleSurface(cfg.IdleSurface)
	normalized.AudioPersistence = companionAudioPersistenceNone
	normalized.CaptureSource = companionCaptureSourceMic
	return normalized
}

func applyCompanionConfigPatch(cfg companionConfig, patch companionConfigPatch) companionConfig {
	if patch.CompanionEnabled != nil {
		cfg.CompanionEnabled = *patch.CompanionEnabled
	}
	if patch.DirectedSpeechGateEnabled != nil {
		cfg.DirectedSpeechGateEnabled = *patch.DirectedSpeechGateEnabled
	}
	if patch.Language != nil {
		cfg.Language = strings.TrimSpace(*patch.Language)
	}
	if patch.MaxSegmentDurationMS != nil && *patch.MaxSegmentDurationMS > 0 {
		cfg.MaxSegmentDurationMS = *patch.MaxSegmentDurationMS
	}
	if patch.SessionRAMCapMB != nil && *patch.SessionRAMCapMB > 0 {
		cfg.SessionRAMCapMB = *patch.SessionRAMCapMB
	}
	if patch.STTModel != nil {
		cfg.STTModel = strings.TrimSpace(*patch.STTModel)
	}
	if patch.IdleSurface != nil {
		cfg.IdleSurface = normalizeCompanionIdleSurface(*patch.IdleSurface)
	}
	cfg.AudioPersistence = companionAudioPersistenceNone
	cfg.CaptureSource = companionCaptureSourceMic
	return normalizeCompanionConfig(cfg)
}

func (a *App) loadCompanionConfig(target any) companionConfig {
	cfg := defaultCompanionConfig()
	var raw string
	switch typed := target.(type) {
	case store.Workspace:
		raw = strings.TrimSpace(typed.CompanionConfigJSON)
	case *store.Workspace:
		if typed != nil {
			raw = strings.TrimSpace(typed.CompanionConfigJSON)
		}
	case store.Project:
		workspace, err := a.ensureWorkspaceForProject(typed, false)
		if err == nil {
			raw = strings.TrimSpace(workspace.CompanionConfigJSON)
		}
	case *store.Project:
		if typed != nil {
			workspace, err := a.ensureWorkspaceForProject(*typed, false)
			if err == nil {
				raw = strings.TrimSpace(workspace.CompanionConfigJSON)
			}
		}
	}
	if raw == "" {
		return cfg
	}
	var patch companionConfigPatch
	if err := json.Unmarshal([]byte(raw), &patch); err != nil {
		return cfg
	}
	return applyCompanionConfigPatch(cfg, patch)
}

func (a *App) saveCompanionConfig(target any, cfg companionConfig) error {
	normalized := normalizeCompanionConfig(cfg)
	data, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	resolveWorkspaceID := func(value any) (int64, error) {
		switch typed := value.(type) {
		case int64:
			return typed, nil
		case store.Workspace:
			return typed.ID, nil
		case *store.Workspace:
			if typed == nil {
				return 0, errors.New("workspace is required")
			}
			return typed.ID, nil
		case string:
			project, err := a.store.GetProject(strings.TrimSpace(typed))
			if err != nil {
				return 0, err
			}
			workspace, err := a.ensureWorkspaceForProject(project, false)
			if err != nil {
				return 0, err
			}
			return workspace.ID, nil
		case store.Project:
			workspace, err := a.ensureWorkspaceForProject(typed, false)
			if err != nil {
				return 0, err
			}
			return workspace.ID, nil
		case *store.Project:
			if typed == nil {
				return 0, errors.New("project is required")
			}
			workspace, err := a.ensureWorkspaceForProject(*typed, false)
			if err != nil {
				return 0, err
			}
			return workspace.ID, nil
		default:
			return 0, errors.New("unsupported companion config target")
		}
	}
	workspaceID, err := resolveWorkspaceID(target)
	if err != nil {
		return err
	}
	return a.store.UpdateWorkspaceCompanionConfig(workspaceID, string(data))
}

func (a *App) activeCompanionWorkspace() (store.Workspace, error) {
	if project, err := a.resolveProjectByIDOrActive("active"); err == nil {
		return a.ensureWorkspaceForProject(project, false)
	} else if err != nil && !isNoRows(err) {
		return store.Workspace{}, err
	}
	workspace, err := a.store.ActiveWorkspace()
	if err == nil {
		return workspace, nil
	}
	if isNoRows(err) {
		return a.ensureStartupWorkspace()
	}
	return store.Workspace{}, err
}

func (a *App) companionKeyForWorkspace(workspace store.Workspace) string {
	if project, err := a.projectForWorkspace(workspace); err == nil && project != nil {
		return strings.TrimSpace(project.ProjectKey)
	}
	return strings.TrimSpace(workspace.DirPath)
}

func (a *App) companionWorkspaceForWorkspaceIDOrActive(workspaceID string) (store.Workspace, *store.Project, error) {
	workspace, err := a.resolveWorkspaceByIDOrActive(workspaceID)
	if err != nil {
		return store.Workspace{}, nil, err
	}
	project, err := a.projectForWorkspace(workspace)
	if err != nil {
		return store.Workspace{}, nil, err
	}
	return workspace, project, nil
}

func (a *App) resolveParticipantProject(chatSessionID string) (string, companionConfig) {
	cleanSessionID := strings.TrimSpace(chatSessionID)
	if cleanSessionID == "" {
		if workspace, err := a.activeCompanionWorkspace(); err == nil {
			return a.companionKeyForWorkspace(workspace), a.loadCompanionConfig(workspace)
		}
		return "default", defaultCompanionConfig()
	}
	if session, err := a.store.GetChatSession(cleanSessionID); err == nil {
		if session.WorkspaceID > 0 {
			if workspace, err := a.store.GetWorkspace(session.WorkspaceID); err == nil {
				return strings.TrimSpace(session.ProjectKey), a.loadCompanionConfig(workspace)
			}
		}
	}
	if workspace, err := a.store.GetWorkspaceByPath(cleanSessionID); err == nil {
		return a.companionKeyForWorkspace(workspace), a.loadCompanionConfig(workspace)
	}
	if project, err := a.store.GetProjectByProjectKey(cleanSessionID); err == nil {
		workspace, workspaceErr := a.ensureWorkspaceForProject(project, false)
		if workspaceErr == nil {
			return project.ProjectKey, a.loadCompanionConfig(workspace)
		}
		return project.ProjectKey, defaultCompanionConfig()
	}
	return cleanSessionID, defaultCompanionConfig()
}

func decodeCompanionConfigPatch(r *http.Request) (companionConfigPatch, error) {
	var patch companionConfigPatch
	if err := decodeJSON(r, &patch); err != nil {
		return companionConfigPatch{}, err
	}
	return patch, nil
}

func (a *App) handleParticipantConfigGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, err := a.activeCompanionWorkspace()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, a.loadCompanionConfig(workspace))
}

func (a *App) handleParticipantConfigPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, err := a.activeCompanionWorkspace()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	patch, err := decodeCompanionConfigPatch(r)
	if err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	current := a.loadCompanionConfig(workspace)
	cfg := applyCompanionConfigPatch(current, patch)
	if err := a.saveCompanionConfig(workspace.ID, cfg); err != nil {
		http.Error(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	if current.CompanionEnabled && !cfg.CompanionEnabled {
		a.disableCompanionCapture(a.companionKeyForWorkspace(workspace))
	}
	writeJSON(w, cfg)
}

func (a *App) handleWorkspaceCompanionConfigGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, _, err := a.companionWorkspaceForWorkspaceIDOrActive(chi.URLParam(r, "workspace_id"))
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, a.loadCompanionConfig(workspace))
}

func (a *App) handleWorkspaceCompanionConfigPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, _, err := a.companionWorkspaceForWorkspaceIDOrActive(chi.URLParam(r, "workspace_id"))
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	patch, err := decodeCompanionConfigPatch(r)
	if err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	current := a.loadCompanionConfig(workspace)
	cfg := applyCompanionConfigPatch(current, patch)
	if err := a.saveCompanionConfig(workspace.ID, cfg); err != nil {
		http.Error(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	if current.CompanionEnabled && !cfg.CompanionEnabled {
		a.disableCompanionCapture(a.companionKeyForWorkspace(workspace))
	}
	writeJSON(w, cfg)
}

func (a *App) handleWorkspaceCompanionState(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, project, err := a.companionWorkspaceForWorkspaceIDOrActive(chi.URLParam(r, "workspace_id"))
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "workspace not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cfg := a.loadCompanionConfig(workspace)
	sessions, err := a.store.ListParticipantSessionsForWorkspace(workspace.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	activeSessions := 0
	activeSessionID := ""
	var latestSession *store.ParticipantSession
	var gateSession *store.ParticipantSession
	for i := range sessions {
		if latestSession == nil {
			latestSession = &sessions[i]
		}
		if sessions[i].EndedAt != 0 {
			continue
		}
		activeSessions++
		if activeSessionID == "" {
			activeSessionID = sessions[i].ID
			gateSession = &sessions[i]
		}
	}
	if gateSession == nil {
		gateSession = latestSession
	}
	companionKey := a.companionKeyForWorkspace(workspace)
	projectID := ""
	if project != nil {
		projectID = project.ID
	}
	runtime := a.currentCompanionRuntimeState(companionKey, cfg)
	gate := a.loadCompanionDirectedSpeechGate(cfg, gateSession)
	policy := a.loadCompanionInteractionPolicy(cfg, gateSession)
	writeJSON(w, companionStateResponse{
		OK:                 true,
		ProjectID:          projectID,
		ProjectKey:         companionKey,
		State:              runtime.State,
		Runtime:            runtime,
		CompanionEnabled:   cfg.CompanionEnabled,
		IdleSurface:        cfg.IdleSurface,
		AudioPersistence:   cfg.AudioPersistence,
		CaptureSource:      cfg.CaptureSource,
		ActiveSessions:     activeSessions,
		ActiveSessionID:    activeSessionID,
		LatestSession:      latestSession,
		DirectedSpeechGate: gate,
		InteractionPolicy:  policy,
		Config:             cfg,
	})
}

func (a *App) stopParticipantCaptureSessions(match func(store.ParticipantSession) bool, reason, errMsg string) {
	if a == nil || a.store == nil || match == nil {
		return
	}
	affectedProjectKeys := map[string]struct{}{}
	if a.hub != nil {
		a.hub.forEachChatConn(func(conn *chatWSConn) {
			conn.participantMu.Lock()
			sessionID := strings.TrimSpace(conn.participantSessionID)
			active := conn.participantActive
			conn.participantMu.Unlock()
			if !active || sessionID == "" {
				return
			}
			session, err := a.store.GetParticipantSession(sessionID)
			if err != nil || !match(session) {
				return
			}
			affectedProjectKeys[session.ProjectKey] = struct{}{}
			stoppedSessionID, ok := releaseParticipantSession(a, conn)
			if !ok {
				return
			}
			_ = conn.writeJSON(participantMessage{Type: "participant_stopped", SessionID: stoppedSessionID})
			_ = conn.writeJSON(participantMessage{Type: "participant_error", Error: errMsg})
		})
	}

	sessions, err := a.store.ListParticipantSessions("")
	if err != nil {
		return
	}
	for _, session := range sessions {
		if session.EndedAt != 0 || !match(session) {
			continue
		}
		affectedProjectKeys[session.ProjectKey] = struct{}{}
		_ = a.store.EndParticipantSession(session.ID)
		_ = a.store.AddParticipantEvent(session.ID, 0, "session_stopped", fmt.Sprintf(`{"reason":%q}`, strings.TrimSpace(reason)))
		a.syncProjectCompanionArtifactsBySessionID(session.ID)
	}
	for projectKey := range affectedProjectKeys {
		a.broadcastCompanionRuntimeState(projectKey, companionRuntimeSnapshot{
			State:      companionRuntimeStateIdle,
			Reason:     strings.TrimSpace(reason),
			ProjectKey: projectKey,
		})
	}
}

func (a *App) disableCompanionCapture(projectKey string) {
	cleanProjectKey := strings.TrimSpace(projectKey)
	if cleanProjectKey == "" {
		return
	}
	a.stopParticipantCaptureSessions(func(session store.ParticipantSession) bool {
		return session.ProjectKey == cleanProjectKey
	}, "companion_disabled", "meeting mode is disabled")
}

func (a *App) disableLiveMeetingCapture() {
	a.stopParticipantCaptureSessions(func(session store.ParticipantSession) bool {
		return true
	}, "live_policy_disabled", "meeting mode is disabled")
}
