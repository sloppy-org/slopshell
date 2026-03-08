package web

import (
	"net/http"
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
	var (
		artifacts []store.Artifact
		err       error
	)
	if kind != "" {
		artifacts, err = a.store.ListArtifactsByKind(kind)
	} else {
		artifacts, err = a.store.ListArtifacts()
	}
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":        true,
		"artifacts": artifacts,
	})
}

func (a *App) handleArtifactCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req artifactCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	artifact, err := a.store.CreateArtifact(store.ArtifactKind(req.Kind), req.RefPath, req.RefURL, req.Title, req.MetaJSON)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"artifact": artifact,
	})
}

func (a *App) handleArtifactGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	artifact, err := a.store.GetArtifact(artifactID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"artifact": artifact,
	})
}

func (a *App) handleArtifactDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteArtifact(artifactID); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":          true,
		"deleted":     true,
		"artifact_id": artifactID,
	})
}
