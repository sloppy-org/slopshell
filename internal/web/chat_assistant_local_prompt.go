package web

import (
	"fmt"
	"strings"

	"github.com/sloppy-org/slopshell/internal/store"
)

const (
	localAssistantHistoryMessageLimit = 6
	localAssistantHistoryCharLimit    = 4000
)

func buildLeanLocalAssistantPrompt(
	workspace *store.Workspace,
	messages []store.ChatMessage,
	canvas *canvasContext,
	companion *companionPromptContext,
	outputMode string,
	detailRequested bool,
) string {
	var b strings.Builder
	appendLeanLocalAssistantWorkspace(&b, workspace)
	appendLeanLocalAssistantCanvas(&b, canvas)
	appendLeanLocalAssistantCompanion(&b, companion)
	if isVoiceOutputMode(outputMode) && !detailRequested {
		b.WriteString("Reply for speech. Default to 1-3 sentences; extend only if the question genuinely needs it. No markdown.\n")
	}
	appendLeanLocalAssistantHistory(&b, messages)
	return strings.TrimSpace(b.String())
}

func buildLocalAssistantFastPrompt(userText string) string {
	body := strings.TrimSpace(normalizeLocalAssistantAddress(userText))
	if body == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join([]string{
		"You are Slopshell, the assistant in this workspace.",
		"Answer in plain text only. Be concise, but do not under-answer: default to 2-4 short sentences for normal questions.",
		"If a single word or short phrase answers the request, reply with exactly that.",
		"Do not use markdown, headings, bullets, or numbered lists unless the user explicitly asks for them.",
		"",
		"User request:",
		body,
	}, "\n"))
}

func appendLeanLocalAssistantWorkspace(b *strings.Builder, workspace *store.Workspace) {
	if b == nil || workspace == nil {
		return
	}
	name := strings.TrimSpace(workspace.Name)
	dir := strings.TrimSpace(workspace.DirPath)
	if name == "" && dir == "" {
		return
	}
	if name != "" && dir != "" {
		fmt.Fprintf(b, "Workspace: %s (%s)\n", name, dir)
		return
	}
	if dir != "" {
		fmt.Fprintf(b, "Workspace: %s\n", dir)
		return
	}
	fmt.Fprintf(b, "Workspace: %s\n", name)
}

func appendLeanLocalAssistantCanvas(b *strings.Builder, canvas *canvasContext) {
	if b == nil || canvas == nil || !canvas.HasArtifact {
		return
	}
	title := strings.TrimSpace(canvas.ArtifactTitle)
	kind := normalizedArtifactKind(canvas.ArtifactKind)
	if title == "" && kind == "" {
		return
	}
	switch {
	case title != "" && kind != "":
		fmt.Fprintf(b, "Canvas: %s [%s]\n", title, kind)
	case title != "":
		fmt.Fprintf(b, "Canvas: %s\n", title)
	default:
		fmt.Fprintf(b, "Canvas kind: %s\n", kind)
	}
	if body := strings.TrimSpace(canvas.ArtifactText); body != "" {
		b.WriteString("Canvas content:\n")
		b.WriteString(body)
		b.WriteByte('\n')
	}
}

func appendLeanLocalAssistantCompanion(b *strings.Builder, companion *companionPromptContext) {
	if b == nil || companion == nil || companion.empty() {
		return
	}
	appendCompanionPromptContext(b, companion)
}

func appendLeanLocalAssistantHistory(b *strings.Builder, messages []store.ChatMessage) {
	if b == nil {
		return
	}
	recent := collectLeanLocalAssistantHistory(messages)
	if len(recent) == 0 {
		return
	}
	b.WriteString("Recent messages:\n")
	for _, msg := range recent {
		role := strings.ToUpper(strings.TrimSpace(msg.Role))
		if role == "" {
			role = "USER"
		}
		content := strings.TrimSpace(msg.ContentPlain)
		if content == "" {
			content = strings.TrimSpace(msg.ContentMarkdown)
		}
		if content == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			content = normalizeLocalAssistantAddress(content)
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteByte('\n')
	}
}

func collectLeanLocalAssistantHistory(messages []store.ChatMessage) []store.ChatMessage {
	if len(messages) == 0 {
		return nil
	}
	selected := make([]store.ChatMessage, 0, minInt(len(messages), localAssistantHistoryMessageLimit))
	usedChars := 0
	for i := len(messages) - 1; i >= 0; i-- {
		content := strings.TrimSpace(messages[i].ContentPlain)
		if content == "" {
			content = strings.TrimSpace(messages[i].ContentMarkdown)
		}
		if content == "" {
			continue
		}
		nextChars := usedChars + len(content)
		if len(selected) > 0 && nextChars > localAssistantHistoryCharLimit {
			break
		}
		selected = append(selected, messages[i])
		usedChars = nextChars
		if len(selected) == localAssistantHistoryMessageLimit {
			break
		}
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}
