package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ParticipantSession struct {
	ID         string `json:"id"`
	ProjectKey string `json:"project_key"`
	StartedAt  int64  `json:"started_at"`
	EndedAt    int64  `json:"ended_at"`
	ConfigJSON string `json:"config_json"`
}

type ParticipantSegment struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	StartTS     int64  `json:"start_ts"`
	EndTS       int64  `json:"end_ts"`
	Speaker     string `json:"speaker"`
	Text        string `json:"text"`
	Model       string `json:"model"`
	LatencyMS   int64  `json:"latency_ms"`
	CommittedAt int64  `json:"committed_at"`
	Status      string `json:"status"`
}

type ParticipantEvent struct {
	ID          int64  `json:"id"`
	SessionID   string `json:"session_id"`
	SegmentID   int64  `json:"segment_id"`
	EventType   string `json:"event_type"`
	PayloadJSON string `json:"payload_json"`
	CreatedAt   int64  `json:"created_at"`
}

type ParticipantRoomState struct {
	ID                int64  `json:"id"`
	SessionID         string `json:"session_id"`
	SummaryText       string `json:"summary_text"`
	EntitiesJSON      string `json:"entities_json"`
	TopicTimelineJSON string `json:"topic_timeline_json"`
	UpdatedAt         int64  `json:"updated_at"`
}

var ErrParticipantSessionEnded = errors.New("participant session is ended")

func (s *Store) AddParticipantSession(projectKey, configJSON string) (ParticipantSession, error) {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return ParticipantSession{}, errors.New("project key is required")
	}
	if strings.TrimSpace(configJSON) == "" {
		configJSON = "{}"
	}
	now := time.Now().Unix()
	id := fmt.Sprintf("psess-%s", randomHex(8))
	_, err := s.db.Exec(
		`INSERT INTO participant_sessions (id, project_key, started_at, ended_at, config_json) VALUES (?,?,?,0,?)`,
		id, key, now, configJSON,
	)
	if err != nil {
		return ParticipantSession{}, err
	}
	return s.GetParticipantSession(id)
}

func (s *Store) GetParticipantSession(id string) (ParticipantSession, error) {
	var out ParticipantSession
	err := s.db.QueryRow(
		`SELECT id, project_key, started_at, ended_at, config_json FROM participant_sessions WHERE id = ?`,
		strings.TrimSpace(id),
	).Scan(&out.ID, &out.ProjectKey, &out.StartedAt, &out.EndedAt, &out.ConfigJSON)
	if err != nil {
		return ParticipantSession{}, err
	}
	return out, nil
}

func (s *Store) ListParticipantSessions(projectKey string) ([]ParticipantSession, error) {
	key := strings.TrimSpace(projectKey)
	var rows *sql.Rows
	var err error
	if key != "" {
		rows, err = s.db.Query(
			`SELECT id, project_key, started_at, ended_at, config_json FROM participant_sessions WHERE project_key = ? ORDER BY started_at DESC`,
			key,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, project_key, started_at, ended_at, config_json FROM participant_sessions ORDER BY started_at DESC`,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParticipantSession{}
	for rows.Next() {
		var item ParticipantSession
		if err := rows.Scan(&item.ID, &item.ProjectKey, &item.StartedAt, &item.EndedAt, &item.ConfigJSON); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) EndParticipantSession(id string) error {
	cleanID := strings.TrimSpace(id)
	if cleanID == "" {
		return errors.New("session id is required")
	}
	now := time.Now().Unix()
	_, err := s.db.Exec(`UPDATE participant_sessions SET ended_at = ? WHERE id = ?`, now, cleanID)
	return err
}

func (s *Store) AddParticipantSegment(seg ParticipantSegment) (ParticipantSegment, error) {
	sessionID := strings.TrimSpace(seg.SessionID)
	if sessionID == "" {
		return ParticipantSegment{}, errors.New("session id is required")
	}
	var endedAt int64
	if err := s.db.QueryRow(
		`SELECT ended_at FROM participant_sessions WHERE id = ?`,
		sessionID,
	).Scan(&endedAt); err != nil {
		return ParticipantSegment{}, err
	}
	if endedAt != 0 {
		return ParticipantSegment{}, ErrParticipantSessionEnded
	}
	now := time.Now().Unix()
	if seg.CommittedAt == 0 {
		seg.CommittedAt = now
	}
	if seg.Status == "" {
		seg.Status = "final"
	}
	res, err := s.db.Exec(
		`INSERT INTO participant_segments (session_id, start_ts, end_ts, speaker, text, model, latency_ms, committed_at, status) VALUES (?,?,?,?,?,?,?,?,?)`,
		sessionID, seg.StartTS, seg.EndTS, seg.Speaker, seg.Text, seg.Model, seg.LatencyMS, seg.CommittedAt, seg.Status,
	)
	if err != nil {
		return ParticipantSegment{}, err
	}
	id, _ := res.LastInsertId()
	seg.ID = id
	seg.SessionID = sessionID
	return seg, nil
}

func (s *Store) ListParticipantSegments(sessionID string, fromTS, toTS int64) ([]ParticipantSegment, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, errors.New("session id is required")
	}
	query := `SELECT id, session_id, start_ts, end_ts, speaker, text, model, latency_ms, committed_at, status FROM participant_segments WHERE session_id = ?`
	args := []interface{}{sid}
	if fromTS > 0 {
		query += ` AND start_ts >= ?`
		args = append(args, fromTS)
	}
	if toTS > 0 {
		query += ` AND start_ts <= ?`
		args = append(args, toTS)
	}
	query += ` ORDER BY start_ts ASC, id ASC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParticipantSegment{}
	for rows.Next() {
		var item ParticipantSegment
		if err := rows.Scan(&item.ID, &item.SessionID, &item.StartTS, &item.EndTS, &item.Speaker, &item.Text, &item.Model, &item.LatencyMS, &item.CommittedAt, &item.Status); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SearchParticipantSegments(sessionID, query string) ([]ParticipantSegment, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, errors.New("session id is required")
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return s.ListParticipantSegments(sid, 0, 0)
	}
	rows, err := s.db.Query(
		`SELECT id, session_id, start_ts, end_ts, speaker, text, model, latency_ms, committed_at, status
		 FROM participant_segments WHERE session_id = ? AND text LIKE ? ORDER BY start_ts ASC, id ASC`,
		sid, "%"+q+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParticipantSegment{}
	for rows.Next() {
		var item ParticipantSegment
		if err := rows.Scan(&item.ID, &item.SessionID, &item.StartTS, &item.EndTS, &item.Speaker, &item.Text, &item.Model, &item.LatencyMS, &item.CommittedAt, &item.Status); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AddParticipantEvent(sessionID string, segmentID int64, eventType, payloadJSON string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return errors.New("session id is required")
	}
	_, err := s.db.Exec(
		`INSERT INTO participant_events (session_id, segment_id, event_type, payload_json, created_at) VALUES (?,?,?,?,?)`,
		sid, segmentID, strings.TrimSpace(eventType), payloadJSON, time.Now().Unix(),
	)
	return err
}

func (s *Store) ListParticipantEvents(sessionID string) ([]ParticipantEvent, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, errors.New("session id is required")
	}
	rows, err := s.db.Query(
		`SELECT id, session_id, segment_id, event_type, payload_json, created_at FROM participant_events WHERE session_id = ? ORDER BY created_at ASC, id ASC`,
		sid,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParticipantEvent{}
	for rows.Next() {
		var item ParticipantEvent
		if err := rows.Scan(&item.ID, &item.SessionID, &item.SegmentID, &item.EventType, &item.PayloadJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertParticipantRoomState(sessionID, summaryText, entitiesJSON, topicTimelineJSON string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return errors.New("session id is required")
	}
	if strings.TrimSpace(entitiesJSON) == "" {
		entitiesJSON = "[]"
	}
	if strings.TrimSpace(topicTimelineJSON) == "" {
		topicTimelineJSON = "[]"
	}
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO participant_room_state (session_id, summary_text, entities_json, topic_timeline_json, updated_at)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT(session_id) DO UPDATE SET summary_text=excluded.summary_text, entities_json=excluded.entities_json, topic_timeline_json=excluded.topic_timeline_json, updated_at=excluded.updated_at`,
		sid, summaryText, entitiesJSON, topicTimelineJSON, now,
	)
	return err
}

func (s *Store) GetParticipantRoomState(sessionID string) (ParticipantRoomState, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return ParticipantRoomState{}, errors.New("session id is required")
	}
	var out ParticipantRoomState
	err := s.db.QueryRow(
		`SELECT id, session_id, summary_text, entities_json, topic_timeline_json, updated_at FROM participant_room_state WHERE session_id = ?`,
		sid,
	).Scan(&out.ID, &out.SessionID, &out.SummaryText, &out.EntitiesJSON, &out.TopicTimelineJSON, &out.UpdatedAt)
	if err != nil {
		return ParticipantRoomState{}, err
	}
	return out, nil
}
