package web

import "net/http"

type workspaceCreateRequest struct {
	Name     string `json:"name"`
	DirPath  string `json:"dir_path"`
	IsActive bool   `json:"is_active"`
}

func (a *App) handleWorkspaceList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":         true,
		"workspaces": workspaces,
	})
}

func (a *App) handleWorkspaceCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req workspaceCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	workspace, err := a.store.CreateWorkspace(req.Name, req.DirPath)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if req.IsActive {
		if err := a.store.SetActiveWorkspace(workspace.ID); err != nil {
			writeDomainStoreError(w, err)
			return
		}
		workspace, err = a.store.GetWorkspace(workspace.ID)
		if err != nil {
			writeDomainStoreError(w, err)
			return
		}
	}
	writeJSON(w, map[string]any{
		"ok":        true,
		"workspace": workspace,
	})
}

func (a *App) handleWorkspaceGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaceID, err := parseURLInt64Param(r, "workspace_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	workspace, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":        true,
		"workspace": workspace,
	})
}

func (a *App) handleWorkspaceDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaceID, err := parseURLInt64Param(r, "workspace_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteWorkspace(workspaceID); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":           true,
		"deleted":      true,
		"workspace_id": workspaceID,
	})
}
