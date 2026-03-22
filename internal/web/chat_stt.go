package web

import (
	"encoding/base64"
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

func handleSTTStart(conn *chatWSConn, sessionID string, mimeType string) {
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
	log.Printf("stt session started: session=%s mime_type=%q", strings.TrimSpace(sessionID), mimeType)

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
func handleSTTStop(a *App, conn *chatWSConn, sessionID string) {
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
		log.Printf("stt session empty: session=%s reason=recording_too_short bytes=%d", strings.TrimSpace(sessionID), len(buf))
		_ = conn.writeJSON(sttMessage{Type: "stt_empty", Reason: "recording_too_short"})
		return
	}

	if a.sttURL == "" {
		log.Printf("stt session error: session=%s reason=sidecar_unconfigured", strings.TrimSpace(sessionID))
		_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: "STT sidecar is not configured"})
		return
	}

	replacements := a.loadSTTReplacements()
	normalizedMimeType, normalizedData, normalizeErr := stt.NormalizeForWhisper(mimeType, buf)
	if normalizeErr != nil {
		log.Printf("stt session error: session=%s reason=normalize_failed err=%v", strings.TrimSpace(sessionID), normalizeErr)
		_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: fmt.Sprintf("audio normalization failed: %v", normalizeErr)})
		return
	}
	options := a.sttTranscribeOptions()
	text, err := stt.TranscribeWithOptions(a.sttURL, normalizedMimeType, normalizedData, replacements, options)
	if err != nil {
		if errors.Is(err, stt.ErrLikelyNoise) {
			log.Printf("stt session empty: session=%s reason=likely_noise bytes=%d", strings.TrimSpace(sessionID), len(normalizedData))
			_ = conn.writeJSON(sttMessage{Type: "stt_empty", Reason: "likely_noise"})
			return
		}
		if stt.IsRetryableNoSpeechError(err) {
			log.Printf("stt session empty: session=%s reason=no_speech_detected bytes=%d", strings.TrimSpace(sessionID), len(normalizedData))
			_ = conn.writeJSON(sttMessage{Type: "stt_empty", Reason: "no_speech_detected"})
			return
		}
		log.Printf("stt transcribe error: session=%s err=%v", strings.TrimSpace(sessionID), err)
		_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: fmt.Sprintf("transcription failed: %v", err)})
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		log.Printf("stt session empty: session=%s reason=empty_transcript", strings.TrimSpace(sessionID))
		_ = conn.writeJSON(sttMessage{Type: "stt_empty", Reason: "empty_transcript"})
		return
	}
	log.Printf("stt session result: session=%s chars=%d mime_type=%q", strings.TrimSpace(sessionID), len([]rune(text)), normalizedMimeType)
	_ = conn.writeJSON(sttMessage{Type: "stt_result", Text: text})
}

// Privacy: buffer is discarded immediately on cancel. See docs/meeting-notes-privacy.md.
func handleSTTCancel(conn *chatWSConn, sessionID string) {
	conn.sttMu.Lock()
	conn.sttActive = false
	conn.sttBuf = nil
	conn.sttMimeType = ""
	conn.sttMu.Unlock()
	log.Printf("stt session cancelled: session=%s", strings.TrimSpace(sessionID))

	_ = conn.writeJSON(sttMessage{Type: "stt_cancelled"})
}

func handleChatWSTextMessage(a *App, conn *chatWSConn, sessionID string, data []byte) {
	var msg struct {
		Type             string                    `json:"type"`
		Kind             string                    `json:"kind"`
		MimeType         string                    `json:"mime_type"`
		Data             string                    `json:"data"`
		Text             string                    `json:"text"`
		Lang             string                    `json:"lang"`
		RequestID        string                    `json:"request_id"`
		Decision         string                    `json:"decision"`
		OutputMode       string                    `json:"output_mode"`
		Gesture          string                    `json:"gesture"`
		RequestResponse  bool                      `json:"request_response"`
		Cursor           *chatCursorContext        `json:"cursor"`
		ArtifactKind     string                    `json:"artifact_kind"`
		SnapshotDataURL  string                    `json:"snapshot_data_url"`
		TotalStrokes     int                       `json:"total_strokes"`
		BoundingBox      *chatCanvasInkBoundingBox `json:"bounding_box"`
		OverlappingLines *chatCanvasInkLineRange   `json:"overlapping_lines"`
		OverlappingText  string                    `json:"overlapping_text"`
		Strokes          []inkSubmitStroke         `json:"strokes"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	switch msg.Type {
	case "stt_start":
		handleSTTStart(conn, sessionID, msg.MimeType)
	case "audio_start":
		handleSTTStart(conn, sessionID, msg.MimeType)
	case "stt_stop":
		handleSTTStop(a, conn, sessionID)
	case "audio_stop":
		handleSTTStop(a, conn, sessionID)
	case "stt_cancel":
		handleSTTCancel(conn, sessionID)
	case "audio_pcm":
		conn.sttMu.Lock()
		sttActive := conn.sttActive
		conn.sttMu.Unlock()
		if !sttActive {
			handleSTTStart(conn, sessionID, firstNonEmptyCursorText(msg.MimeType, "application/octet-stream"))
		}
		audioBytes, err := decodeCaptureAudioData(msg.Data)
		if err != nil {
			_ = conn.writeJSON(sttMessage{Type: "stt_error", Error: "audio data must be base64"})
			return
		}
		if len(audioBytes) == 0 {
			return
		}
		handleSTTBinaryChunk(conn, audioBytes)
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
		enqueueRequestedCanvasPosition(a, sessionID, msg.Cursor, msg.Gesture, msg.RequestResponse, msg.OutputMode)
	case "tap":
		enqueueRequestedCanvasPosition(a, sessionID, msg.Cursor, "tap", msg.RequestResponse, msg.OutputMode)
	case "gesture":
		enqueueRequestedCanvasPosition(a, sessionID, msg.Cursor, firstNonEmptyCursorText(msg.Gesture, msg.Kind, "gesture"), msg.RequestResponse, msg.OutputMode)
	case "canvas_ink":
		enqueueRequestedCanvasInk(a, sessionID, msg.Cursor, msg.ArtifactKind, msg.OutputMode, msg.RequestResponse, msg.SnapshotDataURL, msg.TotalStrokes, msg.BoundingBox, msg.OverlappingLines, msg.OverlappingText, msg.Strokes)
	case "ink_stroke":
		enqueueRequestedCanvasInk(a, sessionID, msg.Cursor, msg.ArtifactKind, msg.OutputMode, msg.RequestResponse, msg.SnapshotDataURL, msg.TotalStrokes, msg.BoundingBox, msg.OverlappingLines, msg.OverlappingText, msg.Strokes)
	case "ink_commit":
		enqueueRequestedCanvasInk(a, sessionID, msg.Cursor, msg.ArtifactKind, msg.OutputMode, msg.RequestResponse, msg.SnapshotDataURL, msg.TotalStrokes, msg.BoundingBox, msg.OverlappingLines, msg.OverlappingText, msg.Strokes)
	}
}

func maxCanvasInkStrokeCount(total, current int) int {
	if total > current {
		return total
	}
	if current > 0 {
		return current
	}
	return 1
}

func enqueueRequestedCanvasPosition(a *App, sessionID string, cursor *chatCursorContext, gesture string, requested bool, outputMode string) {
	if !a.chatCanvasPositions.enqueue(sessionID, &chatCanvasPositionEvent{
		Cursor:    cursor,
		Gesture:   strings.TrimSpace(gesture),
		Requested: requested,
	}) {
		return
	}
	if !requested {
		return
	}
	if a.activeChatTurnCount(sessionID) > 0 || a.queuedChatTurnCount(sessionID) > 0 {
		return
	}
	a.enqueueAssistantTurn(sessionID, normalizeTurnOutputMode(outputMode))
}

func enqueueRequestedCanvasInk(
	a *App,
	sessionID string,
	cursor *chatCursorContext,
	artifactKind string,
	outputMode string,
	requested bool,
	snapshotDataURL string,
	totalStrokes int,
	boundingBox *chatCanvasInkBoundingBox,
	overlappingLines *chatCanvasInkLineRange,
	overlappingText string,
	strokes []inkSubmitStroke,
) {
	snapshotPath := a.persistChatCanvasInkSnapshot(sessionID, snapshotDataURL)
	if !a.chatCanvasInk.enqueue(sessionID, &chatCanvasInkEvent{
		Cursor:           cursor,
		Gesture:          recognizeChatCanvasInkGesture(strokes),
		ArtifactKind:     artifactKind,
		StrokeCount:      maxCanvasInkStrokeCount(totalStrokes, len(strokes)),
		Requested:        requested,
		BoundingBox:      boundingBox,
		OverlappingLines: overlappingLines,
		OverlappingText:  overlappingText,
		SnapshotPath:     snapshotPath,
	}) {
		return
	}
	if !requested {
		return
	}
	if a.activeChatTurnCount(sessionID) > 0 || a.queuedChatTurnCount(sessionID) > 0 {
		return
	}
	a.enqueueAssistantTurn(sessionID, normalizeTurnOutputMode(outputMode))
}

func decodeCaptureAudioData(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "data:") {
		if idx := strings.Index(trimmed, ","); idx >= 0 {
			trimmed = trimmed[idx+1:]
		}
	}
	if data, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(trimmed)
}
