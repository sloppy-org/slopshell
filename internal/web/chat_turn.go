package web

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/sloppy-org/slopshell/internal/appserver"
	"github.com/sloppy-org/slopshell/internal/modelprofile"
	"github.com/sloppy-org/slopshell/internal/plugins"
	"github.com/sloppy-org/slopshell/internal/store"
)

func (a *App) runAssistantTurn(sessionID string, turn dequeuedTurn) {
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
	inkCtx := a.chatCanvasInk.consume(sessionID)
	positionCtx := a.chatCanvasPositions.consume(sessionID)
	cursorCtx := turn.cursor
	userText := queuedUserMessage(messages, turn.messageID)
	directives := parseTurnRoutingDirectives(userText)
	promptText := directives.PromptText
	if strings.TrimSpace(promptText) == "" {
		promptText = strings.TrimSpace(userText)
	}
	if !turn.fastMode && a.maybeRunSilentLiveEditTurn(sessionID, session, promptText, cursorCtx, positionCtx, turn.captureMode) {
		return
	}
	baseProfile := a.appServerModelProfileForWorkspacePath(session.WorkspacePath)
	turnProfile := baseProfile
	if strings.TrimSpace(promptText) != "" {
		turnProfile = routeProfileForRouting(
			directives.ModelAlias,
			baseProfile,
			a.appServerSparkReasoningEffort,
			directives.ReasoningEffort,
		)
	}
	baseProfile = a.appServerProfileForChatSession(session, baseProfile)
	turnProfile = a.appServerProfileForChatSession(session, turnProfile)
	req := &assistantTurnRequest{
		sessionID:       sessionID,
		session:         session,
		messages:        messages,
		canvasCtx:       a.resolveCanvasContext(session.WorkspacePath),
		userText:        userText,
		promptText:      promptText,
		cursorCtx:       cursorCtx,
		inkCtx:          inkCtx,
		positionCtx:     positionCtx,
		captureMode:     turn.captureMode,
		outputMode:      turn.outputMode,
		localOnly:       turn.localOnly,
		fastMode:        turn.fastMode,
		messageID:       turn.messageID,
		turnModel:       directives.ModelAlias,
		detailRequested: directives.DetailRequested,
		transientRemote: directives.ModelAliasExplicit && directives.ModelAlias != "" && directives.ModelAlias != modelprofile.AliasLocal,
		reasoningEffort: directives.ReasoningEffort,
		baseProfile:     baseProfile,
		turnProfile:     turnProfile,
	}
	a.assistantBackendForTurn(req).run(req)
}

func (a *App) runCodexAssistantTurn(req *assistantTurnRequest) {
	if a == nil || req == nil {
		return
	}
	if a.appServerClient == nil {
		errText := "app-server is not configured"
		_, _ = a.store.AddChatMessage(req.sessionID, "system", errText, errText, "text")
		a.finishCompanionPendingTurn(req.sessionID, "assistant_turn_failed")
		a.broadcastChatEvent(req.sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}
	cwd, err := a.effectiveWorkspaceDirForChatSession(req.session)
	if err != nil {
		a.finishCompanionPendingTurn(req.sessionID, "assistant_turn_failed")
		a.broadcastChatEvent(req.sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
		return
	}
	if req.transientRemote || req.fastMode {
		a.runAssistantTurnLegacy(
			req.sessionID,
			req.session,
			req.messages,
			req.messageID,
			req.promptText,
			req.cursorCtx,
			req.inkCtx,
			req.positionCtx,
			req.outputMode,
			req.baseProfile,
			req.turnProfile,
			req.fastMode,
			false,
		)
		return
	}
	turnStartedAt := time.Now()
	responseMeta := newAssistantResponseMetadata(providerForAppServerProfile(req.turnProfile), req.turnProfile.Model, 0)
	appSess, bindingSessionID, resumed, sessErr := a.getOrCreateAppSession(req.sessionID, cwd, req.baseProfile)
	if sessErr != nil {
		a.runAssistantTurnLegacy(req.sessionID, req.session, req.messages, req.messageID, req.promptText, req.cursorCtx, req.inkCtx, req.positionCtx, req.outputMode, req.baseProfile, req.turnProfile, req.fastMode, true)
		return
	}

	canvasCtx := a.resolveCanvasContext(req.session.WorkspacePath)
	companionCtx := a.loadCompanionPromptContextForTurn(req.sessionID, req.session.WorkspacePath)
	var prompt string
	if resumed {
		prompt = buildTurnPromptForSessionWithCompanion(req.sessionID, withQueuedUserMessage(req.messages, req.messageID, req.promptText), canvasCtx, companionCtx, req.outputMode, req.turnProfile.Alias)
	} else {
		prompt = buildPromptFromHistoryForSessionWithCompanionPolicy(req.session.Mode, a.yoloModeEnabled(), req.sessionID, withQueuedUserMessage(req.messages, req.messageID, req.promptText), canvasCtx, companionCtx, req.outputMode, req.turnProfile.Alias)
		_ = a.store.UpdateChatSessionThread(bindingSessionID, appSess.ThreadID())
	}
	prompt = appendChatCursorPrompt(prompt, req.cursorCtx)
	prompt = appendCanvasInkPrompt(prompt, req.inkCtx)
	prompt = appendCanvasPositionPrompt(prompt, req.positionCtx)
	if strings.TrimSpace(prompt) == "" {
		a.broadcastChatEvent(req.sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
		return
	}
	prompt = a.applyWorkspacePromptContext(req.session.WorkspacePath, prompt)
	prompt, err = a.applyPreAssistantPromptHook(context.Background(), req.sessionID, req.session.WorkspacePath, req.outputMode, req.session.Mode, prompt)
	if err != nil {
		errText := err.Error()
		_, _ = a.store.AddChatMessage(req.sessionID, "system", errText, errText, "text")
		a.broadcastChatEvent(req.sessionID, map[string]interface{}{"type": "error", "error": errText})
		return
	}
	if strings.TrimSpace(prompt) == "" {
		a.broadcastChatEvent(req.sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
		return
	}
	turnInput := buildAppServerTurnInput(prompt, latestCanvasPositionVisualAttachment(req.positionCtx))

	ctx, cancel := context.WithCancel(context.Background())
	runID := randomToken()
	a.registerActiveChatTurn(req.sessionID, runID, cancel)
	defer func() {
		cancel()
		a.unregisterActiveChatTurn(req.sessionID, runID)
	}()

	go a.watchCanvasFile(ctx, req.session.WorkspacePath)

	latestMessage := ""
	latestTurnID := ""
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	persistedAssistantPlain := ""
	persistedAssistantFormat := ""
	persistWriteFailed := false
	snapshotOpts := func() []store.ChatMessageOption {
		meta := responseMeta
		meta.ProviderLatency = int(time.Since(turnStartedAt) / time.Millisecond)
		return meta.storeOptions()
	}

	persistAssistantSnapshot := func(text string, renderOnCanvas bool, autoCanvas bool) {
		candidateMarkdown, candidatePlain, candidateFormat := assistantSnapshotContent(text, renderOnCanvas, autoCanvas)
		if candidateMarkdown == "" && candidatePlain == "" {
			return
		}
		if persistedAssistantID == 0 {
			storedAssistant, storeErr := a.store.AddChatMessage(req.sessionID, "assistant", candidateMarkdown, candidatePlain, candidateFormat, snapshotOpts()...)
			if storeErr != nil {
				if !persistWriteFailed {
					persistWriteFailed = true
					a.broadcastChatEvent(req.sessionID, map[string]interface{}{
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
		if storeErr := a.store.UpdateChatMessageContent(persistedAssistantID, candidateMarkdown, candidatePlain, candidateFormat, snapshotOpts()...); storeErr != nil {
			if !persistWriteFailed {
				persistWriteFailed = true
				a.broadcastChatEvent(req.sessionID, map[string]interface{}{
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

	appResp, err := appSess.SendTurnInputWithParams(ctx, turnInput, req.turnProfile.Model, req.turnProfile.TurnParams, func(ev appserver.StreamEvent) {
		payload := map[string]interface{}{
			"type":        ev.Type,
			"thread_id":   ev.ThreadID,
			"turn_id":     ev.TurnID,
			"output_mode": req.outputMode,
		}
		shouldBroadcast := true
		var renderCommand map[string]interface{}
		switch ev.Type {
		case "thread_started":
			// Thread ID already stored on session open.
		case "turn_started":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			a.markCompanionThinking(req.sessionID, req.session.WorkspacePath, latestTurnID, req.outputMode, "assistant_turn_started")
		case "assistant_message":
			latestMessage = ev.Message
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(ev.Message, req.outputMode)
			persistAssistantSnapshot(ev.Message, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = ev.Message
			payload["delta"] = ev.Delta
			renderCommand = assistantRenderChatCommand(latestTurnID, req.outputMode, ev.Message)
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
			renderPlan := assistantRenderPlanForMode(latestMessage, req.outputMode)
			persistAssistantSnapshot(latestMessage, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = latestMessage
			renderCommand = assistantRenderChatCommand(latestTurnID, req.outputMode, latestMessage)
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
		case "approval_request":
			decision, decisionErr := a.requestAppServerApproval(ctx, req.sessionID, ev)
			if decisionErr != nil {
				if ev.Respond != nil {
					_ = ev.Respond("cancel")
				}
				shouldBroadcast = false
				return
			}
			if ev.Respond != nil {
				if respondErr := ev.Respond(decision); respondErr != nil {
					shouldBroadcast = false
					return
				}
			}
			shouldBroadcast = false
			return
		case "error":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			shouldBroadcast = false
		}
		if shouldBroadcast {
			a.broadcastChatEvent(req.sessionID, payload)
			if renderCommand != nil {
				a.broadcastChatEvent(req.sessionID, renderCommand)
			}
		}
	})
	if err != nil {
		a.closeAppSession(req.sessionID)
		if errors.Is(err, context.Canceled) {
			a.finishCompanionPendingTurn(req.sessionID, "assistant_turn_cancelled")
			a.broadcastChatEvent(req.sessionID, map[string]interface{}{
				"type":    "turn_cancelled",
				"turn_id": latestTurnID,
			})
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			a.finishCompanionPendingTurn(req.sessionID, "assistant_turn_failed")
			errText := "assistant request timed out"
			_, _ = a.store.AddChatMessage(req.sessionID, "system", errText, errText, "text")
			payload := map[string]interface{}{
				"type":  "error",
				"error": errText,
			}
			if strings.TrimSpace(latestTurnID) != "" {
				payload["turn_id"] = latestTurnID
			}
			a.broadcastChatEvent(req.sessionID, payload)
			return
		}
		a.finishCompanionPendingTurn(req.sessionID, "assistant_turn_failed")
		errText := normalizeAssistantError(err)
		_, _ = a.store.AddChatMessage(req.sessionID, "system", errText, errText, "text")
		payload := map[string]interface{}{
			"type":  "error",
			"error": errText,
		}
		if strings.TrimSpace(latestTurnID) != "" {
			payload["turn_id"] = latestTurnID
		}
		a.broadcastChatEvent(req.sessionID, payload)
		return
	}

	assistantText := strings.TrimSpace(appResp.Message)
	if assistantText == "" {
		assistantText = strings.TrimSpace(latestMessage)
	}
	if assistantText == "" {
		assistantText = "(assistant returned no content)"
	}

	assistantText = a.finalizeAssistantResponseWithMetadata(
		req.sessionID,
		req.session.WorkspacePath,
		assistantText,
		&persistedAssistantID,
		&persistedAssistantText,
		appResp.TurnID,
		latestTurnID,
		appResp.ThreadID,
		req.outputMode,
		newAssistantResponseMetadata(responseMeta.Provider, responseMeta.ProviderModel, time.Since(turnStartedAt)),
	)
	_ = assistantText
}

func (a *App) finalizeHandledLocalActionTurn(sessionID string, workspacePath string, outputMode string, turnStartedAt time.Time, actionMessage string, actionPayloads []map[string]interface{}) {
	if a == nil {
		return
	}
	if suppressLocalAssistantResponse(actionPayloads) {
		a.finishCompanionPendingTurn(sessionID, "assistant_turn_suppressed")
		return
	}
	runID := randomToken()
	a.broadcastChatEvent(sessionID, map[string]interface{}{
		"type":    "turn_started",
		"turn_id": runID,
	})
	assistantText := strings.TrimSpace(actionMessage)
	if assistantText == "" {
		assistantText = "Done."
	}
	for _, actionPayload := range actionPayloads {
		if actionPayload == nil {
			continue
		}
		a.broadcastSystemActionEvent(sessionID, actionPayload)
	}
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	a.finalizeAssistantResponseWithMetadata(
		sessionID,
		workspacePath,
		assistantText,
		&persistedAssistantID,
		&persistedAssistantText,
		"",
		runID,
		"",
		outputMode,
		newAssistantResponseMetadata(a.localAssistantProvider(), a.localAssistantModelLabel(), time.Since(turnStartedAt)),
	)
}

func (a *App) tryRunLocalSystemActionTurn(sessionID string, session store.ChatSession, userText string, cursorCtx *chatCursorContext, captureMode string, outputMode string, localOnly bool) bool {
	if strings.TrimSpace(userText) == "" {
		return false
	}
	turnStartedAt := time.Now()
	actionMessage, actionPayloads, handled := a.classifyAndExecuteSystemActionForTurn(context.Background(), sessionID, session, userText, cursorCtx, captureMode)
	if !handled && !localOnly {
		return false
	}
	if !handled {
		a.finalizeHandledLocalActionTurn(sessionID, session.WorkspacePath, outputMode, turnStartedAt, "I can only handle system actions in local-only mode.", nil)
		return true
	}
	a.finalizeHandledLocalActionTurn(sessionID, session.WorkspacePath, outputMode, turnStartedAt, actionMessage, actionPayloads)
	return true
}

func suppressLocalAssistantResponse(payloads []map[string]interface{}) bool {
	for _, payload := range payloads {
		if payload == nil {
			continue
		}
		if suppress, ok := parseOptionalBool(payload["suppress_response"]); ok && suppress {
			return true
		}
	}
	return false
}

func systemActionEventType(actionPayload map[string]interface{}) string {
	actionType, _ := actionPayload["type"].(string)
	switch strings.TrimSpace(actionType) {
	case "confirmation_required":
		return "system_action_confirmation_required"
	case "system_action_suppressed":
		return "system_action_suppressed"
	default:
		return "system_action"
	}
}

func (a *App) broadcastSystemActionEvent(sessionID string, actionPayload map[string]interface{}) {
	if a == nil || actionPayload == nil {
		return
	}
	a.broadcastChatEvent(sessionID, map[string]interface{}{
		"type":   systemActionEventType(actionPayload),
		"action": actionPayload,
	})
}

// runAssistantTurnLegacy is the single-shot fallback when persistent session
// fails to connect. Each call creates a new WS + thread.
func (a *App) runAssistantTurnLegacy(sessionID string, session store.ChatSession, messages []store.ChatMessage, messageID int64, promptText string, cursorCtx *chatCursorContext, inkCtx []*chatCanvasInkEvent, positionCtx []*chatCanvasPositionEvent, outputMode string, baseProfile appServerModelProfile, turnProfile appServerModelProfile, fastMode bool, persistThread bool) {
	bindingSession, workspace, err := a.appSessionBindingForChatSessionID(sessionID)
	if err != nil {
		a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
		a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
		return
	}
	cwd := workspace.DirPath
	turnStartedAt := time.Now()
	responseMeta := newAssistantResponseMetadata(providerForAppServerProfile(turnProfile), turnProfile.Model, 0)
	prompt := strings.TrimSpace(promptText)
	var visual *chatVisualAttachment
	if !fastMode {
		visual = latestCanvasPositionVisualAttachment(positionCtx)
		canvasCtx := a.resolveCanvasContext(session.WorkspacePath)
		prompt = buildPromptFromHistoryForSessionWithPolicy(session.Mode, a.yoloModeEnabled(), sessionID, withQueuedUserMessage(messages, messageID, promptText), canvasCtx, outputMode, turnProfile.Alias)
		prompt = appendChatCursorPrompt(prompt, cursorCtx)
		prompt = appendCanvasInkPrompt(prompt, inkCtx)
		prompt = appendCanvasPositionPrompt(prompt, positionCtx)
		if strings.TrimSpace(prompt) == "" {
			a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": "empty prompt"})
			return
		}
		prompt = a.applyWorkspacePromptContext(session.WorkspacePath, prompt)
		prompt, err = a.applyPreAssistantPromptHook(context.Background(), sessionID, session.WorkspacePath, outputMode, session.Mode, prompt)
		if err != nil {
			errText := err.Error()
			_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
			a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": errText})
			return
		}
	}
	turnInput := buildAppServerTurnInput(prompt, visual)
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

	go a.watchCanvasFile(ctx, session.WorkspacePath)

	latestMessage := ""
	latestTurnID := ""
	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	persistedAssistantPlain := ""
	persistedAssistantFormat := ""
	persistWriteFailed := false
	snapshotOpts := func() []store.ChatMessageOption {
		meta := responseMeta
		meta.ProviderLatency = int(time.Since(turnStartedAt) / time.Millisecond)
		return meta.storeOptions()
	}
	persistAssistantSnapshot := func(text string, renderOnCanvas bool, autoCanvas bool) {
		candidateMarkdown, candidatePlain, candidateFormat := assistantSnapshotContent(text, renderOnCanvas, autoCanvas)
		if candidateMarkdown == "" && candidatePlain == "" {
			return
		}
		if persistedAssistantID == 0 {
			storedAssistant, storeErr := a.store.AddChatMessage(sessionID, "assistant", candidateMarkdown, candidatePlain, candidateFormat, snapshotOpts()...)
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
		if storeErr := a.store.UpdateChatMessageContent(persistedAssistantID, candidateMarkdown, candidatePlain, candidateFormat, snapshotOpts()...); storeErr != nil {
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

	requestModel := strings.TrimSpace(baseProfile.Model)
	requestThreadParams := baseProfile.ThreadParams
	if !persistThread {
		requestModel = strings.TrimSpace(turnProfile.Model)
		requestThreadParams = turnProfile.ThreadParams
	}
	appResp, err := a.appServerClient.SendPromptStream(ctx, appserver.PromptRequest{
		CWD:          cwd,
		Prompt:       prompt,
		TurnInput:    turnInput,
		Model:        requestModel,
		TurnModel:    turnProfile.Model,
		ThreadParams: requestThreadParams,
		TurnParams:   turnProfile.TurnParams,
	}, func(ev appserver.StreamEvent) {
		payload := map[string]interface{}{
			"type":        ev.Type,
			"thread_id":   ev.ThreadID,
			"turn_id":     ev.TurnID,
			"output_mode": outputMode,
		}
		shouldBroadcast := true
		var renderCommand map[string]interface{}
		switch ev.Type {
		case "thread_started":
			if persistThread && strings.TrimSpace(ev.ThreadID) != "" {
				_ = a.store.UpdateChatSessionThread(bindingSession.ID, ev.ThreadID)
			}
		case "turn_started":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			a.markCompanionThinking(sessionID, session.WorkspacePath, latestTurnID, outputMode, "assistant_turn_started")
		case "assistant_message":
			latestMessage = ev.Message
			latestTurnID = ev.TurnID
			renderPlan := assistantRenderPlanForMode(ev.Message, outputMode)
			persistAssistantSnapshot(ev.Message, renderPlan.RenderOnCanvas, renderPlan.AutoCanvas)
			payload["message"] = ev.Message
			payload["delta"] = ev.Delta
			renderCommand = assistantRenderChatCommand(latestTurnID, outputMode, ev.Message)
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
			renderCommand = assistantRenderChatCommand(latestTurnID, outputMode, latestMessage)
			if renderPlan.RenderOnCanvas {
				payload["render_on_canvas"] = true
			}
			if renderPlan.AutoCanvas {
				payload["auto_canvas"] = true
			}
		case "approval_request":
			decision, decisionErr := a.requestAppServerApproval(ctx, sessionID, ev)
			if decisionErr != nil {
				if ev.Respond != nil {
					_ = ev.Respond("cancel")
				}
				shouldBroadcast = false
				return
			}
			if ev.Respond != nil {
				if respondErr := ev.Respond(decision); respondErr != nil {
					shouldBroadcast = false
					return
				}
			}
			shouldBroadcast = false
			return
		case "error":
			if strings.TrimSpace(ev.TurnID) != "" {
				latestTurnID = ev.TurnID
			}
			shouldBroadcast = false
		}
		if shouldBroadcast {
			a.broadcastChatEvent(sessionID, payload)
			if renderCommand != nil {
				a.broadcastChatEvent(sessionID, renderCommand)
			}
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

	assistantText = a.finalizeAssistantResponseWithMetadata(
		sessionID,
		session.WorkspacePath,
		assistantText,
		&persistedAssistantID,
		&persistedAssistantText,
		appResp.TurnID,
		latestTurnID,
		appResp.ThreadID,
		outputMode,
		newAssistantResponseMetadata(responseMeta.Provider, responseMeta.ProviderModel, time.Since(turnStartedAt)),
	)
	_ = assistantText
}

// finalizeAssistantResponse handles post-processing shared by both turn paths.
// Assistant prose remains chat-first in every mode; only explicit file-backed
// artifact output is allowed onto the canvas.
func (a *App) finalizeAssistantResponse(
	sessionID, workspacePath, text string,
	persistedID *int64, persistedText *string,
	turnID, fallbackTurnID, threadID string,
	outputMode string,
) string {
	return a.finalizeAssistantResponseWithMetadata(
		sessionID,
		workspacePath,
		text,
		persistedID,
		persistedText,
		turnID,
		fallbackTurnID,
		threadID,
		outputMode,
		assistantResponseMetadata{},
	)
}

func (a *App) finalizeAssistantResponseWithMetadata(
	sessionID, workspacePath, text string,
	persistedID *int64, persistedText *string,
	turnID, fallbackTurnID, threadID string,
	outputMode string,
	metadata assistantResponseMetadata,
) string {
	postResult := a.applyPluginHook(context.Background(), plugins.HookRequest{
		Hook:          plugins.HookChatPostAssistantReply,
		SessionID:     sessionID,
		WorkspacePath: workspacePath,
		OutputMode:    outputMode,
		Text:          text,
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
	text, positionPrompt := stripAssistantPositionRequest(text)

	outputMode = normalizeTurnOutputMode(outputMode)
	canvasSessionID := a.resolveCanvasSessionID(workspacePath)
	renderOnCanvas := false
	content := strings.TrimSpace(text)
	blocks, cleaned := parseFileBlocks(content)
	if len(blocks) > 0 && canvasSessionID != "" {
		if a.isResearchTurn(sessionID) {
			blocks = normalizeResearchFileBlocks(blocks, researchArtifactRoot(sessionID))
		}
		renderOnCanvas = a.executeFileBlocks(workspacePath, canvasSessionID, blocks)
	}
	text = cleaned
	text = stripLangTags(text)
	chatMarkdown, chatPlain, renderFormat := assistantFinalChatContent(text, renderOnCanvas, false)

	a.refreshCanvasFromDisk(workspacePath)

	if *persistedID == 0 {
		stored, err := a.store.AddChatMessage(sessionID, "assistant", chatMarkdown, chatPlain, renderFormat, metadata.storeOptions()...)
		if err != nil {
			a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
			a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
			return chatMarkdown
		}
		*persistedID = stored.ID
		*persistedText = chatMarkdown
	} else {
		if err := a.store.UpdateChatMessageContent(*persistedID, chatMarkdown, chatPlain, renderFormat, metadata.storeOptions()...); err != nil {
			a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
			a.broadcastChatEvent(sessionID, map[string]interface{}{"type": "error", "error": err.Error()})
			return chatMarkdown
		}
		*persistedText = chatMarkdown
	}
	a.markWorkspaceOutput(workspacePath)
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
	metadata.applyToPayload(payload)
	a.finishCompanionPendingTurn(sessionID, "assistant_turn_completed")
	a.broadcastChatEvent(sessionID, payload)
	if renderCommand := assistantRenderChatCommand(tid, outputMode, chatMarkdown); renderCommand != nil {
		metadata.applyToPayload(renderCommand)
		a.broadcastChatEvent(sessionID, renderCommand)
	}
	a.maybeNotifyCompletedTurn(sessionID, workspacePath, chatPlain)
	if strings.TrimSpace(positionPrompt) != "" {
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":        "request_position",
			"turn_id":     tid,
			"output_mode": outputMode,
			"prompt":      positionPrompt,
		})
	}
	if isVoiceOutputMode(outputMode) && strings.TrimSpace(chatPlain) != "" {
		a.broadcastCompanionRuntimeState(workspacePath, companionRuntimeSnapshot{
			State:         companionRuntimeStateTalking,
			Reason:        "assistant_output_ready",
			WorkspacePath: workspacePath,
			TurnID:        tid,
			OutputMode:    outputMode,
		})
	} else {
		if project, err := a.store.GetWorkspaceByStoredPath(workspacePath); err == nil {
			a.settleCompanionRuntimeState(workspacePath, a.loadCompanionConfig(project), "assistant_turn_completed")
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
var assistantRequestPositionRe = regexp.MustCompile(`(?s)\[\[request_position:(.*?)\]\]`)

func stripAssistantPositionRequest(text string) (string, string) {
	matches := assistantRequestPositionRe.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text, ""
	}
	prompt := ""
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if candidate := strings.TrimSpace(match[1]); candidate != "" {
			prompt = candidate
		}
	}
	cleaned := strings.TrimSpace(assistantRequestPositionRe.ReplaceAllString(text, ""))
	return cleaned, prompt
}

func assistantCompanionText(text string) string {
	candidate := strings.TrimSpace(text)
	if candidate == "" {
		return ""
	}
	candidate, _ = stripAssistantPositionRequest(candidate)
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
	candidate, _ = stripAssistantPositionRequest(candidate)
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
