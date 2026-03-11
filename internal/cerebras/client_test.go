package cerebras

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientCompleteSendsOpenAICompatibleRequest(t *testing.T) {
	var seenAuth string
	var seenPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		seenAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&seenPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": `{"text":"Cerebras answer.","confidence":"high"}`,
					},
				},
			},
			"usage": map[string]any{
				"total_tokens": 77,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-123", DefaultModel, DefaultReasoningEffort)
	client.HTTPClient = server.Client()

	resp, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "Return JSON."},
			{Role: "user", Content: "How does routing work?"},
		},
		MaxTokens:   321,
		Temperature: 0.1,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if seenAuth != "Bearer token-123" {
		t.Fatalf("Authorization = %q, want Bearer token-123", seenAuth)
	}
	if got := seenPayload["model"]; got != DefaultModel {
		t.Fatalf("model = %#v, want %q", got, DefaultModel)
	}
	if got := seenPayload["reasoning_effort"]; got != DefaultReasoningEffort {
		t.Fatalf("reasoning_effort = %#v, want %q", got, DefaultReasoningEffort)
	}
	if got := int(seenPayload["max_tokens"].(float64)); got != 321 {
		t.Fatalf("max_tokens = %d, want 321", got)
	}
	if resp.Text != "Cerebras answer." {
		t.Fatalf("Text = %q, want Cerebras answer.", resp.Text)
	}
	if resp.Confidence != "high" {
		t.Fatalf("Confidence = %q, want high", resp.Confidence)
	}
	if resp.TokensUsed != 77 {
		t.Fatalf("TokensUsed = %d, want 77", resp.TokensUsed)
	}
	if resp.Latency < 0 {
		t.Fatalf("Latency = %v, want >= 0", resp.Latency)
	}
}

func TestClientQuotaExhaustionResetsOnNextUTCDay(t *testing.T) {
	clock := time.Date(2026, 3, 11, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-123", DefaultModel, DefaultReasoningEffort)
	client.HTTPClient = server.Client()
	client.now = func() time.Time { return clock }

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if !errors.Is(err, ErrQuotaExhausted) {
		t.Fatalf("Complete() error = %v, want ErrQuotaExhausted", err)
	}
	if client.IsAvailable() {
		t.Fatal("IsAvailable() = true, want false after 429")
	}

	clock = clock.Add(13 * time.Hour)
	if !client.IsAvailable() {
		t.Fatal("IsAvailable() = false, want true on next UTC day")
	}
}

func TestClientBacksOffAfterServerError(t *testing.T) {
	clock := time.Date(2026, 3, 11, 9, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary failure", http.StatusBadGateway)
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-123", DefaultModel, DefaultReasoningEffort)
	client.HTTPClient = server.Client()
	client.now = func() time.Time { return clock }

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want backoff-triggering error")
	}
	if client.IsAvailable() {
		t.Fatal("IsAvailable() = true, want false during backoff")
	}

	clock = clock.Add(defaultBackoff + time.Second)
	if !client.IsAvailable() {
		t.Fatal("IsAvailable() = false, want true after backoff")
	}
}
