package web

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/appserver"
	"github.com/krystophny/tabura/internal/cerebras"
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
		payload["source_label"] = assistantProviderDisplayLabel(cleanSource, "")
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

func parallelCommandResponseMatchesState(evaluation localTurnEvaluation, responseText string) bool {
	expected := normalizeParallelCommandComparisonText(evaluation.fallbackText())
	actual := normalizeParallelCommandComparisonText(responseText)
	if expected == "" || actual == "" {
		return false
	}
	if actual == expected || strings.Contains(actual, expected) {
		return true
	}
	expectedTokens := significantParallelCommandTokens(expected)
	if len(expectedTokens) == 0 {
		return false
	}
	actualTokens := significantParallelCommandTokens(actual)
	for token := range expectedTokens {
		if _, ok := actualTokens[token]; !ok {
			return false
		}
	}
	return true
}

func normalizeParallelCommandComparisonText(raw string) string {
	clean := strings.ToLower(strings.TrimSpace(raw))
	if clean == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		".", " ",
		",", " ",
		";", " ",
		":", " ",
		"!", " ",
		"?", " ",
		"(", " ",
		")", " ",
		"[", " ",
		"]", " ",
		"{", " ",
		"}", " ",
		"\"", " ",
		"'", " ",
		"`", " ",
		"/", " ",
		"\\", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(clean)), " ")
}

func significantParallelCommandTokens(raw string) map[string]struct{} {
	stopwords := map[string]struct{}{
		"a": {}, "an": {}, "and": {}, "at": {}, "for": {}, "in": {}, "into": {}, "of": {}, "on": {}, "the": {}, "to": {}, "with": {},
	}
	tokens := strings.Fields(raw)
	out := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if _, skip := stopwords[token]; skip {
			continue
		}
		out[token] = struct{}{}
	}
	return out
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

	appSess, bindingSessionID, resumed, sessErr := a.getOrCreateAppSession(sessionID, cwd, profile)
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
		_ = a.store.UpdateChatSessionThread(bindingSessionID, appSess.ThreadID())
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
	geminiCh := make(chan geminiTurnResult, 1)
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
	startGemini := func() {
		if a.geminiClient == nil || !a.geminiClient.IsAvailable() {
			return
		}
		go func() {
			geminiCh <- a.runGeminiTurn(ctx, prompt)
		}()
	}

	policy := a.LivePolicy()
	var precomputedLocal *localTurnEvaluation
	if policy.RequiresExplicitAddress() {
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
		startGemini()
		startSpark()
	} else {
		go func() {
			localCh <- a.evaluateLocalTurn(context.Background(), sessionID, session, userText, cursorCtx, turn.captureMode)
		}()
		startCerebras()
		startGemini()
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
	cerebrasResult := cerebrasTurnResult{}
	geminiDone := false
	geminiResult := geminiTurnResult{}
	provisionalEmitted := false
	provisionalText := ""
	commandPayloadsBroadcast := false
	sparkDone := false
	sparkResult := sparkTurnResult{}

	localMetadata := newAssistantResponseMetadata(a.localAssistantProvider(), a.localAssistantModelLabel(), 0)
	cerebrasMetadata := newAssistantResponseMetadata(assistantProviderCerebras, a.cerebrasModelLabel(), 0)
	geminiMetadata := newAssistantResponseMetadata(assistantProviderGoogle, a.geminiModelLabel(), 0)
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
	geminiPending := func() bool {
		return a.geminiClient != nil && a.geminiClient.IsAvailable() && !geminiDone
	}
	tryCommitGeminiClaim := func() bool {
		if !localReady || localEvaluation.isCommand() || localEvaluation.isHighConfidenceLocalAnswer() || !geminiResult.canClaim() {
			return false
		}
		a.emitTurnStage(sessionID, "turn_claimed", turnID, assistantProviderGoogle, geminiResult.text)
		commitFinal(geminiResult.text, newAssistantResponseMetadata(geminiMetadata.Provider, geminiMetadata.ProviderModel, geminiResult.latency), assistantProviderGoogle, "")
		return true
	}
	tryCommitCerebrasClaim := func() bool {
		if !localReady || localEvaluation.isCommand() || !cerebrasResult.canClaim() {
			return false
		}
		a.emitTurnStage(sessionID, "turn_claimed", turnID, assistantProviderCerebras, cerebrasResult.text)
		commitFinal(cerebrasResult.text, newAssistantResponseMetadata(cerebrasMetadata.Provider, cerebrasMetadata.ProviderModel, cerebrasResult.latency), assistantProviderCerebras, "")
		return true
	}
	tryCommitSpark := func() bool {
		if sparkResult.err != nil || strings.TrimSpace(sparkResult.text) == "" {
			return false
		}
		if localReady && localEvaluation.isCommand() && !parallelCommandResponseMatchesState(localEvaluation, sparkResult.text) {
			return false
		}
		commitFinal(sparkResult.text, newAssistantResponseMetadata(responseMeta.Provider, responseMeta.ProviderModel, sparkResult.latency), responseMeta.Provider, sparkResult.threadID)
		return true
	}
	tryCommitCerebrasFallback := func() bool {
		if !cerebrasResult.canFallback() {
			return false
		}
		commitFinal(cerebrasResult.text, newAssistantResponseMetadata(cerebrasMetadata.Provider, cerebrasMetadata.ProviderModel, cerebrasResult.latency), assistantProviderCerebras, "")
		return true
	}
	tryCommitGeminiFallback := func() bool {
		if !geminiResult.canFallback() {
			return false
		}
		commitFinal(geminiResult.text, newAssistantResponseMetadata(geminiMetadata.Provider, geminiMetadata.ProviderModel, geminiResult.latency), assistantProviderGoogle, "")
		return true
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
				if tryCommitGeminiClaim() {
					return true
				}
				if geminiPending() {
					continue
				}
				if tryCommitCerebrasClaim() {
					return true
				}
				if tryCommitSpark() {
					return true
				}
				if tryCommitCerebrasFallback() {
					return true
				}
				if tryCommitGeminiFallback() {
					return true
				}
				if fallback := evaluation.fallbackText(); fallback != "" {
					commitFinal(fallback, localMetadata, assistantProviderLocal, "")
					return true
				}
			}
		case result := <-cerebrasCh:
			cerebrasResult = result
			if errors.Is(result.err, cerebras.ErrQuotaExhausted) {
				a.notifyCerebrasQuotaExhausted(sessionID)
			}
			if !localReady || localEvaluation.isCommand() || localEvaluation.isHighConfidenceLocalAnswer() {
				continue
			}
			if tryCommitGeminiClaim() {
				return true
			}
			if result.canClaim() {
				if geminiPending() {
					continue
				}
				if tryCommitCerebrasClaim() {
					return true
				}
			}
			if sparkDone {
				if tryCommitSpark() {
					return true
				}
				if tryCommitCerebrasFallback() {
					return true
				}
				if tryCommitGeminiFallback() {
					return true
				}
				if fallback := localEvaluation.fallbackText(); fallback != "" {
					commitFinal(fallback, localMetadata, assistantProviderLocal, "")
					return true
				}
			}
		case result := <-geminiCh:
			geminiDone = true
			geminiResult = result
			if !localReady || localEvaluation.isCommand() || localEvaluation.isHighConfidenceLocalAnswer() {
				continue
			}
			if tryCommitGeminiClaim() {
				return true
			}
			if tryCommitCerebrasClaim() {
				return true
			}
			if sparkDone {
				if tryCommitSpark() {
					return true
				}
				if tryCommitCerebrasFallback() {
					return true
				}
				if tryCommitGeminiFallback() {
					return true
				}
				if fallback := localEvaluation.fallbackText(); fallback != "" {
					commitFinal(fallback, localMetadata, assistantProviderLocal, "")
					return true
				}
			}
		case result := <-sparkCh:
			sparkDone = true
			sparkResult = result
			if !localReady {
				continue
			}
			if tryCommitGeminiClaim() {
				return true
			}
			if geminiPending() && !localEvaluation.isCommand() {
				continue
			}
			if tryCommitCerebrasClaim() {
				return true
			}
			if tryCommitSpark() {
				return true
			}
			if localReady {
				if localEvaluation.isCommand() {
					commitFinal(finalParallelCommandText(localEvaluation, provisionalText), localMetadata, assistantProviderLocal, "")
					return true
				}
				if tryCommitCerebrasFallback() {
					return true
				}
				if tryCommitGeminiFallback() {
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
				if tryCommitGeminiClaim() {
					return true
				}
				if tryCommitCerebrasFallback() {
					return true
				}
				if tryCommitGeminiFallback() {
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
