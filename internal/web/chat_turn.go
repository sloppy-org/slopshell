package web

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/plugins"
	"github.com/krystophny/tabura/internal/store"
)

func (a *App) runAssistantTurn(sessionID string, outputMode string, localOnly bool) {
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
		return
	}
	messages, err := a.store.ListChatMessages(sessionID, 200)
	if err != nil {
		a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
		return
	}
	userText := latestUserMessage(messages)
	if project, projectErr := a.store.GetProjectByProjectKey(session.ProjectKey); projectErr == nil && isHubProject(project) {
		a.runHubTurn(sessionID, session, messages, outputMode, localOnly)
		return
	}
	if a.tryRunLocalSystemActionTurn(sessionID, session, messages, outputMode, localOnly) {
		return
	}
	if a.appServerClient == nil {
		errText := "app-server is not configured"
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}

	cwd := a.cwdForProjectKey(session.ProjectKey)
	profile := a.appServerModelProfileForProjectKey(session.ProjectKey)
	if strings.TrimSpace(userText) != "" {
		profile = routeProfileForRouting(
			classifyRoutingRoute(userText),
			profile,
			a.appServerSparkReasoningEffort,
		)
	}
	appSess, resumed, sessErr := a.getOrCreateAppSession(sessionID, cwd, profile)
	if sessErr != nil {
		a.runAssistantTurnLegacy(sessionID, session, messages, outputMode, profile)
		return
	}

	canvasCtx := a.resolveCanvasContext(session.ProjectKey)
	companionCtx := a.loadCompanionPromptContext(session.ProjectKey)
	var prompt string
	if resumed {
		prompt = buildTurnPromptForModeWithCompanion(messages, canvasCtx, companionCtx, outputMode, profile.Alias)
	} else {
		prompt = buildPromptFromHistoryForModeWithCompanion(session.Mode, messages, canvasCtx, companionCtx, outputMode, profile.Alias)
		_ = a.store.UpdateChatSessionThread(sessionID, appSess.ThreadID())
	}
	if strings.TrimSpace(prompt) == "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
		return
	}
	prompt = a.applyWorkspacePromptContext(session.ProjectKey, prompt)
	prompt, err = a.applyPreAssistantPromptHook(context.Background(), sessionID, session.ProjectKey, outputMode, session.Mode, prompt)
	if err != nil {
		errText := err.Error()
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}
	if strings.TrimSpace(prompt) == "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	runID := randomToken()
	a.registerActiveChatTurn(sessionID, runID, cancel)
	defer func() {
		cancel()
		a.unregisterActiveChatTurn(sessionID, runID)
	}()

	go a.watchCanvasFile(ctx, session.ProjectKey)

	latestMessage := ""
	latestTurnID := ""
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	persistedAssistantPlain := ""
	persistedAssistantFormat := ""
	persistWriteFailed := false

	persistAssistantSnapshot := func(text string, renderOnCanvas bool, autoCanvas bool) {
		candidateMarkdown, candidatePlain, candidateFormat := assistantSnapshotContent(text, renderOnCanvas, autoCanvas)
		if candidateMarkdown == "" && candidatePlain == "" {
			return
		}
		if persistedAssistantID == 0 {
			storedAssistant, storeErr := a.store.AddChatMessage(sessionID, "assistant", candidateMarkdown, candidatePlain, candidateFormat)
			if storeErr != nil {
				if !persistWriteFailed {
					persistWriteFailed = true
					a.broadcastChatEvent(sessionID, map[string]interface{}{
						"type":  "error",
						"error": storeErr.Error(),
					})
				}
				return
			}
			persistedAssistantID = storedAssistant.ID
			persistedAssistantText = candidateMarkdown
			persistedAssistantPlain = candidatePlain
			persistedAssistantFormat = candidateFormat
			return
		}
		if candidateMarkdown == persistedAssistantText &&
			candidatePlain == persistedAssistantPlain &&
			candidateFormat == persistedAssistantFormat {
			return
		}
		if storeErr := a.store.UpdateChatMessageContent(persistedAssistantID, candidateMarkdown, candidatePlain, candidateFormat); storeErr != nil {
			if !persistWriteFailed {
				persistWriteFailed = true
				a.broadcastChatEvent(sessionID, map[string]interface{}{
					"type":  "error",
					"error": storeErr.Error(),
				})
			}
			return
		}
		persistedAssistantText = candidateMarkdown
		persistedAssistantPlain = candidatePlain
		persistedAssistantFormat = candidateFormat
	}

	appResp, err := appSess.SendTurnWithParams(ctx, prompt, profile.Model, profile.TurnParams, func(ev appserver.StreamEvent) {
		payload := map[string]interface{}{
			"type":        ev.Type,
			"thread_id":   ev.ThreadID,
			"turn_id":     ev.TurnID,
			"output_mode": outputMode,
		}
		shouldBroadcast := true
		switch ev.Type {
		case "thread_started":
			// Thread ID already stored on session open.
		case "turn_started":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			a.markCompanionThinking(sessionID, session.ProjectKey, latestTurnID, outputMode, "assistant_turn_started")
		case "assistant_message":
			latestMessage = ev.Message
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(ev.Message, outputMode)
			persistAssistantSnapshot(ev.Message, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = ev.Message
			payload["delta"] = ev.Delta
			if renderPlan.RenderOnCanvas {
				payload["render_on_canvas"] = true
			}
			if renderPlan.AutoCanvas {
				payload["auto_canvas"] = true
			}
		case "turn_completed":
			if strings.TrimSpace(ev.Message) != "" {
				latestMessage = ev.Message
			}
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(latestMessage, outputMode)
			persistAssistantSnapshot(latestMessage, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = latestMessage
			if renderPlan.RenderOnCanvas {
				payload["render_on_canvas"] = true
			}
			if renderPlan.AutoCanvas {
				payload["auto_canvas"] = true
			}
		case "item_completed":
			payload["item_type"] = ev.Message
			if ev.Detail != "" {
				payload["detail"] = ev.Detail
			}
		case "context_usage":
			payload["context_used"] = ev.ContextUsed
			payload["context_max"] = ev.ContextMax
		case "context_compact":
			// pass through to frontend
		case "error":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			shouldBroadcast = false
		}
		if shouldBroadcast {
			a.broadcastChatEvent(sessionID, payload)
		}
	})
	if err != nil {
		a.closeAppSession(sessionID)
		if errors.Is(err, context.Canceled) {
			a.finishCompanionPendingTurn(sessionID, "assistant_turn_cancelled")
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":    "turn_cancelled",
				"turn_id": latestTurnID,
			})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
			errText := "assistant request timed out"
			_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
			payload := map[string]interface{}{
				"type":  "error",
				"error": errText,
			}
			if strings.TrimSpace(latestTurnID) != "" {
				payload["turn_id"] = latestTurnID
			}
			a.broadcastChatEvent(sessionID, payload)
			return
		}
		a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
		errText := normalizeAssistantError(err)
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		payload := map[string]interface{}{
			"type":  "error",
			"error": errText,
		}
		if strings.TrimSpace(latestTurnID) != "" {
			payload["turn_id"] = latestTurnID
		}
		a.broadcastChatEvent(sessionID, payload)
		return
	}

	assistantText := strings.TrimSpace(appResp.Message)
	if assistantText == "" {
		assistantText = strings.TrimSpace(latestMessage)
	}
	if assistantText == "" {
		assistantText = "(assistant returned no content)"
	}

	assistantText = a.finalizeAssistantResponse(sessionID, session.ProjectKey, assistantText,
		&persistedAssistantID, &persistedAssistantText, appResp.TurnID, latestTurnID, appResp.ThreadID, outputMode)
	_ = assistantText
}

func (a *App) tryRunLocalSystemActionTurn(sessionID string, session store.ChatSession, messages []store.ChatMessage, outputMode string, localOnly bool) bool {
	userText := latestUserMessage(messages)
	if strings.TrimSpace(userText) == "" {
		return false
	}
	actionMessage, actionPayloads, handled := a.classifyAndExecuteSystemAction(context.Background(), sessionID, session, userText)
	if !handled && !localOnly {
		return false
	}
	runID := randomToken()
	a.broadcastChatEvent(sessionID, map[string]interface{}{
		"type":    "turn_started",
		"turn_id": runID,
	})
	assistantText := strings.TrimSpace(actionMessage)
	if handled {
		if assistantText == "" {
			assistantText = "Done."
		}
		for _, actionPayload := range actionPayloads {
			if actionPayload == nil {
				continue
			}
			eventType := "system_action"
			actionType, _ := actionPayload["type"].(string)
			if strings.EqualFold(strings.TrimSpace(actionType), "confirmation_required") {
				eventType = "system_action_confirmation_required"
			}
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":   eventType,
				"action": actionPayload,
			})
		}
	} else {
		assistantText = "I can only handle system actions in local-only mode."
	}
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	a.finalizeAssistantResponse(
		sessionID,
		session.ProjectKey,
		assistantText,
		&persistedAssistantID,
		&persistedAssistantText,
		"",
		runID,
		"",
		outputMode,
	)
	return true
}

// runAssistantTurnLegacy is the single-shot fallback when persistent session
// fails to connect. Each call creates a new WS + thread.
func (a *App) runAssistantTurnLegacy(sessionID string, session store.ChatSession, messages []store.ChatMessage, outputMode string, profile appServerModelProfile) {
	canvasCtx := a.resolveCanvasContext(session.ProjectKey)
	prompt := buildPromptFromHistoryForMode(session.Mode, messages, canvasCtx, outputMode, profile.Alias)
	if strings.TrimSpace(prompt) == "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
		return
	}
	prompt = a.applyWorkspacePromptContext(session.ProjectKey, prompt)
	var err error
	prompt, err = a.applyPreAssistantPromptHook(context.Background(), sessionID, session.ProjectKey, outputMode, session.Mode, prompt)
	if err != nil {
		errText := err.Error()
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}
	if strings.TrimSpace(prompt) == "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	runID := randomToken()
	a.registerActiveChatTurn(sessionID, runID, cancel)
	defer func() {
		cancel()
		a.unregisterActiveChatTurn(sessionID, runID)
	}()

	go a.watchCanvasFile(ctx, session.ProjectKey)

	latestMessage := ""
	latestTurnID := ""
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	persistedAssistantPlain := ""
	persistedAssistantFormat := ""
	persistWriteFailed := false
	persistAssistantSnapshot := func(text string, renderOnCanvas bool, autoCanvas bool) {
		candidateMarkdown, candidatePlain, candidateFormat := assistantSnapshotContent(text, renderOnCanvas, autoCanvas)
		if candidateMarkdown == "" && candidatePlain == "" {
			return
		}
		if persistedAssistantID == 0 {
			storedAssistant, storeErr := a.store.AddChatMessage(sessionID, "assistant", candidateMarkdown, candidatePlain, candidateFormat)
			if storeErr != nil {
				if !persistWriteFailed {
					persistWriteFailed = true
					a.broadcastChatEvent(sessionID, map[string]interface{}{
						"type":  "error",
						"error": storeErr.Error(),
					})
				}
				return
			}
			persistedAssistantID = storedAssistant.ID
			persistedAssistantText = candidateMarkdown
			persistedAssistantPlain = candidatePlain
			persistedAssistantFormat = candidateFormat
			return
		}
		if candidateMarkdown == persistedAssistantText &&
			candidatePlain == persistedAssistantPlain &&
			candidateFormat == persistedAssistantFormat {
			return
		}
		if storeErr := a.store.UpdateChatMessageContent(persistedAssistantID, candidateMarkdown, candidatePlain, candidateFormat); storeErr != nil {
			if !persistWriteFailed {
				persistWriteFailed = true
				a.broadcastChatEvent(sessionID, map[string]interface{}{
					"type":  "error",
					"error": storeErr.Error(),
				})
			}
			return
		}
		persistedAssistantText = candidateMarkdown
		persistedAssistantPlain = candidatePlain
		persistedAssistantFormat = candidateFormat
	}

	appResp, err := a.appServerClient.SendPromptStream(ctx, appserver.PromptRequest{
		CWD:          a.cwdForProjectKey(session.ProjectKey),
		Prompt:       prompt,
		Model:        profile.Model,
		TurnModel:    profile.Model,
		ThreadParams: profile.ThreadParams,
		TurnParams:   profile.TurnParams,
	}, func(ev appserver.StreamEvent) {
		payload := map[string]interface{}{
			"type":        ev.Type,
			"thread_id":   ev.ThreadID,
			"turn_id":     ev.TurnID,
			"output_mode": outputMode,
		}
		shouldBroadcast := true
		switch ev.Type {
		case "thread_started":
			if strings.TrimSpace(ev.ThreadID) != "" {
				_ = a.store.UpdateChatSessionThread(sessionID, ev.ThreadID)
			}
		case "turn_started":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			a.markCompanionThinking(sessionID, session.ProjectKey, latestTurnID, outputMode, "assistant_turn_started")
		case "assistant_message":
			latestMessage = ev.Message
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(ev.Message, outputMode)
			persistAssistantSnapshot(ev.Message, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = ev.Message
			payload["delta"] = ev.Delta
			if renderPlan.RenderOnCanvas {
				payload["render_on_canvas"] = true
			}
			if renderPlan.AutoCanvas {
				payload["auto_canvas"] = true
			}
		case "turn_completed":
			if strings.TrimSpace(ev.Message) != "" {
				latestMessage = ev.Message
			}
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(latestMessage, outputMode)
			persistAssistantSnapshot(latestMessage, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = latestMessage
			if renderPlan.RenderOnCanvas {
				payload["render_on_canvas"] = true
			}
			if renderPlan.AutoCanvas {
				payload["auto_canvas"] = true
			}
		case "error":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			shouldBroadcast = false
		}
		if shouldBroadcast {
			a.broadcastChatEvent(sessionID, payload)
		}
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			a.finishCompanionPendingTurn(sessionID, "assistant_turn_cancelled")
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":    "turn_cancelled",
				"turn_id": latestTurnID,
			})
			return
		}
		a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
		errText := normalizeAssistantError(err)
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		payload := map[string]interface{}{
			"type":  "error",
			"error": errText,
		}
		if strings.TrimSpace(latestTurnID) != "" {
			payload["turn_id"] = latestTurnID
		}
		a.broadcastChatEvent(sessionID, payload)
		return
	}

	assistantText := strings.TrimSpace(appResp.Message)
	if assistantText == "" {
		assistantText = strings.TrimSpace(latestMessage)
	}
	if assistantText == "" {
		assistantText = "(assistant returned no content)"
	}

	assistantText = a.finalizeAssistantResponse(sessionID, session.ProjectKey, assistantText,
		&persistedAssistantID, &persistedAssistantText, appResp.TurnID, latestTurnID, appResp.ThreadID, outputMode)
	_ = assistantText
}

// finalizeAssistantResponse handles post-processing shared by both turn paths:
// voice mode stays chat-only, while silent mode mirrors assistant text to canvas,
// then persists final content and broadcasts assistant_output.
func (a *App) finalizeAssistantResponse(
	sessionID, projectKey, text string,
	persistedID *int64, persistedText *string,
	turnID, fallbackTurnID, threadID string,
	outputMode string,
) string {
	postResult := a.applyPluginHook(context.Background(), plugins.HookRequest{
		Hook:       plugins.HookChatPostAssistantReply,
		SessionID:  sessionID,
		ProjectKey: projectKey,
		OutputMode: outputMode,
		Text:       text,
		Metadata: map[string]interface{}{
			"turn_id":   strings.TrimSpace(turnID),
			"thread_id": strings.TrimSpace(threadID),
		},
	})
	if postResult.Blocked {
		errText := strings.TrimSpace(postResult.Reason)
		if errText == "" {
			errText = "assistant response blocked by plugin"
		}
		_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
		a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":  "error",
			"error": errText,
		})
		return ""
	}
	text = postResult.Text

	outputMode = normalizeTurnOutputMode(outputMode)
	canvasSessionID := a.resolveCanvasSessionID(projectKey)
	autoCanvas := false
	renderOnCanvas := false
	if isVoiceOutputMode(outputMode) {
		_, cleaned := parseFileBlocks(text)
		text = cleaned
	} else {
		canvasCtx := a.resolveCanvasContext(projectKey)
		content := strings.TrimSpace(text)
		if content != "" && canvasSessionID != "" {
			block := fileBlock{
				Path:    "",
				Content: content,
			}
			if canOverwriteSilentAutoCanvasArtifact(canvasCtx) {
				block.Path = canvasCtx.ArtifactTitle
			}
			autoCanvas = a.writeCanvasFileBlock(projectKey, canvasSessionID, block)
			if !autoCanvas && strings.TrimSpace(block.Path) != "" {
				block.Path = ""
				autoCanvas = a.writeCanvasFileBlock(projectKey, canvasSessionID, block)
			}
		}
		renderOnCanvas = autoCanvas
	}
	text = stripLangTags(text)
	chatMarkdown, chatPlain, renderFormat := assistantFinalChatContent(text, renderOnCanvas, autoCanvas)

	a.refreshCanvasFromDisk(projectKey)

	if *persistedID == 0 {
		stored, err := a.store.AddChatMessage(sessionID, "assistant", chatMarkdown, chatPlain, renderFormat)
		if err != nil {
			a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
			a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
			return chatMarkdown
		}
		*persistedID = stored.ID
		*persistedText = chatMarkdown
	} else {
		if err := a.store.UpdateChatMessageContent(*persistedID, chatMarkdown, chatPlain, renderFormat); err != nil {
			a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
			a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
			return chatMarkdown
		}
		*persistedText = chatMarkdown
	}
	a.markProjectOutput(projectKey)
	tid := strings.TrimSpace(turnID)
	if tid == "" {
		tid = fallbackTurnID
	}
	payload := map[string]interface{}{
		"type":             "assistant_output",
		"role":             "assistant",
		"id":               *persistedID,
		"turn_id":          tid,
		"thread_id":        threadID,
		"output_mode":      outputMode,
		"message":          chatMarkdown,
		"render_on_canvas": renderOnCanvas,
	}
	if autoCanvas {
		payload["auto_canvas"] = true
	}
	a.finishCompanionPendingTurn(sessionID, "assistant_turn_completed")
	a.broadcastChatEvent(sessionID, payload)
	if isVoiceOutputMode(outputMode) && strings.TrimSpace(chatPlain) != "" {
		a.broadcastCompanionRuntimeState(projectKey, companionRuntimeSnapshot{
			State:      companionRuntimeStateTalking,
			Reason:     "assistant_output_ready",
			ProjectKey: projectKey,
			TurnID:     tid,
			OutputMode: outputMode,
		})
	} else {
		if project, err := a.store.GetProjectByProjectKey(projectKey); err == nil {
			a.settleCompanionRuntimeState(projectKey, a.loadCompanionConfig(project), "assistant_turn_completed")
		}
	}
	return chatMarkdown
}

func assistantFinalChatContent(text string, _ bool, _ bool) (string, string, string) {
	trimmed := strings.TrimSpace(text)
	companion := strings.TrimSpace(stripCanvasFileMarkers(trimmed))
	return companion, companion, "markdown"
}

func assistantMessageUsesCanvasBlocks(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, ":::file{")
}

type assistantRenderDecision struct {
	RenderOnCanvas bool
	AutoCanvas     bool
}

var assistantParagraphSplitRe = regexp.MustCompile(`\n\s*\n+`)

func assistantCompanionText(text string) string {
	candidate := strings.TrimSpace(text)
	if candidate == "" {
		return ""
	}
	if _, cleaned := parseFileBlocks(candidate); cleaned != "" {
		candidate = cleaned
	}
	candidate = stripLangTags(candidate)
	candidate = stripCanvasFileMarkers(candidate)
	return strings.TrimSpace(candidate)
}

func assistantParagraphCount(text string) int {
	cleaned := assistantCompanionText(text)
	if cleaned == "" {
		return 0
	}
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\n")
	parts := assistantParagraphSplitRe.Split(cleaned, -1)
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	return count
}

func assistantNeedsAutoCanvas(text string) bool {
	return assistantParagraphCount(text) > 1
}

func assistantRenderPlan(text string) assistantRenderDecision {
	return assistantRenderPlanForMode(text, turnOutputModeVoice)
}

func assistantRenderPlanForMode(text string, outputMode string) assistantRenderDecision {
	_ = text
	_ = outputMode
	return assistantRenderDecision{RenderOnCanvas: false, AutoCanvas: false}
}

func assistantSnapshotContent(text string, renderOnCanvas bool, _ bool) (string, string, string) {
	candidate := stripLangTags(strings.TrimSpace(text))
	if candidate == "" {
		return "", "", "markdown"
	}
	chat := assistantCompanionText(candidate)
	if chat == "" {
		if renderOnCanvas {
			return "", "", "text"
		}
		return "", "", "markdown"
	}
	return chat, chat, "markdown"
}

func normalizeTurnOutputMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", turnOutputModeVoice:
		return turnOutputModeVoice
	case turnOutputModeSilent:
		return turnOutputModeSilent
	}
	return turnOutputModeVoice
}

func isVoiceOutputMode(mode string) bool {
	return normalizeTurnOutputMode(mode) == turnOutputModeVoice
}
