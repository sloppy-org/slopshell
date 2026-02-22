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

// TranscribeWithVoxType converts audio to WAV via ffmpeg, then transcribes
// it with the voxtype CLI. It returns the transcript text or an error.
func TranscribeWithVoxType(mimeType string, data []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "tabula-voxtype-*")
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
		return "", fmt.Errorf("failed to start voxtype: %w", err)
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
		return "", errors.New("voxtype produced no transcript output")
	}
	return text, nil
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
