package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultTTSURL     = "http://127.0.0.1:8424"
	ttsRequestTimeout = 30 * time.Second
)

func (a *App) handleTTSSpeak(sessionID string, conn *chatWSConn, seq int64, text, lang string) {
	wavData, clientErr := a.synthesizeTTSAudio(sessionID, seq, text, lang)
	ready := conn.completeTTSSeq(seq, wavData, clientErr)
	for _, result := range ready {
		if result.err != "" {
			log.Printf("tts emit error: session=%s seq=%d err=%s", sessionID, result.seq, result.err)
			_ = conn.writeJSON(map[string]string{"type": "tts_error", "error": result.err})
			continue
		}
		if err := conn.writeBinary(result.audio); err != nil {
			log.Printf("tts websocket write error: session=%s seq=%d bytes=%d err=%v", sessionID, result.seq, len(result.audio), err)
			continue
		}
		log.Printf("tts delivered: session=%s seq=%d bytes=%d", sessionID, result.seq, len(result.audio))
	}
}

func (a *App) synthesizeTTSAudio(sessionID string, seq int64, text, lang string) ([]byte, string) {
	text = strings.TrimSpace(text)
	if text == "" {
		log.Printf("tts dropped: session=%s seq=%d reason=empty_text", sessionID, seq)
		return nil, "text is required"
	}
	if lang == "" {
		lang = "en"
	}

	ttsURL := strings.TrimSpace(a.ttsURL)
	if ttsURL == "" {
		log.Printf("tts dropped: session=%s seq=%d reason=service_not_configured", sessionID, seq)
		return nil, "TTS service not configured"
	}
	log.Printf("tts start: session=%s seq=%d chars=%d lang=%q", sessionID, seq, len([]rune(text)), strings.TrimSpace(lang))

	body, _ := json.Marshal(map[string]interface{}{
		"input":           text,
		"voice":           lang,
		"response_format": "wav",
	})

	ctx, cancel := context.WithTimeout(context.Background(), ttsRequestTimeout)
	defer cancel()

	upstream := fmt.Sprintf("%s/v1/audio/speech", strings.TrimRight(ttsURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream, bytes.NewReader(body))
	if err != nil {
		log.Printf("tts request build error: session=%s seq=%d err=%v", sessionID, seq, err)
		return nil, "failed to create TTS request"
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("tts upstream error: session=%s seq=%d err=%v", sessionID, seq, err)
		return nil, "TTS service unavailable"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("tts upstream HTTP %d: session=%s seq=%d body=%s", resp.StatusCode, sessionID, seq, strings.TrimSpace(string(errBody)))
		return nil, fmt.Sprintf("TTS error: HTTP %d", resp.StatusCode)
	}

	wavData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("tts read body error: session=%s seq=%d err=%v", sessionID, seq, err)
		return nil, "failed to read TTS response"
	}
	return wavData, ""
}
