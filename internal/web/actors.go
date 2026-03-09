package web

import (
	"net/http"

	"github.com/krystophny/tabura/internal/store"
)

type actorCreateRequest struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Email       *string `json:"email"`
	Provider    *string `json:"provider"`
	ProviderRef *string `json:"provider_ref"`
	MetaJSON    *string `json:"meta_json"`
}

func (a *App) handleActorList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	actors, err := a.store.ListActors()
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"actors": actors,
	})
}

func (a *App) handleActorCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req actorCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	actor, err := a.store.CreateActorWithOptions(req.Name, req.Kind, store.ActorOptions{
		Email:       req.Email,
		Provider:    req.Provider,
		ProviderRef: req.ProviderRef,
		MetaJSON:    req.MetaJSON,
	})
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusCreated, map[string]any{
		"actor": actor,
	})
}

func (a *App) handleActorGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	actorID, err := parseURLInt64Param(r, "actor_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	actor, err := a.store.GetActor(actorID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"actor": actor,
	})
}

func (a *App) handleActorDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	actorID, err := parseURLInt64Param(r, "actor_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.DeleteActor(actorID); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeNoContent(w)
}
