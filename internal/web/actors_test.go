package web

import (
	"net/http"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestActorCRUDAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/actors", map[string]any{
		"name": "Codex",
		"kind": store.ActorKindAgent,
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("create actor status = %d, want 200: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createPayload := decodeJSONResponse(t, rrCreate)
	actorPayload, ok := createPayload["actor"].(map[string]any)
	if !ok {
		t.Fatalf("create actor payload = %#v", createPayload)
	}
	actorID := int64(actorPayload["id"].(float64))

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/actors", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("list actors status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	listPayload := decodeJSONResponse(t, rrList)
	actors, ok := listPayload["actors"].([]any)
	if !ok || len(actors) != 1 {
		t.Fatalf("list actors payload = %#v", listPayload)
	}

	rrGet := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/actors/"+itoa(actorID), nil)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("get actor status = %d, want 200: %s", rrGet.Code, rrGet.Body.String())
	}

	rrBadCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/actors", map[string]any{
		"name": "Nobody",
		"kind": "robot",
	})
	if rrBadCreate.Code != http.StatusBadRequest {
		t.Fatalf("invalid actor status = %d, want 400: %s", rrBadCreate.Code, rrBadCreate.Body.String())
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/actors/"+itoa(actorID), nil)
	if rrDelete.Code != http.StatusOK {
		t.Fatalf("delete actor status = %d, want 200: %s", rrDelete.Code, rrDelete.Body.String())
	}

	rrMissing := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/actors/"+itoa(actorID), nil)
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("deleted actor status = %d, want 404: %s", rrMissing.Code, rrMissing.Body.String())
	}
}
