package web

import (
	"strings"

	"github.com/sloppy-org/slopshell/internal/modelprofile"
	"github.com/sloppy-org/slopshell/internal/store"
)

type assistantTurnRequest struct {
	sessionID       string
	session         store.ChatSession
	messages        []store.ChatMessage
	canvasCtx       *canvasContext
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
	detailRequested bool
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
	app *App
}

func (b *localAssistantBackend) mode() string {
	return assistantModeLocal
}

func (b *localAssistantBackend) run(req *assistantTurnRequest) {
	if b == nil || b.app == nil || req == nil {
		return
	}
	b.app.runLocalAssistantTurn(req)
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
	if req.turnModel != "" && req.turnModel != modelprofile.AliasLocal {
		return &codexAssistantBackend{app: a}
	}
	// Everything without an explicit remote alias stays local. Only fall
	// through to Codex when the local assistant has no way to run at all
	// (no local LLM URL configured AND an app-server client is available).
	localConfigured := strings.TrimSpace(a.assistantLLMURL) != "" || a.appServerClient == nil || a.assistantRoutingMode() == assistantModeLocal
	if localConfigured {
		return &localAssistantBackend{app: a}
	}
	if a.appServerClient != nil {
		return &codexAssistantBackend{app: a}
	}
	return &localAssistantBackend{app: a}
}
