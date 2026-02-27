package stt

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func TestParseVoxTypeTranscript(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "single transcript line",
			raw:  "Hello world",
			want: "Hello world",
		},
		{
			name: "filters diagnostic lines",
			raw: "Loading audio file: /tmp/input.wav\n" +
				"Audio format: 16000 Hz, mono\n" +
				"Resampling from 48000 to 16000\n" +
				"Processing audio...\n" +
				"VAD: detected speech\n" +
				"This is the transcript",
			want: "This is the transcript",
		},
		{
			name: "empty input",
			raw:  "",
			want: "",
		},
		{
			name: "only diagnostic lines",
			raw:  "Loading audio file: /tmp/input.wav\nAudio format: 16000 Hz",
			want: "",
		},
		{
			name: "whitespace only",
			raw:  "   \n\n   ",
			want: "",
		},
		{
			name: "windows line endings",
			raw:  "Loading audio file: foo\r\nThe quick brown fox",
			want: "The quick brown fox",
		},
		{
			name: "returns last non-diagnostic line",
			raw:  "first line\nsecond line\nthird line",
			want: "third line",
		},
		{
			name: "trims surrounding whitespace",
			raw:  "  some text  ",
			want: "some text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseVoxTypeTranscript(tt.raw)
			if got != tt.want {
				t.Errorf("ParseVoxTypeTranscript(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFileExtFromMime(t *testing.T) {
	tests := []struct {
		mimeType string
		want     string
	}{
		{"audio/wav", ".wav"},
		{"audio/x-wav", ".wav"},
		{"audio/ogg", ".ogg"},
		{"audio/ogg; codecs=opus", ".ogg"},
		{"audio/mp4", ".m4a"},
		{"audio/aac", ".m4a"},
		{"audio/x-m4a", ".m4a"},
		{"audio/mpeg", ".mp3"},
		{"audio/webm", ".webm"},
		{"audio/webm; codecs=opus", ".webm"},
		{"application/octet-stream", ".webm"},
		{"", ".webm"},
		{"  AUDIO/WAV  ", ".wav"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := FileExtFromMime(tt.mimeType)
			if got != tt.want {
				t.Errorf("FileExtFromMime(%q) = %q, want %q", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestNormalizeMimeType(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty defaults to audio/webm", "", "audio/webm"},
		{"whitespace defaults to audio/webm", "   ", "audio/webm"},
		{"strips codec params", "audio/webm; codecs=opus", "audio/webm"},
		{"lowercases", "Audio/OGG", "audio/ogg"},
		{"strips params and lowercases", "Audio/WAV; rate=16000", "audio/wav"},
		{"plain type unchanged", "audio/mp4", "audio/mp4"},
		{"semicolon only", ";", "audio/webm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeMimeType(tt.raw)
			if got != tt.want {
				t.Errorf("NormalizeMimeType(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestIsAllowedMimeType(t *testing.T) {
	tests := []struct {
		mimeType string
		want     bool
	}{
		{"audio/webm", true},
		{"audio/ogg", true},
		{"audio/wav", true},
		{"audio/mpeg", true},
		{"Audio/MP4", true},
		{"application/octet-stream", true},
		{"video/mp4", false},
		{"text/plain", false},
		{"", false},
		{"application/json", false},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := IsAllowedMimeType(tt.mimeType)
			if got != tt.want {
				t.Errorf("IsAllowedMimeType(%q) = %v, want %v", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestIsWhisperHallucination(t *testing.T) {
	hallucinations := []string{
		"Thank you.",
		"thank you",
		"THANK YOU!",
		"Thank you for watching.",
		"Thanks for watching",
		"Subscribe to my channel",
		"you",
		"Bye.",
		"The end",
		"  thank you  ",
	}
	for _, s := range hallucinations {
		if !IsWhisperHallucination(s) {
			t.Errorf("IsWhisperHallucination(%q) = false, want true", s)
		}
	}
	legitimate := []string{
		"Hello world",
		"Thank you very much for helping me with the code",
		"Please fix the bug in the login form",
		"Can you subscribe me to the newsletter",
		"The meeting ended",
	}
	for _, s := range legitimate {
		if IsWhisperHallucination(s) {
			t.Errorf("IsWhisperHallucination(%q) = true, want false", s)
		}
	}
}

func TestIsLikelyNoise(t *testing.T) {
	noise := []string{
		"um",
		"uh hmm",
		"okay",
		"yeah",
		"",
		"  ",
		"coming up next",
		"Brought to you by.",
		"Stay tuned!",
		"mhm",
		"oh ok",
	}
	for _, s := range noise {
		if !IsLikelyNoise(s) {
			t.Errorf("IsLikelyNoise(%q) = false, want true", s)
		}
	}
	legitimate := []string{
		"turn off the lights",
		"what time is it",
		"set a timer for five minutes",
		"switch to the other project",
		"um I think we should fix the login form",
	}
	for _, s := range legitimate {
		if IsLikelyNoise(s) {
			t.Errorf("IsLikelyNoise(%q) = true, want false", s)
		}
	}
}

func TestWrapVoxTypeStartError(t *testing.T) {
	t.Run("not found returns install hint", func(t *testing.T) {
		err := wrapVoxTypeStartError(fmt.Errorf("exec: %w", exec.ErrNotFound))
		want := "STT requires voxtype; install from https://github.com/pbizopoulos/voxtype"
		if err == nil || err.Error() != want {
			t.Fatalf("wrapVoxTypeStartError(not found) = %v, want %q", err, want)
		}
	})

	t.Run("other startup error is wrapped", func(t *testing.T) {
		base := errors.New("permission denied")
		err := wrapVoxTypeStartError(base)
		if err == nil {
			t.Fatal("wrapVoxTypeStartError returned nil")
		}
		if !strings.Contains(err.Error(), "failed to start voxtype: permission denied") {
			t.Fatalf("unexpected error text: %q", err.Error())
		}
	})
}
