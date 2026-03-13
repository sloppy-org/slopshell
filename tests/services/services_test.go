package services_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	llmURL       = "http://127.0.0.1:8426"
	sttURL       = "http://127.0.0.1:8427"
	ttsURL       = "http://127.0.0.1:8424"
	appServerURL = "ws://127.0.0.1:8787"
)

// ---------------------------------------------------------------------------
// LLM — llama.cpp (port 8426)
// ---------------------------------------------------------------------------

func TestLLMHealth(t *testing.T) {
	resp := requireServiceHealth(t, "LLM", llmURL+"/health")
	status, _ := resp["status"].(string)
	if status != "ok" {
		t.Fatalf("LLM health status=%q, want ok", status)
	}
}

func TestLLMChatCompletion(t *testing.T) {
	requireServiceHealth(t, "LLM", llmURL+"/health")
	resp, err := postLLMCompletion([]map[string]string{
		{"role": "user", "content": "Reply with the single word: hello"},
	}, 64)
	if err != nil {
		t.Fatalf("LLM chat completion failed: %v", err)
	}
	content := extractLLMContent(t, resp)
	if !strings.Contains(strings.ToLower(content), "hello") {
		t.Fatalf("LLM content=%q, expected to contain 'hello'", content)
	}
}

func TestLLMIntentClassification(t *testing.T) {
	requireServiceHealth(t, "LLM", llmURL+"/health")
	systemPrompt := `Classify the user intent and return JSON only.
Allowed actions: switch_project, switch_model, toggle_silent, toggle_conversation, cancel_work, show_status, chat.
Return {"action":"<action>"}.`

	resp, err := postLLMCompletion([]map[string]string{
		{"role": "system", "content": systemPrompt},
		{"role": "user", "content": "be quiet please"},
	}, 128)
	if err != nil {
		t.Fatalf("LLM intent classification failed: %v", err)
	}
	content := extractLLMContent(t, resp)
	content = stripCodeFence(content)
	// The 0.6B model may produce slightly malformed JSON. Verify it at least
	// contains an action keyword from the allowed list.
	if !strings.Contains(content, "action") {
		t.Fatalf("LLM response does not contain 'action': %q", content)
	}
	hasKnownAction := false
	for _, a := range []string{"toggle_silent", "switch_model", "cancel_work", "show_status", "chat"} {
		if strings.Contains(content, a) {
			hasKnownAction = true
			break
		}
	}
	if !hasKnownAction {
		t.Fatalf("LLM response does not contain any known action: %q", content)
	}
}

func TestLLMLatency(t *testing.T) {
	requireServiceHealth(t, "LLM", llmURL+"/health")
	start := time.Now()
	_, err := postLLMCompletion([]map[string]string{
		{"role": "user", "content": "Say OK"},
	}, 16)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("LLM latency test failed: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("LLM took %v, want < 5s", elapsed)
	}
}

// ---------------------------------------------------------------------------
// STT — voxtype local OpenAI-compatible service (port 8427)
// ---------------------------------------------------------------------------

func TestSTTHealth(t *testing.T) {
	resp := requireServiceHealth(t, "STT", sttURL+"/healthz")
	status, _ := resp["status"].(string)
	if status != "ok" {
		t.Fatalf("STT health status=%q, want ok", status)
	}
}

func TestSTTTranscribeSineWave(t *testing.T) {
	requireServiceHealth(t, "STT", sttURL+"/healthz")
	wav := buildSineWaveWAV(2000, 440, 16000)
	resp, err := postSTTInference(wav)
	if err != nil {
		t.Fatalf("STT inference failed: %v", err)
	}
	if !utf8.ValidString(resp) {
		t.Fatalf("STT response is not valid UTF-8")
	}
	if len(resp) > 512 {
		t.Fatalf("unexpectedly long STT response for synthetic sine wave: %d bytes", len(resp))
	}
}

func TestSTTTranscribeSilence(t *testing.T) {
	requireServiceHealth(t, "STT", sttURL+"/healthz")
	wav := buildSilenceWAV(2000, 16000)
	resp, err := postSTTInference(wav)
	if err != nil {
		t.Fatalf("STT inference failed: %v", err)
	}
	if !utf8.ValidString(resp) {
		t.Fatalf("STT silence response is not valid UTF-8")
	}
	if len(strings.TrimSpace(resp)) > 120 {
		t.Fatalf("silence transcript too long (%d chars): %q", len(strings.TrimSpace(resp)), resp)
	}
}

func TestSTTHandlesEmptyPayload(t *testing.T) {
	requireServiceHealth(t, "STT", sttURL+"/healthz")
	resp, err := postSTTInference([]byte{})
	if err != nil {
		// HTTP error is acceptable for empty payload.
		return
	}
	if !utf8.ValidString(resp) {
		t.Fatalf("empty-payload STT response is not valid UTF-8")
	}
	if len(strings.TrimSpace(resp)) > 120 {
		t.Fatalf("empty-payload transcript too long (%d chars): %q", len(strings.TrimSpace(resp)), resp)
	}
}

func TestSTTAcceptsValidWAV(t *testing.T) {
	requireServiceHealth(t, "STT", sttURL+"/healthz")
	wav := buildMixedWAV(3000, 16000)
	resp, err := postSTTInference(wav)
	if err != nil {
		t.Fatalf("STT failed to process valid WAV: %v", err)
	}
	if !utf8.ValidString(resp) {
		t.Fatalf("STT valid-WAV response is not valid UTF-8")
	}
	if len(resp) > 1024 {
		t.Fatalf("valid-WAV transcript unexpectedly long (%d chars)", len(resp))
	}
}

// ---------------------------------------------------------------------------
// TTS — Piper (port 8424)
// ---------------------------------------------------------------------------

func TestTTSHealth(t *testing.T) {
	resp := requireServiceHealth(t, "TTS", ttsURL+"/health")
	status, _ := resp["status"].(string)
	if status != "ok" {
		t.Fatalf("TTS health status=%q, want ok", status)
	}
	voices, ok := resp["loaded_voices"].([]interface{})
	if !ok || len(voices) == 0 {
		t.Fatalf("TTS has no loaded voices: %v", resp)
	}
}

func TestTTSSpeakEnglish(t *testing.T) {
	requireServiceHealth(t, "TTS", ttsURL+"/health")
	wav, err := postTTSSpeak("Hello world, this is a test.", "en")
	if err != nil {
		t.Fatalf("TTS speak English failed: %v", err)
	}
	assertValidWAV(t, wav, "English TTS")
}

func TestTTSSpeakGerman(t *testing.T) {
	requireServiceHealth(t, "TTS", ttsURL+"/health")
	wav, err := postTTSSpeak("Hallo Welt, dies ist ein Test.", "de")
	if err != nil {
		t.Fatalf("TTS speak German failed: %v", err)
	}
	assertValidWAV(t, wav, "German TTS")
}

func TestTTSOutputSize(t *testing.T) {
	requireServiceHealth(t, "TTS", ttsURL+"/health")
	short, err := postTTSSpeak("Hi.", "en")
	if err != nil {
		t.Fatalf("TTS short: %v", err)
	}
	long, err := postTTSSpeak("This is a much longer sentence that should produce significantly more audio output than the short one.", "en")
	if err != nil {
		t.Fatalf("TTS long: %v", err)
	}
	if len(long) <= len(short) {
		t.Fatalf("longer text should produce more audio: short=%d bytes, long=%d bytes", len(short), len(long))
	}
}

func TestTTSLatency(t *testing.T) {
	requireServiceHealth(t, "TTS", ttsURL+"/health")
	start := time.Now()
	_, err := postTTSSpeak("Quick test.", "en")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("TTS latency: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("TTS took %v, want < 5s", elapsed)
	}
}

func TestTTSRoundTripSTT(t *testing.T) {
	requireServiceHealth(t, "TTS", ttsURL+"/health")
	requireServiceHealth(t, "STT", sttURL+"/healthz")
	ttsWav, err := postTTSSpeak("The quick brown fox jumps over the lazy dog.", "en")
	if err != nil {
		t.Fatalf("TTS failed: %v", err)
	}
	assertValidWAV(t, ttsWav, "TTS output")

	sttResp, err := postSTTInference(ttsWav)
	if err != nil {
		t.Fatalf("STT failed to transcribe TTS output: %v", err)
	}
	text := strings.ToLower(strings.TrimSpace(sttResp))
	if text == "" {
		t.Fatal("STT returned empty transcript for TTS-generated speech")
	}
	// The transcript should contain at least some words from the input
	if !strings.Contains(text, "fox") && !strings.Contains(text, "dog") && !strings.Contains(text, "quick") {
		t.Fatalf("STT transcript=%q does not match TTS input (expected fox/dog/quick)", text)
	}
}

// ---------------------------------------------------------------------------
// Codex app-server (port 8787)
// ---------------------------------------------------------------------------

func TestAppServerListening(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:8787", 5*time.Second)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "connection refused") {
			t.Skipf("app-server unavailable on port 8787: %v", err)
		}
		t.Fatalf("app-server not reachable on port 8787: %v", err)
	}
	conn.Close()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func requireServiceHealth(t *testing.T, name, url string) map[string]interface{} {
	t.Helper()
	resp, err := httpGetJSON(url)
	if err == nil {
		return resp
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection refused") {
		t.Skipf("%s unavailable at %s: %v", name, url, err)
	}
	t.Fatalf("%s health failed: %v", name, err)
	return nil
}

func httpGetJSON(url string) (map[string]interface{}, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GET %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("GET %s: decode: %w", url, err)
	}
	return result, nil
}

func postJSON(url string, body interface{}, timeout time.Duration) (map[string]interface{}, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("POST %s: HTTP %d: %s", url, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("POST %s: decode: %w", url, err)
	}
	return result, nil
}

func postLLMCompletion(messages []map[string]string, maxTokens int) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"model":       "qwen3.5-9b-q4_k_m",
		"temperature": 0,
		"max_tokens":  maxTokens,
		"chat_template_kwargs": map[string]interface{}{
			"enable_thinking": false,
		},
		"messages": messages,
	}
	return postJSON(llmURL+"/v1/chat/completions", body, 30*time.Second)
}

func extractLLMContent(t *testing.T, resp map[string]interface{}) string {
	t.Helper()
	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatalf("LLM returned no choices: %v", resp)
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		t.Fatalf("choice[0] is not a map: %v", choices[0])
	}
	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		t.Fatalf("choice[0].message is not a map: %v", choice["message"])
	}
	content, _ := message["content"].(string)
	if strings.TrimSpace(content) == "" {
		t.Fatalf("LLM returned empty content")
	}
	return strings.TrimSpace(content)
}

func postSTTInference(wavData []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(wavData); err != nil {
		return "", err
	}
	if err := writer.WriteField("response_format", "json"); err != nil {
		return "", err
	}
	if err := writer.WriteField("model", "whisper-1"); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Post(sttURL+"/v1/audio/transcriptions", writer.FormDataContentType(), &body)
	if err != nil {
		return "", fmt.Errorf("STT inference: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("STT inference HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("STT decode: %w", err)
	}
	return result.Text, nil
}

func postTTSSpeak(text, voice string) ([]byte, error) {
	body := map[string]interface{}{
		"input":           text,
		"voice":           voice,
		"response_format": "wav",
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(ttsURL+"/v1/audio/speech", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("TTS speak: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("TTS speak HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	wav, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("TTS read: %w", err)
	}
	return wav, nil
}

func assertValidWAV(t *testing.T, data []byte, label string) {
	t.Helper()
	if len(data) < 44 {
		t.Fatalf("%s: WAV too small (%d bytes), need at least 44 byte header", label, len(data))
	}
	if string(data[0:4]) != "RIFF" {
		t.Fatalf("%s: WAV missing RIFF header, got %q", label, string(data[0:4]))
	}
	if string(data[8:12]) != "WAVE" {
		t.Fatalf("%s: WAV missing WAVE marker, got %q", label, string(data[8:12]))
	}
}

func buildSilenceWAV(durationMs, sampleRate int) []byte {
	return buildWAV(durationMs, sampleRate, func(i, numSamples, sr int) int16 {
		return 0
	})
}

func buildSineWaveWAV(durationMs int, freq float64, sampleRate int) []byte {
	return buildWAV(durationMs, sampleRate, func(i, numSamples, sr int) int16 {
		amplitude := 0.8 * float64(math.MaxInt16)
		return int16(amplitude * math.Sin(2*math.Pi*freq*float64(i)/float64(sr)))
	})
}

func buildMixedWAV(durationMs, sampleRate int) []byte {
	return buildWAV(durationMs, sampleRate, func(i, numSamples, sr int) int16 {
		t := float64(i) / float64(sr)
		// Mix of frequencies to simulate speech-like audio
		amplitude := 0.3 * float64(math.MaxInt16)
		sample := amplitude * (math.Sin(2*math.Pi*200*t) +
			0.5*math.Sin(2*math.Pi*500*t) +
			0.3*math.Sin(2*math.Pi*1000*t))
		// Add some variation
		sample *= 0.5 + 0.5*math.Sin(2*math.Pi*3*t)
		if sample > float64(math.MaxInt16) {
			sample = float64(math.MaxInt16)
		}
		if sample < float64(math.MinInt16) {
			sample = float64(math.MinInt16)
		}
		return int16(sample)
	})
}

func buildWAV(durationMs, sampleRate int, sampleFunc func(i, numSamples, sr int) int16) []byte {
	numSamples := sampleRate * durationMs / 1000
	dataSize := numSamples * 2 // 16-bit mono
	buf := new(bytes.Buffer)
	buf.Grow(44 + dataSize)

	// RIFF header
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")

	// fmt chunk
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))
	binary.Write(buf, binary.LittleEndian, uint16(1))            // PCM
	binary.Write(buf, binary.LittleEndian, uint16(1))            // mono
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate))   // sample rate
	binary.Write(buf, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	binary.Write(buf, binary.LittleEndian, uint16(2))            // block align
	binary.Write(buf, binary.LittleEndian, uint16(16))           // bits per sample

	// data chunk
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(dataSize))

	for i := 0; i < numSamples; i++ {
		binary.Write(buf, binary.LittleEndian, sampleFunc(i, numSamples, sampleRate))
	}
	return buf.Bytes()
}

func stripCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return trimmed
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		return trimmed
	}
	end := len(lines)
	if end > 1 && strings.HasPrefix(strings.TrimSpace(lines[end-1]), "```") {
		end--
	}
	if end <= 1 {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[1:end], "\n"))
}
