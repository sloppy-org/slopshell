package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestMaterializeArtifactWritesEmailEMLAndUpdatesRefPath(t *testing.T) {
	st := newMaterializeTestStore(t)
	workspaceDir := t.TempDir()
	title := "Quarterly follow-up"
	meta := `{"subject":"Quarterly follow-up","sender":"alice@example.com","recipients":["bob@example.com"],"date":"2026-03-10T12:00:00Z","body":"Hello Bob\nPlease review."}`
	artifact, err := st.CreateArtifact(store.ArtifactKindEmail, nil, nil, stringPtr(title), stringPtr(meta))
	if err != nil {
		t.Fatalf("CreateArtifact(email) error: %v", err)
	}

	result, err := materializeArtifact(st, artifact.ID, workspaceDir, true)
	if err != nil {
		t.Fatalf("materializeArtifact(email) error: %v", err)
	}
	if filepath.Ext(result.AbsolutePath) != ".eml" {
		t.Fatalf("materialized email ext = %q, want .eml", filepath.Ext(result.AbsolutePath))
	}
	content, err := os.ReadFile(result.AbsolutePath)
	if err != nil {
		t.Fatalf("ReadFile(email) error: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"Subject: Quarterly follow-up",
		"From: alice@example.com",
		"To: bob@example.com",
		"Hello Bob",
		"Please review.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("materialized email missing %q in %q", want, text)
		}
	}
	updated, err := st.GetArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetArtifact(updated email) error: %v", err)
	}
	if updated.RefPath == nil || *updated.RefPath != result.AbsolutePath {
		t.Fatalf("updated ref_path = %v, want %q", updated.RefPath, result.AbsolutePath)
	}
}

func TestMaterializeArtifactWritesGitHubIssueMarkdown(t *testing.T) {
	st := newMaterializeTestStore(t)
	workspaceDir := t.TempDir()
	title := "Parser panic"
	refURL := "https://github.com/owner/repo/issues/77"
	meta := `{"owner_repo":"owner/repo","number":77,"state":"open","labels":["bug","parser"],"assignees":["octocat"]}`
	artifact, err := st.CreateArtifact(store.ArtifactKindGitHubIssue, nil, &refURL, stringPtr(title), stringPtr(meta))
	if err != nil {
		t.Fatalf("CreateArtifact(github issue) error: %v", err)
	}

	result, err := materializeArtifact(st, artifact.ID, workspaceDir, true)
	if err != nil {
		t.Fatalf("materializeArtifact(github issue) error: %v", err)
	}
	if filepath.Ext(result.AbsolutePath) != ".md" {
		t.Fatalf("materialized github issue ext = %q, want .md", filepath.Ext(result.AbsolutePath))
	}
	content, err := os.ReadFile(result.AbsolutePath)
	if err != nil {
		t.Fatalf("ReadFile(markdown) error: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"# Parser panic",
		"- Source URL: https://github.com/owner/repo/issues/77",
		"- State: open",
		"- Labels: bug, parser",
		"- Assignees: octocat",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("materialized markdown missing %q in %q", want, text)
		}
	}
}

func TestMaterializeArtifactWritesCalendarICS(t *testing.T) {
	st := newMaterializeTestStore(t)
	workspaceDir := t.TempDir()
	title := "Design review"
	meta := `{"summary":"Design review","description":"Review archive flow","location":"Room 1","start":"2026-03-12T09:00:00Z","end":"2026-03-12T10:00:00Z"}`
	artifact, err := st.CreateArtifact(store.ArtifactKind("calendar_event"), nil, nil, stringPtr(title), stringPtr(meta))
	if err != nil {
		t.Fatalf("CreateArtifact(calendar event) error: %v", err)
	}

	result, err := materializeArtifact(st, artifact.ID, workspaceDir, true)
	if err != nil {
		t.Fatalf("materializeArtifact(calendar event) error: %v", err)
	}
	if filepath.Ext(result.AbsolutePath) != ".ics" {
		t.Fatalf("materialized calendar ext = %q, want .ics", filepath.Ext(result.AbsolutePath))
	}
	content, err := os.ReadFile(result.AbsolutePath)
	if err != nil {
		t.Fatalf("ReadFile(ics) error: %v", err)
	}
	text := string(content)
	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"SUMMARY:Design review",
		"DTSTART:20260312T090000Z",
		"DTEND:20260312T100000Z",
		"LOCATION:Room 1",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("materialized ics missing %q in %q", want, text)
		}
	}
}

func TestArchiveWorkspaceWritesManifestAndSelfContainedArtifacts(t *testing.T) {
	st := newMaterializeTestStore(t)
	workspaceDir := t.TempDir()
	projectName := filepath.Base(workspaceDir)
	project, err := st.CreateProject(projectName, "archive-key", workspaceDir, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	workspace, err := st.CreateWorkspace("Archive Workspace", workspaceDir, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if workspace.ProjectID == nil || *workspace.ProjectID != project.ID {
		t.Fatalf("workspace project_id = %v, want %q", workspace.ProjectID, project.ID)
	}

	emailTitle := "Customer follow-up"
	emailMeta := `{"subject":"Customer follow-up","sender":"sales@example.com","recipients":["team@example.com"],"labels":["important"],"body":"Thanks for the update."}`
	emailArtifact, err := st.CreateArtifact(store.ArtifactKindEmail, nil, nil, stringPtr(emailTitle), stringPtr(emailMeta))
	if err != nil {
		t.Fatalf("CreateArtifact(email) error: %v", err)
	}
	if err := st.LinkArtifactToWorkspace(workspace.ID, emailArtifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace(email) error: %v", err)
	}

	issueTitle := "Parser panic"
	issueURL := "https://github.com/owner/repo/issues/77"
	issueMeta := `{"owner_repo":"owner/repo","number":77,"state":"open","labels":["bug"]}`
	issueArtifact, err := st.CreateArtifact(store.ArtifactKindGitHubIssue, nil, &issueURL, stringPtr(issueTitle), stringPtr(issueMeta))
	if err != nil {
		t.Fatalf("CreateArtifact(github issue) error: %v", err)
	}
	if err := st.LinkArtifactToWorkspace(workspace.ID, issueArtifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace(github issue) error: %v", err)
	}

	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "notes.md")
	if err := os.WriteFile(outsidePath, []byte("# external notes\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside) error: %v", err)
	}
	fileTitle := "External Notes"
	fileArtifact, err := st.CreateArtifact(store.ArtifactKindMarkdown, &outsidePath, nil, stringPtr(fileTitle), nil)
	if err != nil {
		t.Fatalf("CreateArtifact(file) error: %v", err)
	}
	if err := st.LinkArtifactToWorkspace(workspace.ID, fileArtifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace(file) error: %v", err)
	}

	item, err := st.CreateItem("Follow up on parser", store.ItemOptions{
		State:       store.ItemStateWaiting,
		WorkspaceID: &workspace.ID,
		ArtifactID:  &issueArtifact.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	if item.ProjectID == nil || *item.ProjectID != project.ID {
		t.Fatalf("item project_id = %v, want %q", item.ProjectID, project.ID)
	}

	result, err := archiveWorkspace(st, workspaceDir)
	if err != nil {
		t.Fatalf("archiveWorkspace() error: %v", err)
	}
	if len(result.Artifacts) != 3 {
		t.Fatalf("archived artifact count = %d, want 3", len(result.Artifacts))
	}
	if len(result.Items) != 1 {
		t.Fatalf("archived item count = %d, want 1", len(result.Items))
	}
	if result.Materialized != 2 {
		t.Fatalf("materialized count = %d, want 2", result.Materialized)
	}
	if result.CopiedExisting != 1 {
		t.Fatalf("copied count = %d, want 1", result.CopiedExisting)
	}
	if !fileExists(result.ManifestPath) {
		t.Fatalf("manifest %q does not exist", result.ManifestPath)
	}
	for _, artifact := range result.Artifacts {
		if !fileExists(artifact.AbsolutePath) {
			t.Fatalf("artifact path %q does not exist", artifact.AbsolutePath)
		}
		if !pathWithinDir(artifact.AbsolutePath, workspaceDir) {
			t.Fatalf("artifact path %q is outside workspace %q", artifact.AbsolutePath, workspaceDir)
		}
	}

	raw, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatalf("ReadFile(manifest) error: %v", err)
	}
	var manifest archiveManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("Unmarshal(manifest) error: %v", err)
	}
	if manifest.Workspace.ProjectID == nil || *manifest.Workspace.ProjectID != project.ID {
		t.Fatalf("manifest workspace project_id = %v, want %q", manifest.Workspace.ProjectID, project.ID)
	}
	if len(manifest.Items) != 1 || manifest.Items[0].State != store.ItemStateWaiting {
		t.Fatalf("manifest items = %+v, want one waiting item", manifest.Items)
	}
	if !containsString(manifest.Items[0].Labels, project.ID) {
		t.Fatalf("manifest item labels = %v, want %q", manifest.Items[0].Labels, project.ID)
	}

	issueEntry := findArchiveArtifact(manifest.Artifacts, issueArtifact.ID)
	if issueEntry == nil {
		t.Fatalf("missing issue artifact %d in manifest", issueArtifact.ID)
	}
	if !strings.HasSuffix(issueEntry.MaterializedPath, ".md") {
		t.Fatalf("issue materialized_path = %q, want .md suffix", issueEntry.MaterializedPath)
	}
	if !containsString(issueEntry.Labels, "bug") || !containsString(issueEntry.Labels, project.ID) {
		t.Fatalf("issue labels = %v, want bug and %q", issueEntry.Labels, project.ID)
	}

	emailEntry := findArchiveArtifact(manifest.Artifacts, emailArtifact.ID)
	if emailEntry == nil || !strings.HasSuffix(emailEntry.MaterializedPath, ".eml") {
		t.Fatalf("email entry = %+v, want .eml path", emailEntry)
	}

	fileEntry := findArchiveArtifact(manifest.Artifacts, fileArtifact.ID)
	if fileEntry == nil || !strings.HasSuffix(fileEntry.MaterializedPath, ".md") {
		t.Fatalf("file entry = %+v, want copied .md path", fileEntry)
	}
	if fileEntry.OriginalRefPath == nil || *fileEntry.OriginalRefPath != outsidePath {
		t.Fatalf("file original_ref_path = %v, want %q", fileEntry.OriginalRefPath, outsidePath)
	}
}

func newMaterializeTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "tabura.db")
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func stringPtr(value string) *string {
	return &value
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func findArchiveArtifact(entries []archiveArtifactEntry, artifactID int64) *archiveArtifactEntry {
	for i := range entries {
		if entries[i].ID == artifactID {
			return &entries[i]
		}
	}
	return nil
}
