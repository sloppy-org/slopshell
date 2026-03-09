package web

import (
	"net/http"
	"os"
	"path/filepath"
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
	if rrCreate.Code != http.StatusCreated {
		t.Fatalf("create artifact status = %d, want 201: %s", rrCreate.Code, rrCreate.Body.String())
	}
	createPayload := decodeJSONDataResponse(t, rrCreate)
	artifactPayload, ok := createPayload["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("create artifact payload = %#v", createPayload)
	}
	artifactID := int64(artifactPayload["id"].(float64))

	rrList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts?kind=markdown", nil)
	if rrList.Code != http.StatusOK {
		t.Fatalf("list artifacts status = %d, want 200: %s", rrList.Code, rrList.Body.String())
	}
	listPayload := decodeJSONDataResponse(t, rrList)
	artifacts, ok := listPayload["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("list artifacts payload = %#v", listPayload)
	}

	rrGet := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts/"+itoa(artifactID), nil)
	if rrGet.Code != http.StatusOK {
		t.Fatalf("get artifact status = %d, want 200: %s", rrGet.Code, rrGet.Body.String())
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/artifacts/"+itoa(artifactID), nil)
	if rrDelete.Code != http.StatusNoContent {
		t.Fatalf("delete artifact status = %d, want 204: %s", rrDelete.Code, rrDelete.Body.String())
	}
	if rrDelete.Body.Len() != 0 {
		t.Fatalf("delete artifact body = %q, want empty", rrDelete.Body.String())
	}

	rrMissing := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts/"+itoa(artifactID), nil)
	if rrMissing.Code != http.StatusNotFound {
		t.Fatalf("deleted artifact status = %d, want 404: %s", rrMissing.Code, rrMissing.Body.String())
	}
}

func TestArtifactListAPIIncludesLinkedArtifactsForWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)

	sourceDir := filepath.Join(t.TempDir(), "source")
	targetDir := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	sourceWorkspace, err := app.store.CreateWorkspace("Source", sourceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(source) error: %v", err)
	}
	targetWorkspace, err := app.store.CreateWorkspace("Target", targetDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(target) error: %v", err)
	}

	sourcePath := filepath.Join(sourceDir, "results.pdf")
	if err := os.WriteFile(sourcePath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write source artifact: %v", err)
	}
	sourceTitle := "results.pdf"
	sourceArtifact, err := app.store.CreateArtifact(store.ArtifactKindPDF, &sourcePath, nil, &sourceTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(source) error: %v", err)
	}
	targetPath := filepath.Join(targetDir, "notes.md")
	if err := os.WriteFile(targetPath, []byte("# notes\n"), 0o644); err != nil {
		t.Fatalf("write target artifact: %v", err)
	}
	targetTitle := "notes.md"
	targetArtifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &targetPath, nil, &targetTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(target) error: %v", err)
	}
	if err := app.store.LinkArtifactToWorkspace(targetWorkspace.ID, sourceArtifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace() error: %v", err)
	}

	rrWorkspaceList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts?workspace_id="+itoa(targetWorkspace.ID), nil)
	if rrWorkspaceList.Code != http.StatusOK {
		t.Fatalf("workspace list status = %d, want 200: %s", rrWorkspaceList.Code, rrWorkspaceList.Body.String())
	}
	workspacePayload := decodeJSONDataResponse(t, rrWorkspaceList)
	workspaceArtifacts, ok := workspacePayload["artifacts"].([]any)
	if !ok || len(workspaceArtifacts) != 2 {
		t.Fatalf("workspace artifacts payload = %#v", workspacePayload)
	}
	seen := map[int64]bool{}
	for _, raw := range workspaceArtifacts {
		entry, _ := raw.(map[string]any)
		seen[int64(entry["id"].(float64))] = true
	}
	if !seen[sourceArtifact.ID] || !seen[targetArtifact.ID] {
		t.Fatalf("workspace artifact ids = %#v", seen)
	}

	rrLinkedList := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts?workspace_id="+itoa(targetWorkspace.ID)+"&linked=true", nil)
	if rrLinkedList.Code != http.StatusOK {
		t.Fatalf("linked list status = %d, want 200: %s", rrLinkedList.Code, rrLinkedList.Body.String())
	}
	linkedPayload := decodeJSONDataResponse(t, rrLinkedList)
	linkedArtifacts, ok := linkedPayload["artifacts"].([]any)
	if !ok || len(linkedArtifacts) != 1 {
		t.Fatalf("linked artifacts payload = %#v", linkedPayload)
	}
	if got := int64(linkedArtifacts[0].(map[string]any)["id"].(float64)); got != sourceArtifact.ID {
		t.Fatalf("linked artifact id = %d, want %d", got, sourceArtifact.ID)
	}

	rrBadWorkspace := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts?workspace_id=bad", nil)
	if rrBadWorkspace.Code != http.StatusBadRequest {
		t.Fatalf("bad workspace_id status = %d, want 400: %s", rrBadWorkspace.Code, rrBadWorkspace.Body.String())
	}
	if got := decodeJSONResponse(t, rrBadWorkspace)["error"]; got != "workspace_id must be a positive integer" {
		t.Fatalf("bad workspace_id error = %#v", got)
	}

	rrSourceLinked := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/artifacts?workspace_id="+itoa(sourceWorkspace.ID)+"&linked=true", nil)
	if rrSourceLinked.Code != http.StatusOK {
		t.Fatalf("source linked list status = %d, want 200: %s", rrSourceLinked.Code, rrSourceLinked.Body.String())
	}
	sourceLinkedPayload := decodeJSONDataResponse(t, rrSourceLinked)
	sourceLinkedArtifacts, ok := sourceLinkedPayload["artifacts"].([]any)
	if !ok || len(sourceLinkedArtifacts) != 0 {
		t.Fatalf("source linked artifacts payload = %#v", sourceLinkedPayload)
	}
}
