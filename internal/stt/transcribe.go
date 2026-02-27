package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// MaxAudioBytes is the upper limit for a single STT audio payload.
const MaxAudioBytes = 10 * 1024 * 1024

var (
	// ErrNoTranscriptOutput means the STT backend produced no usable text.
	ErrNoTranscriptOutput = errors.New("stt produced no transcript output")
	// ErrLikelyHallucination means Whisper returned a known silent-audio phantom.
	ErrLikelyHallucination = errors.New("rejected likely hallucination on silent audio")
	// ErrLikelyNoise means the transcript looks like background noise, not directed speech.
	ErrLikelyNoise = errors.New("rejected likely background noise")
	// ErrSTTDisabled means the STT sidecar URL is not configured.
	ErrSTTDisabled = errors.New("stt sidecar is not configured")
)

// whisperResponse is the JSON envelope returned by whisper-server /inference.
type whisperResponse struct {
	Text string `json:"text"`
}

// Transcribe sends audio data to a whisper.cpp server's /inference endpoint
// and returns the transcript text. serverURL is the base URL of the whisper
// sidecar (e.g. "http://127.0.0.1:8427").
// Privacy: audio is transmitted only via HTTP POST; no temp files are created.
// See docs/meeting-notes-privacy.md.
func Transcribe(serverURL, mimeType string, data []byte, replacements []Replacement) (string, error) {
	if strings.TrimSpace(serverURL) == "" {
		return "", ErrSTTDisabled
	}

	ext := FileExtFromMime(mimeType)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "audio"+ext)
	if err != nil {
		return "", fmt.Errorf("stt multipart create: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("stt multipart write: %w", err)
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", fmt.Errorf("stt multipart field: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("stt multipart close: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	endpoint := strings.TrimRight(strings.TrimSpace(serverURL), "/") + "/inference"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("stt request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("stt HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result whisperResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(&result); err != nil {
		return "", fmt.Errorf("stt response decode: %w", err)
	}

	text := strings.TrimSpace(result.Text)
	if text == "" {
		return "", ErrNoTranscriptOutput
	}
	if IsWhisperHallucination(text) {
		return "", ErrLikelyHallucination
	}
	if IsLikelyNoise(text) {
		return "", ErrLikelyNoise
	}

	text = ApplyReplacements(text, replacements)
	return text, nil
}

// IsRetryableNoSpeechError reports whether err means "no usable speech yet"
// and the caller should keep listening instead of failing hard.
func IsRetryableNoSpeechError(err error) bool {
	return errors.Is(err, ErrNoTranscriptOutput) || errors.Is(err, ErrLikelyHallucination) || errors.Is(err, ErrLikelyNoise)
}

// IsWhisperHallucination returns true if the text matches a known Whisper
// phantom output produced on silent or near-silent audio.
func IsWhisperHallucination(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.TrimRight(t, ".!?,;: ")
	for _, h := range whisperHallucinations {
		if t == h {
			return true
		}
	}
	return false
}

// IsLikelyNoise returns true when the transcript looks like background noise
// rather than directed speech: too short, filler-only, or matching common
// TV/radio patterns.
func IsLikelyNoise(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	words := strings.Fields(t)
	if len(words) < 3 {
		allFiller := true
		for _, w := range words {
			if !isFillerWord(strings.ToLower(w)) {
				allFiller = false
				break
			}
		}
		if allFiller {
			return true
		}
	}
	lower := strings.ToLower(t)
	lower = strings.TrimRight(lower, ".!?,;: ")
	for _, p := range backgroundPatterns {
		if lower == p {
			return true
		}
	}
	return false
}

func isFillerWord(w string) bool {
	switch w {
	case "um", "uh", "hmm", "mm", "okay", "ok", "yeah", "yep", "right", "ah", "oh", "hm", "mhm":
		return true
	}
	return false
}

var backgroundPatterns = []string{
	"coming up next",
	"brought to you by",
	"stay tuned",
	"we'll be right back",
	"and now a word from",
	"breaking news",
}

var whisperHallucinations = []string{
	"thank you",
	"thanks for watching",
	"thank you for watching",
	"thanks for listening",
	"thank you for listening",
	"subscribe",
	"subscribe to my channel",
	"like and subscribe",
	"please subscribe",
	"subtitles by the amara.org community",
	"subtitles created by amara.org community",
	"you",
	"bye",
	"the end",
}

// FileExtFromMime returns a file extension (with leading dot) for common
// audio MIME types. Defaults to ".webm" for unknown types.
func FileExtFromMime(mimeType string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.Contains(mt, "wav") {
		return ".wav"
	}
	if strings.Contains(mt, "ogg") {
		return ".ogg"
	}
	if strings.Contains(mt, "mp4") || strings.Contains(mt, "aac") || strings.Contains(mt, "m4a") {
		return ".m4a"
	}
	if strings.Contains(mt, "mpeg") {
		return ".mp3"
	}
	return ".webm"
}

// NormalizeMimeType strips parameters (e.g. ";codecs=opus") and lowercases
// the MIME type. Returns "audio/webm" for empty input.
func NormalizeMimeType(raw string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "audio/webm"
	}
	if i := strings.Index(candidate, ";"); i >= 0 {
		candidate = strings.TrimSpace(candidate[:i])
	}
	if candidate == "" {
		return "audio/webm"
	}
	return strings.ToLower(candidate)
}

// IsAllowedMimeType returns true when mimeType is audio/* or
// application/octet-stream.
func IsAllowedMimeType(mimeType string) bool {
	if mimeType == "application/octet-stream" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "audio/")
}

// Replacement is a single find/replace pair applied to transcription output.
type Replacement struct {
	From string
	To   string
}

// ApplyReplacements applies text replacements case-insensitively to the
// transcript. Each replacement is applied sequentially.
func ApplyReplacements(text string, replacements []Replacement) string {
	if len(replacements) == 0 {
		return text
	}
	for _, r := range replacements {
		if r.From == "" {
			continue
		}
		text = replaceAllCaseInsensitive(text, r.From, r.To)
	}
	return text
}

func replaceAllCaseInsensitive(text, from, to string) string {
	lower := strings.ToLower(text)
	lowerFrom := strings.ToLower(from)
	var result strings.Builder
	result.Grow(len(text))
	pos := 0
	for {
		idx := strings.Index(lower[pos:], lowerFrom)
		if idx < 0 {
			result.WriteString(text[pos:])
			break
		}
		result.WriteString(text[pos : pos+idx])
		result.WriteString(to)
		pos += idx + len(from)
	}
	return result.String()
}
