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
	writeAPIData(w, http.StatusOK, map[string]any{
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
		writeAPIError(w, http.StatusBadRequest, err.Error())
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
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req itemArtifactLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.ArtifactID <= 0 {
		writeAPIError(w, http.StatusBadRequest, "artifact_id is required")
		return
	}
	if err := a.store.LinkItemArtifact(itemID, req.ArtifactID, req.Role); err != nil {
		writeDomainStoreError(w, err)
		return
	}
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
	writeAPIData(w, http.StatusCreated, map[string]any{
		"item":      item,
		"artifacts": artifacts,
	})
}

func (a *App) handleItemArtifactUnlink(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
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
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, err := a.store.ListArtifactItems(artifactID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"items": items,
	})
}
