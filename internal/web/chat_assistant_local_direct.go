package web

import (
	"context"
	"errors"
	"strings"
)

func (a *App) runLocalAssistantDirectOpenFileCanvas(ctx context.Context, state *localAssistantTurnState, catalog localAssistantToolCatalog, path string) (string, error) {
	if _, ok := catalog.ToolsByName["action__open_file_canvas"]; !ok {
		return "", errors.New("open_file_canvas is not available")
	}
	result, err := a.executeLocalAssistantToolCall(ctx, state, catalog, localAssistantToolCall{
		ID:   randomToken(),
		Name: "action__open_file_canvas",
		Arguments: map[string]any{
			"path": path,
		},
	})
	if err != nil {
		return "", err
	}
	if result.IsError {
		return "", errors.New(strings.TrimSpace(result.Error))
	}
	for _, resultPayload := range localAssistantToolPayloads(result, state.workspace.ID) {
		if resultPayload == nil {
			continue
		}
		a.broadcastSystemActionEvent(state.sessionID, resultPayload)
	}
	reply := strings.TrimSpace(result.Output)
	if reply == "" {
		reply = "Shown on canvas."
	}
	return reply, nil
}
