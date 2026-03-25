package web

import (
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

func localAssistantToolUserPrompt(req *assistantTurnRequest, prompt string) string {
	if req != nil {
		text := strings.TrimSpace(normalizeLocalAssistantAddress(req.userText))
		if text == "" {
			text = strings.TrimSpace(normalizeLocalAssistantAddress(latestUserMessage(req.messages)))
		}
		if text != "" && !localAssistantNeedsFullPromptContext(text) {
			return text
		}
		if history := buildLocalAssistantToolHistoryPrompt(req.messages); history != "" {
			return history
		}
	}
	return strings.TrimSpace(prompt)
}

func buildLocalAssistantToolHistoryPrompt(messages []store.ChatMessage) string {
	recent := collectLeanLocalAssistantHistory(messages)
	if len(recent) == 0 {
		return ""
	}
	var b strings.Builder
	for _, msg := range recent {
		content := strings.TrimSpace(msg.ContentPlain)
		if content == "" {
			content = strings.TrimSpace(msg.ContentMarkdown)
		}
		if content == "" || strings.EqualFold(strings.TrimSpace(msg.Role), "system") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			content = normalizeLocalAssistantAddress(content)
		}
		b.WriteString(strings.ToUpper(strings.TrimSpace(msg.Role)))
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func localAssistantNeedsFullPromptContext(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return true
	}
	words := strings.Fields(lower)
	if lower == "on the canvas" ||
		lower == "in the canvas" ||
		lower == "show it" ||
		lower == "show them" ||
		lower == "draw it" ||
		lower == "open it" ||
		lower == "open that" ||
		lower == "auf der canvas" ||
		lower == "auf dem canvas" ||
		lower == "zeig es" ||
		lower == "zeige es" ||
		lower == "zeichne es" ||
		lower == "öffne es" ||
		lower == "oeffne es" {
		return true
	}
	if len(words) > 4 {
		return false
	}
	return containsAnyLocalAssistantKeyword(lower,
		" it", " them", " this", " that", " there", " here",
		" es", " sie", " dort", " hier",
	)
}

func localAssistantInitialToolMaxTokens(family localAssistantToolFamily) int {
	if family == localAssistantToolFamilyCanvas {
		return assistantLLMDirectMaxTokens
	}
	return assistantLLMToolPlanMaxTokens
}
