package web

import (
	"context"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/cerebras"
)

const cerebrasTurnSystemPrompt = `You are Tabura's smart-fast middle-tier assistant.
Return a compact JSON object with:
- text: the user-facing answer
- confidence: one of high, medium, low
Do not include markdown fences.`

type cerebrasTurnResult struct {
	text       string
	confidence string
	err        error
	latency    time.Duration
}

func (r cerebrasTurnResult) canClaim() bool {
	return strings.TrimSpace(r.text) != "" && r.confidence == "high"
}

func (r cerebrasTurnResult) canFallback() bool {
	return strings.TrimSpace(r.text) != ""
}

func (a *App) cerebrasModelLabel() string {
	if a == nil || a.cerebrasClient == nil {
		return cerebras.DefaultModel
	}
	if model := strings.TrimSpace(a.cerebrasClient.Model); model != "" {
		return model
	}
	return cerebras.DefaultModel
}

func buildCerebrasTurnMessages(prompt string) []cerebras.Message {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return nil
	}
	return []cerebras.Message{
		{Role: "system", Content: cerebrasTurnSystemPrompt},
		{Role: "user", Content: trimmed},
	}
}

func (a *App) runCerebrasTurn(ctx context.Context, prompt string) cerebrasTurnResult {
	if a == nil || a.cerebrasClient == nil {
		return cerebrasTurnResult{}
	}
	resp, err := a.cerebrasClient.Complete(ctx, cerebras.CompletionRequest{
		Messages:    buildCerebrasTurnMessages(prompt),
		MaxTokens:   512,
		Temperature: 0.1,
	})
	if err != nil {
		return cerebrasTurnResult{err: err}
	}
	return cerebrasTurnResult{
		text:       strings.TrimSpace(resp.Text),
		confidence: strings.TrimSpace(resp.Confidence),
		latency:    resp.Latency,
	}
}
