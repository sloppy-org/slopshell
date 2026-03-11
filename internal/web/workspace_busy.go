package web

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

type workspaceBusyState struct {
	WorkspaceID   int64  `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	DirPath       string `json:"dir_path"`
	ChatSessionID string `json:"chat_session_id,omitempty"`
	IsDaily       bool   `json:"is_daily"`
	IsAnchor      bool   `json:"is_anchor"`
	IsFocused     bool   `json:"is_focused"`
	ActiveTurns   int    `json:"active_turns"`
	QueuedTurns   int    `json:"queued_turns"`
	IsWorking     bool   `json:"is_working"`
	Status        string `json:"status"`
	ActiveTurnID  string `json:"active_turn_id,omitempty"`
}

func (a *App) allWorkspaceBusyStates() ([]workspaceBusyState, error) {
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	focusSnapshot, err := a.workspaceFocusSnapshot()
	if err != nil {
		return nil, err
	}
	states := make([]workspaceBusyState, 0, len(workspaces))
	for _, workspace := range workspaces {
		runState := projectRunState{}
		sessionID := ""
		session, err := a.store.GetChatSessionByWorkspaceID(workspace.ID)
		switch {
		case err == nil:
			sessionID = session.ID
			runState = a.projectRunStateForSession(session.ID)
		case !isNoRows(err):
			return nil, err
		}
		states = append(states, workspaceBusyState{
			WorkspaceID:   workspace.ID,
			WorkspaceName: strings.TrimSpace(workspace.Name),
			DirPath:       strings.TrimSpace(workspace.DirPath),
			ChatSessionID: sessionID,
			IsDaily:       workspace.IsDaily,
			IsAnchor:      workspace.ID == focusSnapshot.Anchor.ID,
			IsFocused:     workspace.ID == focusSnapshot.Focus.ID,
			ActiveTurns:   runState.ActiveTurns,
			QueuedTurns:   runState.QueuedTurns,
			IsWorking:     runState.IsWorking,
			Status:        runState.Status,
			ActiveTurnID:  runState.ActiveTurnID,
		})
	}
	sort.SliceStable(states, func(i, j int) bool {
		left := workspaceBusySortKey(states[i])
		right := workspaceBusySortKey(states[j])
		switch {
		case left != right:
			return left < right
		case states[i].WorkspaceName != states[j].WorkspaceName:
			return states[i].WorkspaceName < states[j].WorkspaceName
		default:
			return states[i].WorkspaceID < states[j].WorkspaceID
		}
	})
	return states, nil
}

func workspaceBusySortKey(state workspaceBusyState) int {
	switch {
	case state.IsAnchor:
		return 0
	case state.IsFocused:
		return 1
	case state.Status == "running":
		return 2
	case state.Status == "queued":
		return 3
	default:
		return 4
	}
}

func formatWorkspaceBusyOverview(states []workspaceBusyState) string {
	if len(states) == 0 {
		return "No workspaces are available."
	}
	lines := make([]string, 0, len(states))
	for _, state := range states {
		lines = append(lines, fmt.Sprintf("%s: %s", workspaceBusyLabel(state), workspaceBusySummary(state)))
	}
	return strings.Join(lines, "\n")
}

func workspaceBusyLabel(state workspaceBusyState) string {
	name := strings.TrimSpace(state.WorkspaceName)
	if name == "" {
		name = fmt.Sprintf("workspace-%d", state.WorkspaceID)
	}
	tags := make([]string, 0, 3)
	if state.IsDaily {
		tags = append(tags, "Daily")
	}
	if state.IsAnchor {
		tags = append(tags, "anchor")
	}
	if state.IsFocused && !state.IsAnchor {
		tags = append(tags, "focus")
	}
	if len(tags) == 0 {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, strings.Join(tags, ", "))
}

func workspaceBusySummary(state workspaceBusyState) string {
	switch state.Status {
	case "running":
		details := make([]string, 0, 2)
		if state.ActiveTurns > 0 {
			details = append(details, fmt.Sprintf("%d active", state.ActiveTurns))
		}
		if state.QueuedTurns > 0 {
			details = append(details, fmt.Sprintf("%d queued", state.QueuedTurns))
		}
		if len(details) == 0 {
			return "running"
		}
		return fmt.Sprintf("running (%s)", strings.Join(details, ", "))
	case "queued":
		if state.QueuedTurns > 0 {
			return fmt.Sprintf("queued (%d queued)", state.QueuedTurns)
		}
		return "queued"
	default:
		return "idle"
	}
}

func looksLikeWorkspaceBusyQuery(text string) bool {
	switch normalizeItemCommandText(text) {
	case "what's running", "whats running", "what is running", "show busy state", "show workspace busy state", "show running workspaces":
		return true
	default:
		return false
	}
}

func (a *App) workspaceBusyOverview() (string, error) {
	states, err := a.allWorkspaceBusyStates()
	if err != nil {
		return "", err
	}
	return formatWorkspaceBusyOverview(states), nil
}

func (a *App) broadcastWorkspaceBusyChanged() {
	if a == nil || a.hub == nil {
		return
	}
	states, err := a.allWorkspaceBusyStates()
	if err != nil {
		return
	}
	payload := map[string]interface{}{
		"type":   "workspace_busy_changed",
		"states": states,
	}
	a.hub.forEachChatConn(func(conn *chatWSConn) {
		_ = conn.writeJSON(payload)
	})
}

func (a *App) handleWorkspaceBusyList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	states, err := a.allWorkspaceBusyStates()
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"states": states,
	})
}

func workspaceByName(states []workspaceBusyState, name string) *workspaceBusyState {
	for i := range states {
		if states[i].WorkspaceName == name {
			return &states[i]
		}
	}
	return nil
}

func workspaceByID(states []workspaceBusyState, id int64) *workspaceBusyState {
	for i := range states {
		if states[i].WorkspaceID == id {
			return &states[i]
		}
	}
	return nil
}
