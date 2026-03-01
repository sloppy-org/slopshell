package stt

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestTranscribe(t *testing.T) {
	t.Run("successful transcription", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/v1/audio/transcriptions" {
				t.Errorf("expected /v1/audio/transcriptions, got %s", r.URL.Path)
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
			if r.FormValue("model") != "whisper-1" {
				t.Errorf("expected model=whisper-1, got %q", r.FormValue("model"))
			}
			_, fh, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("form file: %v", err)
			}
			if fh.Filename != "audio.webm" {
				t.Errorf("expected audio.webm, got %q", fh.Filename)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sttResponse{Text: "Hello world"})
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
			json.NewEncoder(w).Encode(sttResponse{Text: "Some speech"})
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
			json.NewEncoder(w).Encode(sttResponse{Text: "Thank you."})
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
			json.NewEncoder(w).Encode(sttResponse{Text: "um"})
		}))
		defer srv.Close()

		_, err := Transcribe(srv.URL, "audio/webm", []byte("fake"), nil)
		if err != ErrLikelyNoise {
			t.Errorf("expected ErrLikelyNoise, got %v", err)
		}
	})

	t.Run("language prompt echo rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sttResponse{Text: "The spoken language is one of: en, de."})
		}))
		defer srv.Close()

		_, err := Transcribe(srv.URL, "audio/webm", []byte("fake"), nil)
		if err != ErrLikelyHallucination {
			t.Errorf("expected ErrLikelyHallucination, got %v", err)
		}
	})

	t.Run("empty transcript", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(sttResponse{Text: ""})
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
			json.NewEncoder(w).Encode(sttResponse{Text: "The stelorators use rungicata methods"})
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
		"The spoken language is one of: en, de.",
		"the spoken language is one of: en, fr, de",
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

func TestTranscribeWithOptionsConstrainedLanguages(t *testing.T) {
	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.FormValue("language") != "auto" {
			t.Fatalf("language=%q, want auto", r.FormValue("language"))
		}
		if r.FormValue("response_format") != "json" {
			t.Fatalf("response_format=%q, want json", r.FormValue("response_format"))
		}
		if r.FormValue("model") != "whisper-1" {
			t.Fatalf("model=%q, want whisper-1", r.FormValue("model"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text":     "hallo welt",
			"language": "de",
		})
	}))
	defer srv.Close()

	text, err := TranscribeWithOptions(srv.URL, "audio/webm", []byte("fake-audio-data"), nil, TranscribeOptions{
		AllowedLanguages: []string{"en", "de"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hallo welt" {
		t.Fatalf("text=%q, want hallo welt", text)
	}
	if got := atomic.LoadInt32(&requestCount); got != 1 {
		t.Fatalf("request count=%d, want 1", got)
	}
}

func TestTranscribeWithOptionsUsesProvidedOpenAIEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Fatalf("path=%q, want /v1/audio/transcriptions", r.URL.Path)
		}
		if r.FormValue("language") != "de" {
			t.Fatalf("language=%q, want de", r.FormValue("language"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "hallo welt"})
	}))
	defer srv.Close()

	text, err := TranscribeWithOptions(srv.URL+"/v1/audio/transcriptions", "audio/webm", []byte("fake-audio-data"), nil, TranscribeOptions{
		AllowedLanguages: []string{"de"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hallo welt" {
		t.Fatalf("text=%q, want hallo welt", text)
	}
}

func TestTranscribeWithOptionsTranslateUsesTranslationsEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.URL.Path != "/v1/audio/translations" {
			t.Fatalf("path=%q, want /v1/audio/translations", r.URL.Path)
		}
		if r.FormValue("translate") != "true" {
			t.Fatalf("translate=%q, want true", r.FormValue("translate"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "hello world"})
	}))
	defer srv.Close()

	text, err := TranscribeWithOptions(srv.URL, "audio/webm", []byte("fake-audio-data"), nil, TranscribeOptions{
		AllowedLanguages: []string{"de"},
		Translate:        true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello world" {
		t.Fatalf("text=%q, want hello world", text)
	}
}

func TestIsPromptEcho(t *testing.T) {
	if !IsPromptEcho("The spoken language is one of: en, de.", "The spoken language is one of: en, de.") {
		t.Fatal("expected prompt echo to be detected")
	}
	if IsPromptEcho("hallo welt", "The spoken language is one of: en, de.") {
		t.Fatal("did not expect prompt echo for normal transcript")
	}
	if IsPromptEcho("The spoken language is one of: en, de.", "") {
		t.Fatal("empty prompt should not trigger IsPromptEcho")
	}
}

func TestTranscribeWithOptionsSingleLanguageAndPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.FormValue("language") != "de" {
			t.Fatalf("language=%q, want de", r.FormValue("language"))
		}
		if r.FormValue("prompt") != "Physics terms: stellarator, adiabatic." {
			t.Fatalf("prompt=%q", r.FormValue("prompt"))
		}
		if r.FormValue("response_format") != "json" {
			t.Fatalf("response_format=%q, want json", r.FormValue("response_format"))
		}
		if r.FormValue("model") != "whisper-x" {
			t.Fatalf("model=%q, want whisper-x", r.FormValue("model"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sttResponse{Text: "hallo welt"})
	}))
	defer srv.Close()
	t.Setenv("TABURA_STT_MODEL_NAME", "whisper-x")

	text, err := TranscribeWithOptions(srv.URL, "audio/webm", []byte("fake-audio-data"), nil, TranscribeOptions{
		AllowedLanguages: []string{"de"},
		InitialPrompt:    "Physics terms: stellarator, adiabatic.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hallo welt" {
		t.Fatalf("text=%q, want hallo welt", text)
	}
}

func TestTranscribeWithOptionsPreVADSkipsSilentWAV(t *testing.T) {
	var requestCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sttResponse{Text: "should not be called"})
	}))
	defer srv.Close()

	silent := buildTestPCM16WAV(16000, 1, 1500, 0)
	_, err := TranscribeWithOptions(srv.URL, "audio/wav", silent, nil, TranscribeOptions{
		PreVAD: PreVADConfig{
			Enabled:     true,
			ThresholdDB: -55,
			MinSpeechMS: 200,
			FrameMS:     20,
		},
	})
	if err != ErrLikelyNoise {
		t.Fatalf("err=%v, want ErrLikelyNoise", err)
	}
	if got := atomic.LoadInt32(&requestCount); got != 0 {
		t.Fatalf("request count=%d, want 0", got)
	}
}

func TestDetectSpeechPCM16WAV(t *testing.T) {
	silence := buildTestPCM16WAV(16000, 1, 1200, 0)
	speech := buildTestPCM16WAV(16000, 1, 1200, 0.25)

	detected, err := detectSpeechPCM16WAV(silence, PreVADConfig{
		Enabled:     true,
		ThresholdDB: -55,
		MinSpeechMS: 120,
		FrameMS:     20,
	})
	if err != nil {
		t.Fatalf("silence detection error: %v", err)
	}
	if detected {
		t.Fatal("silence detected as speech")
	}

	detected, err = detectSpeechPCM16WAV(speech, PreVADConfig{
		Enabled:     true,
		ThresholdDB: -55,
		MinSpeechMS: 120,
		FrameMS:     20,
	})
	if err != nil {
		t.Fatalf("speech detection error: %v", err)
	}
	if !detected {
		t.Fatal("speech not detected")
	}
}

func buildTestPCM16WAV(sampleRate, channels, durationMS int, amplitude float64) []byte {
	if channels <= 0 {
		channels = 1
	}
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if durationMS <= 0 {
		durationMS = 500
	}
	if amplitude < 0 {
		amplitude = 0
	}
	if amplitude > 1 {
		amplitude = 1
	}
	totalSamples := sampleRate * durationMS / 1000
	dataSize := totalSamples * channels * 2
	out := make([]byte, 44+dataSize)

	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+dataSize))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(sampleRate*channels*2))
	binary.LittleEndian.PutUint16(out[32:34], uint16(channels*2))
	binary.LittleEndian.PutUint16(out[34:36], 16)
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(dataSize))

	pos := 44
	for i := 0; i < totalSamples; i++ {
		t := float64(i) / float64(sampleRate)
		sample := int16(amplitude * 32767.0 * math.Sin(2*math.Pi*220*t))
		for c := 0; c < channels; c++ {
			binary.LittleEndian.PutUint16(out[pos:pos+2], uint16(sample))
			pos += 2
		}
	}
	return out
}

// Privacy: docs/meeting-notes-privacy.md

func TestPrivacyTranscribeNoTempFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sttResponse{Text: "Hello world"})
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
