package web

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/krystophny/tabura/internal/stt"
	"github.com/krystophny/tabura/internal/store"
)

const (
	participantConfigStateKey = "participant_config"
	participantMaxBufBytes    = 10 * 1024 * 1024
)

type participantMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
	Text      string `json:"text,omitempty"`
	Speaker   string `json:"speaker,omitempty"`
	Error     string `json:"error,omitempty"`
	SegmentID int64  `json:"segment_id,omitempty"`
	StartTS   int64  `json:"start_ts,omitempty"`
	EndTS     int64  `json:"end_ts,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

type participantConfig struct {
	Language              string `json:"language"`
	MaxSegmentDurationMS  int    `json:"max_segment_duration_ms"`
	SessionRAMCapMB       int    `json:"session_ram_cap_mb"`
	STTModel              string `json:"stt_model"`
	AudioPersistence      string `json:"audio_persistence"`
}

func defaultParticipantConfig() participantConfig {
	return participantConfig{
		Language:             "en",
		MaxSegmentDurationMS: 30000,
		SessionRAMCapMB:      64,
		STTModel:             "whisper-1",
		AudioPersistence:     "none",
	}
}

func handleParticipantStart(a *App, conn *chatWSConn, chatSessionID string) {
	conn.participantMu.Lock()
	defer conn.participantMu.Unlock()

	if conn.participantActive {
		_ = conn.writeJSON(participantMessage{Type: "participant_error", Error: "participant session already active"})
		return
	}

	projectKey := strings.TrimSpace(chatSessionID)
	if projectKey == "" {
		projectKey = "default"
	}

	cfg := a.loadParticipantConfig()
	cfgJSON, _ := json.Marshal(cfg)
	sess, err := a.store.AddParticipantSession(projectKey, string(cfgJSON))
	if err != nil {
		_ = conn.writeJSON(participantMessage{Type: "participant_error", Error: fmt.Sprintf("failed to create session: %v", err)})
		return
	}

	conn.participantActive = true
	conn.participantSessionID = sess.ID
	conn.participantBuf = make([]byte, 0, 4096)

	_ = a.store.AddParticipantEvent(sess.ID, 0, "session_started", "{}")
	_ = conn.writeJSON(participantMessage{Type: "participant_started", SessionID: sess.ID})
	log.Printf("participant session started: %s", sess.ID)
}

// Privacy: buffer is accumulated in RAM only. See docs/meeting-notes-privacy.md.
func handleParticipantBinaryChunk(a *App, conn *chatWSConn, data []byte) {
	conn.participantMu.Lock()
	if !conn.participantActive {
		conn.participantMu.Unlock()
		return
	}

	cfg := a.loadParticipantConfig()
	ramCap := cfg.SessionRAMCapMB * 1024 * 1024
	if ramCap <= 0 {
		ramCap = participantMaxBufBytes
	}

	if len(conn.participantBuf)+len(data) > ramCap {
		// Drop oldest data to stay under RAM cap
		excess := len(conn.participantBuf) + len(data) - ramCap
		if excess >= len(conn.participantBuf) {
			conn.participantBuf = make([]byte, 0, 4096)
		} else {
			conn.participantBuf = conn.participantBuf[excess:]
		}
	}

	conn.participantBuf = append(conn.participantBuf, data...)
	sessionID := conn.participantSessionID

	if len(conn.participantBuf) < 4096 {
		conn.participantMu.Unlock()
		return
	}

	buf := make([]byte, len(conn.participantBuf))
	copy(buf, conn.participantBuf)
	conn.participantBuf = conn.participantBuf[:0]
	conn.participantMu.Unlock()

	go transcribeParticipantChunk(a, conn, sessionID, buf)
}

func transcribeParticipantChunk(a *App, conn *chatWSConn, sessionID string, buf []byte) {
	if a.sttURL == "" {
		_ = conn.writeJSON(participantMessage{Type: "participant_error", Error: "STT sidecar is not configured"})
		return
	}

	startTime := time.Now()
	replacements := a.loadSTTReplacements()
	mimeType := "audio/webm"
	normalizedMimeType, normalizedData, normalizeErr := stt.NormalizeForWhisper(mimeType, buf)
	if normalizeErr != nil {
		log.Printf("participant normalize error: %v", normalizeErr)
		return
	}

	options := a.sttTranscribeOptions()
	text, err := stt.TranscribeWithOptions(a.sttURL, normalizedMimeType, normalizedData, replacements, options)
	if err != nil {
		if stt.IsRetryableNoSpeechError(err) {
			return
		}
		log.Printf("participant transcribe error: %v", err)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	latencyMS := time.Since(startTime).Milliseconds()
	now := time.Now().Unix()
	seg, err := a.store.AddParticipantSegment(store.ParticipantSegment{
		SessionID:   sessionID,
		StartTS:     now,
		EndTS:       now,
		Text:        text,
		Model:       "whisper-1",
		LatencyMS:   latencyMS,
		CommittedAt: now,
		Status:      "final",
	})
	if err != nil {
		log.Printf("participant store segment error: %v", err)
		return
	}

	_ = a.store.AddParticipantEvent(sessionID, seg.ID, "segment_committed", fmt.Sprintf(`{"text":%q}`, text))

	_ = conn.writeJSON(participantMessage{
		Type:      "participant_segment_text",
		SessionID: sessionID,
		Text:      text,
		SegmentID: seg.ID,
		StartTS:   seg.StartTS,
		EndTS:     seg.EndTS,
		LatencyMS: latencyMS,
	})
}

// Privacy: buffer is set to nil after stop. See docs/meeting-notes-privacy.md.
func handleParticipantStop(a *App, conn *chatWSConn) {
	conn.participantMu.Lock()
	if !conn.participantActive {
		conn.participantMu.Unlock()
		_ = conn.writeJSON(participantMessage{Type: "participant_error", Error: "no active participant session"})
		return
	}

	sessionID := conn.participantSessionID
	remainingBuf := conn.participantBuf

	conn.participantActive = false
	conn.participantSessionID = ""
	conn.participantBuf = nil
	conn.participantMu.Unlock()

	if len(remainingBuf) >= 1024 {
		transcribeParticipantChunk(a, conn, sessionID, remainingBuf)
	}

	_ = a.store.EndParticipantSession(sessionID)
	_ = a.store.AddParticipantEvent(sessionID, 0, "session_stopped", "{}")
	_ = conn.writeJSON(participantMessage{Type: "participant_stopped", SessionID: sessionID})
	log.Printf("participant session stopped: %s", sessionID)
}

func (a *App) loadParticipantConfig() participantConfig {
	cfg := defaultParticipantConfig()
	raw, err := a.store.AppState(participantConfigStateKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return cfg
	}
	var persisted participantConfig
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		return cfg
	}
	if strings.TrimSpace(persisted.Language) != "" {
		cfg.Language = persisted.Language
	}
	if persisted.MaxSegmentDurationMS > 0 {
		cfg.MaxSegmentDurationMS = persisted.MaxSegmentDurationMS
	}
	if persisted.SessionRAMCapMB > 0 {
		cfg.SessionRAMCapMB = persisted.SessionRAMCapMB
	}
	if strings.TrimSpace(persisted.STTModel) != "" {
		cfg.STTModel = persisted.STTModel
	}
	cfg.AudioPersistence = "none"
	return cfg
}

func (a *App) saveParticipantConfig(cfg participantConfig) error {
	cfg.AudioPersistence = "none"
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return a.store.SetAppState(participantConfigStateKey, string(data))
}

func (a *App) handleParticipantConfigGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	writeJSON(w, a.loadParticipantConfig())
}

func (a *App) handleParticipantConfigPut(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var patch participantConfig
	if err := json.Unmarshal(body, &patch); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	cfg := a.loadParticipantConfig()
	if strings.TrimSpace(patch.Language) != "" {
		cfg.Language = strings.TrimSpace(patch.Language)
	}
	if patch.MaxSegmentDurationMS > 0 {
		cfg.MaxSegmentDurationMS = patch.MaxSegmentDurationMS
	}
	if patch.SessionRAMCapMB > 0 {
		cfg.SessionRAMCapMB = patch.SessionRAMCapMB
	}
	if strings.TrimSpace(patch.STTModel) != "" {
		cfg.STTModel = strings.TrimSpace(patch.STTModel)
	}
	cfg.AudioPersistence = "none"

	if err := a.saveParticipantConfig(cfg); err != nil {
		http.Error(w, "failed to save config", http.StatusInternalServerError)
		return
	}
	writeJSON(w, cfg)
}

func (a *App) handleParticipantStatus(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	active := 0
	a.hub.forEachChatConn(func(conn *chatWSConn) {
		conn.participantMu.Lock()
		if conn.participantActive {
			active++
		}
		conn.participantMu.Unlock()
	})
	writeJSON(w, map[string]interface{}{
		"ok":              true,
		"active_sessions": active,
	})
}

func (a *App) handleParticipantSessionsList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	projectKey := strings.TrimSpace(r.URL.Query().Get("project_key"))
	sessions, err := a.store.ListParticipantSessions(projectKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"sessions": sessions,
	})
}

func (a *App) handleParticipantTranscript(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "id"))
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	var fromTS, toTS int64
	if v := strings.TrimSpace(r.URL.Query().Get("from")); v != "" {
		fromTS, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := strings.TrimSpace(r.URL.Query().Get("to")); v != "" {
		toTS, _ = strconv.ParseInt(v, 10, 64)
	}

	segments, err := a.store.ListParticipantSegments(sessionID, fromTS, toTS)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"segments": segments,
	})
}

func (a *App) handleParticipantSearch(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "id"))
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	segments, err := a.store.SearchParticipantSegments(sessionID, query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"segments": segments,
	})
}

func (a *App) handleParticipantExport(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "id"))
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	format := strings.TrimSpace(r.URL.Query().Get("format"))
	if format == "" {
		format = "txt"
	}

	segments, err := a.store.ListParticipantSegments(sessionID, 0, 0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sess, err := a.store.GetParticipantSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	switch format {
	case "json":
		writeJSON(w, map[string]interface{}{
			"ok":       true,
			"session":  sess,
			"segments": segments,
		})
	case "md":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		fmt.Fprintf(w, "# Meeting Transcript\n\n")
		fmt.Fprintf(w, "Session: %s  \nStarted: %s\n\n", sess.ID, time.Unix(sess.StartedAt, 0).UTC().Format(time.RFC3339))
		for _, seg := range segments {
			speaker := seg.Speaker
			if speaker == "" {
				speaker = "Speaker"
			}
			ts := time.Unix(seg.StartTS, 0).UTC().Format("15:04:05")
			fmt.Fprintf(w, "**%s** (%s): %s\n\n", speaker, ts, seg.Text)
		}
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, seg := range segments {
			speaker := seg.Speaker
			if speaker == "" {
				speaker = "Speaker"
			}
			ts := time.Unix(seg.StartTS, 0).UTC().Format("15:04:05")
			fmt.Fprintf(w, "[%s] %s: %s\n", ts, speaker, seg.Text)
		}
	}
}
