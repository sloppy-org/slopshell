package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/krystophny/tabura/internal/stt"
)

type sttMessage struct {
	Type     string `json:"type"`
	MimeType string `json:"mime_type,omitempty"`
	Text     string `json:"text,omitempty"`
	Error    string `json:"error,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func handleSTTStart(conn *chatWSConn, mimeType string) {
	conn.sttMu.Lock()
	defer conn.sttMu.Unlock()

	mimeType = stt.NormalizeMimeType(mimeType)
	if !stt.IsAllowedMimeType(mimeType) {
		_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: "mime_type must be audio/* or application/octet-stream"})
		return
	}

	conn.sttActive = true
	conn.sttMimeType = mimeType
	conn.sttBuf = make([]byte, 0, 4096)

	_ = conn.writeJSON(sttMessage{Type: "stt_started"})
}

func handleSTTBinaryChunk(conn *chatWSConn, data []byte) {
	conn.sttMu.Lock()
	defer conn.sttMu.Unlock()

	if !conn.sttActive {
		return
	}
	if len(conn.sttBuf)+len(data) > stt.MaxAudioBytes {
		conn.sttActive = false
		conn.sttBuf = nil
		conn.sttMimeType = ""
		_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: "audio payload exceeds max size"})
		return
	}
	conn.sttBuf = append(conn.sttBuf, data...)
}

// Privacy: buffer is set to nil after transcription or on error. See docs/meeting-notes-privacy.md.
func handleSTTStop(a *App, conn *chatWSConn) {
	conn.sttMu.Lock()
	if !conn.sttActive {
		conn.sttMu.Unlock()
		_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: "no active STT session"})
		return
	}
	conn.sttActive = false
	buf := conn.sttBuf
	mimeType := conn.sttMimeType
	conn.sttBuf = nil
	conn.sttMimeType = ""
	conn.sttMu.Unlock()

	if len(buf) < 1024 {
		_ = conn.writeJSON(sttMessage{Type: "stt_empty", Reason: "recording_too_short"})
		return
	}

	if a.sttURL == "" {
		_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: "STT sidecar is not configured"})
		return
	}

	replacements := a.loadSTTReplacements()
	normalizedMimeType, normalizedData, normalizeErr := stt.NormalizeForWhisper(mimeType, buf)
	if normalizeErr != nil {
		_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: fmt.Sprintf("audio normalization failed: %v", normalizeErr)})
		return
	}
	options := a.sttTranscribeOptions()
	text, err := stt.TranscribeWithOptions(a.sttURL, normalizedMimeType, normalizedData, replacements, options)
	if err != nil {
		if errors.Is(err, stt.ErrLikelyNoise) {
			_ = conn.writeJSON(sttMessage{Type: "stt_empty", Reason: "likely_noise"})
			return
		}
		if stt.IsRetryableNoSpeechError(err) {
			_ = conn.writeJSON(sttMessage{Type: "stt_empty", Reason: "no_speech_detected"})
			return
		}
		log.Printf("stt transcribe error: %v", err)
		_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: fmt.Sprintf("transcription failed: %v", err)})
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		_ = conn.writeJSON(sttMessage{Type: "stt_empty", Reason: "empty_transcript"})
		return
	}
	_ = conn.writeJSON(sttMessage{Type: "stt_result", Text: text})
}

// Privacy: buffer is discarded immediately on cancel. See docs/meeting-notes-privacy.md.
func handleSTTCancel(conn *chatWSConn) {
	conn.sttMu.Lock()
	conn.sttActive = false
	conn.sttBuf = nil
	conn.sttMimeType = ""
	conn.sttMu.Unlock()

	_ = conn.writeJSON(sttMessage{Type: "stt_cancelled"})
}

func handleChatWSTextMessage(a *App, conn *chatWSConn, sessionID string, data []byte) {
	var msg struct {
		Type            string             `json:"type"`
		MimeType        string             `json:"mime_type"`
		Text            string             `json:"text"`
		Lang            string             `json:"lang"`
		RequestID       string             `json:"request_id"`
		Decision        string             `json:"decision"`
		OutputMode      string             `json:"output_mode"`
		Gesture         string             `json:"gesture"`
		RequestResponse bool               `json:"request_response"`
		Cursor          *chatCursorContext `json:"cursor"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	switch msg.Type {
	case "stt_start":
		handleSTTStart(conn, msg.MimeType)
	case "stt_stop":
		handleSTTStop(a, conn)
	case "stt_cancel":
		handleSTTCancel(conn)
	case "tts_speak":
		trimmedText := strings.TrimSpace(msg.Text)
		seq := conn.reserveTTSSeq()
		log.Printf("tts_speak received: session=%s seq=%d chars=%d lang=%q", sessionID, seq, len([]rune(trimmedText)), strings.TrimSpace(msg.Lang))
		go a.handleTTSSpeak(sessionID, conn, seq, msg.Text, msg.Lang)
	case "participant_start":
		handleParticipantStart(a, conn, sessionID)
	case "participant_stop":
		handleParticipantStop(a, conn)
	case "approval_response":
		if !a.resolvePendingAppServerApproval(sessionID, msg.RequestID, msg.Decision) {
			_ = conn.writeJSON(map[string]interface{}{
				"type":       "approval_error",
				"request_id": strings.TrimSpace(msg.RequestID),
				"error":      "approval request not found",
			})
		}
	case "canvas_position":
		if !a.chatCanvasPositions.enqueue(sessionID, &chatCanvasPositionEvent{
			Cursor:    msg.Cursor,
			Gesture:   msg.Gesture,
			Requested: msg.RequestResponse,
		}) {
			return
		}
		if msg.RequestResponse {
			a.enqueueAssistantTurn(sessionID, normalizeTurnOutputMode(msg.OutputMode))
		}
	}
}
