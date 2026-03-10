package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func doAuthedRequest(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: testAuthToken})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestItemPrintHTMLIncludesCoverSheetAndArtifactFileContent(t *testing.T) {
	app := newAuthedTestApp(t)

	workspaceDir := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Writer", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	actor, err := app.store.CreateActor("Alice", store.ActorKindHuman)
	if err != nil {
		t.Fatalf("CreateActor() error: %v", err)
	}
	artifactPath := filepath.Join(workspaceDir, "notes.md")
	if err := os.WriteFile(artifactPath, []byte("# Review\n\n- tighten cover sheet\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}
	artifactTitle := "notes.md"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindMarkdown, &artifactPath, nil, &artifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	source := "github"
	sourceRef := "owner/tabura#193"
	item, err := app.store.CreateItem("Print packet", store.ItemOptions{
		WorkspaceID: &workspace.ID,
		ArtifactID:  &artifact.ID,
		ActorID:     &actor.ID,
		Source:      &source,
		SourceRef:   &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rr := doAuthedRequest(t, app.Router(), http.MethodGet, "/api/items/"+itoa(item.ID)+"/print?format=html")
	if rr.Code != http.StatusOK {
		t.Fatalf("print html status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, snippet := range []string{
		"Printable item review packet",
		"Workspace",
		"Writer",
		"Alice",
		"github",
		"owner/tabura#193",
		`data-print-marker="line"`,
		"L001",
		"L002",
		"# Review",
		"- tighten cover sheet",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("print body missing %q:\n%s", snippet, body)
		}
	}
	if strings.Contains(body, "window.print()") {
		t.Fatalf("html-only print response should not auto-print:\n%s", body)
	}
}

func TestItemPrintEmailArtifactUsesParagraphMarkers(t *testing.T) {
	app := newAuthedTestApp(t)

	workspace, err := app.store.CreateWorkspace("Mail", filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	metaJSON := `{"text":"First paragraph.\n\nSecond paragraph."}`
	title := "Re: annotated thread"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindEmail, nil, nil, &title, &metaJSON)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Review email", store.ItemOptions{
		WorkspaceID: &workspace.ID,
		ArtifactID:  &artifact.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rr := doAuthedRequest(t, app.Router(), http.MethodGet, "/api/items/"+itoa(item.ID)+"/print?format=html")
	if rr.Code != http.StatusOK {
		t.Fatalf("print html status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, snippet := range []string{
		`data-print-marker="paragraph"`,
		"P01",
		"P02",
		"First paragraph.",
		"Second paragraph.",
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("print body missing %q:\n%s", snippet, body)
		}
	}
}

func TestItemPrintDefaultIncludesAutoPrintAndArtifactMetadataFallback(t *testing.T) {
	app := newAuthedTestApp(t)

	workspace, err := app.store.CreateWorkspace("Default", filepath.Join(t.TempDir(), "workspace"))
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	metaJSON := `{"source":"assistant","text":"Capture all open review notes."}`
	artifactTitle := "Review Notes"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindPlanNote, nil, nil, &artifactTitle, &metaJSON)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Review packet", store.ItemOptions{
		WorkspaceID: &workspace.ID,
		ArtifactID:  &artifact.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	rr := doAuthedRequest(t, app.Router(), http.MethodGet, "/api/items/"+itoa(item.ID)+"/print")
	if rr.Code != http.StatusOK {
		t.Fatalf("print status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, snippet := range []string{
		"window.print()",
		"Capture all open review notes.",
		"Artifact metadata",
		`&#34;source&#34;: &#34;assistant&#34;`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("print body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClassifyAndExecuteSystemActionPrintItemUsesActiveWorkspaceItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	item, err := app.store.CreateItem("Print me", store.ItemOptions{
		WorkspaceID: &workspace.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "print this item")
	if !handled {
		t.Fatal("expected print command to be handled")
	}
	if message != `Opened print view for "Print me".` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads len = %d, want 1", len(payloads))
	}
	payload := payloads[0]
	if got := strFromAny(payload["type"]); got != "print_item" {
		t.Fatalf("payload type = %q, want print_item", got)
	}
	if got := int64FromAny(payload["item_id"]); got != item.ID {
		t.Fatalf("payload item_id = %d, want %d", got, item.ID)
	}
	if got := strFromAny(payload["url"]); got != "/api/items/"+itoa(item.ID)+"/print" {
		t.Fatalf("payload url = %q", got)
	}
}
