package web

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestItemAssignmentLifecycleAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	actor, err := app.store.CreateActor("Codex", store.ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	item, err := app.store.CreateItem("Delegate this", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rrAssign := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/assign", map[string]any{
		"actor_id": actor.ID,
	})
	if rrAssign.Code != http.StatusOK {
		t.Fatalf("assign status = %d, want 200: %s", rrAssign.Code, rrAssign.Body.String())
	}
	gotItem, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(assigned) error: %v", err)
	}
	if gotItem.ActorID == nil || *gotItem.ActorID != actor.ID {
		t.Fatalf("assigned ActorID = %v, want %d", gotItem.ActorID, actor.ID)
	}
	if gotItem.State != store.ItemStateWaiting {
		t.Fatalf("assigned State = %q, want %q", gotItem.State, store.ItemStateWaiting)
	}

	rrUnassign := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/unassign", map[string]any{})
	if rrUnassign.Code != http.StatusOK {
		t.Fatalf("unassign status = %d, want 200: %s", rrUnassign.Code, rrUnassign.Body.String())
	}
	gotItem, err = app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(unassigned) error: %v", err)
	}
	if gotItem.ActorID != nil {
		t.Fatalf("unassigned ActorID = %v, want nil", gotItem.ActorID)
	}
	if gotItem.State != store.ItemStateInbox {
		t.Fatalf("unassigned State = %q, want %q", gotItem.State, store.ItemStateInbox)
	}

	rrReassign := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/assign", map[string]any{
		"actor_id": actor.ID,
	})
	if rrReassign.Code != http.StatusOK {
		t.Fatalf("reassign status = %d, want 200: %s", rrReassign.Code, rrReassign.Body.String())
	}
	rrComplete := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/complete", map[string]any{
		"actor_id": actor.ID,
	})
	if rrComplete.Code != http.StatusOK {
		t.Fatalf("complete status = %d, want 200: %s", rrComplete.Code, rrComplete.Body.String())
	}
	gotItem, err = app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(completed) error: %v", err)
	}
	if gotItem.State != store.ItemStateDone {
		t.Fatalf("completed State = %q, want %q", gotItem.State, store.ItemStateDone)
	}
}

func TestItemCRUDAndStateAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	workspace, err := app.store.CreateWorkspace("Default", filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	artifactTitle := "Plan"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, nil, nil, &artifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	actor, err := app.store.CreateActor("Codex", store.ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items", map[string]any{
		"title":        "Wire REST endpoints",
		"workspace_id": workspace.ID,
		"artifact_id":  artifact.ID,
		"source":       "github",
		"source_ref":   "owner/repo#175",
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("create item status = %d, want 200: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createPayload := decodeJSONResponse(t, rrCreate)
	itemPayload, ok := createPayload["item"].(map[string]any)
	if !ok {
		t.Fatalf("create item payload = %#v", createPayload)
	}
	itemID := int64(itemPayload["id"].(float64))

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items?state=inbox", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("list items status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	listPayload := decodeJSONResponse(t, rrList)
	items, ok := listPayload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("list items payload = %#v", listPayload)
	}

	visibleAfter := "2026-03-10T09:00:00Z"
	rrUpdate := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(itemID), map[string]any{
		"title":         "Wire core REST endpoints",
		"visible_after": visibleAfter,
	})
	if rrUpdate.Code != http.StatusOK {
		t.Fatalf("update item status = %d, want 200: %s", rrUpdate.Code, rrUpdate.Body.String())
	}
	updated, err := app.store.GetItem(itemID)
	if err != nil {
		t.Fatalf("GetItem(updated) error: %v", err)
	}
	if updated.Title != "Wire core REST endpoints" {
		t.Fatalf("updated title = %q, want %q", updated.Title, "Wire core REST endpoints")
	}
	if updated.VisibleAfter == nil || *updated.VisibleAfter != visibleAfter {
		t.Fatalf("updated visible_after = %v, want %q", updated.VisibleAfter, visibleAfter)
	}

	rrAssign := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(itemID)+"/assign", map[string]any{
		"actor_id": actor.ID,
	})
	if rrAssign.Code != http.StatusOK {
		t.Fatalf("assign item status = %d, want 200: %s", rrAssign.Code, rrAssign.Body.String())
	}

	rrState := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(itemID)+"/state", map[string]any{
		"state": store.ItemStateDone,
	})
	if rrState.Code != http.StatusOK {
		t.Fatalf("item state status = %d, want 200: %s", rrState.Code, rrState.Body.String())
	}
	doneItem, err := app.store.GetItem(itemID)
	if err != nil {
		t.Fatalf("GetItem(done) error: %v", err)
	}
	if doneItem.State != store.ItemStateDone {
		t.Fatalf("done state = %q, want %q", doneItem.State, store.ItemStateDone)
	}

	rrGet := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/"+itoa(itemID), nil)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("get item status = %d, want 200: %s", rrGet.Code, rrGet.Body.String())
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/items/"+itoa(itemID), nil)
	if rrDelete.Code != http.StatusOK {
		t.Fatalf("delete item status = %d, want 200: %s", rrDelete.Code, rrDelete.Body.String())
	}

	rrMissing := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/"+itoa(itemID), nil)
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("deleted item status = %d, want 404: %s", rrMissing.Code, rrMissing.Body.String())
	}
}

func TestItemStateViewAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	workspace, err := app.store.CreateWorkspace("Default", filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	artifactTitle := "Inbox plan"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindIdeaNote, nil, nil, &artifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	actor, err := app.store.CreateActor("Alice", store.ActorKindHuman)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}

	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	future := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)
	source := "github"
	sourceRef := "owner/repo#177"
	visibleInbox, err := app.store.CreateItem("Visible inbox", store.ItemOptions{
		State:        store.ItemStateInbox,
		WorkspaceID:  &workspace.ID,
		ArtifactID:   &artifact.ID,
		ActorID:      &actor.ID,
		VisibleAfter: &past,
		Source:       &source,
		SourceRef:    &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(visible inbox) error: %v", err)
	}
	if _, err := app.store.CreateItem("Hidden inbox", store.ItemOptions{
		State:        store.ItemStateInbox,
		VisibleAfter: &future,
	}); err != nil {
		t.Fatalf("CreateItem(hidden inbox) error: %v", err)
	}
	waitingItem, err := app.store.CreateItem("Waiting item", store.ItemOptions{State: store.ItemStateWaiting})
	if err != nil {
		t.Fatalf("CreateItem(waiting) error: %v", err)
	}
	if _, err := app.store.CreateItem("Someday item", store.ItemOptions{State: store.ItemStateSomeday}); err != nil {
		t.Fatalf("CreateItem(someday) error: %v", err)
	}
	doneItem, err := app.store.CreateItem("Done item", store.ItemOptions{State: store.ItemStateDone})
	if err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}

	rrInbox := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/inbox", nil)
	if rrInbox.Code != http.StatusOK {
		t.Fatalf("inbox status = %d, want 200: %s", rrInbox.Code, rrInbox.Body.String())
	}
	inboxPayload := decodeJSONResponse(t, rrInbox)
	inboxItems, ok := inboxPayload["items"].([]any)
	if !ok || len(inboxItems) != 1 {
		t.Fatalf("inbox payload = %#v", inboxPayload)
	}
	inboxRow, ok := inboxItems[0].(map[string]any)
	if !ok {
		t.Fatalf("inbox row = %#v", inboxItems[0])
	}
	if got := int64(inboxRow["id"].(float64)); got != visibleInbox.ID {
		t.Fatalf("inbox item id = %d, want %d", got, visibleInbox.ID)
	}
	if got := strFromAny(inboxRow["artifact_title"]); got != artifactTitle {
		t.Fatalf("artifact_title = %q, want %q", got, artifactTitle)
	}
	if got := strFromAny(inboxRow["artifact_kind"]); got != string(store.ArtifactKindIdeaNote) {
		t.Fatalf("artifact_kind = %q, want %q", got, store.ArtifactKindIdeaNote)
	}
	if got := strFromAny(inboxRow["actor_name"]); got != "Alice" {
		t.Fatalf("actor_name = %q, want Alice", got)
	}

	rrWaiting := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/waiting", nil)
	if rrWaiting.Code != http.StatusOK {
		t.Fatalf("waiting status = %d, want 200: %s", rrWaiting.Code, rrWaiting.Body.String())
	}
	waitingPayload := decodeJSONResponse(t, rrWaiting)
	waitingItems, ok := waitingPayload["items"].([]any)
	if !ok || len(waitingItems) != 1 {
		t.Fatalf("waiting payload = %#v", waitingPayload)
	}
	waitingRow, ok := waitingItems[0].(map[string]any)
	if !ok || int64(waitingRow["id"].(float64)) != waitingItem.ID {
		t.Fatalf("waiting row = %#v, want id %d", waitingRow, waitingItem.ID)
	}

	rrDone := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/done?limit=1", nil)
	if rrDone.Code != http.StatusOK {
		t.Fatalf("done status = %d, want 200: %s", rrDone.Code, rrDone.Body.String())
	}
	donePayload := decodeJSONResponse(t, rrDone)
	doneItems, ok := donePayload["items"].([]any)
	if !ok || len(doneItems) != 1 {
		t.Fatalf("done payload = %#v", donePayload)
	}
	doneRow, ok := doneItems[0].(map[string]any)
	if !ok || int64(doneRow["id"].(float64)) != doneItem.ID {
		t.Fatalf("done row = %#v, want id %d", doneRow, doneItem.ID)
	}

	rrCounts := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/counts", nil)
	if rrCounts.Code != http.StatusOK {
		t.Fatalf("counts status = %d, want 200: %s", rrCounts.Code, rrCounts.Body.String())
	}
	countsPayload := decodeJSONResponse(t, rrCounts)
	counts, ok := countsPayload["counts"].(map[string]any)
	if !ok {
		t.Fatalf("counts payload = %#v", countsPayload)
	}
	if got := int(counts[store.ItemStateInbox].(float64)); got != 1 {
		t.Fatalf("counts[inbox] = %d, want 1", got)
	}
	if got := int(counts[store.ItemStateWaiting].(float64)); got != 1 {
		t.Fatalf("counts[waiting] = %d, want 1", got)
	}
	if got := int(counts[store.ItemStateSomeday].(float64)); got != 1 {
		t.Fatalf("counts[someday] = %d, want 1", got)
	}
	if got := int(counts[store.ItemStateDone].(float64)); got != 1 {
		t.Fatalf("counts[done] = %d, want 1", got)
	}

	rrBadLimit := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/done?limit=bad", nil)
	if rrBadLimit.Code != http.StatusBadRequest {
		t.Fatalf("bad done limit status = %d, want 400: %s", rrBadLimit.Code, rrBadLimit.Body.String())
	}
}

func TestItemDomainAPIRejectsConflictAndInvalidState(t *testing.T) {
	app := newAuthedTestApp(t)

	rrConflict := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items", map[string]any{
		"title":        "Bad foreign key",
		"workspace_id": 999999,
	})
	if rrConflict.Code != http.StatusConflict {
		t.Fatalf("create item conflict status = %d, want 409: %s", rrConflict.Code, rrConflict.Body.String())
	}

	item, err := app.store.CreateItem("Stateful item", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rrBadState := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/state", map[string]any{
		"state": "paused",
	})
	if rrBadState.Code != http.StatusBadRequest {
		t.Fatalf("bad state status = %d, want 400: %s", rrBadState.Code, rrBadState.Body.String())
	}
}

func TestItemAssignmentAPIRejectsMissingActor(t *testing.T) {
	app := newAuthedTestApp(t)

	item, err := app.store.CreateItem("Delegate this", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/assign", map[string]any{
		"actor_id": 999,
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("assign missing actor status = %d, want 400: %s", rr.Code, rr.Body.String())
	}
}

func TestItemAssignmentAPIRejectsDoneItems(t *testing.T) {
	app := newAuthedTestApp(t)

	actor, err := app.store.CreateActor("Codex", store.ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	item, err := app.store.CreateItem("Done already", store.ItemOptions{
		State:   store.ItemStateWaiting,
		ActorID: &actor.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if err := app.store.CompleteItemByActor(item.ID, actor.ID); err != nil {
		t.Fatalf("CompleteItemByActor() error: %v", err)
	}

	rrAssign := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/assign", map[string]any{
		"actor_id": actor.ID,
	})
	if rrAssign.Code != http.StatusBadRequest {
		t.Fatalf("assign done item status = %d, want 400: %s", rrAssign.Code, rrAssign.Body.String())
	}

	rrUnassign := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/unassign", map[string]any{})
	if rrUnassign.Code != http.StatusBadRequest {
		t.Fatalf("unassign done item status = %d, want 400: %s", rrUnassign.Code, rrUnassign.Body.String())
	}
}

func TestItemCompletionAPIRejectsWrongActorAndMissingItem(t *testing.T) {
	app := newAuthedTestApp(t)

	owner, err := app.store.CreateActor("Owner", store.ActorKindHuman)
	if err != nil {
		t.Fatalf("CreateActor(owner) error: %v", err)
	}
	other, err := app.store.CreateActor("Other", store.ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor(other) error: %v", err)
	}
	item, err := app.store.CreateItem("Assigned task", store.ItemOptions{
		State:   store.ItemStateWaiting,
		ActorID: &owner.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rrWrongActor := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/"+itoa(item.ID)+"/complete", map[string]any{
		"actor_id": other.ID,
	})
	if rrWrongActor.Code != http.StatusBadRequest {
		t.Fatalf("complete wrong actor status = %d, want 400: %s", rrWrongActor.Code, rrWrongActor.Body.String())
	}

	rrMissingItem := doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/items/999999/complete", map[string]any{
		"actor_id": owner.ID,
	})
	if rrMissingItem.Code != http.StatusNotFound {
		t.Fatalf("complete missing item status = %d, want 404: %s", rrMissingItem.Code, rrMissingItem.Body.String())
	}
}

func TestItemTriageAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	actor, err := app.store.CreateActor("Codex", store.ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}

	doneItem, err := app.store.CreateItem("Done me", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}
	rrDone := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/"+itoa(doneItem.ID)+"/triage", map[string]any{
		"action": "done",
	})
	if rrDone.Code != http.StatusOK {
		t.Fatalf("triage done status = %d, want 200: %s", rrDone.Code, rrDone.Body.String())
	}
	gotDone, err := app.store.GetItem(doneItem.ID)
	if err != nil {
		t.Fatalf("GetItem(done) error: %v", err)
	}
	if gotDone.State != store.ItemStateDone {
		t.Fatalf("done state = %q, want %q", gotDone.State, store.ItemStateDone)
	}

	laterItem, err := app.store.CreateItem("Later me", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(later) error: %v", err)
	}
	visibleAfter := "2026-03-10T09:00:00Z"
	rrLater := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/"+itoa(laterItem.ID)+"/triage", map[string]any{
		"action":        "later",
		"visible_after": visibleAfter,
	})
	if rrLater.Code != http.StatusOK {
		t.Fatalf("triage later status = %d, want 200: %s", rrLater.Code, rrLater.Body.String())
	}
	gotLater, err := app.store.GetItem(laterItem.ID)
	if err != nil {
		t.Fatalf("GetItem(later) error: %v", err)
	}
	if gotLater.State != store.ItemStateWaiting {
		t.Fatalf("later state = %q, want %q", gotLater.State, store.ItemStateWaiting)
	}
	if gotLater.VisibleAfter == nil || *gotLater.VisibleAfter != visibleAfter {
		t.Fatalf("later visible_after = %v, want %q", gotLater.VisibleAfter, visibleAfter)
	}

	delegateItem, err := app.store.CreateItem("Delegate me", store.ItemOptions{
		VisibleAfter: &visibleAfter,
	})
	if err != nil {
		t.Fatalf("CreateItem(delegate) error: %v", err)
	}
	rrDelegate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/"+itoa(delegateItem.ID)+"/triage", map[string]any{
		"action":   "delegate",
		"actor_id": actor.ID,
	})
	if rrDelegate.Code != http.StatusOK {
		t.Fatalf("triage delegate status = %d, want 200: %s", rrDelegate.Code, rrDelegate.Body.String())
	}
	gotDelegate, err := app.store.GetItem(delegateItem.ID)
	if err != nil {
		t.Fatalf("GetItem(delegate) error: %v", err)
	}
	if gotDelegate.State != store.ItemStateWaiting {
		t.Fatalf("delegate state = %q, want %q", gotDelegate.State, store.ItemStateWaiting)
	}
	if gotDelegate.ActorID == nil || *gotDelegate.ActorID != actor.ID {
		t.Fatalf("delegate actor = %v, want %d", gotDelegate.ActorID, actor.ID)
	}
	if gotDelegate.VisibleAfter != nil {
		t.Fatalf("delegate visible_after = %v, want nil", gotDelegate.VisibleAfter)
	}

	somedayItem, err := app.store.CreateItem("Someday me", store.ItemOptions{
		VisibleAfter: &visibleAfter,
		FollowUpAt:   &visibleAfter,
	})
	if err != nil {
		t.Fatalf("CreateItem(someday) error: %v", err)
	}
	rrSomeday := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/"+itoa(somedayItem.ID)+"/triage", map[string]any{
		"action": "someday",
	})
	if rrSomeday.Code != http.StatusOK {
		t.Fatalf("triage someday status = %d, want 200: %s", rrSomeday.Code, rrSomeday.Body.String())
	}
	gotSomeday, err := app.store.GetItem(somedayItem.ID)
	if err != nil {
		t.Fatalf("GetItem(someday) error: %v", err)
	}
	if gotSomeday.State != store.ItemStateSomeday {
		t.Fatalf("someday state = %q, want %q", gotSomeday.State, store.ItemStateSomeday)
	}
	if gotSomeday.VisibleAfter != nil || gotSomeday.FollowUpAt != nil {
		t.Fatalf("someday timestamps = visible_after:%v follow_up_at:%v, want nil", gotSomeday.VisibleAfter, gotSomeday.FollowUpAt)
	}

	deleteItem, err := app.store.CreateItem("Delete me", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(delete) error: %v", err)
	}
	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/"+itoa(deleteItem.ID)+"/triage", map[string]any{
		"action": "delete",
	})
	if rrDelete.Code != http.StatusOK {
		t.Fatalf("triage delete status = %d, want 200: %s", rrDelete.Code, rrDelete.Body.String())
	}
	if _, err := app.store.GetItem(deleteItem.ID); err == nil {
		t.Fatal("expected deleted item to be gone")
	} else if err != sql.ErrNoRows {
		t.Fatalf("GetItem(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestItemTriageAPIRejectsInvalidRequests(t *testing.T) {
	app := newAuthedTestApp(t)

	actor, err := app.store.CreateActor("Codex", store.ActorKindAgent)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	doneItem, err := app.store.CreateItem("Already done", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(done) error: %v", err)
	}
	if err := app.store.TriageItemDone(doneItem.ID); err != nil {
		t.Fatalf("TriageItemDone() setup error: %v", err)
	}
	inboxItem, err := app.store.CreateItem("Inbox item", store.ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem(inbox) error: %v", err)
	}

	for _, tc := range []struct {
		name   string
		path   string
		body   map[string]any
		status int
	}{
		{
			name:   "missing visible_after",
			path:   "/api/items/" + itoa(inboxItem.ID) + "/triage",
			body:   map[string]any{"action": "later"},
			status: http.StatusBadRequest,
		},
		{
			name:   "invalid visible_after",
			path:   "/api/items/" + itoa(inboxItem.ID) + "/triage",
			body:   map[string]any{"action": "later", "visible_after": "tomorrow"},
			status: http.StatusBadRequest,
		},
		{
			name:   "missing actor",
			path:   "/api/items/" + itoa(inboxItem.ID) + "/triage",
			body:   map[string]any{"action": "delegate"},
			status: http.StatusBadRequest,
		},
		{
			name:   "unknown actor",
			path:   "/api/items/" + itoa(inboxItem.ID) + "/triage",
			body:   map[string]any{"action": "delegate", "actor_id": actor.ID + 999},
			status: http.StatusBadRequest,
		},
		{
			name:   "unknown action",
			path:   "/api/items/" + itoa(inboxItem.ID) + "/triage",
			body:   map[string]any{"action": "archive"},
			status: http.StatusBadRequest,
		},
		{
			name:   "missing item",
			path:   "/api/items/999999/triage",
			body:   map[string]any{"action": "done"},
			status: http.StatusNotFound,
		},
		{
			name:   "done item",
			path:   "/api/items/" + itoa(doneItem.ID) + "/triage",
			body:   map[string]any{"action": "someday"},
			status: http.StatusBadRequest,
		},
	} {
		rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, tc.path, tc.body)
		if rr.Code != tc.status {
			t.Fatalf("%s status = %d, want %d: %s", tc.name, rr.Code, tc.status, rr.Body.String())
		}
	}
}
