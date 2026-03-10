package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"

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
	kind := resolveCanvasArtifactKind(active)
	return &canvasContext{HasArtifact: true, ArtifactTitle: title, ArtifactKind: kind}
}

type canvasContext struct {
	HasArtifact   bool
	ArtifactTitle string
	ArtifactKind  string
}

func resolveCanvasArtifactKind(active map[string]interface{}) string {
	if active == nil {
		return ""
	}
	meta, _ := active["meta"].(map[string]interface{})
	if meta != nil {
		kind := strings.TrimSpace(fmt.Sprint(meta["artifact_kind"]))
		if kind != "" && kind != "<nil>" {
			return normalizedArtifactKind(kind)
		}
	}
	kind := strings.TrimSpace(fmt.Sprint(active["kind"]))
	if kind == "<nil>" {
		return ""
	}
	return normalizedArtifactKind(kind)
}

func appendArtifactCapabilityPrompt(b *strings.Builder, canvas *canvasContext) {
	if b == nil || canvas == nil || !canvas.HasArtifact {
		return
	}
	kind := normalizedArtifactKind(canvas.ArtifactKind)
	actions := artifactPromptActions(kind)
	b.WriteString("## Current Artifact\n")
	fmt.Fprintf(b, "- Active artifact tab: %q\n", canvas.ArtifactTitle)
	if kind != "" {
		fmt.Fprintf(b, "- Artifact kind: %s\n", kind)
	}
	if len(actions) > 0 {
		fmt.Fprintf(b, "- Canonical actions for this kind, in emphasis order: %s\n", strings.Join(actions, "; "))
	}
	b.WriteString("- When the user asks what they can do with this artifact, answer from this taxonomy rather than generic assumptions.\n\n")
}

func buildPromptFromHistory(mode string, messages []store.ChatMessage, canvas *canvasContext) string {
	return buildPromptFromHistoryForModeWithCompanion(mode, messages, canvas, nil, turnOutputModeVoice, "")
}

func buildPromptFromHistoryForMode(mode string, messages []store.ChatMessage, canvas *canvasContext, outputMode string, modelAlias string) string {
	return buildPromptFromHistoryForModeWithCompanion(mode, messages, canvas, nil, outputMode, modelAlias)
}

func buildPromptFromHistoryForModeWithCompanion(mode string, messages []store.ChatMessage, canvas *canvasContext, companion *companionPromptContext, outputMode string, modelAlias string) string {
	return buildPromptFromHistoryForSessionWithCompanion(mode, "", messages, canvas, companion, outputMode, modelAlias)
}

func buildPromptFromHistoryForSession(mode, sessionID string, messages []store.ChatMessage, canvas *canvasContext, outputMode string, modelAlias string) string {
	return buildPromptFromHistoryForSessionWithPolicy(mode, false, sessionID, messages, canvas, outputMode, modelAlias)
}

func buildPromptFromHistoryForSessionWithCompanion(mode, sessionID string, messages []store.ChatMessage, canvas *canvasContext, companion *companionPromptContext, outputMode string, modelAlias string) string {
	return buildPromptFromHistoryForSessionWithCompanionPolicy(mode, false, sessionID, messages, canvas, companion, outputMode, modelAlias)
}

func buildPromptFromHistoryForSessionWithPolicy(mode string, autonomous bool, sessionID string, messages []store.ChatMessage, canvas *canvasContext, outputMode string, modelAlias string) string {
	return buildPromptFromHistoryForSessionWithCompanionPolicy(mode, autonomous, sessionID, messages, canvas, nil, outputMode, modelAlias)
}

func buildPromptFromHistoryForSessionWithCompanionPolicy(mode string, autonomous bool, sessionID string, messages []store.ChatMessage, canvas *canvasContext, companion *companionPromptContext, outputMode string, modelAlias string) string {
	isVoiceMode := isVoiceOutputMode(outputMode)
	const maxHistory = 80
	if len(messages) > maxHistory {
		messages = messages[len(messages)-maxHistory:]
	}
	userText := latestUserMessage(messages)
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

	_ = modelAlias
	appendExecutionPolicyPrompt(&b, mode, autonomous)

	appendArtifactCapabilityPrompt(&b, canvas)
	appendResearchArtifactPrompt(&b, outputMode, userText, researchArtifactRoot(sessionID))

	appendCompanionPromptContext(&b, companion)
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
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	if isVoiceMode {
		b.WriteString("Reply as ASSISTANT.")
	}
	return b.String()
}

func appendExecutionPolicyPrompt(b *strings.Builder, mode string, autonomous bool) {
	if b == nil {
		return
	}
	switch executionPolicyForSession(mode, autonomous).Name {
	case executionPolicyReviewed:
		b.WriteString("Execution policy is reviewed. Propose actions step by step and wait for approval before executing risky or tool-driven work.\n")
		b.WriteString("Explain what you intend to do and why, then continue once the approval decision is available.\n")
		b.WriteString("For research tasks, propose each retrieval step clearly and present findings as artifacts or concise chat updates.\n\n")
	case executionPolicyAutonomous:
		b.WriteString("Execution policy is autonomous. Do not stall for approval unless the platform explicitly blocks the action.\n")
		b.WriteString("Keep using the normal canvas/artifact flow for proposed changes, findings, and status updates.\n\n")
	}
}

// buildTurnPrompt constructs a prompt for a resumed thread: only the latest
// user message plus optional canvas context update.
func buildTurnPrompt(messages []store.ChatMessage, canvas *canvasContext) string {
	return buildTurnPromptForModeWithCompanion(messages, canvas, nil, turnOutputModeVoice, "")
}

func buildTurnPromptForMode(messages []store.ChatMessage, canvas *canvasContext, outputMode string, modelAlias string) string {
	return buildTurnPromptForModeWithCompanion(messages, canvas, nil, outputMode, modelAlias)
}

func buildTurnPromptForModeWithCompanion(messages []store.ChatMessage, canvas *canvasContext, companion *companionPromptContext, outputMode string, modelAlias string) string {
	return buildTurnPromptForSessionWithCompanion("", messages, canvas, companion, outputMode, modelAlias)
}

func buildTurnPromptForSession(sessionID string, messages []store.ChatMessage, canvas *canvasContext, outputMode string, modelAlias string) string {
	return buildTurnPromptForSessionWithCompanion(sessionID, messages, canvas, nil, outputMode, modelAlias)
}

func buildTurnPromptForSessionWithCompanion(sessionID string, messages []store.ChatMessage, canvas *canvasContext, companion *companionPromptContext, outputMode string, modelAlias string) string {
	isVoiceMode := isVoiceOutputMode(outputMode)
	_ = modelAlias
	lastUserMsg := latestUserMessage(messages)
	if lastUserMsg == "" {
		return ""
	}
	var b strings.Builder
	if isVoiceMode {
		b.WriteString(loadModePromptTemplate(outputMode, defaultVoiceTurnPrompt, ""))
	}
	appendArtifactCapabilityPrompt(&b, canvas)
	appendResearchArtifactPrompt(&b, outputMode, lastUserMsg, researchArtifactRoot(sessionID))
	appendCompanionPromptContext(&b, companion)
	b.WriteString(lastUserMsg)
	return b.String()
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
