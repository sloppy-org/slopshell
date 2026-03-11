package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

type workspaceFocusSnapshot struct {
	Anchor   store.Workspace `json:"anchor"`
	Focus    store.Workspace `json:"focus"`
	Explicit bool            `json:"explicit"`
}

type workspaceFocusRequest struct {
	WorkspaceID int64 `json:"workspace_id"`
}

func (a *App) anchorWorkspace() (store.Workspace, error) {
	workspace, err := a.store.ActiveWorkspace()
	switch {
	case err == nil:
		if workspace.IsDaily && workspaceDailyDate(workspace) != dailyWorkspaceDate(a.runtimeNow()) {
			return a.ensureTodayDailyWorkspace()
		}
		return workspace, nil
	case !isNoRows(err):
		return store.Workspace{}, err
	default:
		return a.ensureTodayDailyWorkspace()
	}
}

func (a *App) focusedWorkspace() (store.Workspace, bool, error) {
	focusedID, err := a.store.FocusedWorkspaceID()
	if err != nil {
		return store.Workspace{}, false, err
	}
	if focusedID > 0 {
		workspace, err := a.store.GetWorkspace(focusedID)
		return workspace, true, err
	}
	anchor, err := a.anchorWorkspace()
	if err != nil {
		return store.Workspace{}, false, err
	}
	return anchor, false, nil
}

func (a *App) workspaceFocusSnapshot() (workspaceFocusSnapshot, error) {
	anchor, err := a.anchorWorkspace()
	if err != nil {
		return workspaceFocusSnapshot{}, err
	}
	focus, explicit, err := a.focusedWorkspace()
	if err != nil {
		return workspaceFocusSnapshot{}, err
	}
	return workspaceFocusSnapshot{
		Anchor:   anchor,
		Focus:    focus,
		Explicit: explicit,
	}, nil
}

func (a *App) setFocusedWorkspace(id int64) error {
	if err := a.store.SetFocusedWorkspaceID(id); err != nil {
		return err
	}
	a.closeAllAppSessions()
	return nil
}

func (a *App) effectiveWorkspaceForChatSession(session store.ChatSession) (store.Workspace, error) {
	workspace, explicit, err := a.focusedWorkspace()
	if err != nil {
		return store.Workspace{}, err
	}
	if explicit {
		return workspace, nil
	}
	return a.workspaceForChatSession(session)
}

func (a *App) effectiveWorkspaceDirForChatSession(session store.ChatSession) (string, error) {
	workspace, err := a.effectiveWorkspaceForChatSession(session)
	if err != nil {
		return "", err
	}
	return workspace.DirPath, nil
}

func (a *App) effectiveWorkspaceDirForChatSessionID(sessionID string) (string, error) {
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		return "", err
	}
	return a.effectiveWorkspaceDirForChatSession(session)
}

func (a *App) appSessionBindingForChatSessionID(sessionID string) (store.ChatSession, store.Workspace, error) {
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		return store.ChatSession{}, store.Workspace{}, err
	}
	workspace, err := a.effectiveWorkspaceForChatSession(session)
	if err != nil {
		return store.ChatSession{}, store.Workspace{}, err
	}
	if strings.TrimSpace(workspace.DirPath) == "" {
		return store.ChatSession{}, store.Workspace{}, errors.New("workspace path is required")
	}
	bindingSession, err := a.store.GetOrCreateChatSessionForWorkspace(workspace.ID)
	if err != nil {
		return store.ChatSession{}, store.Workspace{}, err
	}
	return bindingSession, workspace, nil
}

func (a *App) broadcastWorkspaceFocusChanged() {
	if a == nil || a.hub == nil {
		return
	}
	snapshot, err := a.workspaceFocusSnapshot()
	if err != nil {
		return
	}
	payload := map[string]interface{}{
		"type":     "workspace_focus_changed",
		"anchor":   snapshot.Anchor,
		"focus":    snapshot.Focus,
		"explicit": snapshot.Explicit,
	}
	a.hub.forEachChatConn(func(conn *chatWSConn) {
		_ = conn.writeJSON(payload)
	})
}

func (a *App) handleWorkspaceFocusGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	snapshot, err := a.workspaceFocusSnapshot()
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"anchor":   snapshot.Anchor,
		"focus":    snapshot.Focus,
		"explicit": snapshot.Explicit,
	})
}

func (a *App) handleWorkspaceFocusPost(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req workspaceFocusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.WorkspaceID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if err := a.setFocusedWorkspace(req.WorkspaceID); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.broadcastWorkspaceFocusChanged()
	snapshot, err := a.workspaceFocusSnapshot()
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"anchor":   snapshot.Anchor,
		"focus":    snapshot.Focus,
		"explicit": snapshot.Explicit,
	})
}

func (a *App) handleWorkspaceFocusDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	if err := a.setFocusedWorkspace(0); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.broadcastWorkspaceFocusChanged()
	snapshot, err := a.workspaceFocusSnapshot()
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"anchor":   snapshot.Anchor,
		"focus":    snapshot.Focus,
		"explicit": snapshot.Explicit,
	})
}
