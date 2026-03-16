package mailtriage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIClassifierParsesStructuredJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error: %v", err)
		}
		if got := strings.TrimSpace(r.URL.Path); got != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", got)
		}
		if payload["model"] != "qwen3.5-9b" {
			t.Fatalf("model = %#v, want qwen3.5-9b", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"action\\\":\\\"archive\\\",\\\"archive_label\\\":\\\"simons24\\\",\\\"confidence\\\":0.96,\\\"reason\\\":\\\"project update\\\",\\\"signals\\\":[\\\"direct update\\\"]}\\n```\"}}]}"))
	}))
	defer server.Close()

	classifier := OpenAIClassifier{
		BaseURL: server.URL,
		Model:   "qwen3.5-9b",
	}
	decision, err := classifier.Classify(context.Background(), Message{
		ID:       "m1",
		Subject:  "Project update",
		Snippet:  "FYI",
		Body:     "Body",
		Provider: "exchange_ews",
	})
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if decision.Action != ActionArchive {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionArchive)
	}
	if decision.ArchiveLabel != "simons24" {
		t.Fatalf("ArchiveLabel = %q, want simons24", decision.ArchiveLabel)
	}
	if decision.Model != "qwen3.5-9b" {
		t.Fatalf("Model = %q, want qwen3.5-9b", decision.Model)
	}
}

func TestOpenAIClassifierParsesThinkingPreamble(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"</think>\\n\\n{\\\"action\\\":\\\"cc\\\",\\\"confidence\\\":0.81,\\\"reason\\\":\\\"newsletter\\\",\\\"signals\\\":[\\\"fyi\\\"]}\"}}]}"))
	}))
	defer server.Close()

	classifier := OpenAIClassifier{
		BaseURL: server.URL,
		Model:   "qwen3.5-9b",
	}
	decision, err := classifier.Classify(context.Background(), Message{ID: "m2", Subject: "FYI"})
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if decision.Action != ActionCC {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionCC)
	}
	if decision.Confidence != 0.81 {
		t.Fatalf("Confidence = %v, want 0.81", decision.Confidence)
	}
}

func TestOpenAIClassifierReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	classifier := OpenAIClassifier{BaseURL: server.URL}
	if _, err := classifier.Classify(context.Background(), Message{ID: "m1"}); err == nil {
		t.Fatal("Classify() error = nil, want non-nil")
	}
}

func TestBuildUserPromptIncludesFlagged(t *testing.T) {
	prompt := buildUserPrompt(Message{
		ID:        "m3",
		Subject:   "Important",
		IsRead:    true,
		IsFlagged: true,
	})
	if !strings.Contains(prompt, "Is flagged: true") {
		t.Fatalf("prompt missing flagged state: %q", prompt)
	}
}

func TestBuildUserPromptIncludesRecentManualExamples(t *testing.T) {
	prompt := buildUserPrompt(Message{
		ID:      "m4",
		Subject: "Suspicious invite",
		Examples: []Example{
			{
				Action:  "trash",
				Folder:  "Junk-E-Mail",
				Sender:  "spam@example.com",
				Subject: "Win a prize",
			},
		},
	})
	if !strings.Contains(prompt, "Recent reviewed examples from this mailbox:") {
		t.Fatalf("prompt missing examples header: %q", prompt)
	}
	if !strings.Contains(prompt, "action=trash; folder=Junk-E-Mail; from=spam@example.com; subject=Win a prize") {
		t.Fatalf("prompt missing example detail: %q", prompt)
	}
}
