package web

import (
	"net/http"
	"strings"
)

type workspaceCreateRequest struct {
	Name     string `json:"name"`
	DirPath  string `json:"dir_path"`
	Sphere   string `json:"sphere"`
	IsActive bool   `json:"is_active"`
}

type workspaceUpdateRequest struct {
	Name     *string `json:"name"`
	Sphere   *string `json:"sphere"`
	IsActive *bool   `json:"is_active"`
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
	if strings.TrimSpace(req.Sphere) == "" {
		http.Error(w, "sphere is required", http.StatusBadRequest)
		return
	}
	workspace, err := a.store.CreateWorkspace(req.Name, req.DirPath, req.Sphere)
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

func (a *App) handleWorkspaceUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaceID, err := parseURLInt64Param(r, "workspace_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req workspaceUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Name == nil && req.Sphere == nil && req.IsActive == nil {
		http.Error(w, "at least one workspace update is required", http.StatusBadRequest)
		return
	}
	workspace, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if req.Name != nil {
		workspace, err = a.store.UpdateWorkspaceName(workspaceID, *req.Name)
		if err != nil {
			writeDomainStoreError(w, err)
			return
		}
	}
	if req.Sphere != nil {
		workspace, err = a.store.SetWorkspaceSphere(workspaceID, *req.Sphere)
		if err != nil {
			writeDomainStoreError(w, err)
			return
		}
	}
	if req.IsActive != nil {
		if !*req.IsActive {
			http.Error(w, "is_active=false is not supported", http.StatusBadRequest)
			return
		}
		if err := a.store.SetActiveWorkspace(workspaceID); err != nil {
			writeDomainStoreError(w, err)
			return
		}
		workspace, err = a.store.GetWorkspace(workspaceID)
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
