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
	http.Error(w, err.Error(), itemResponseErrorStatus(err))
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
	var (
		items []store.Item
		err   error
	)
	if state != "" {
		items, err = a.store.ListItemsByState(state)
	} else {
		items, err = a.store.ListItems()
	}
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":    true,
		"items": items,
	})
}

func (a *App) writeItemSummaryList(w http.ResponseWriter, items []store.ItemSummary) {
	writeJSON(w, map[string]any{
		"ok":    true,
		"items": items,
	})
}

func (a *App) handleItemInbox(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	items, err := a.store.ListInboxItems(time.Now())
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
	items, err := a.store.ListWaitingItems()
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
	items, err := a.store.ListSomedayItems()
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
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		limit = value
	}
	items, err := a.store.ListDoneItems(limit)
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
	counts, err := a.store.CountItemsByState(time.Now())
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":     true,
		"counts": counts,
	})
}

func (a *App) handleItemCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req itemCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	item, err := a.store.CreateItem(req.Title, store.ItemOptions{
		State:        req.State,
		WorkspaceID:  req.WorkspaceID,
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
	writeJSON(w, map[string]any{
		"ok":   true,
		"item": item,
	})
}

func (a *App) handleItemGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

func (a *App) handleItemUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req itemUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.ActorID != nil && *req.ActorID > 0 {
		if err := a.ensureActorExists(*req.ActorID); err != nil {
			if errors.Is(err, errItemActorNotFound) || errors.Is(err, errItemActorRequired) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := a.store.UpdateItem(itemID, store.ItemUpdate{
		Title:        req.Title,
		State:        req.State,
		WorkspaceID:  req.WorkspaceID,
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
	writeJSON(w, map[string]any{
		"ok":   true,
		"item": item,
	})
}

func (a *App) handleItemDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := a.store.DeleteItem(itemID); err != nil {
		writeItemStoreError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"ok":      true,
		"deleted": true,
		"item_id": itemID,
	})
}

func (a *App) handleItemStateUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req itemStateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
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
	writeJSON(w, map[string]any{
		"ok":   true,
		"item": item,
	})
}

func (a *App) handleItemAssign(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req itemAssignRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.ensureActorExists(req.ActorID); err != nil {
		if errors.Is(err, errItemActorNotFound) || errors.Is(err, errItemActorRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	writeJSON(w, map[string]interface{}{
		"ok":   true,
		"item": item,
	})
}

func (a *App) handleItemUnassign(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
	writeJSON(w, map[string]interface{}{
		"ok":   true,
		"item": item,
	})
}

func (a *App) handleItemComplete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req itemCompleteRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.ensureActorExists(req.ActorID); err != nil {
		if errors.Is(err, errItemActorNotFound) || errors.Is(err, errItemActorRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
	writeJSON(w, map[string]interface{}{
		"ok":   true,
		"item": item,
	})
}

func (a *App) handleItemTriage(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req itemTriageRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "done":
		err = a.store.TriageItemDone(itemID)
	case "later":
		if strings.TrimSpace(req.VisibleAfter) == "" {
			http.Error(w, "visible_after is required", http.StatusBadRequest)
			return
		}
		err = a.store.TriageItemLater(itemID, req.VisibleAfter)
	case "delegate":
		if err := a.ensureActorExists(req.ActorID); err != nil {
			if errors.Is(err, errItemActorNotFound) || errors.Is(err, errItemActorRequired) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = a.store.TriageItemDelegate(itemID, req.ActorID)
	case "delete":
		err = a.store.TriageItemDelete(itemID)
	case "someday":
		err = a.store.TriageItemSomeday(itemID)
	default:
		http.Error(w, "action must be one of done, later, delegate, delete, someday", http.StatusBadRequest)
		return
	}
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	if strings.EqualFold(req.Action, "delete") {
		writeJSON(w, map[string]interface{}{
			"ok":      true,
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
	writeJSON(w, map[string]interface{}{
		"ok":   true,
		"item": item,
	})
}
