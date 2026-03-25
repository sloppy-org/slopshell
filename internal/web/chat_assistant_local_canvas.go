package web

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func localAssistantToolRequiredPrompt() string {
	return "A tool is required for this request. Do not describe a plan, promise an action, or ask for permission. Return only the exact JSON tool call now, then give a short final reply after the tool result."
}

func localAssistantCanvasContentRequiredPrompt() string {
	return "This request needs canvas content now. Reply with only the exact text that should appear on the canvas. Do not describe a plan, do not ask for permission, and do not return JSON."
}

func localAssistantCanvasDiagramRequiredPrompt() string {
	return "Reply with only a readable ASCII diagram for the canvas. Do not return a title alone. Use at least 8 non-empty lines or an equally rich boxed flowchart with multiple connected stages. Include connectors such as |, ->, or boxed steps, and label the main components or phases."
}

func buildLocalAssistantCanvasGenerationPrompt(userText string, promptContext string, reasoningHint string) string {
	lines := []string{
		"You are Tabura, the assistant in the current workspace.",
		"If the user says Tabura, Sloppy, or computer, they are addressing you, not asking about those words.",
		"The user wants new generated text to appear on the canvas.",
		"Reply with only the exact canvas text.",
		"Do not return JSON, tool calls, markdown fences, plans, apologies, or explanations.",
		"Do not ask for permission.",
		"Preserve the user's main subject words in the diagram labels when possible.",
		"Make the first non-empty line name the main subject from the request.",
	}
	if localAssistantCanvasNeedsStructuredDiagram(userText) {
		lines = append(lines,
			"This request needs a readable, information-rich ASCII diagram.",
			"Prefer 8-14 non-empty lines unless a denser boxed flowchart is clearly better.",
			"Include connectors such as ->, |, or boxed steps.",
			"Show the main stages, components, and at least one relationship, dependency, or flow between them.",
			"Do not collapse the diagram into a tiny glossary or two-column word list.",
			"Keep labels concrete and informative.",
		)
	}
	if localAssistantCanvasHasStructuredDiagramText(promptContext) {
		lines = append(lines,
			"The current canvas already contains a structured diagram. Keep the revised result as a structured multi-line diagram.",
			"Preserve the main subject and the key existing labels unless the user explicitly asks to replace them.",
			"When revising, improve clarity and detail rather than shrinking the diagram.",
		)
	}
	if strings.TrimSpace(reasoningHint) != "" {
		lines = append(lines, strings.TrimSpace(reasoningHint))
	}
	return strings.Join(lines, "\n")
}

func localAssistantLooksLikeCanvasPlanningText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "\n") || strings.Contains(lower, "->") || strings.Contains(lower, "[") || strings.Contains(lower, "|") {
		return false
	}
	return containsAnyLocalAssistantKeyword(lower,
		"i will", "i'll", "let me", "i can", "i cannot", "i am going to", "i need to", "we need to",
		"ich werde", "ich erstelle", "ich zeichne", "ich kann", "ich werde jetzt", "ich zeige", "ich öffne", "ich oeffne",
		"creating", "drawing", "showing", "opening", "erstelle", "zeichne", "zeige", "öffne", "oeffne",
	)
}

func localAssistantCanvasNeedsStructuredDiagram(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(normalizeLocalAssistantAddress(text)))
	if lower == "" {
		return false
	}
	return containsAnyLocalAssistantKeyword(lower,
		"flowchart", "diagram", "block diagram", "process map", "workflow", "pipeline", "state machine", "architecture", "schematic", "sketch", "chart", "draw", "draw ", "overview",
		"flussdiagramm", "diagramm", "blockdiagramm", "ablaufdiagramm", "prozess", "prozessablauf", "workflow", "zustandsdiagramm", "architektur", "schema", "schaubild", "skizze", "zeichne", "übersicht", "uebersicht",
	)
}

func localAssistantCanvasHasStructuredDiagramText(text string) bool {
	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines++
	}
	lower := strings.ToLower(text)
	hasConnectors := strings.Contains(lower, "->") || strings.Contains(lower, "|") || strings.Contains(lower, "[")
	if lines >= 4 && hasConnectors {
		return true
	}
	return (strings.Count(lower, "->") >= 2 || strings.Count(lower, "|") >= 3) && strings.Count(lower, "[") >= 3
}

func localAssistantCanvasRenderText(structured bool, text string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	if !structured && !localAssistantCanvasHasStructuredDiagramText(clean) {
		return clean
	}
	if strings.HasPrefix(clean, "```") {
		return clean
	}
	return "```text\n" + clean + "\n```"
}

func recoverLocalAssistantCanvasTextFromMalformedToolOutput(raw string) (string, bool) {
	clean := strings.TrimSpace(stripCodeFence(raw))
	lower := strings.ToLower(clean)
	if !strings.Contains(lower, "canvas_write_text") &&
		!strings.Contains(lower, "canvas_artifact_show") &&
		!strings.Contains(lower, "mcp__canvas_artifact_show") {
		return "", false
	}
	for _, field := range []string{"content", "markdown_or_text", "text", "body"} {
		value, ok := extractLocalAssistantJSONStringField(clean, field)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "" || localAssistantLooksLikeCanvasPlanningText(value) {
			continue
		}
		return value, true
	}
	return "", false
}

func localAssistantCanvasToolNameAllowed(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "canvas_write_text", "canvas_artifact_show", "mcp__canvas_artifact_show":
		return true
	default:
		return false
	}
}

func extractLocalAssistantJSONStringField(raw string, field string) (string, bool) {
	key := `"` + field + `"`
	start := strings.Index(raw, key)
	if start < 0 {
		return "", false
	}
	cursor := start + len(key)
	for cursor < len(raw) && (raw[cursor] == ' ' || raw[cursor] == '\n' || raw[cursor] == '\r' || raw[cursor] == '\t') {
		cursor++
	}
	if cursor >= len(raw) || raw[cursor] != ':' {
		return "", false
	}
	cursor++
	for cursor < len(raw) && (raw[cursor] == ' ' || raw[cursor] == '\n' || raw[cursor] == '\r' || raw[cursor] == '\t') {
		cursor++
	}
	if cursor >= len(raw) || raw[cursor] != '"' {
		return "", false
	}
	cursor++
	var builder strings.Builder
	escaped := false
	for cursor < len(raw) {
		ch := raw[cursor]
		cursor++
		if escaped {
			builder.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			builder.WriteByte(ch)
			escaped = true
			continue
		}
		if ch == '"' {
			decoded, err := strconv.Unquote(`"` + builder.String() + `"`)
			if err != nil {
				return "", false
			}
			return decoded, true
		}
		builder.WriteByte(ch)
	}
	return "", false
}

func stripLocalAssistantThinkingPreamble(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	clean := strings.TrimLeft(raw, " \t\r\n")
	if strings.HasPrefix(clean, "<think>") {
		if idx := strings.Index(clean, "</think>"); idx >= 0 {
			clean = clean[idx+len("</think>"):]
		}
		return strings.TrimLeft(clean, " \t\r\n")
	}
	if strings.HasPrefix(clean, "</think>") {
		clean = clean[len("</think>"):]
		return strings.TrimLeft(clean, " \t\r\n")
	}
	return raw
}

func localAssistantCanvasTextFromToolCalls(calls []localAssistantToolCall) (string, bool) {
	for _, call := range calls {
		if !localAssistantCanvasToolNameAllowed(call.Name) {
			continue
		}
		for _, key := range []string{"content", "markdown_or_text", "text", "body"} {
			value := strings.TrimSpace(fmt.Sprint(call.Arguments[key]))
			if value == "<nil>" {
				value = ""
			}
			if value != "" {
				return value, true
			}
		}
	}
	return "", false
}

func (a *App) renderLocalAssistantCanvasText(ctx context.Context, state *localAssistantTurnState, text string) (string, error) {
	tool := localAssistantCanvasWriteTextTool(*state)
	if strings.TrimSpace(tool.InternalName) == "" {
		return "", errors.New("canvas is not available for this workspace")
	}
	result, err := a.executeLocalAssistantCanvasWriteTextTool(ctx, state, tool, localAssistantToolCall{
		ID:   randomToken(),
		Name: "canvas_write_text",
		Arguments: map[string]any{
			"content": text,
		},
	}, map[string]any{
		"content": text,
	})
	if err != nil {
		return "", err
	}
	if result.IsError {
		return "", errors.New(strings.TrimSpace(result.Error))
	}
	for _, resultPayload := range localAssistantToolPayloads(result, state.workspace.ID) {
		if resultPayload == nil {
			continue
		}
		a.broadcastSystemActionEvent(state.sessionID, resultPayload)
	}
	return "Shown on canvas.", nil
}

func (a *App) handleLocalAssistantGeneratedCanvasText(ctx context.Context, state *localAssistantTurnState, structured bool, text string, retries int) (string, string, error) {
	if localAssistantLooksLikeCanvasPlanningText(text) {
		if retries >= 1 {
			return "", "", errors.New("local assistant answered with a canvas plan instead of canvas content")
		}
		return "", localAssistantCanvasContentRequiredPrompt(), nil
	}
	if structured && !localAssistantCanvasHasStructuredDiagramText(text) {
		if retries >= 1 {
			return "", "", errors.New("local assistant answered without a usable multi-line canvas diagram")
		}
		return "", localAssistantCanvasDiagramRequiredPrompt(), nil
	}
	confirmation, err := a.renderLocalAssistantCanvasText(ctx, state, localAssistantCanvasRenderText(structured, text))
	return confirmation, "", err
}

func (a *App) runLocalAssistantGeneratedCanvasTurn(ctx context.Context, req *assistantTurnRequest, visual *chatVisualAttachment, state localAssistantTurnState, userText string, promptContext string) (string, error) {
	if a == nil || req == nil {
		return "", errors.New("assistant turn request is required")
	}
	promptText := strings.TrimSpace(normalizeLocalAssistantAddress(userText))
	if promptText == "" {
		return "", errors.New("canvas request is empty")
	}
	userContentText := strings.TrimSpace(promptContext)
	if userContentText == "" {
		userContentText = promptText
	}
	requiresStructured := localAssistantCanvasNeedsStructuredDiagram(promptText) || localAssistantCanvasHasStructuredDiagramText(userContentText)
	conversation := []map[string]any{
		{
			"role":    "system",
			"content": buildLocalAssistantCanvasGenerationPrompt(promptText, userContentText, localAssistantReasoningHint(req)),
		},
		{
			"role":    "user",
			"content": buildLocalAssistantUserContent(userContentText, visual),
		},
	}
	enableThinking := localAssistantThinkingEnabled(req)
	for retries := 0; retries <= assistantLLMMalformedRetries+1; retries++ {
		message, err := a.requestLocalAssistantCompletionWithConfig(
			ctx,
			conversation,
			nil,
			"",
			enableThinking,
			assistantLLMDirectMaxTokens,
			nil,
		)
		if err != nil {
			return "", err
		}
		decision, err := parseLocalAssistantDecision(message)
		if err == nil {
			if recovered, ok := localAssistantCanvasTextFromToolCalls(decision.ToolCalls); ok {
				confirmation, retryPrompt, renderErr := a.handleLocalAssistantGeneratedCanvasText(ctx, &state, requiresStructured, recovered, retries)
				if renderErr != nil {
					return "", renderErr
				}
				if retryPrompt == "" {
					return confirmation, nil
				}
				conversation = append(conversation, localAssistantAssistantMessage(message))
				conversation = append(conversation, map[string]any{
					"role":    "user",
					"content": retryPrompt,
				})
				continue
			}
			if text := strings.TrimSpace(decision.FinalText); text != "" {
				confirmation, retryPrompt, renderErr := a.handleLocalAssistantGeneratedCanvasText(ctx, &state, requiresStructured, text, retries)
				if renderErr != nil {
					return "", renderErr
				}
				if retryPrompt == "" {
					return confirmation, nil
				}
				conversation = append(conversation, localAssistantAssistantMessage(message))
				conversation = append(conversation, map[string]any{
					"role":    "user",
					"content": retryPrompt,
				})
				continue
			}
		}
		if recovered, ok := recoverLocalAssistantCanvasTextFromMalformedToolOutput(message.Content); ok {
			confirmation, retryPrompt, renderErr := a.handleLocalAssistantGeneratedCanvasText(ctx, &state, requiresStructured, recovered, retries)
			if renderErr != nil {
				return "", renderErr
			}
			if retryPrompt == "" {
				return confirmation, nil
			}
			conversation = append(conversation, localAssistantAssistantMessage(message))
			conversation = append(conversation, map[string]any{
				"role":    "user",
				"content": retryPrompt,
			})
			continue
		}
		if retries >= assistantLLMMalformedRetries+1 {
			if err != nil {
				return "", fmt.Errorf("local assistant emitted malformed canvas output: %w", err)
			}
			return "", errors.New("local assistant did not return usable canvas content")
		}
		conversation = append(conversation, localAssistantAssistantMessage(message))
		conversation = append(conversation, map[string]any{
			"role":    "user",
			"content": localAssistantCanvasContentRequiredPrompt(),
		})
	}
	return "", errors.New("local assistant did not return usable canvas content")
}
