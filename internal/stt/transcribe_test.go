package stt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestTranscribe(t *testing.T) {
	t.Run("successful transcription", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/inference" {
				t.Errorf("expected /inference, got %s", r.URL.Path)
			}
			ct := r.Header.Get("Content-Type")
			if ct == "" {
				t.Error("missing Content-Type header")
			}
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Fatalf("parse multipart: %v", err)
			}
			if r.FormValue("response_format") != "json" {
				t.Errorf("expected response_format=json, got %q", r.FormValue("response_format"))
			}
			_, fh, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("form file: %v", err)
			}
			if fh.Filename != "audio.webm" {
				t.Errorf("expected audio.webm, got %q", fh.Filename)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(whisperResponse{Text: "Hello world"})
		}))
		defer srv.Close()

		text, err := Transcribe(srv.URL, "audio/webm", []byte("fake-audio-data"), nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if text != "Hello world" {
			t.Errorf("got %q, want %q", text, "Hello world")
		}
	})

	t.Run("correct file extension per mime type", func(t *testing.T) {
		var gotFilename string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Fatalf("parse multipart: %v", err)
			}
			_, fh, _ := r.FormFile("file")
			gotFilename = fh.Filename
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(whisperResponse{Text: "Some speech"})
		}))
		defer srv.Close()

		Transcribe(srv.URL, "audio/wav", []byte("fake"), nil)
		if gotFilename != "audio.wav" {
			t.Errorf("wav: got filename %q, want audio.wav", gotFilename)
		}

		Transcribe(srv.URL, "audio/ogg", []byte("fake"), nil)
		if gotFilename != "audio.ogg" {
			t.Errorf("ogg: got filename %q, want audio.ogg", gotFilename)
		}
	})

	t.Run("hallucination rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(whisperResponse{Text: "Thank you."})
		}))
		defer srv.Close()

		_, err := Transcribe(srv.URL, "audio/webm", []byte("fake"), nil)
		if err != ErrLikelyHallucination {
			t.Errorf("expected ErrLikelyHallucination, got %v", err)
		}
	})

	t.Run("noise rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(whisperResponse{Text: "um"})
		}))
		defer srv.Close()

		_, err := Transcribe(srv.URL, "audio/webm", []byte("fake"), nil)
		if err != ErrLikelyNoise {
			t.Errorf("expected ErrLikelyNoise, got %v", err)
		}
	})

	t.Run("empty transcript", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(whisperResponse{Text: ""})
		}))
		defer srv.Close()

		_, err := Transcribe(srv.URL, "audio/webm", []byte("fake"), nil)
		if err != ErrNoTranscriptOutput {
			t.Errorf("expected ErrNoTranscriptOutput, got %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := Transcribe(srv.URL, "audio/webm", []byte("fake"), nil)
		if err == nil {
			t.Fatal("expected error for server error response")
		}
	})

	t.Run("disabled when URL empty", func(t *testing.T) {
		_, err := Transcribe("", "audio/webm", []byte("fake"), nil)
		if err != ErrSTTDisabled {
			t.Errorf("expected ErrSTTDisabled, got %v", err)
		}
	})

	t.Run("applies text replacements", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(whisperResponse{Text: "The stelorators use rungicata methods"})
		}))
		defer srv.Close()

		replacements := []Replacement{
			{From: "stelorators", To: "stellarators"},
			{From: "rungicata", To: "Runge-Kutta"},
		}
		text, err := Transcribe(srv.URL, "audio/webm", []byte("fake"), replacements)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "The stellarators use Runge-Kutta methods"
		if text != want {
			t.Errorf("got %q, want %q", text, want)
		}
	})
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

func TestApplyReplacements(t *testing.T) {
	t.Run("empty replacements", func(t *testing.T) {
		got := ApplyReplacements("hello world", nil)
		if got != "hello world" {
			t.Errorf("got %q, want %q", got, "hello world")
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		rs := []Replacement{{From: "HELLO", To: "hi"}}
		got := ApplyReplacements("Hello World", rs)
		if got != "hi World" {
			t.Errorf("got %q, want %q", got, "hi World")
		}
	})

	t.Run("multiple occurrences", func(t *testing.T) {
		rs := []Replacement{{From: "foo", To: "bar"}}
		got := ApplyReplacements("foo and foo", rs)
		if got != "bar and bar" {
			t.Errorf("got %q, want %q", got, "bar and bar")
		}
	})

	t.Run("empty from is skipped", func(t *testing.T) {
		rs := []Replacement{{From: "", To: "bar"}}
		got := ApplyReplacements("hello", rs)
		if got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})

	t.Run("sequential application", func(t *testing.T) {
		rs := []Replacement{
			{From: "stelorators", To: "stellarators"},
			{From: "idiopatic", To: "adiabatic"},
		}
		got := ApplyReplacements("stelorators and idiopatic", rs)
		if got != "stellarators and adiabatic" {
			t.Errorf("got %q, want %q", got, "stellarators and adiabatic")
		}
	})
}

// Privacy: docs/meeting-notes-privacy.md

func TestPrivacyTranscribeNoTempFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(whisperResponse{Text: "Hello world"})
	}))
	defer srv.Close()

	// Use a dedicated temp directory so parallel tests don't cause false positives.
	scopedTmp := t.TempDir()
	t.Setenv("TMPDIR", scopedTmp)

	before, err := listDirEntries(scopedTmp)
	if err != nil {
		t.Fatalf("list temp dir before: %v", err)
	}

	_, err = Transcribe(srv.URL, "audio/webm", make([]byte, 2048), nil)
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}

	after, err := listDirEntries(scopedTmp)
	if err != nil {
		t.Fatalf("list temp dir after: %v", err)
	}

	newFiles := diffEntries(before, after)
	for _, f := range newFiles {
		t.Errorf("Transcribe created unexpected temp file: %s", f)
	}
}

func listDirEntries(dir string) (map[string]struct{}, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	m := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		m[filepath.Join(dir, e.Name())] = struct{}{}
	}
	return m, nil
}

func diffEntries(before, after map[string]struct{}) []string {
	var added []string
	for k := range after {
		if _, ok := before[k]; !ok {
			added = append(added, k)
		}
	}
	return added
}
