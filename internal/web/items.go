package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

type itemCreateRequest struct {
	Title        string  `json:"title"`
	State        string  `json:"state"`
	WorkspaceID  *int64  `json:"workspace_id"`
	Sphere       *string `json:"sphere"`
	ArtifactID   *int64  `json:"artifact_id"`
	ActorID      *int64  `json:"actor_id"`
	VisibleAfter *string `json:"visible_after"`
	FollowUpAt   *string `json:"follow_up_at"`
	Source       *string `json:"source"`
	SourceRef    *string `json:"source_ref"`
}

type itemUpdateRequest struct {
	Title        *string `json:"title"`
	State        *string `json:"state"`
	WorkspaceID  *int64  `json:"workspace_id"`
	Sphere       *string `json:"sphere"`
	ArtifactID   *int64  `json:"artifact_id"`
	ActorID      *int64  `json:"actor_id"`
	VisibleAfter *string `json:"visible_after"`
	FollowUpAt   *string `json:"follow_up_at"`
	Source       *string `json:"source"`
	SourceRef    *string `json:"source_ref"`
}

type itemStateRequest struct {
	State string `json:"state"`
}

type itemAssignRequest struct {
	ActorID int64 `json:"actor_id"`
}

type itemCompleteRequest struct {
	ActorID int64 `json:"actor_id"`
}

type itemCountResponse struct {
	Counts map[string]int `json:"counts"`
}

type itemTriageRequest struct {
	Action       string `json:"action"`
	ActorID      int64  `json:"actor_id"`
	VisibleAfter string `json:"visible_after"`
}

var (
	errItemActorRequired = errors.New("actor_id is required")
	errItemActorNotFound = errors.New("actor not found")
)

func parseItemIDParam(r *http.Request) (int64, error) {
	return parseURLInt64Param(r, "item_id")
}

func itemResponseErrorStatus(err error) int {
	return domainResponseErrorStatus(err)
}

func writeItemStoreError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	writeAPIError(w, itemResponseErrorStatus(err), err.Error())
}

func (a *App) ensureActorExists(actorID int64) error {
	if actorID <= 0 {
		return errItemActorRequired
	}
	if _, err := a.store.GetActor(actorID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errItemActorNotFound
		}
		return err
	}
	return nil
}

func (a *App) handleItemList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	sphere := strings.TrimSpace(r.URL.Query().Get("sphere"))
	var (
		items []store.Item
		err   error
	)
	if state != "" {
		items, err = a.store.ListItemsByStateForSphere(state, sphere)
	} else {
		items, err = a.store.ListItems()
	}
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (a *App) writeItemSummaryList(w http.ResponseWriter, items []store.ItemSummary) {
	writeAPIData(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (a *App) handleItemInbox(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	items, err := a.store.ListInboxItemsForSphere(time.Now(), strings.TrimSpace(r.URL.Query().Get("sphere")))
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	a.writeItemSummaryList(w, items)
}

func (a *App) handleItemWaiting(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	items, err := a.store.ListWaitingItemsForSphere(strings.TrimSpace(r.URL.Query().Get("sphere")))
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	a.writeItemSummaryList(w, items)
}

func (a *App) handleItemSomeday(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	items, err := a.store.ListSomedayItemsForSphere(strings.TrimSpace(r.URL.Query().Get("sphere")))
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	a.writeItemSummaryList(w, items)
}

func (a *App) handleItemDone(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 {
			writeAPIError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = value
	}
	items, err := a.store.ListDoneItemsForSphere(limit, strings.TrimSpace(r.URL.Query().Get("sphere")))
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	a.writeItemSummaryList(w, items)
}

func (a *App) handleItemCounts(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	counts, err := a.store.CountItemsByStateForSphere(time.Now(), strings.TrimSpace(r.URL.Query().Get("sphere")))
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"counts": counts,
	})
}

func (a *App) handleItemCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req itemCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	item, err := a.store.CreateItem(req.Title, store.ItemOptions{
		State:        req.State,
		WorkspaceID:  req.WorkspaceID,
		Sphere:       req.Sphere,
		ArtifactID:   req.ArtifactID,
		ActorID:      req.ActorID,
		VisibleAfter: req.VisibleAfter,
		FollowUpAt:   req.FollowUpAt,
		Source:       req.Source,
		SourceRef:    req.SourceRef,
	})
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusCreated, map[string]any{
		"item": item,
	})
}

func (a *App) handleItemGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"item": item,
	})
}

func (a *App) handleItemUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req itemUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.ActorID != nil && *req.ActorID > 0 {
		if err := a.ensureActorExists(*req.ActorID); err != nil {
			if errors.Is(err, errItemActorNotFound) || errors.Is(err, errItemActorRequired) {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := a.store.UpdateItem(itemID, store.ItemUpdate{
		Title:        req.Title,
		State:        req.State,
		WorkspaceID:  req.WorkspaceID,
		Sphere:       req.Sphere,
		ArtifactID:   req.ArtifactID,
		ActorID:      req.ActorID,
		VisibleAfter: req.VisibleAfter,
		FollowUpAt:   req.FollowUpAt,
		Source:       req.Source,
		SourceRef:    req.SourceRef,
	}); err != nil {
		writeItemStoreError(w, err)
		return
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"item": item,
	})
}

func (a *App) handleItemDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.DeleteItem(itemID); err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeNoContent(w)
}

func (a *App) handleItemStateUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req itemStateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := a.store.UpdateItemState(itemID, req.State); err != nil {
		writeItemStoreError(w, err)
		return
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"item": item,
	})
}

func (a *App) handleItemAssign(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req itemAssignRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := a.ensureActorExists(req.ActorID); err != nil {
		if errors.Is(err, errItemActorNotFound) || errors.Is(err, errItemActorRequired) {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.store.AssignItem(itemID, req.ActorID); err != nil {
		writeItemStoreError(w, err)
		return
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"item": item,
	})
}

func (a *App) handleItemUnassign(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.UnassignItem(itemID); err != nil {
		writeItemStoreError(w, err)
		return
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"item": item,
	})
}

func (a *App) handleItemComplete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req itemCompleteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := a.ensureActorExists(req.ActorID); err != nil {
		if errors.Is(err, errItemActorNotFound) || errors.Is(err, errItemActorRequired) {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.store.CompleteItemByActor(itemID, req.ActorID); err != nil {
		writeItemStoreError(w, err)
		return
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"item": item,
	})
}

func (a *App) handleItemTriage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req itemTriageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "done":
		err = a.store.TriageItemDone(itemID)
	case "later":
		if strings.TrimSpace(req.VisibleAfter) == "" {
			writeAPIError(w, http.StatusBadRequest, "visible_after is required")
			return
		}
		err = a.store.TriageItemLater(itemID, req.VisibleAfter)
	case "delegate":
		if err := a.ensureActorExists(req.ActorID); err != nil {
			if errors.Is(err, errItemActorNotFound) || errors.Is(err, errItemActorRequired) {
				writeAPIError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		err = a.store.TriageItemDelegate(itemID, req.ActorID)
	case "delete":
		err = a.store.TriageItemDelete(itemID)
	case "someday":
		err = a.store.TriageItemSomeday(itemID)
	default:
		writeAPIError(w, http.StatusBadRequest, "action must be one of done, later, delegate, delete, someday")
		return
	}
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	if strings.EqualFold(req.Action, "delete") {
		writeAPIData(w, http.StatusOK, map[string]any{
			"deleted": true,
			"item_id": itemID,
		})
		return
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"item": item,
	})
}
