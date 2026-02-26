package web

import (
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestDetectDelegationHint(t *testing.T) {
	tests := []struct {
		input    string
		detected bool
		model    string
	}{
		{"let codex analyze this code", true, "codex"},
		{"ask gpt about performance", true, "gpt"},
		{"use spark for quick check", true, "spark"},
		{"Let Codex do it", true, "codex"},
		{"ASK GPT: summarize", true, "gpt"},
		{"use the big model to review", true, "gpt"},
		{"explain this function", false, ""},
		{"hello", false, ""},
		{"", false, ""},
		{"codex should handle this", false, ""},
	}
	for _, tc := range tests {
		hint := detectDelegationHint(tc.input)
		if hint.Detected != tc.detected {
			t.Errorf("detectDelegationHint(%q).Detected = %v, want %v", tc.input, hint.Detected, tc.detected)
		}
		if hint.Model != tc.model {
			t.Errorf("detectDelegationHint(%q).Model = %q, want %q", tc.input, hint.Model, tc.model)
		}
	}
}

func TestApplyDelegationHints(t *testing.T) {
	t.Run("hint detected", func(t *testing.T) {
		got := applyDelegationHints("let codex analyze this")
		if !strings.Contains(got, `[Delegation hint: user wants model="codex"]`) {
			t.Errorf("expected delegation hint prefix, got %q", got)
		}
		if !strings.Contains(got, "let codex analyze this") {
			t.Error("original text should be preserved")
		}
	})

	t.Run("no hint", func(t *testing.T) {
		input := "explain this function"
		got := applyDelegationHints(input)
		if got != input {
			t.Errorf("expected passthrough, got %q", got)
		}
	})
}

func TestBuildPromptFromHistoryContainsDelegationSection(t *testing.T) {
	messages := []store.ChatMessage{
		{Role: "user", ContentPlain: "hello"},
	}
	prompt := buildPromptFromHistory("chat", messages, nil)
	if !strings.Contains(prompt, "## Delegation") {
		t.Error("prompt should contain Delegation section")
	}
	if !strings.Contains(prompt, "delegate_to_model") {
		t.Error("prompt should mention delegate_to_model tool")
	}
	if !strings.Contains(prompt, "edit files directly on disk") {
		t.Error("prompt should explain delegates edit files directly")
	}
	if !strings.Contains(prompt, "Do NOT parse or apply patches") {
		t.Error("prompt should tell Spark not to apply patches")
	}
	if !strings.Contains(prompt, "files_changed") {
		t.Error("prompt should mention files_changed in delegate result")
	}
}

func TestBuildPromptFromHistoryAppliesHints(t *testing.T) {
	messages := []store.ChatMessage{
		{Role: "user", ContentPlain: "let codex review the code"},
	}
	prompt := buildPromptFromHistory("chat", messages, nil)
	if !strings.Contains(prompt, `[Delegation hint: user wants model="codex"]`) {
		t.Error("prompt should contain delegation hint for user message")
	}
}

func TestBuildTurnPromptAppliesHints(t *testing.T) {
	messages := []store.ChatMessage{
		{Role: "user", ContentPlain: "ask gpt about this"},
	}
	prompt := buildTurnPrompt(messages, nil)
	if !strings.Contains(prompt, `[Delegation hint: user wants model="gpt"]`) {
		t.Error("turn prompt should contain delegation hint")
	}
}

func TestBuildTurnPromptNoHintPassthrough(t *testing.T) {
	messages := []store.ChatMessage{
		{Role: "user", ContentPlain: "explain this function"},
	}
	prompt := buildTurnPrompt(messages, nil)
	if strings.Contains(prompt, "[Delegation hint") {
		t.Error("should not inject hint for non-delegation message")
	}
	if !strings.Contains(prompt, "Voice mode is chat-only") {
		t.Error("turn prompt should define chat-only voice mode")
	}
	if !strings.Contains(prompt, "Do not emit :::file blocks.") {
		t.Error("turn prompt should explicitly disallow file blocks")
	}
	if !strings.Contains(prompt, "show/open an existing file") {
		t.Error("turn prompt should define existing-file canvas behavior")
	}
	if !strings.Contains(prompt, "explain this function") {
		t.Error("original message should be present")
	}
}
