package web

import (
	"net/http"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestActorCRUDAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/actors", map[string]any{
		"name":         "Codex",
		"kind":         store.ActorKindAgent,
		"email":        "codex@example.com",
		"provider":     "manual",
		"provider_ref": "codex-local",
		"meta_json":    `{"organization":"OpenAI"}`,
	})
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create actor status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createPayload := decodeJSONDataResponse(t, rrCreate)
	actorPayload, ok := createPayload["actor"].(map[string]any)
	if !ok {
		t.Fatalf("create actor payload = %#v", createPayload)
	}
	actorID := int64(actorPayload["id"].(float64))
	if got := strFromAny(actorPayload["email"]); got != "codex@example.com" {
		t.Fatalf("create actor email = %q, want codex@example.com", got)
	}
	if got := strFromAny(actorPayload["provider"]); got != "manual" {
		t.Fatalf("create actor provider = %q, want manual", got)
	}

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/actors", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("list actors status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	listPayload := decodeJSONDataResponse(t, rrList)
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
	if got := decodeJSONResponse(t, rrBadCreate)["error"]; got == nil {
		t.Fatalf("invalid actor payload = %#v, want error", decodeJSONResponse(t, rrBadCreate))
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/actors/"+itoa(actorID), nil)
	if rrDelete.Code != http.StatusNoContent {
		t.Fatalf("delete actor status = %d, want 204: %s", rrDelete.Code, rrDelete.Body.String())
	}
	if rrDelete.Body.Len() != 0 {
		t.Fatalf("delete actor body = %q, want empty", rrDelete.Body.String())
	}

	rrMissing := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/actors/"+itoa(actorID), nil)
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("deleted actor status = %d, want 404: %s", rrMissing.Code, rrMissing.Body.String())
	}
}
