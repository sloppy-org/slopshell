package web

import (
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

type itemWorkspaceUpdateRequest struct {
	WorkspaceID *int64 `json:"workspace_id"`
}

type itemProjectUpdateRequest struct {
	ProjectID *string `json:"project_id"`
}

var (
	errItemWorkspaceNotFound = errors.New("workspace not found")
	errItemProjectNotFound   = errors.New("project not found")
)

func (a *App) ensureWorkspaceExists(workspaceID int64) error {
	if workspaceID <= 0 {
		return errItemWorkspaceNotFound
	}
	if _, err := a.store.GetWorkspace(workspaceID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errItemWorkspaceNotFound
		}
		return err
	}
	return nil
}

func (a *App) ensureProjectExists(projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return errItemProjectNotFound
	}
	if _, err := a.store.GetProject(projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errItemProjectNotFound
		}
		return err
	}
	return nil
}

func (a *App) handleItemWorkspaceUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req itemWorkspaceUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.WorkspaceID != nil {
		if err := a.ensureWorkspaceExists(*req.WorkspaceID); err != nil {
			if errors.Is(err, errItemWorkspaceNotFound) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	warning, err := a.itemWorkspaceChangeWarning(item, req.WorkspaceID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.store.SetItemWorkspace(itemID, req.WorkspaceID); err != nil {
		writeItemStoreError(w, err)
		return
	}
	item, err = a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"item":    item,
		"warning": warning,
	})
}

func (a *App) handleItemProjectUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req itemProjectUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.ProjectID != nil && strings.TrimSpace(*req.ProjectID) != "" {
		if err := a.ensureProjectExists(*req.ProjectID); err != nil {
			if errors.Is(err, errItemProjectNotFound) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := a.store.SetItemProject(itemID, req.ProjectID); err != nil {
		writeItemStoreError(w, err)
		return
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":   true,
		"item": item,
	})
}

func (a *App) itemWorkspaceChangeWarning(item store.Item, nextWorkspaceID *int64) (string, error) {
	if item.WorkspaceID == nil || item.ArtifactID == nil {
		return "", nil
	}
	if nextWorkspaceID != nil && *nextWorkspaceID == *item.WorkspaceID {
		return "", nil
	}
	artifact, err := a.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if artifact.RefPath == nil || strings.TrimSpace(*artifact.RefPath) == "" {
		return "", nil
	}
	workspace, err := a.store.GetWorkspace(*item.WorkspaceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	artifactPath := filepath.Clean(strings.TrimSpace(*artifact.RefPath))
	if !pathWithinRoot(artifactPath, workspace.DirPath) {
		return "", nil
	}
	return "Artifact link kept: the referenced file still points into the previous workspace.", nil
}
