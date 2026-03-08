package web

import (
	"net/http"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestArtifactCRUDAPI(t *testing.T) {
	app := newAuthedTestApp(t)
	refPath := "/tmp/spec.md"
	refURL := "https://example.com/spec"
	title := "Spec"
	metaJSON := `{"source":"test"}`

	rrCreate := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/artifacts", map[string]any{
		"kind":      string(store.ArtifactKindMarkdown),
		"ref_path":  refPath,
		"ref_url":   refURL,
		"title":     title,
		"meta_json": metaJSON,
	})
	if rrCreate.Code != http.StatusOK {
		t.Fatalf("create artifact status = %d, want 200: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createPayload := decodeJSONResponse(t, rrCreate)
	artifactPayload, ok := createPayload["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("create artifact payload = %#v", createPayload)
	}
	artifactID := int64(artifactPayload["id"].(float64))

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts?kind=markdown", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("list artifacts status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	listPayload := decodeJSONResponse(t, rrList)
	artifacts, ok := listPayload["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("list artifacts payload = %#v", listPayload)
	}

	rrGet := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts/"+itoa(artifactID), nil)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("get artifact status = %d, want 200: %s", rrGet.Code, rrGet.Body.String())
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/artifacts/"+itoa(artifactID), nil)
	if rrDelete.Code != http.StatusOK {
		t.Fatalf("delete artifact status = %d, want 200: %s", rrDelete.Code, rrDelete.Body.String())
	}

	rrMissing := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts/"+itoa(artifactID), nil)
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("deleted artifact status = %d, want 404: %s", rrMissing.Code, rrMissing.Body.String())
	}
}
