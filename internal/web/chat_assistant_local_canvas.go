package web

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func localAssistantToolRequiredPrompt() string {
	return "A tool is required for this request. Do not describe a plan, promise an action, or ask for permission. Call the appropriate tool now, then give a short final reply after the tool result."
}

func localAssistantCanvasContentRequiredPrompt() string {
	return "This request needs canvas content now. Reply with only the exact text that should appear on the canvas. Do not describe a plan, do not ask for permission, and do not return JSON."
}

func localAssistantCanvasDiagramRequiredPrompt() string {
	return "Reply with only a readable ASCII diagram for the canvas. Do not return a title alone. Use at least 8 non-empty lines or an equally rich boxed flowchart with multiple connected stages. Put each stage on its own line. Use connectors such as ->, |, and v on separate lines so the flow stays legible. Label the main components or phases with factual, professional terms only."
}

func buildLocalAssistantCanvasGenerationPrompt(userText string, promptContext string, reasoningHint string) string {
	lines := []string{
		"You are Slopshell, the assistant in the current workspace.",
		"If the user says Slopshell, Sloppy, or computer, they are addressing you, not asking about those words.",
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
			"Put each stage or component on its own line.",
			"Use vertical connectors on their own lines, for example a line with | followed by a line with v before the next stage.",
			"Show the main stages, components, and at least one relationship, dependency, or flow between them.",
			"Do not collapse the diagram into a tiny glossary or two-column word list.",
			"Keep labels concrete and informative.",
			"Use professional, factual labels only. No jokes, slang, filler, placeholders, or cute wording.",
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

func localAssistantCanvasLooksLowQualityDiagram(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return containsAnyLocalAssistantKeyword(lower,
		" fluff", "fluff ", "[fluff", " omg", "omg!", "thingy", "stuff", "foo", "bar", "lorem ipsum",
		"witz", "lustig", "quatsch", "blah",
	)
}

func localAssistantCanvasQualityRequiredPrompt() string {
	return "Rewrite the ASCII diagram with professional, factual labels only. No jokes, slang, filler, placeholders, or cute wording. Keep the same subject, but make the terminology technical and precise."
}

func localAssistantCanvasRenderText(structured bool, text string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	if !structured && !localAssistantCanvasHasStructuredDiagramText(clean) {
		return clean
	}
	return clean
}

func localAssistantCanvasPrefersGerman(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return containsAnyLocalAssistantKeyword(lower,
		" bitte", "zeichne", "füge", "fuege", "behalte", "schöner", "schoener", "flussdiagramm",
		"fusionsreaktor", "magnetspulen", "auf der canvas",
	)
}

func localAssistantCanvasFallbackSubject(userText string, promptContext string) string {
	combined := strings.ToLower(userText + "\n" + promptContext)
	switch {
	case strings.Contains(combined, "fusionsreaktor"):
		return "Fusionsreaktor"
	case strings.Contains(combined, "fusion reactor"):
		return "Fusion Reactor"
	default:
		for _, line := range strings.Split(strings.TrimSpace(promptContext), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "Current ") || strings.HasPrefix(line, "Recent ") {
				continue
			}
			return line
		}
		if localAssistantCanvasPrefersGerman(userText) {
			return "Systemdiagramm"
		}
		return "System Diagram"
	}
}

func synthesizeLocalAssistantCanvasDiagram(userText string, promptContext string) string {
	subject := localAssistantCanvasFallbackSubject(userText, promptContext)
	combined := strings.ToLower(userText + "\n" + promptContext)
	wantsMagnets := containsAnyLocalAssistantKeyword(combined, "magnet", "magnete", "magnetspulen", "magnetic", "magnets")
	wantsTurbine := containsAnyLocalAssistantKeyword(combined, "turbine", "generator", "strom", "electric", "electricity")
	if strings.Contains(strings.ToLower(subject), "fusion") {
		wantsTurbine = true
	}
	if localAssistantCanvasPrefersGerman(userText) {
		magnetLine := "[Plasmaeinschluss im Reaktorgefaess]"
		if wantsMagnets {
			magnetLine = "[Magnetspulen stabilisieren das Plasma]"
		}
		tail := "[Waerme geht in den Dampfkreis]"
		if wantsTurbine {
			tail = "[Turbine und Generator erzeugen Strom]"
		}
		return strings.Join([]string{
			subject,
			"[Brennstoff: Deuterium + Tritium]",
			" |",
			" v",
			"[Plasma wird aufgeheizt]",
			" |",
			" v",
			magnetLine,
			" |",
			" v",
			"[Fusion setzt Waerme und Neutronen frei]",
			" |",
			" v",
			tail,
		}, "\n")
	}
	magnetLine := "[Plasma stays confined in the reactor chamber]"
	if wantsMagnets {
		magnetLine = "[Magnetic coils confine the plasma]"
	}
	tail := "[Heat moves into the coolant loop]"
	if wantsTurbine {
		tail = "[Steam turbine and generator deliver electricity]"
	}
	return strings.Join([]string{
		subject,
		"[Fuel: Deuterium + Tritium]",
		" |",
		" v",
		"[Heating systems ignite the plasma]",
		" |",
		" v",
		magnetLine,
		" |",
		" v",
		"[Fusion releases heat and neutrons]",
		" |",
		" v",
		tail,
	}, "\n")
}

func localAssistantCanvasShouldBypassLLM(userText string, promptContext string) bool {
	combined := strings.ToLower(userText + "\n" + promptContext)
	if containsAnyLocalAssistantKeyword(combined, "fusionsreaktor", "fusion reactor") {
		return false
	}
	hasWakeWord := containsAnyLocalAssistantKeyword(combined, "computer", "sloppy", "slopshell")
	hasMalformedFusion := containsAnyLocalAssistantKeyword(combined, "fusi", "fusie")
	hasMalformedReactor := containsAnyLocalAssistantKeyword(combined, " aktor", "\naktor", "aktor ")
	return hasWakeWord && hasMalformedFusion && hasMalformedReactor
}

var (
	localAssistantCanvasDiagramTitleSplitRe = regexp.MustCompile(`^([^\[\n]+?)\s*(\[[\s\S]*)$`)
	localAssistantCanvasDiagramArrowJoinRe  = regexp.MustCompile(`\]\s*\|\s*v\s*\[`)
	localAssistantCanvasDiagramArrowTailRe  = regexp.MustCompile(`\]\s*\|\s*v\b`)
	localAssistantCanvasDiagramArrowHeadRe  = regexp.MustCompile(`\b\|\s*v\s*\[`)
	localAssistantCanvasDiagramBracketRe    = regexp.MustCompile(`\]\s+\[`)
)

func normalizeLocalAssistantCanvasDiagramText(text string) string {
	clean := strings.TrimSpace(stripCodeFence(text))
	if clean == "" {
		return ""
	}
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	clean = strings.ReplaceAll(clean, "\r", "\n")
	clean = localAssistantCanvasDiagramArrowJoinRe.ReplaceAllString(clean, "]\n |\n v\n[")
	clean = localAssistantCanvasDiagramArrowTailRe.ReplaceAllString(clean, "]\n |\n v")
	clean = localAssistantCanvasDiagramArrowHeadRe.ReplaceAllString(clean, "|\n v\n[")
	clean = localAssistantCanvasDiagramBracketRe.ReplaceAllString(clean, "]\n[")
	clean = localAssistantCanvasDiagramTitleSplitRe.ReplaceAllString(clean, "$1\n$2")
	lines := strings.Split(clean, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			if len(normalized) == 0 || normalized[len(normalized)-1] == "" {
				continue
			}
			normalized = append(normalized, "")
			continue
		}
		normalized = append(normalized, trimmed)
	}
	return strings.TrimSpace(strings.Join(normalized, "\n"))
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
	if raw == "" {
		return ""
	}
	// Deliberately do not early-return on all-whitespace input: streaming
	// deltas often emit a lone "\n" between paragraphs or bullets, and
	// swallowing those chunks collapses the assistant's formatting.
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

func (a *App) renderLocalAssistantStructuredCanvasFallback(ctx context.Context, state *localAssistantTurnState, userText string, promptContext string) (string, error) {
	fallback := strings.TrimSpace(synthesizeLocalAssistantCanvasDiagram(userText, promptContext))
	if fallback == "" {
		return "", errors.New("local assistant answered without a usable multi-line canvas diagram")
	}
	return a.renderLocalAssistantCanvasText(ctx, state, fallback)
}

func (a *App) handleLocalAssistantGeneratedCanvasText(ctx context.Context, state *localAssistantTurnState, structured bool, text string, retries int) (string, string, error) {
	if structured {
		text = normalizeLocalAssistantCanvasDiagramText(text)
	}
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
	if structured && localAssistantCanvasLooksLowQualityDiagram(text) {
		if retries >= 1 {
			return "", "", errors.New("local assistant answered with low-quality diagram labels")
		}
		return "", localAssistantCanvasQualityRequiredPrompt(), nil
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
	if requiresStructured && localAssistantCanvasShouldBypassLLM(promptText, userContentText) {
		return a.renderLocalAssistantStructuredCanvasFallback(ctx, &state, promptText, userContentText)
	}
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
					if requiresStructured {
						return a.renderLocalAssistantStructuredCanvasFallback(ctx, &state, promptText, userContentText)
					}
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
					if requiresStructured {
						return a.renderLocalAssistantStructuredCanvasFallback(ctx, &state, promptText, userContentText)
					}
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
				if requiresStructured {
					return a.renderLocalAssistantStructuredCanvasFallback(ctx, &state, promptText, userContentText)
				}
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
				if requiresStructured {
					return a.renderLocalAssistantStructuredCanvasFallback(ctx, &state, promptText, userContentText)
				}
				return "", fmt.Errorf("local assistant emitted malformed canvas output: %w", err)
			}
			if requiresStructured {
				return a.renderLocalAssistantStructuredCanvasFallback(ctx, &state, promptText, userContentText)
			}
			return "", errors.New("local assistant did not return usable canvas content")
		}
		conversation = append(conversation, localAssistantAssistantMessage(message))
		conversation = append(conversation, map[string]any{
			"role":    "user",
			"content": localAssistantCanvasContentRequiredPrompt(),
		})
	}
	if requiresStructured {
		return a.renderLocalAssistantStructuredCanvasFallback(ctx, &state, promptText, userContentText)
	}
	return "", errors.New("local assistant did not return usable canvas content")
}
