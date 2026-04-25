package web

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sloppy-org/slopshell/internal/store"
)

const (
	assistantModeAuto                = "auto"
	assistantModeLocal               = "local"
	assistantModeCodex               = "codex"
	DefaultAssistantMode             = assistantModeLocal
	defaultAssistantLLMTimeout       = 2 * time.Minute
	assistantLLMFastMaxTokens        = 512
	assistantLLMDirectMaxTokens      = 1024
	assistantLLMToolPlanMaxTokens    = 256
	assistantLLMToolMaxTokens        = 1024
	assistantLLMResponseLimit        = 256 * 1024
	assistantLLMMaxToolRounds        = 16
	assistantLLMMalformedRetries     = 1
	assistantLLMToolPlanRetries      = 2
	localAssistantDialoguePromptBase = "You are Slopshell, the assistant inside the current workspace. If the user says Slopshell, Sloppy, or computer, they are addressing you, not asking about those words. Use the explicit tools in this request instead of inventing plans or wrapper calls. Answer directly when no tool is needed. Default to plain text, not markdown. Do not use headings, bullets, numbered lists, or tables unless the user explicitly asks for them. Give complete answers by default: for substantive questions, answer with a compact but satisfying explanation, usually one short paragraph or 3-6 sentences. For simple factual prompts, keep the answer short. If a single word or short phrase fully answers the request, reply with exactly that. No markdown fences. No <think> tags."
)

func assistantLLMRequestTimeout() time.Duration {
	return parseEnvDurationDefault("SLOPSHELL_ASSISTANT_LLM_TIMEOUT", defaultAssistantLLMTimeout)
}

func normalizeAssistantMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case assistantModeLocal:
		return assistantModeLocal
	case assistantModeCodex:
		return assistantModeCodex
	case assistantModeAuto:
		return assistantModeAuto
	default:
		return DefaultAssistantMode
	}
}

func (a *App) assistantRoutingMode() string {
	if a == nil {
		return DefaultAssistantMode
	}
	return normalizeAssistantMode(a.assistantMode)
}

func (a *App) assistantTurnMode(localOnly bool) string {
	if localOnly {
		return assistantModeLocal
	}
	switch a.assistantRoutingMode() {
	case assistantModeLocal:
		return assistantModeLocal
	case assistantModeCodex:
		return assistantModeCodex
	default:
		if a == nil || a.appServerClient == nil {
			return assistantModeLocal
		}
		return assistantModeCodex
	}
}

func (a *App) assistantLLMBaseURL() string {
	if a == nil {
		return ""
	}
	baseURL := strings.TrimSpace(a.assistantLLMURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(a.intentLLMURL)
	}
	return strings.TrimRight(baseURL, "/")
}

func (a *App) localAssistantLLMModel() string {
	if a == nil {
		return DefaultIntentLLMModel
	}
	if model := strings.TrimSpace(a.assistantLLMModel); model != "" {
		return model
	}
	return a.localIntentLLMModel()
}

func buildLocalAssistantDialoguePrompt(toolPolicy string, reasoningHint string) string {
	parts := []string{localAssistantDialoguePromptBase}
	if strings.TrimSpace(toolPolicy) != "" {
		parts = append(parts, strings.TrimSpace(toolPolicy))
	}
	if strings.TrimSpace(reasoningHint) != "" {
		parts = append(parts, strings.TrimSpace(reasoningHint))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (a *App) buildLocalAssistantPrompt(sessionID string, session store.ChatSession, messages []store.ChatMessage, cursorCtx *chatCursorContext, inkCtx []*chatCanvasInkEvent, positionCtx []*chatCanvasPositionEvent, outputMode string, detailRequested bool) (string, error) {
	var workspaceRef *store.Workspace
	if workspace, err := a.effectiveWorkspaceForChatSession(session); err == nil {
		workspaceRef = &workspace
	}
	canvasCtx := a.resolveCanvasContext(session.WorkspacePath)
	companionCtx := a.loadCompanionPromptContextForTurn(sessionID, session.WorkspacePath)
	prompt := buildLeanLocalAssistantPrompt(workspaceRef, messages, canvasCtx, companionCtx, outputMode, detailRequested)
	prompt = appendChatCursorPrompt(prompt, cursorCtx)
	prompt = appendCanvasInkPrompt(prompt, inkCtx)
	prompt = appendCanvasPositionPrompt(prompt, positionCtx)
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("empty prompt")
	}
	prompt, err := a.applyPreAssistantPromptHook(context.Background(), sessionID, session.WorkspacePath, outputMode, session.Mode, prompt)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(prompt) == "" {
		return "", errors.New("empty prompt")
	}
	return prompt, nil
}

func (a *App) runLocalAssistantTurn(req *assistantTurnRequest) {
	if a == nil || req == nil {
		return
	}
	if strings.TrimSpace(a.assistantLLMBaseURL()) == "" {
		if a.tryRunLocalSystemActionTurn(req.sessionID, req.session, req.userText, req.cursorCtx, req.captureMode, req.outputMode, req.localOnly) {
			return
		}
		errText := errLocalAssistantNotConfigured.Error()
		_, _ = a.store.AddChatMessage(req.sessionID, "system", errText, errText, "text")
		a.finishCompanionPendingTurn(req.sessionID, "assistant_turn_failed")
		a.broadcastChatEvent(req.sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}
	turnStartedAt := time.Now()

	prompt := strings.TrimSpace(req.promptText)
	if !req.fastMode {
		promptMessages := withQueuedUserMessage(req.messages, req.messageID, req.promptText)
		var err error
		prompt, err = a.buildLocalAssistantPrompt(req.sessionID, req.session, promptMessages, req.cursorCtx, req.inkCtx, req.positionCtx, req.outputMode, req.detailRequested)
		if err != nil {
			errText := err.Error()
			_, _ = a.store.AddChatMessage(req.sessionID, "system", errText, errText, "text")
			a.finishCompanionPendingTurn(req.sessionID, "assistant_turn_failed")
			a.broadcastChatEvent(req.sessionID, map[string]interface{}{"type": "error", "error": errText})
			return
		}
	}
	if !req.fastMode {
		if compactedPrompt, compacted := compactLocalAssistantPrompt(prompt); compacted {
			prompt = compactedPrompt
			a.broadcastChatEvent(req.sessionID, map[string]any{
				"type": "context_compact",
			})
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	runID := randomToken()
	a.registerActiveChatTurn(req.sessionID, runID, cancel)
	defer func() {
		cancel()
		a.unregisterActiveChatTurn(req.sessionID, runID)
	}()

	go a.watchCanvasFile(ctx, req.session.WorkspacePath)
	a.broadcastChatEvent(req.sessionID, map[string]interface{}{
		"type":    "turn_started",
		"turn_id": runID,
	})

	reply, err := a.runLocalAssistantToolLoop(
		ctx,
		req,
		prompt,
		latestCanvasPositionVisualAttachment(req.positionCtx),
		func(fullText string, delta string) {
			fullText = strings.TrimLeft(fullText, " \t\r\n")
			if fullText == "" || delta == "" {
				return
			}
			a.broadcastChatEvent(req.sessionID, map[string]any{
				"type":        "assistant_message",
				"turn_id":     runID,
				"output_mode": req.outputMode,
				"message":     fullText,
				"delta":       delta,
			})
		},
	)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			a.finishCompanionPendingTurn(req.sessionID, "assistant_turn_cancelled")
			a.broadcastChatEvent(req.sessionID, map[string]interface{}{
				"type":    "turn_cancelled",
				"turn_id": runID,
			})
			return
		}
		errText := normalizeAssistantError(err)
		_, _ = a.store.AddChatMessage(req.sessionID, "system", errText, errText, "text")
		a.finishCompanionPendingTurn(req.sessionID, "assistant_turn_failed")
		a.broadcastChatEvent(req.sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}

	assistantText := strings.TrimSpace(reply)
	if assistantText == "" {
		assistantText = "(assistant returned no content)"
	}
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	a.finalizeAssistantResponseWithMetadata(
		req.sessionID,
		req.session.WorkspacePath,
		assistantText,
		&persistedAssistantID,
		&persistedAssistantText,
		"",
		runID,
		"",
		req.outputMode,
		newAssistantResponseMetadata(a.localAssistantProvider(), a.localAssistantModelLabel(), time.Since(turnStartedAt)),
	)
}
