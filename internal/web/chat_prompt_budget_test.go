package web

import (
	"strings"
	"testing"
)

// TestSystemPromptWordBudget guards against prompt drift. The voice/history
// system prompt should stay compact; tool docs live in the tools parameter
// and project rules live on-demand via MCP, not inlined here.
func TestSystemPromptWordBudget(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		limit  int
	}{
		{"defaultVoiceHistoryPrompt", defaultVoiceHistoryPrompt, 140},
		{"defaultVoiceTurnPrompt", defaultVoiceTurnPrompt, 40},
	}
	for _, tc := range cases {
		words := len(strings.Fields(tc.prompt))
		if words > tc.limit {
			t.Fatalf("%s word count = %d, want <= %d. prompt:\n%s", tc.name, words, tc.limit, tc.prompt)
		}
	}
}
