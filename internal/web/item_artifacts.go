package web

import "net/http"

type itemArtifactLinkRequest struct {
	ArtifactID int64  `json:"artifact_id"`
	Role       string `json:"role"`
}

func (a *App) writeItemArtifactsResponse(w http.ResponseWriter, itemID int64) {
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	artifacts, err := a.store.ListItemArtifacts(itemID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":        true,
		"item":      item,
		"artifacts": artifacts,
	})
}

func (a *App) handleItemArtifactList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.writeItemArtifactsResponse(w, itemID)
}

func (a *App) handleItemArtifactLink(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req itemArtifactLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.ArtifactID <= 0 {
		http.Error(w, "artifact_id is required", http.StatusBadRequest)
		return
	}
	if err := a.store.LinkItemArtifact(itemID, req.ArtifactID, req.Role); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.writeItemArtifactsResponse(w, itemID)
}

func (a *App) handleItemArtifactUnlink(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.UnlinkItemArtifact(itemID, artifactID); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.writeItemArtifactsResponse(w, itemID)
}

func (a *App) handleArtifactItemList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	items, err := a.store.ListArtifactItems(artifactID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":    true,
		"items": items,
	})
}
