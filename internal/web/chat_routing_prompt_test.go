package web

import (
	"strings"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

func TestBuildPromptFromHistoryOmitsLegacyAsyncToolSection(t *testing.T) {
	messages := []store.ChatMessage{{Role: "user", ContentPlain: "hello"}}
	prompt := buildPromptFromHistory("chat", messages, nil)
	if strings.Contains(prompt, "cancel-delegates") {
		t.Error("prompt should not include removed cancel-delegates endpoint")
	}
}

func TestBuildPromptFromHistoryKeepsOriginalUserText(t *testing.T) {
	messages := []store.ChatMessage{{Role: "user", ContentPlain: "let codex review the code"}}
	prompt := buildPromptFromHistory("chat", messages, nil)
	if !strings.Contains(prompt, "let codex review the code") {
		t.Error("prompt should include original user text")
	}
}

func TestBuildTurnPromptKeepsOriginalUserText(t *testing.T) {
	messages := []store.ChatMessage{{Role: "user", ContentPlain: "ask gpt about this"}}
	prompt := buildTurnPrompt(messages, nil)
	if !strings.Contains(prompt, "ask gpt about this") {
		t.Error("turn prompt should include original user text")
	}
}

func TestBuildTurnPromptChatOnlyContract(t *testing.T) {
	messages := []store.ChatMessage{{Role: "user", ContentPlain: "explain this function"}}
	prompt := buildTurnPrompt(messages, nil)
	if !strings.Contains(prompt, "Chat-first") {
		t.Error("turn prompt should define chat-first voice mode")
	}
	if !strings.Contains(prompt, `emit :::file{path="..."} only when the user explicitly asks`) {
		t.Error("turn prompt should limit file blocks to explicit canvas requests")
	}
	if !strings.Contains(prompt, "explain this function") {
		t.Error("original message should be present")
	}
}
