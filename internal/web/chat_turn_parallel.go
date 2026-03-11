package web

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/store"
)

const (
	defaultParallelTurnSparkDeadline = 5 * time.Second
	parallelTurnAckDelay             = 500 * time.Millisecond
)

type sparkTurnResult struct {
	text     string
	threadID string
	err      error
	latency  time.Duration
}

func (a *App) sparkTurnDeadline() time.Duration {
	raw := strings.TrimSpace(os.Getenv("TABURA_SPARK_DEADLINE"))
	if raw == "" {
		return defaultParallelTurnSparkDeadline
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return defaultParallelTurnSparkDeadline
	}
	return parsed
}

func (a *App) emitTurnStage(sessionID, eventType, turnID, source, text string) {
	payload := map[string]interface{}{
		"type":    strings.TrimSpace(eventType),
		"turn_id": strings.TrimSpace(turnID),
	}
	if cleanSource := normalizeAssistantProvider(source); cleanSource != "" {
		payload["source"] = cleanSource
		payload["source_label"] = assistantProviderDisplayLabel(cleanSource)
	}
	if cleanText := strings.TrimSpace(text); cleanText != "" {
		payload["text"] = cleanText
	}
	a.broadcastChatEvent(sessionID, payload)
}

func (a *App) emitParallelProvisional(sessionID, turnID, outputMode, text string, metadata assistantResponseMetadata) {
	payload := map[string]interface{}{
		"type":        "assistant_message",
		"turn_id":     strings.TrimSpace(turnID),
		"output_mode": normalizeTurnOutputMode(outputMode),
		"message":     strings.TrimSpace(text),
	}
	metadata.applyToPayload(payload)
	a.broadcastChatEvent(sessionID, payload)
}

func looksLikeParallelTurnStatusQuery(lower string) bool {
	switch {
	case strings.HasPrefix(lower, "status"),
		strings.HasPrefix(lower, "what's running"),
		strings.HasPrefix(lower, "whats running"),
		strings.HasPrefix(lower, "what is running"),
		strings.HasPrefix(lower, "are you"),
		strings.HasPrefix(lower, "can you"),
		strings.HasPrefix(lower, "did you"),
		strings.HasPrefix(lower, "have you"):
		return true
	default:
		return false
	}
}

func defaultParallelTurnAck(userText string, evaluation localTurnEvaluation) string {
	if ack := strings.TrimSpace(evaluation.ack); ack != "" {
		return ack
	}
	trimmed := strings.TrimSpace(userText)
	if trimmed == "" {
		return "One moment."
	}
	lower := strings.ToLower(trimmed)
	switch {
	case evaluation.isCommand():
		return "On it."
	case looksLikeParallelTurnStatusQuery(lower):
		return "One moment."
	case strings.HasPrefix(lower, "what "), strings.HasPrefix(lower, "how "), strings.HasPrefix(lower, "why "):
		return "Let me check."
	case strings.HasPrefix(lower, "please "), strings.HasPrefix(lower, "run "), strings.HasPrefix(lower, "open "):
		return "On it."
	default:
		return "Let me think."
	}
}

func commandParallelTurnProvisionalText(userText string, evaluation localTurnEvaluation) string {
	if ack := strings.TrimSpace(evaluation.ack); ack != "" {
		return ack
	}
	if text := strings.TrimSpace(evaluation.text); text != "" {
		return text
	}
	return defaultParallelTurnAck(userText, evaluation)
}

func finalParallelCommandText(evaluation localTurnEvaluation, provisionalText string) string {
	if text := evaluation.fallbackText(); text != "" {
		return text
	}
	if text := strings.TrimSpace(provisionalText); text != "" {
		return text
	}
	return "Done."
}

func (a *App) runSparkTurn(ctx context.Context, sessionID string, appSess *appserver.Session, prompt string, profile appServerModelProfile) sparkTurnResult {
	startedAt := time.Now()
	latestMessage := ""
	appResp, err := appSess.SendTurnWithParams(ctx, prompt, profile.Model, profile.TurnParams, func(ev appserver.StreamEvent) {
		switch ev.Type {
		case "assistant_message", "turn_completed":
			if strings.TrimSpace(ev.Message) != "" {
				latestMessage = strings.TrimSpace(ev.Message)
			}
		case "approval_request":
			decision, decisionErr := a.requestAppServerApproval(ctx, sessionID, ev)
			if decisionErr != nil {
				if ev.Respond != nil {
					_ = ev.Respond("cancel")
				}
				return
			}
			if ev.Respond != nil {
				_ = ev.Respond(decision)
			}
		}
	})
	result := sparkTurnResult{
		err:     err,
		latency: time.Since(startedAt),
	}
	if appResp != nil {
		result.threadID = appResp.ThreadID
		result.text = strings.TrimSpace(appResp.Message)
	}
	if result.text == "" {
		result.text = latestMessage
	}
	return result
}

func (a *App) runAssistantTurnParallel(
	sessionID string,
	session store.ChatSession,
	messages []store.ChatMessage,
	userText string,
	cursorCtx *chatCursorContext,
	inkCtx []*chatCanvasInkEvent,
	positionCtx []*chatCanvasPositionEvent,
	turn dequeuedTurn,
) bool {
	if a == nil || a.appServerClient == nil || turn.localOnly {
		return false
	}

	cwd, err := a.effectiveWorkspaceDirForChatSession(session)
	if err != nil {
		return false
	}
	profile := a.appServerModelProfileForProjectKey(session.ProjectKey)
	if strings.TrimSpace(userText) != "" {
		profile = routeProfileForRouting(
			classifyRoutingRoute(userText),
			profile,
			a.appServerSparkReasoningEffort,
		)
	}
	profile = a.appServerProfileForChatSession(session, profile)
	responseMeta := newAssistantResponseMetadata(providerForAppServerProfile(profile), profile.Model, 0)

	appSess, resumed, sessErr := a.getOrCreateAppSession(sessionID, cwd, profile)
	if sessErr != nil {
		return false
	}

	canvasCtx := a.resolveCanvasContext(session.ProjectKey)
	companionCtx := a.loadCompanionPromptContext(session.ProjectKey)
	var prompt string
	if resumed {
		prompt = buildTurnPromptForSessionWithCompanion(sessionID, messages, canvasCtx, companionCtx, turn.outputMode, profile.Alias)
	} else {
		prompt = buildPromptFromHistoryForSessionWithCompanionPolicy(session.Mode, a.yoloModeEnabled(), sessionID, messages, canvasCtx, companionCtx, turn.outputMode, profile.Alias)
		_ = a.store.UpdateChatSessionThread(sessionID, appSess.ThreadID())
	}
	prompt = appendChatCursorPrompt(prompt, cursorCtx)
	prompt = appendCanvasInkPrompt(prompt, inkCtx)
	prompt = appendCanvasPositionPrompt(prompt, positionCtx)
	if strings.TrimSpace(prompt) == "" {
		return false
	}
	prompt = a.applyWorkspacePromptContext(session.ProjectKey, prompt)
	prompt, err = a.applyPreAssistantPromptHook(context.Background(), sessionID, session.ProjectKey, turn.outputMode, session.Mode, prompt)
	if err != nil || strings.TrimSpace(prompt) == "" {
		return false
	}

	ctx, cancel := context.WithCancel(context.Background())
	turnID := randomToken()
	a.registerActiveChatTurn(sessionID, turnID, cancel)
	defer func() {
		cancel()
		a.unregisterActiveChatTurn(sessionID, turnID)
	}()

	go a.watchCanvasFile(ctx, session.ProjectKey)

	localCh := make(chan localTurnEvaluation, 1)
	cerebrasCh := make(chan cerebrasTurnResult, 1)
	sparkCh := make(chan sparkTurnResult, 1)

	startSpark := func() {
		go func() {
			sparkCtx, sparkCancel := context.WithCancel(ctx)
			defer sparkCancel()
			sparkCh <- a.runSparkTurn(sparkCtx, sessionID, appSess, prompt, profile)
		}()
	}
	startCerebras := func() {
		if a.cerebrasClient == nil || !a.cerebrasClient.IsAvailable() {
			return
		}
		go func() {
			cerebrasCh <- a.runCerebrasTurn(ctx, prompt)
		}()
	}

	policy := normalizeLivePolicy(a.LivePolicy().String())
	var precomputedLocal *localTurnEvaluation
	if policy == LivePolicyMeeting {
		evaluation := a.evaluateLocalTurn(context.Background(), sessionID, session, userText, cursorCtx, turn.captureMode)
		precomputedLocal = &evaluation
		if evaluation.suppressesResponse() {
			a.finishCompanionPendingTurn(sessionID, "assistant_turn_suppressed")
			return true
		}
	}

	a.broadcastChatEvent(sessionID, map[string]interface{}{
		"type":        "turn_started",
		"turn_id":     turnID,
		"output_mode": turn.outputMode,
	})
	a.markCompanionThinking(sessionID, session.ProjectKey, turnID, turn.outputMode, "assistant_turn_started")

	if precomputedLocal != nil {
		localCh <- *precomputedLocal
		startCerebras()
		startSpark()
	} else {
		go func() {
			localCh <- a.evaluateLocalTurn(context.Background(), sessionID, session, userText, cursorCtx, turn.captureMode)
		}()
		startCerebras()
		startSpark()
	}

	ackTimer := time.NewTimer(parallelTurnAckDelay)
	defer ackTimer.Stop()
	deadlineTimer := time.NewTimer(a.sparkTurnDeadline())
	defer deadlineTimer.Stop()

	persistedAssistantID := int64(0)
	persistedAssistantText := ""
	localReady := false
	localEvaluation := localTurnEvaluation{}
	cerebrasDone := false
	cerebrasResult := cerebrasTurnResult{}
	provisionalEmitted := false
	provisionalText := ""
	commandPayloadsBroadcast := false
	sparkDone := false
	sparkResult := sparkTurnResult{}

	localMetadata := newAssistantResponseMetadata(assistantProviderLocal, a.localAssistantModelLabel(), 0)
	cerebrasMetadata := newAssistantResponseMetadata(assistantProviderCerebras, a.cerebrasModelLabel(), 0)

	commitFinal := func(text string, metadata assistantResponseMetadata, source string, threadID string) {
		a.emitTurnStage(sessionID, "turn_committed", turnID, source, text)
		a.emitTurnStage(sessionID, "turn_source", turnID, source, "")
		a.finalizeAssistantResponseWithMetadata(
			sessionID,
			session.ProjectKey,
			text,
			&persistedAssistantID,
			&persistedAssistantText,
			turnID,
			turnID,
			threadID,
			turn.outputMode,
			metadata,
		)
	}

	for {
		select {
		case evaluation := <-localCh:
			localReady = true
			localEvaluation = evaluation
			if evaluation.suppressesResponse() {
				a.finishCompanionPendingTurn(sessionID, "assistant_turn_suppressed")
				return true
			}
			if evaluation.isHighConfidenceLocalAnswer() {
				a.emitTurnStage(sessionID, "turn_claimed", turnID, assistantProviderLocal, evaluation.text)
				commitFinal(evaluation.text, localMetadata, assistantProviderLocal, "")
				return true
			}
			if evaluation.isCommand() {
				nextText := commandParallelTurnProvisionalText(userText, evaluation)
				previousText := provisionalText
				provisionalText = nextText
				if !commandPayloadsBroadcast {
					for _, actionPayload := range evaluation.payloads {
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
					commandPayloadsBroadcast = true
				}
				if !provisionalEmitted || previousText != nextText {
					a.emitTurnStage(sessionID, "turn_provisional", turnID, assistantProviderLocal, provisionalText)
					a.emitParallelProvisional(sessionID, turnID, turn.outputMode, provisionalText, localMetadata)
					provisionalEmitted = true
				}
				if sparkDone && (sparkResult.err != nil || strings.TrimSpace(sparkResult.text) == "") {
					commitFinal(finalParallelCommandText(evaluation, provisionalText), localMetadata, assistantProviderLocal, "")
					return true
				}
			} else if sparkDone {
				if cerebrasDone && cerebrasResult.canClaim() {
					a.emitTurnStage(sessionID, "turn_claimed", turnID, assistantProviderCerebras, cerebrasResult.text)
					commitFinal(cerebrasResult.text, newAssistantResponseMetadata(cerebrasMetadata.Provider, cerebrasMetadata.ProviderModel, cerebrasResult.latency), assistantProviderCerebras, "")
					return true
				}
				if sparkResult.err == nil && strings.TrimSpace(sparkResult.text) != "" {
					commitFinal(sparkResult.text, newAssistantResponseMetadata(responseMeta.Provider, responseMeta.ProviderModel, sparkResult.latency), responseMeta.Provider, sparkResult.threadID)
					return true
				}
				if cerebrasDone && cerebrasResult.canFallback() {
					commitFinal(cerebrasResult.text, newAssistantResponseMetadata(cerebrasMetadata.Provider, cerebrasMetadata.ProviderModel, cerebrasResult.latency), assistantProviderCerebras, "")
					return true
				}
				if fallback := evaluation.fallbackText(); fallback != "" {
					commitFinal(fallback, localMetadata, assistantProviderLocal, "")
					return true
				}
			}
		case result := <-cerebrasCh:
			cerebrasDone = true
			cerebrasResult = result
			if !localReady || localEvaluation.isCommand() || localEvaluation.isHighConfidenceLocalAnswer() {
				continue
			}
			if result.canClaim() {
				a.emitTurnStage(sessionID, "turn_claimed", turnID, assistantProviderCerebras, result.text)
				commitFinal(result.text, newAssistantResponseMetadata(cerebrasMetadata.Provider, cerebrasMetadata.ProviderModel, result.latency), assistantProviderCerebras, "")
				return true
			}
		case result := <-sparkCh:
			sparkDone = true
			sparkResult = result
			if !localReady {
				continue
			}
			if cerebrasDone && cerebrasResult.canClaim() && !localEvaluation.isCommand() {
				a.emitTurnStage(sessionID, "turn_claimed", turnID, assistantProviderCerebras, cerebrasResult.text)
				commitFinal(cerebrasResult.text, newAssistantResponseMetadata(cerebrasMetadata.Provider, cerebrasMetadata.ProviderModel, cerebrasResult.latency), assistantProviderCerebras, "")
				return true
			}
			if result.err == nil && strings.TrimSpace(result.text) != "" {
				commitFinal(result.text, newAssistantResponseMetadata(responseMeta.Provider, responseMeta.ProviderModel, result.latency), responseMeta.Provider, result.threadID)
				return true
			}
			if localReady {
				if localEvaluation.isCommand() {
					commitFinal(finalParallelCommandText(localEvaluation, provisionalText), localMetadata, assistantProviderLocal, "")
					return true
				}
				if cerebrasDone && cerebrasResult.canFallback() {
					commitFinal(cerebrasResult.text, newAssistantResponseMetadata(cerebrasMetadata.Provider, cerebrasMetadata.ProviderModel, cerebrasResult.latency), assistantProviderCerebras, "")
					return true
				}
				if fallback := localEvaluation.fallbackText(); fallback != "" {
					commitFinal(fallback, localMetadata, assistantProviderLocal, "")
					return true
				}
			}
			if result.err != nil && errors.Is(result.err, context.Canceled) {
				return true
			}
			if result.err != nil {
				a.closeAppSession(sessionID)
				a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
				errText := normalizeAssistantError(result.err)
				_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
				a.broadcastChatEvent(sessionID, map[string]interface{}{
					"type":    "error",
					"error":   errText,
					"turn_id": turnID,
				})
				return true
			}
		case <-ackTimer.C:
			if provisionalEmitted || sparkDone {
				continue
			}
			if localReady && (localEvaluation.isHighConfidenceLocalAnswer() || localEvaluation.isCommand()) {
				continue
			}
			provisionalText = defaultParallelTurnAck(userText, localEvaluation)
			a.emitTurnStage(sessionID, "turn_provisional", turnID, assistantProviderLocal, provisionalText)
			a.emitParallelProvisional(sessionID, turnID, turn.outputMode, provisionalText, localMetadata)
			provisionalEmitted = true
		case <-deadlineTimer.C:
			if localReady {
				if localEvaluation.isCommand() {
					commitFinal(finalParallelCommandText(localEvaluation, provisionalText), localMetadata, assistantProviderLocal, "")
					return true
				}
				if cerebrasDone && cerebrasResult.canFallback() {
					commitFinal(cerebrasResult.text, newAssistantResponseMetadata(cerebrasMetadata.Provider, cerebrasMetadata.ProviderModel, cerebrasResult.latency), assistantProviderCerebras, "")
					return true
				}
				if fallback := localEvaluation.fallbackText(); fallback != "" {
					commitFinal(fallback, localMetadata, assistantProviderLocal, "")
					return true
				}
			}
			a.closeAppSession(sessionID)
			a.finishCompanionPendingTurn(sessionID, "assistant_turn_failed")
			errText := "assistant request timed out"
			_, _ = a.store.AddChatMessage(sessionID, "system", errText, errText, "text")
			a.broadcastChatEvent(sessionID, map[string]interface{}{
				"type":    "error",
				"error":   errText,
				"turn_id": turnID,
			})
			return true
		}
	}
}
