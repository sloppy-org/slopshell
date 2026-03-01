package ptt

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/krystophny/tabura/internal/stt"
)

func TestWrapWAV(t *testing.T) {
	t.Parallel()
	pcm := make([]byte, 32000) // 1 second of 16kHz mono 16-bit silence
	wav := WrapWAV(pcm)

	if len(wav) != 44+len(pcm) {
		t.Fatalf("expected WAV length %d, got %d", 44+len(pcm), len(wav))
	}
	if string(wav[0:4]) != "RIFF" {
		t.Fatal("missing RIFF header")
	}
	if string(wav[8:12]) != "WAVE" {
		t.Fatal("missing WAVE format")
	}
	if string(wav[12:16]) != "fmt " {
		t.Fatal("missing fmt chunk")
	}
	if string(wav[36:40]) != "data" {
		t.Fatal("missing data chunk")
	}

	audioFormat := binary.LittleEndian.Uint16(wav[20:22])
	if audioFormat != 1 {
		t.Fatalf("expected PCM format (1), got %d", audioFormat)
	}
	channels := binary.LittleEndian.Uint16(wav[22:24])
	if channels != 1 {
		t.Fatalf("expected 1 channel, got %d", channels)
	}
	rate := binary.LittleEndian.Uint32(wav[24:28])
	if rate != 16000 {
		t.Fatalf("expected sample rate 16000, got %d", rate)
	}
	bits := binary.LittleEndian.Uint16(wav[34:36])
	if bits != 16 {
		t.Fatalf("expected 16 bits per sample, got %d", bits)
	}
	dataSize := binary.LittleEndian.Uint32(wav[40:44])
	if dataSize != uint32(len(pcm)) {
		t.Fatalf("expected data size %d, got %d", len(pcm), dataSize)
	}
}

func TestWrapWAVEmpty(t *testing.T) {
	t.Parallel()
	wav := WrapWAV(nil)
	if len(wav) != 44 {
		t.Fatalf("expected 44-byte WAV header for empty PCM, got %d", len(wav))
	}
}

func TestTranscribeAudioWithMockServer(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file", http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		if len(data) == 0 {
			http.Error(w, "empty file", http.StatusBadRequest)
			return
		}
		format := r.FormValue("response_format")
		if format != "json" {
			http.Error(w, "only json format supported", http.StatusBadRequest)
			return
		}
		if r.FormValue("model") != "whisper-1" {
			http.Error(w, "missing model", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "hello world from the microphone"})
	}))
	defer srv.Close()

	wav := WrapWAV(make([]byte, 16000))
	text, err := TranscribeAudio(srv.URL, wav, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello world from the microphone" {
		t.Fatalf("unexpected text: %s", text)
	}
}

func TestTranscribeAudioAppliesReplacements(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "mating center dynamics"})
	}))
	defer srv.Close()

	replacements := []stt.Replacement{
		{From: "mating center", To: "guiding center"},
	}
	wav := WrapWAV(make([]byte, 16000))
	text, err := TranscribeAudio(srv.URL, wav, replacements)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "guiding center dynamics" {
		t.Fatalf("expected 'guiding center dynamics', got %q", text)
	}
}

func TestTranscribeAudioRejectsHallucination(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "Thank you."})
	}))
	defer srv.Close()

	wav := WrapWAV(make([]byte, 16000))
	_, err := TranscribeAudio(srv.URL, wav, nil)
	if err == nil {
		t.Fatal("expected hallucination rejection")
	}
	if !stt.IsRetryableNoSpeechError(err) {
		t.Fatalf("expected retryable no-speech error, got: %v", err)
	}
}

func TestTranscribeAudioRejectsEmpty(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": ""})
	}))
	defer srv.Close()

	wav := WrapWAV(make([]byte, 16000))
	_, err := TranscribeAudio(srv.URL, wav, nil)
	if err == nil {
		t.Fatal("expected empty transcript error")
	}
}

func TestTranscribeAudioHTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model loading", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	wav := WrapWAV(make([]byte, 16000))
	_, err := TranscribeAudio(srv.URL, wav, nil)
	if err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestFetchReplacementsFromMockAPI(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/stt/replacements" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"stelorators": "stellarators",
			"rungicata":   "Runge-Kutta",
		})
	}))
	defer srv.Close()

	reps := FetchReplacements(srv.URL)
	if len(reps) != 2 {
		t.Fatalf("expected 2 replacements, got %d", len(reps))
	}
	found := map[string]string{}
	for _, r := range reps {
		found[r.From] = r.To
	}
	if found["stelorators"] != "stellarators" {
		t.Fatal("missing stelorators replacement")
	}
	if found["rungicata"] != "Runge-Kutta" {
		t.Fatal("missing rungicata replacement")
	}
}

func TestFetchReplacementsEmptyURL(t *testing.T) {
	t.Parallel()
	reps := FetchReplacements("")
	if reps != nil {
		t.Fatal("expected nil for empty URL")
	}
}

func TestFetchReplacementsUnreachable(t *testing.T) {
	t.Parallel()
	reps := FetchReplacements("http://127.0.0.1:1")
	if reps != nil {
		t.Fatal("expected nil for unreachable server")
	}
}

func TestDefaultConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultConfig()
	if cfg.KeyCode != 183 {
		t.Fatalf("expected key code 183, got %d", cfg.KeyCode)
	}
	if cfg.WhisperURL != "http://127.0.0.1:8427" {
		t.Fatalf("unexpected whisper URL: %s", cfg.WhisperURL)
	}
	if cfg.WebAPIURL != "http://127.0.0.1:8420" {
		t.Fatalf("unexpected web API URL: %s", cfg.WebAPIURL)
	}
	if cfg.OutputMode != "type" {
		t.Fatalf("unexpected output mode: %s", cfg.OutputMode)
	}
}
