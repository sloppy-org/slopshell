package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

type artifactCreateRequest struct {
	Kind     string  `json:"kind"`
	RefPath  *string `json:"ref_path"`
	RefURL   *string `json:"ref_url"`
	Title    *string `json:"title"`
	MetaJSON *string `json:"meta_json"`
}

func (a *App) handleArtifactList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	kind := store.ArtifactKind(strings.TrimSpace(r.URL.Query().Get("kind")))
	workspaceIDText := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	linkedOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("linked")), "true")
	var (
		artifacts []store.Artifact
		err       error
	)
	switch {
	case workspaceIDText != "":
		workspaceID, parseErr := strconv.ParseInt(workspaceIDText, 10, 64)
		if parseErr != nil || workspaceID <= 0 {
			writeAPIError(w, http.StatusBadRequest, "workspace_id must be a positive integer")
			return
		}
		if linkedOnly {
			artifacts, err = a.store.ListLinkedArtifacts(workspaceID)
		} else {
			artifacts, err = a.store.ListArtifactsForWorkspace(workspaceID)
		}
	case kind != "":
		artifacts, err = a.store.ListArtifactsByKind(kind)
	default:
		artifacts, err = a.store.ListArtifacts()
	}
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"artifacts": artifacts,
	})
}

func (a *App) handleArtifactCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req artifactCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	artifact, err := a.store.CreateArtifact(store.ArtifactKind(req.Kind), req.RefPath, req.RefURL, req.Title, req.MetaJSON)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusCreated, map[string]any{
		"artifact": artifact,
	})
}

func (a *App) handleArtifactGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	artifact, err := a.store.GetArtifact(artifactID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"artifact": artifact,
	})
}

func (a *App) handleArtifactDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.DeleteArtifact(artifactID); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeNoContent(w)
}
