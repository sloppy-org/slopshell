package web

import (
	"context"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

type assistantTurnRequest struct {
	sessionID   string
	session     store.ChatSession
	messages    []store.ChatMessage
	userText    string
	cursorCtx   *chatCursorContext
	inkCtx      []*chatCanvasInkEvent
	positionCtx []*chatCanvasPositionEvent
	captureMode string
	outputMode  string
	localOnly   bool
	turnModel   string
	baseProfile appServerModelProfile
	turnProfile appServerModelProfile
}

type assistantTurnBackend interface {
	mode() string
	run(*assistantTurnRequest)
}

type localAssistantBackend struct {
	app        *App
	evaluation *localTurnEvaluation
}

func (b *localAssistantBackend) mode() string {
	return assistantModeLocal
}

func (b *localAssistantBackend) run(req *assistantTurnRequest) {
	if b == nil || b.app == nil || req == nil {
		return
	}
	b.app.runLocalAssistantTurn(req, b.evaluation)
}

type codexAssistantBackend struct {
	app *App
}

func (b *codexAssistantBackend) mode() string {
	return assistantModeCodex
}

func (b *codexAssistantBackend) run(req *assistantTurnRequest) {
	if b == nil || b.app == nil || req == nil {
		return
	}
	b.app.runCodexAssistantTurn(req)
}

func (a *App) assistantBackendForTurn(req *assistantTurnRequest) assistantTurnBackend {
	if a == nil {
		return &localAssistantBackend{}
	}
	if req == nil {
		return &localAssistantBackend{app: a}
	}
	if req.localOnly {
		return &localAssistantBackend{app: a}
	}
	switch a.assistantRoutingMode() {
	case assistantModeLocal:
		return &localAssistantBackend{app: a}
	case assistantModeCodex:
		return &codexAssistantBackend{app: a}
	default:
		return a.assistantBackendForAutoMode(req)
	}
}

func (a *App) assistantBackendForAutoMode(req *assistantTurnRequest) assistantTurnBackend {
	if a == nil {
		return &localAssistantBackend{}
	}
	if a.appServerClient == nil {
		return &localAssistantBackend{app: a}
	}
	if req == nil {
		return &codexAssistantBackend{app: a}
	}
	evaluation := a.evaluateLocalTurn(
		context.Background(),
		req.sessionID,
		req.session,
		req.userText,
		req.cursorCtx,
		req.captureMode,
	)
	threadBound := strings.TrimSpace(req.session.AppThreadID) != "" && a.appServerClient != nil
	if evaluation.handled {
		// Once a chat session is bound to a persistent app-server thread, keep
		// short local-answer classifications from breaking the remote dialogue.
		if !(evaluation.isHighConfidenceLocalAnswer() && threadBound) {
			return &localAssistantBackend{app: a, evaluation: &evaluation}
		}
	}
	if evaluation.isHighConfidenceLocalAnswer() && !threadBound {
		return &localAssistantBackend{app: a, evaluation: &evaluation}
	}
	if a.localAssistantAvailable() && localAssistantAutoRouteCandidate(req.userText) {
		return &localAssistantBackend{app: a}
	}
	return &codexAssistantBackend{app: a}
}

func (a *App) localAssistantAvailable() bool {
	return strings.TrimSpace(a.assistantLLMBaseURL()) != ""
}

func localAssistantAutoRouteCandidate(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if requestRequiresOpenCanvasAction(lower) {
		return true
	}
	indicators := []string{
		"shell",
		"command",
		"terminal",
		"repo",
		"repository",
		"workspace",
		"file",
		"folder",
		"directory",
		"canvas",
		"mcp",
		"calendar",
		"mail",
		"email",
		"todoist",
		"evernote",
		"zotero",
		"github",
		"issue",
		"pull request",
		"pr ",
		"rg ",
		"grep ",
		"find ",
		"ls ",
		"pwd",
		"cat ",
		"open ",
		"show ",
		"display ",
		"list ",
		"inspect ",
	}
	for _, indicator := range indicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}
