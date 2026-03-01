package stt

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const ffmpegNormalizeTimeout = 25 * time.Second

// NormalizeForWhisper converts any incoming audio payload to a deterministic
// whisper-friendly format: mono 16k WAV.
func NormalizeForWhisper(mimeType string, data []byte) (string, []byte, error) {
	_ = NormalizeMimeType(mimeType)
	wav, err := transcodeToMono16kWAV(data)
	if err != nil {
		return "", nil, err
	}
	return "audio/wav", wav, nil
}

func transcodeToMono16kWAV(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audio payload is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), ffmpegNormalizeTimeout)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-nostdin",
		"-i", "pipe:0",
		"-ac", "1",
		"-ar", "16000",
		"-acodec", "pcm_s16le",
		"-f", "s16le",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(data)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(err.Error())
		}
		if msg == "" {
			msg = "unknown ffmpeg failure"
		}
		return nil, fmt.Errorf("ffmpeg normalize failed: %s", msg)
	}
	out := stdout.Bytes()
	if len(out) == 0 {
		return nil, fmt.Errorf("ffmpeg produced empty PCM output")
	}
	if len(out)%2 != 0 {
		return nil, fmt.Errorf("ffmpeg produced misaligned PCM output (%d bytes)", len(out))
	}
	return wrapPCM16Mono16kWAV(out), nil
}

func wrapPCM16Mono16kWAV(pcm []byte) []byte {
	dataLen := len(pcm)
	out := make([]byte, 44+dataLen)
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+dataLen))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(out[22:24], 1) // mono
	binary.LittleEndian.PutUint32(out[24:28], 16000)
	binary.LittleEndian.PutUint32(out[28:32], 32000) // sampleRate * channels * bytesPerSample
	binary.LittleEndian.PutUint16(out[32:34], 2)     // channels * bytesPerSample
	binary.LittleEndian.PutUint16(out[34:36], 16)    // bits per sample
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataLen))
	copy(out[44:], pcm)
	return out
}
