package stt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MaxAudioBytes is the upper limit for a single STT audio payload.
const MaxAudioBytes = 10 * 1024 * 1024

var (
	// ErrNoTranscriptOutput means the STT backend produced no usable text.
	ErrNoTranscriptOutput = errors.New("voxtype produced no transcript output")
	// ErrLikelyHallucination means Whisper returned a known silent-audio phantom.
	ErrLikelyHallucination = errors.New("rejected likely hallucination on silent audio")
	// ErrLikelyNoise means the transcript looks like background noise, not directed speech.
	ErrLikelyNoise = errors.New("rejected likely background noise")
)

// TranscribeWithVoxType converts audio to WAV via ffmpeg, then transcribes
// it with the voxtype CLI. It returns the transcript text or an error.
func TranscribeWithVoxType(mimeType string, data []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "tabura-voxtype-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inExt := FileExtFromMime(mimeType)
	inputPath := filepath.Join(tmpDir, "input"+inExt)
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to write input audio: %w", err)
	}

	wavPath := filepath.Join(tmpDir, "input.wav")
	ffmpegCtx, ffmpegCancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer ffmpegCancel()
	ffmpegCmd := exec.CommandContext(
		ffmpegCtx,
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-ac", "1",
		"-ar", "16000",
		"-f", "wav",
		wavPath,
	)
	ffmpegOut, ffmpegErr := ffmpegCmd.CombinedOutput()
	if ffmpegErr != nil {
		return "", fmt.Errorf("ffmpeg conversion failed: %v: %s", ffmpegErr, strings.TrimSpace(string(ffmpegOut)))
	}

	voxCtx, voxCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer voxCancel()
	voxCmd := exec.CommandContext(voxCtx, "voxtype", "-q", "transcribe", wavPath)
	stdout, err := voxCmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("voxtype stdout pipe: %w", err)
	}
	stderr, err := voxCmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("voxtype stderr pipe: %w", err)
	}
	if err := voxCmd.Start(); err != nil {
		return "", wrapVoxTypeStartError(err)
	}
	outBytes, _ := io.ReadAll(stdout)
	errBytes, _ := io.ReadAll(stderr)
	waitErr := voxCmd.Wait()
	if waitErr != nil {
		return "", fmt.Errorf("voxtype transcribe failed: %v: %s", waitErr, strings.TrimSpace(string(errBytes)))
	}
	text := ParseVoxTypeTranscript(string(outBytes))
	if text == "" {
		text = ParseVoxTypeTranscript(string(errBytes))
	}
	if text == "" {
		return "", ErrNoTranscriptOutput
	}
	if IsWhisperHallucination(text) {
		return "", ErrLikelyHallucination
	}
	if IsLikelyNoise(text) {
		return "", ErrLikelyNoise
	}
	return text, nil
}

// IsRetryableNoSpeechError reports whether err means "no usable speech yet"
// and the caller should keep listening instead of failing hard.
func IsRetryableNoSpeechError(err error) bool {
	return errors.Is(err, ErrNoTranscriptOutput) || errors.Is(err, ErrLikelyHallucination) || errors.Is(err, ErrLikelyNoise)
}

// ParseVoxTypeTranscript extracts the transcript line from voxtype output,
// filtering out diagnostic lines (audio format, resampling, VAD, etc.).
func ParseVoxTypeTranscript(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Loading audio file:") ||
			strings.HasPrefix(line, "Audio format:") ||
			strings.HasPrefix(line, "Resampling from") ||
			strings.HasPrefix(line, "Processing ") ||
			strings.HasPrefix(line, "VAD:") {
			continue
		}
		parts = append(parts, line)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

// IsWhisperHallucination returns true if the text matches a known Whisper
// phantom output produced on silent or near-silent audio.
func IsWhisperHallucination(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	// Strip trailing punctuation for matching.
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

func wrapVoxTypeStartError(err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return errors.New("STT requires voxtype; install from https://github.com/pbizopoulos/voxtype")
	}
	return fmt.Errorf("failed to start voxtype: %w", err)
}
