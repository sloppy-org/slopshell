package stt

import (
	"encoding/binary"
	"os/exec"
	"testing"
)

func TestWrapPCM16Mono16kWAV(t *testing.T) {
	pcm := make([]byte, 320)
	wav := wrapPCM16Mono16kWAV(pcm)
	if len(wav) != 44+len(pcm) {
		t.Fatalf("wav length=%d, want %d", len(wav), 44+len(pcm))
	}
	if string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		t.Fatalf("invalid RIFF/WAVE header")
	}
	if got := binary.LittleEndian.Uint16(wav[20:22]); got != 1 {
		t.Fatalf("audioFormat=%d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(wav[22:24]); got != 1 {
		t.Fatalf("channels=%d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 16000 {
		t.Fatalf("sampleRate=%d, want 16000", got)
	}
	if got := binary.LittleEndian.Uint16(wav[34:36]); got != 16 {
		t.Fatalf("bitsPerSample=%d, want 16", got)
	}
	if got := int(binary.LittleEndian.Uint32(wav[40:44])); got != len(pcm) {
		t.Fatalf("dataLen=%d, want %d", got, len(pcm))
	}
}

func TestNormalizeForWhisperProducesStrictPCM16WAV(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	in := buildTestPCM16WAV(22050, 2, 700, 0.35)
	mimeType, out, err := NormalizeForWhisper("audio/wav", in)
	if err != nil {
		t.Fatalf("NormalizeForWhisper error: %v", err)
	}
	if mimeType != "audio/wav" {
		t.Fatalf("mimeType=%q, want audio/wav", mimeType)
	}
	if len(out) < 44 {
		t.Fatalf("normalized wav too short: %d", len(out))
	}
	if string(out[0:4]) != "RIFF" || string(out[8:12]) != "WAVE" {
		t.Fatalf("invalid normalized RIFF/WAVE header")
	}
	if got := binary.LittleEndian.Uint16(out[20:22]); got != 1 {
		t.Fatalf("audioFormat=%d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(out[22:24]); got != 1 {
		t.Fatalf("channels=%d, want 1", got)
	}
	if got := binary.LittleEndian.Uint32(out[24:28]); got != 16000 {
		t.Fatalf("sampleRate=%d, want 16000", got)
	}
	if got := int(binary.LittleEndian.Uint32(out[40:44])); got != len(out)-44 {
		t.Fatalf("dataLen=%d, want %d", got, len(out)-44)
	}
	if ((len(out) - 44) % 2) != 0 {
		t.Fatalf("data section not aligned to 16-bit samples: %d", len(out)-44)
	}
}
