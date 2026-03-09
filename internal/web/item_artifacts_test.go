package web

import (
	"net/http"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func mustArtifactIDFromItemArtifactsPayload(t *testing.T, payload map[string]any, index int) int64 {
	t.Helper()
	artifacts, ok := payload["artifacts"].([]any)
	if !ok || len(artifacts) <= index {
		t.Fatalf("artifacts payload = %#v", payload["artifacts"])
	}
	entry, ok := artifacts[index].(map[string]any)
	if !ok {
		t.Fatalf("artifact entry = %#v", artifacts[index])
	}
	artifact, ok := entry["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("nested artifact payload = %#v", entry["artifact"])
	}
	return int64(artifact["id"].(float64))
}

func mustRoleFromItemArtifactsPayload(t *testing.T, payload map[string]any, index int) string {
	t.Helper()
	artifacts, ok := payload["artifacts"].([]any)
	if !ok || len(artifacts) <= index {
		t.Fatalf("artifacts payload = %#v", payload["artifacts"])
	}
	entry, ok := artifacts[index].(map[string]any)
	if !ok {
		t.Fatalf("artifact entry = %#v", artifacts[index])
	}
	return strFromAny(entry["role"])
}

func TestItemArtifactsAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	sourceTitle := "Source note"
	sourceArtifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, nil, nil, &sourceTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(source) error: %v", err)
	}
	relatedTitle := "Related email"
	relatedArtifact, err := app.store.CreateArtifact(store.ArtifactKindEmail, nil, nil, &relatedTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(related) error: %v", err)
	}
	outputTitle := "Output PDF"
	outputArtifact, err := app.store.CreateArtifact(store.ArtifactKindPDF, nil, nil, &outputTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(output) error: %v", err)
	}
	item, err := app.store.CreateItem("Review artifacts", store.ItemOptions{ArtifactID: &sourceArtifact.ID})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rrInitial := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/"+itoa(item.ID)+"/artifacts", nil)
	if rrInitial.Code != http.StatusOK {
		t.Fatalf("initial list status = %d, want 200: %s", rrInitial.Code, rrInitial.Body.String())
	}
	initialPayload := decodeJSONResponse(t, rrInitial)
	if got := mustArtifactIDFromItemArtifactsPayload(t, initialPayload, 0); got != sourceArtifact.ID {
		t.Fatalf("initial primary artifact = %d, want %d", got, sourceArtifact.ID)
	}
	if got := mustRoleFromItemArtifactsPayload(t, initialPayload, 0); got != "source" {
		t.Fatalf("initial primary role = %q, want source", got)
	}

	rrLinkRelated := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/"+itoa(item.ID)+"/artifacts", map[string]any{
		"artifact_id": relatedArtifact.ID,
		"role":        "related",
	})
	if rrLinkRelated.Code != http.StatusCreated {
		t.Fatalf("link related status = %d, want 201: %s", rrLinkRelated.Code, rrLinkRelated.Body.String())
	}

	rrLinkSource := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/items/"+itoa(item.ID)+"/artifacts", map[string]any{
		"artifact_id": outputArtifact.ID,
		"role":        "source",
	})
	if rrLinkSource.Code != http.StatusCreated {
		t.Fatalf("link source status = %d, want 201: %s", rrLinkSource.Code, rrLinkSource.Body.String())
	}
	sourcePayload := decodeJSONDataResponse(t, rrLinkSource)
	if got := mustArtifactIDFromItemArtifactsPayload(t, sourcePayload, 0); got != outputArtifact.ID {
		t.Fatalf("linked primary artifact = %d, want %d", got, outputArtifact.ID)
	}
	if got := mustRoleFromItemArtifactsPayload(t, sourcePayload, 0); got != "source" {
		t.Fatalf("linked primary role = %q, want source", got)
	}

	updatedItem, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(updated) error: %v", err)
	}
	if updatedItem.ArtifactID == nil || *updatedItem.ArtifactID != outputArtifact.ID {
		t.Fatalf("GetItem(updated).ArtifactID = %v, want %d", updatedItem.ArtifactID, outputArtifact.ID)
	}

	rrArtifactItems := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts/"+itoa(relatedArtifact.ID)+"/items", nil)
	if rrArtifactItems.Code != http.StatusOK {
		t.Fatalf("artifact items status = %d, want 200: %s", rrArtifactItems.Code, rrArtifactItems.Body.String())
	}
	artifactItemsPayload := decodeJSONResponse(t, rrArtifactItems)
	items, ok := artifactItemsPayload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("artifact items payload = %#v", artifactItemsPayload)
	}

	rrUnlinkPrimary := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/items/"+itoa(item.ID)+"/artifacts/"+itoa(outputArtifact.ID), nil)
	if rrUnlinkPrimary.Code != http.StatusOK {
		t.Fatalf("unlink primary status = %d, want 200: %s", rrUnlinkPrimary.Code, rrUnlinkPrimary.Body.String())
	}
	unlinkPayload := decodeJSONDataResponse(t, rrUnlinkPrimary)
	if got := mustArtifactIDFromItemArtifactsPayload(t, unlinkPayload, 0); got != sourceArtifact.ID {
		t.Fatalf("restored primary artifact = %d, want %d", got, sourceArtifact.ID)
	}
	if got := mustRoleFromItemArtifactsPayload(t, unlinkPayload, 0); got != "source" {
		t.Fatalf("restored primary role = %q, want source", got)
	}

	restoredItem, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(restored) error: %v", err)
	}
	if restoredItem.ArtifactID == nil || *restoredItem.ArtifactID != sourceArtifact.ID {
		t.Fatalf("GetItem(restored).ArtifactID = %v, want %d", restoredItem.ArtifactID, sourceArtifact.ID)
	}
}
