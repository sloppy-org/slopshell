package ptt

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/stt"
)

const (
	sampleRate    = 16000
	numChannels   = 1
	bitsPerSample = 16
)

// Config holds the PTT daemon configuration.
type Config struct {
	DevicePath string // evdev device path, e.g. /dev/input/event5
	KeyCode    uint16 // evdev key code (default: KEY_F13 = 183)
	WhisperURL string // STT sidecar URL (default: http://127.0.0.1:8427)
	WebAPIURL  string // tabura web API URL for replacements (default: http://127.0.0.1:8420)
	OutputMode string // "type" (ydotool) or "clipboard" (wl-copy)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		KeyCode:    183, // KEY_F13
		WhisperURL: "http://127.0.0.1:8427",
		WebAPIURL:  "http://127.0.0.1:8420",
		OutputMode: "type",
	}
}

// WrapWAV wraps raw 16-bit LE mono 16kHz PCM data in a WAV container.
func WrapWAV(pcm []byte) []byte {
	dataLen := uint32(len(pcm))
	fileLen := dataLen + 36
	buf := make([]byte, 44+dataLen)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], fileLen)
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(buf[22:24], numChannels)
	binary.LittleEndian.PutUint32(buf[24:28], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:32], sampleRate*numChannels*bitsPerSample/8)
	binary.LittleEndian.PutUint16(buf[32:34], numChannels*bitsPerSample/8)
	binary.LittleEndian.PutUint16(buf[34:36], bitsPerSample)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], dataLen)
	copy(buf[44:], pcm)
	return buf
}

// TranscribeAudio sends WAV audio to an OpenAI-compatible STT sidecar and returns text.
func TranscribeAudio(whisperURL string, wav []byte, replacements []stt.Replacement) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("ptt multipart create: %w", err)
	}
	if _, err := part.Write(wav); err != nil {
		return "", fmt.Errorf("ptt multipart write: %w", err)
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", fmt.Errorf("ptt multipart field: %w", err)
	}
	model := strings.TrimSpace(os.Getenv("TABURA_STT_MODEL_NAME"))
	if model == "" {
		model = "whisper-1"
	}
	if err := writer.WriteField("model", model); err != nil {
		return "", fmt.Errorf("ptt model field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("ptt multipart close: %w", err)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(whisperURL), "/")
	endpoint := baseURL
	if !strings.Contains(baseURL, "/v1/audio/transcriptions") {
		endpoint = baseURL + "/v1/audio/transcriptions"
	}
	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("ptt request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ptt request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("ptt HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&result); err != nil {
		return "", fmt.Errorf("ptt response decode: %w", err)
	}

	text := strings.TrimSpace(result.Text)
	if text == "" {
		return "", stt.ErrNoTranscriptOutput
	}
	if stt.IsWhisperHallucination(text) {
		return "", stt.ErrLikelyHallucination
	}
	if stt.IsLikelyNoise(text) {
		return "", stt.ErrLikelyNoise
	}

	text = stt.ApplyReplacements(text, replacements)
	return text, nil
}

// FetchReplacements tries to load STT replacements from the web API.
// Returns nil on failure (caller should proceed without replacements).
func FetchReplacements(webAPIURL string) []stt.Replacement {
	if strings.TrimSpace(webAPIURL) == "" {
		return nil
	}
	endpoint := strings.TrimRight(strings.TrimSpace(webAPIURL), "/") + "/api/stt/replacements"
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var m map[string]string
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&m); err != nil {
		return nil
	}
	out := make([]stt.Replacement, 0, len(m))
	for from, to := range m {
		if strings.TrimSpace(from) != "" {
			out = append(out, stt.Replacement{From: from, To: to})
		}
	}
	return out
}
