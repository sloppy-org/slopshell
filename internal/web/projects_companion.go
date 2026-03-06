package web

import (
	"encoding/json"
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
	OK                 bool                        `json:"ok"`
	ProjectID          string                      `json:"project_id"`
	ProjectKey         string                      `json:"project_key"`
	State              string                      `json:"state"`
	CompanionEnabled   bool                        `json:"companion_enabled"`
	IdleSurface        string                      `json:"idle_surface"`
	AudioPersistence   string                      `json:"audio_persistence"`
	CaptureSource      string                      `json:"capture_source"`
	ActiveSessions     int                         `json:"active_sessions"`
	ActiveSessionID    string                      `json:"active_session_id,omitempty"`
	LatestSession      *store.ParticipantSession   `json:"latest_session,omitempty"`
	DirectedSpeechGate companionDirectedSpeechGate `json:"directed_speech_gate"`
	Config             companionConfig             `json:"config"`
}

func defaultCompanionConfig() companionConfig {
	return companionConfig{
		CompanionEnabled:          true,
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

func (a *App) loadCompanionConfig(project store.Project) companionConfig {
	cfg := defaultCompanionConfig()
	raw := strings.TrimSpace(project.CompanionConfigJSON)
	if raw == "" {
		return cfg
	}
	var patch companionConfigPatch
	if err := json.Unmarshal([]byte(raw), &patch); err != nil {
		return cfg
	}
	return applyCompanionConfigPatch(cfg, patch)
}

func (a *App) saveCompanionConfig(projectID string, cfg companionConfig) error {
	normalized := normalizeCompanionConfig(cfg)
	data, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	return a.store.UpdateProjectCompanionConfig(projectID, string(data))
}

func (a *App) activeCompanionProject() (store.Project, error) {
	project, err := a.resolveProjectByIDOrActive("active")
	if err != nil {
		return store.Project{}, err
	}
	if isHubProject(project) {
		return a.hubPrimaryProject()
	}
	return project, nil
}

func (a *App) resolveParticipantProject(chatSessionID string) (string, companionConfig) {
	cleanSessionID := strings.TrimSpace(chatSessionID)
	if cleanSessionID == "" {
		if project, err := a.activeCompanionProject(); err == nil {
			return project.ProjectKey, a.loadCompanionConfig(project)
		}
		return "default", defaultCompanionConfig()
	}
	if session, err := a.store.GetChatSession(cleanSessionID); err == nil {
		projectKey := strings.TrimSpace(session.ProjectKey)
		if projectKey != "" {
			if project, err := a.store.GetProjectByProjectKey(projectKey); err == nil {
				return projectKey, a.loadCompanionConfig(project)
			}
			return projectKey, defaultCompanionConfig()
		}
	}
	if project, err := a.store.GetProjectByProjectKey(cleanSessionID); err == nil {
		return project.ProjectKey, a.loadCompanionConfig(project)
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
	project, err := a.activeCompanionProject()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, a.loadCompanionConfig(project))
}

func (a *App) handleParticipantConfigPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	project, err := a.activeCompanionProject()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	patch, err := decodeCompanionConfigPatch(r)
	if err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	cfg := applyCompanionConfigPatch(a.loadCompanionConfig(project), patch)
	if err := a.saveCompanionConfig(project.ID, cfg); err != nil {
		http.Error(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	writeJSON(w, cfg)
}

func (a *App) handleProjectCompanionConfigGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	project, err := a.resolveProjectByIDOrActive(chi.URLParam(r, "project_id"))
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, a.loadCompanionConfig(project))
}

func (a *App) handleProjectCompanionConfigPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	project, err := a.resolveProjectByIDOrActive(chi.URLParam(r, "project_id"))
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "project not found", http.StatusNotFound)
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
	cfg := applyCompanionConfigPatch(a.loadCompanionConfig(project), patch)
	if err := a.saveCompanionConfig(project.ID, cfg); err != nil {
		http.Error(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	writeJSON(w, cfg)
}

func (a *App) handleProjectCompanionState(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	project, err := a.resolveProjectByIDOrActive(chi.URLParam(r, "project_id"))
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cfg := a.loadCompanionConfig(project)
	sessions, err := a.store.ListParticipantSessions(project.ProjectKey)
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
	state := companionRuntimeStateIdle
	if activeSessions > 0 {
		state = companionRuntimeStateListening
	}
	gate := a.loadCompanionDirectedSpeechGate(cfg, gateSession)
	writeJSON(w, companionStateResponse{
		OK:                 true,
		ProjectID:          project.ID,
		ProjectKey:         project.ProjectKey,
		State:              state,
		CompanionEnabled:   cfg.CompanionEnabled,
		IdleSurface:        cfg.IdleSurface,
		AudioPersistence:   cfg.AudioPersistence,
		CaptureSource:      cfg.CaptureSource,
		ActiveSessions:     activeSessions,
		ActiveSessionID:    activeSessionID,
		LatestSession:      latestSession,
		DirectedSpeechGate: gate,
		Config:             cfg,
	})
}
