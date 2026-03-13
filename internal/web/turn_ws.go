package web

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/turn"
)

type turnWSConn struct {
	conn       *websocket.Conn
	writeMu    sync.Mutex
	controller *turn.Controller
}

func newTurnWSConn(ws *websocket.Conn) *turnWSConn {
	conn := &turnWSConn{conn: ws}
	conn.controller = turn.NewController(turn.Callbacks{
		OnAction: func(signal turn.Signal) {
			_ = conn.writeJSON(map[string]any{
				"type":                "turn_action",
				"action":              string(signal.Action),
				"text":                strings.TrimSpace(signal.Text),
				"reason":              strings.TrimSpace(signal.Reason),
				"wait_ms":             signal.WaitMS,
				"interrupt_assistant": signal.InterruptAssistant,
				"rollback_audio_ms":   signal.RollbackAudioMS,
			})
		},
	})
	return conn
}

func (c *turnWSConn) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

func (a *App) handleTurnWS(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	if _, err := a.store.GetChatSession(sessionID); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	ws, err := a.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn := newTurnWSConn(ws)
	defer func() {
		conn.controller.Close()
		_ = ws.Close()
	}()
	_ = conn.writeJSON(map[string]any{
		"type":                 "turn_ready",
		"session_id":           sessionID,
		"turn_intelligence":    true,
		"turn_endpoint_authed": true,
	})
	for {
		mt, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		if mt != websocket.TextMessage {
			continue
		}
		handleTurnWSTextMessage(conn, data)
	}
}

func handleTurnWSTextMessage(conn *turnWSConn, data []byte) {
	if conn == nil || conn.controller == nil {
		return
	}
	var msg struct {
		Type                 string  `json:"type"`
		Active               *bool   `json:"active"`
		Text                 string  `json:"text"`
		DurationMS           int     `json:"duration_ms"`
		InterruptedAssistant bool    `json:"interrupted_assistant"`
		SpeechProb           float64 `json:"speech_prob"`
		Playing              *bool   `json:"playing"`
		PlayedMS             int     `json:"played_ms"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	switch strings.TrimSpace(msg.Type) {
	case "turn_reset":
		conn.controller.Reset()
	case "turn_listen_state":
		if msg.Active != nil && !*msg.Active {
			conn.controller.Reset()
		}
	case "turn_speech_start":
		conn.controller.HandleSpeechStart(msg.InterruptedAssistant)
	case "turn_speech_prob":
		conn.controller.HandleSpeechProbability(msg.SpeechProb, msg.InterruptedAssistant)
	case "turn_transcript_segment":
		conn.controller.ConsumeSegment(turn.Segment{
			Text:                 msg.Text,
			DurationMS:           msg.DurationMS,
			InterruptedAssistant: msg.InterruptedAssistant,
		})
	case "turn_playback":
		if msg.Playing == nil {
			return
		}
		conn.controller.UpdatePlayback(*msg.Playing, msg.PlayedMS)
	}
}
