package web

import (
	"strings"

	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

type assistantTurnRequest struct {
	sessionID       string
	session         store.ChatSession
	messages        []store.ChatMessage
	userText        string
	promptText      string
	cursorCtx       *chatCursorContext
	inkCtx          []*chatCanvasInkEvent
	positionCtx     []*chatCanvasPositionEvent
	captureMode     string
	outputMode      string
	localOnly       bool
	fastMode        bool
	messageID       int64
	turnModel       string
	searchTurn      bool
	transientRemote bool
	reasoningEffort string
	baseProfile     appServerModelProfile
	turnProfile     appServerModelProfile
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
	if req.searchTurn || (req.turnModel != "" && req.turnModel != modelprofile.AliasLocal) {
		return &codexAssistantBackend{app: a}
	}
	if modelprofile.ResolveAlias(req.baseProfile.Alias, modelprofile.AliasLocal) == modelprofile.AliasLocal {
		return &localAssistantBackend{app: a}
	}
	switch a.assistantRoutingMode() {
	case assistantModeLocal:
		return &localAssistantBackend{app: a}
	case assistantModeCodex:
		return &codexAssistantBackend{app: a}
	default:
		if a.appServerClient != nil {
			return &codexAssistantBackend{app: a}
		}
		return &localAssistantBackend{app: a}
	}
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
		"tool",
		"tools",
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
