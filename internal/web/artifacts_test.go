package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestArtifactTaxonomyAPI(t *testing.T) {
	app := newAuthedTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/artifacts/taxonomy", nil)
	rr := httptest.NewRecorder()

	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("taxonomy status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	payload := decodeJSONResponse(t, rr)
	order, ok := payload["canonical_action_order"].([]any)
	if !ok || len(order) != 7 {
		t.Fatalf("canonical_action_order = %#v", payload["canonical_action_order"])
	}
	actions, ok := payload["actions"].(map[string]any)
	if !ok {
		t.Fatalf("actions = %#v", payload["actions"])
	}
	if _, ok := actions["compose"]; !ok {
		t.Fatalf("compose action missing from %#v", actions)
	}
	kinds, ok := payload["kinds"].(map[string]any)
	if !ok {
		t.Fatalf("kinds = %#v", payload["kinds"])
	}
	emailThread, ok := kinds["email_thread"].(map[string]any)
	if !ok {
		t.Fatalf("email_thread kind = %#v", kinds["email_thread"])
	}
	if got := strFromAny(emailThread["canvas_surface"]); got != "text_artifact" {
		t.Fatalf("email_thread canvas_surface = %q, want text_artifact", got)
	}
	if got := strFromAny(emailThread["preferred_tool"]); got != "text_note" {
		t.Fatalf("email_thread preferred_tool = %q, want text_note", got)
	}
	if got := boolFromAny(emailThread["mail_actions"]); !got {
		t.Fatalf("email_thread mail_actions = %#v, want true", emailThread["mail_actions"])
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

func TestArtifactFigureExtractAPIProducesImageArtifactsAndLinksOutputs(t *testing.T) {
	app := newAuthedTestApp(t)
	binDir := t.TempDir()
	writeTestExecutable(t, filepath.Join(binDir, "pdfimages"), `#!/bin/sh
set -eu
if [ "$1" = "-list" ]; then
cat <<'EOF'
page   num  type   width height color comp bpc  enc interp  object ID x-ppi y-ppi size ratio
--------------------------------------------------------------------------------------------
   2     0 image     640   480  rgb    3   8  image  no         7  0    72    72  12K 1.3%
EOF
exit 0
fi
if [ "$1" = "-png" ]; then
prefix="$3"
printf 'PNGDATA' > "${prefix}-000.png"
exit 0
fi
echo "unexpected args: $*" >&2
exit 1
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	workspaceDir := filepath.Join(t.TempDir(), "paper-workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceDir) error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Paper Workspace", workspaceDir, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	pdfPath := filepath.Join(workspaceDir, "paper.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("WriteFile(paper.pdf) error: %v", err)
	}
	title := "paper.pdf"
	sourceArtifact, err := app.store.CreateArtifact(store.ArtifactKindPDF, &pdfPath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(source) error: %v", err)
	}
	item, err := app.store.CreateItem("Review extracted figures", store.ItemOptions{WorkspaceID: &workspace.ID, ArtifactID: &sourceArtifact.ID})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/artifacts/"+itoa(sourceArtifact.ID)+"/extract-figures", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("extract figures status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONDataResponse(t, rr)
	artifacts, ok := payload["artifacts"].([]any)
	if !ok || len(artifacts) != 1 {
		t.Fatalf("extract artifacts payload = %#v", payload)
	}
	artifactPayload, ok := artifacts[0].(map[string]any)
	if !ok {
		t.Fatalf("artifact payload = %#v", artifacts[0])
	}
	if got := strFromAny(artifactPayload["kind"]); got != string(store.ArtifactKindImage) {
		t.Fatalf("artifact kind = %q, want %q", got, store.ArtifactKindImage)
	}
	refPath := strFromAny(artifactPayload["ref_path"])
	if !strings.HasPrefix(filepath.ToSlash(refPath), filepath.ToSlash(filepath.Join(workspaceDir, ".tabura", "artifacts", "figures"))+"/") {
		t.Fatalf("ref_path = %q, want workspace figures artifact path", refPath)
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(strFromAny(artifactPayload["meta_json"])), &meta); err != nil {
		t.Fatalf("artifact meta_json error: %v", err)
	}
	if got := intFromAny(meta["source_artifact_id"], 0); got != int(sourceArtifact.ID) {
		t.Fatalf("source_artifact_id = %d, want %d", got, sourceArtifact.ID)
	}
	if got := intFromAny(meta["page"], 0); got != 2 {
		t.Fatalf("page = %d, want 2", got)
	}
	if got := strFromAny(meta["type"]); got != "figure" {
		t.Fatalf("meta type = %q, want figure", got)
	}

	figureArtifactID := int64(artifactPayload["id"].(float64))
	linkedItems, err := app.store.ListArtifactItems(figureArtifactID)
	if err != nil {
		t.Fatalf("ListArtifactItems() error: %v", err)
	}
	if len(linkedItems) != 1 || linkedItems[0].ID != item.ID {
		t.Fatalf("ListArtifactItems() = %+v, want only item %d", linkedItems, item.ID)
	}
	itemArtifacts, err := app.store.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts() error: %v", err)
	}
	foundOutput := false
	for _, entry := range itemArtifacts {
		if entry.ArtifactID == figureArtifactID && entry.Role == "output" {
			foundOutput = true
			break
		}
	}
	if !foundOutput {
		t.Fatalf("item artifact links = %+v, want output link to artifact %d", itemArtifacts, figureArtifactID)
	}
}

func TestArtifactFigureExtractAPIRejectsNonPDFArtifacts(t *testing.T) {
	app := newAuthedTestApp(t)
	title := "notes.md"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, nil, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/artifacts/"+itoa(artifact.ID)+"/extract-figures", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("extract figures status = %d, want 400: %s", rr.Code, rr.Body.String())
	}
	if got := decodeJSONResponse(t, rr)["error"]; got != "artifact must be a pdf" {
		t.Fatalf("extract figures error = %#v, want artifact must be a pdf", got)
	}
}

func writeTestExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%s) error: %v", path, err)
	}
}
