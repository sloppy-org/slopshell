package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

func (a *App) cwdForProjectKey(projectKey string) string {
	key := strings.TrimSpace(projectKey)
	if key != "" {
		if project, err := a.store.GetProjectByProjectKey(key); err == nil {
			root := strings.TrimSpace(project.RootPath)
			if root != "" {
				return root
			}
		}
		return key
	}
	if strings.TrimSpace(a.localProjectDir) != "" {
		return strings.TrimSpace(a.localProjectDir)
	}
	return "."
}

func normalizeAssistantError(err error) string {
	if err == nil {
		return "assistant request failed"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "assistant request timed out"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "assistant request timed out"
	}
	errText := strings.TrimSpace(err.Error())
	if errText == "" {
		return "assistant request failed"
	}
	if strings.Contains(strings.ToLower(errText), "i/o timeout") {
		return "assistant request timed out"
	}
	return errText
}

func (a *App) resolveCanvasSessionID(projectKey string) string {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return ""
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return ""
	}
	return a.canvasSessionIDForProject(project)
}

func (a *App) resolveCanvasContext(projectKey string) *canvasContext {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return nil
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return nil
	}
	sid := a.canvasSessionIDForProject(project)
	port, ok := a.tunnels.getPort(sid)
	if !ok {
		return nil
	}
	status, err := a.mcpToolsCall(port, "canvas_status", map[string]interface{}{"session_id": sid})
	if err != nil {
		return nil
	}
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		return nil
	}
	title := strings.TrimSpace(fmt.Sprint(active["title"]))
	if title == "<nil>" {
		title = ""
	}
	kind := strings.TrimSpace(fmt.Sprint(active["kind"]))
	if kind == "<nil>" {
		kind = ""
	}
	return &canvasContext{HasArtifact: true, ArtifactTitle: title, ArtifactKind: kind}
}

type canvasContext struct {
	HasArtifact   bool
	ArtifactTitle string
	ArtifactKind  string
}

func buildPromptFromHistory(mode string, messages []store.ChatMessage, canvas *canvasContext) string {
	return buildPromptFromHistoryForMode(mode, messages, canvas, turnOutputModeVoice, "")
}

func buildPromptFromHistoryForMode(mode string, messages []store.ChatMessage, canvas *canvasContext, outputMode string, modelAlias string) string {
	isVoiceMode := isVoiceOutputMode(outputMode)
	const maxHistory = 80
	if len(messages) > maxHistory {
		messages = messages[len(messages)-maxHistory:]
	}
	var b strings.Builder

	promptTemplate := loadModePromptTemplate(outputMode, defaultVoiceHistoryPrompt, "")
	if isVoiceMode {
		b.WriteString(promptTemplate)
	}
	if !isVoiceMode {
		silentPrompt := loadModePromptTemplate(outputMode, "", "")
		if silentPrompt != "" {
			b.WriteString(silentPrompt)
		}
	}

	appendDelegationSection(&b)
	if hints := modelprofile.ModelSystemHints(modelAlias); hints != "" {
		b.WriteString(hints)
		b.WriteString("\n")
	}

	if isVoiceMode && canvas != nil && canvas.HasArtifact {
		b.WriteString("## Current Artifact\n")
		fmt.Fprintf(&b, "- Active artifact tab: %q (kind: %s)\n\n", canvas.ArtifactTitle, canvas.ArtifactKind)
	}

	if strings.EqualFold(strings.TrimSpace(mode), "plan") {
		b.WriteString("You are in plan mode. Focus on analysis, design, and specification before implementation.\n\n")
	}

	b.WriteString("Conversation transcript:\n")
	for _, msg := range messages {
		content := strings.TrimSpace(msg.ContentPlain)
		if content == "" {
			content = strings.TrimSpace(msg.ContentMarkdown)
		}
		if content == "" {
			continue
		}
		role := strings.ToUpper(strings.TrimSpace(msg.Role))
		if role == "" {
			role = "USER"
		}
		b.WriteString(role)
		b.WriteString(":\n")
		if role == "USER" {
			content = applyDelegationHints(content)
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	if isVoiceMode {
		b.WriteString("Reply as ASSISTANT.")
	}
	return b.String()
}

// buildTurnPrompt constructs a prompt for a resumed thread: only the latest
// user message plus optional canvas context update.
func buildTurnPrompt(messages []store.ChatMessage, canvas *canvasContext) string {
	return buildTurnPromptForMode(messages, canvas, turnOutputModeVoice, "")
}

func buildTurnPromptForMode(messages []store.ChatMessage, canvas *canvasContext, outputMode string, modelAlias string) string {
	isVoiceMode := isVoiceOutputMode(outputMode)
	var lastUserMsg string
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(messages[i].Role), "user") {
			lastUserMsg = strings.TrimSpace(messages[i].ContentPlain)
			if lastUserMsg == "" {
				lastUserMsg = strings.TrimSpace(messages[i].ContentMarkdown)
			}
			break
		}
	}
	if lastUserMsg == "" {
		return ""
	}
	var b strings.Builder
	if isVoiceMode {
		b.WriteString(loadModePromptTemplate(outputMode, defaultVoiceTurnPrompt, ""))
		if canvas != nil && canvas.HasArtifact {
			fmt.Fprintf(&b, "[Active artifact tab: %q (kind: %s)]\n\n", canvas.ArtifactTitle, canvas.ArtifactKind)
		}
	} else {
		appendDelegationSection(&b)
	}
	if hints := modelprofile.ModelSystemHints(modelAlias); hints != "" {
		b.WriteString(hints)
		b.WriteString("\n")
	}
	b.WriteString(applyDelegationHints(lastUserMsg))
	return b.String()
}

func appendDelegationSection(b *strings.Builder) {
	b.WriteString("## Delegation\n")
	b.WriteString("Use `delegate_to_model` for tasks that benefit from another model.\n")
	b.WriteString("- 'let codex do this' / 'ask codex' -> model='codex'. 'ask gpt' / 'use the big model' -> model='gpt'.\n")
	b.WriteString("- Auto-delegate complex multi-file coding or deep analysis to 'codex'.\n")
	b.WriteString("- Provide 'context' and 'system_prompt' when delegating.\n")
	b.WriteString("- Do NOT delegate simple conversational replies.\n")
	b.WriteString("- Delegates have full filesystem access and edit files directly on disk.\n")
	b.WriteString("- Do NOT parse or apply patches/diffs from the delegate response.\n")
	b.WriteString("- `delegate_to_model` starts an async job and returns `job_id` immediately.\n")
	b.WriteString("- Use `delegate_to_model_status` with `job_id` and `after_seq` to fetch incremental progress.\n")
	b.WriteString("- Summarize progress updates for the user periodically while polling status.\n")
	b.WriteString("- Use `delegate_to_model_cancel` if the user asks to stop.\n")
	b.WriteString("- Final status includes `files_changed` and final `message`; relay that summary to the user.\n\n")
}

func loadModePromptTemplate(outputMode, defaultPrompt, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	switch strings.TrimSpace(outputMode) {
	case turnOutputModeVoice:
		return normalizePromptTemplate(firstNonEmptyPrompt(defaultPrompt, fallback))
	case turnOutputModeSilent:
		return normalizePromptTemplate(firstNonEmptyPrompt(defaultPrompt, fallback))
	default:
		return normalizePromptTemplate(fallback)
	}
}

func firstNonEmptyPrompt(primary, fallback string) string {
	primary = strings.TrimSpace(primary)
	if primary != "" {
		return primary
	}
	return strings.TrimSpace(fallback)
}

func normalizePromptTemplate(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	return prompt + "\n\n"
}

func currentPromptContractDigest() string {
	historyPrompt := buildPromptFromHistory("chat", nil, nil)
	turnPrompt := buildTurnPrompt([]store.ChatMessage{{
		Role:         "user",
		ContentPlain: "prompt-contract-sentinel",
	}}, nil)
	sum := sha256.Sum256([]byte(historyPrompt + "\n---\n" + turnPrompt))
	return hex.EncodeToString(sum[:])
}

func (a *App) ensurePromptContractFresh() error {
	currentDigest := strings.TrimSpace(currentPromptContractDigest())
	if currentDigest == "" {
		return nil
	}
	storedDigest, err := a.store.AppState(promptContractStateKey)
	if err != nil {
		return err
	}
	storedDigest = strings.TrimSpace(storedDigest)
	if storedDigest == "" {
		return a.store.SetAppState(promptContractStateKey, currentDigest)
	}
	if storedDigest == currentDigest {
		return nil
	}
	if _, err := a.clearAllAgentsAndContexts(""); err != nil {
		return err
	}
	return a.store.SetAppState(promptContractStateKey, currentDigest)
}

type delegationHint struct {
	Detected bool
	Model    string
	Task     string
}

var delegationPatterns = regexp.MustCompile(
	`(?i)^(?:let |ask |use )(codex|gpt|spark|the big model)\b[,: ]*(.*)`,
)

func detectDelegationHint(text string) delegationHint {
	m := delegationPatterns.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return delegationHint{}
	}
	model := strings.ToLower(m[1])
	if model == "the big model" {
		model = "gpt"
	}
	task := strings.TrimSpace(m[2])
	if task == "" {
		task = text
	}
	return delegationHint{Detected: true, Model: model, Task: task}
}

func applyDelegationHints(text string) string {
	hint := detectDelegationHint(text)
	if !hint.Detected {
		return text
	}
	return fmt.Sprintf("[Delegation hint: user wants model=%q] %s", hint.Model, text)
}
