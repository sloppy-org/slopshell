package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineArtifactLinkIntent(t *testing.T) {
	cases := []struct {
		text         string
		wantAction   string
		wantArtifact string
		wantSource   string
		wantTarget   string
		wantListFor  string
	}{
		{
			text:         "link results.pdf from eurofusion-sim to eurofusion-paper",
			wantAction:   "link_workspace_artifact",
			wantArtifact: "results.pdf",
			wantSource:   "eurofusion-sim",
			wantTarget:   "eurofusion-paper",
		},
		{
			text:       "show linked artifacts",
			wantAction: "list_linked_artifacts",
		},
		{
			text:        "list linked artifacts for paper workspace",
			wantAction:  "list_linked_artifacts",
			wantListFor: "paper workspace",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			action := parseInlineArtifactLinkIntent(tc.text)
			if action == nil {
				t.Fatal("expected artifact link action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if got := systemActionArtifactRef(action.Params); got != tc.wantArtifact {
				t.Fatalf("artifact ref = %q, want %q", got, tc.wantArtifact)
			}
			if got := strings.TrimSpace(systemActionStringParam(action.Params, "source_workspace")); tc.wantSource != "" && got != tc.wantSource {
				t.Fatalf("source_workspace = %q, want %q", got, tc.wantSource)
			}
			if got := strings.TrimSpace(systemActionStringParam(action.Params, "target_workspace")); tc.wantTarget != "" && got != tc.wantTarget {
				t.Fatalf("target_workspace = %q, want %q", got, tc.wantTarget)
			}
			if got := systemActionWorkspaceRef(action.Params); got != tc.wantListFor {
				t.Fatalf("workspace ref = %q, want %q", got, tc.wantListFor)
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionLinkWorkspaceArtifact(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	sourceDir := filepath.Join(t.TempDir(), "sim")
	targetDir := filepath.Join(t.TempDir(), "paper")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir sim: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir paper: %v", err)
	}
	sourceWorkspace, err := app.store.CreateWorkspace("eurofusion-sim", sourceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(sim) error: %v", err)
	}
	targetWorkspace, err := app.store.CreateWorkspace("eurofusion-paper", targetDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(paper) error: %v", err)
	}
	artifactPath := filepath.Join(sourceDir, "results.pdf")
	if err := os.WriteFile(artifactPath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write results.pdf: %v", err)
	}
	title := "results.pdf"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindPDF, &artifactPath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(
		context.Background(),
		session.ID,
		session,
		"link results.pdf from eurofusion-sim to eurofusion-paper",
	)
	if !handled {
		t.Fatal("expected artifact link command to be handled")
	}
	if message != "Linked results.pdf from eurofusion-sim to eurofusion-paper." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "link_workspace_artifact" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := int64FromAny(payloads[0]["artifact_id"]); got != artifact.ID {
		t.Fatalf("artifact_id = %d, want %d", got, artifact.ID)
	}
	if got := int64FromAny(payloads[0]["target_workspace_id"]); got != targetWorkspace.ID {
		t.Fatalf("target_workspace_id = %d, want %d", got, targetWorkspace.ID)
	}

	linked, err := app.store.ListLinkedArtifacts(targetWorkspace.ID)
	if err != nil {
		t.Fatalf("ListLinkedArtifacts() error: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != artifact.ID {
		t.Fatalf("ListLinkedArtifacts() = %+v, want linked results.pdf", linked)
	}

	_, _ = sourceWorkspace, targetWorkspace
}

func TestClassifyAndExecuteSystemActionListLinkedArtifactsUsesActiveWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	sourceDir := filepath.Join(t.TempDir(), "sim")
	targetDir := filepath.Join(t.TempDir(), "paper")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir sim: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir paper: %v", err)
	}
	sourceWorkspace, err := app.store.CreateWorkspace("eurofusion-sim", sourceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(sim) error: %v", err)
	}
	targetWorkspace, err := app.store.CreateWorkspace("eurofusion-paper", targetDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(paper) error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(targetWorkspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace(target) error: %v", err)
	}
	artifactPath := filepath.Join(sourceDir, "results.pdf")
	if err := os.WriteFile(artifactPath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("write results.pdf: %v", err)
	}
	title := "results.pdf"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindPDF, &artifactPath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	if err := app.store.LinkArtifactToWorkspace(targetWorkspace.ID, artifact.ID); err != nil {
		t.Fatalf("LinkArtifactToWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show linked artifacts")
	if !handled {
		t.Fatal("expected linked artifact listing to be handled")
	}
	if !strings.Contains(message, "Linked artifacts for workspace eurofusion-paper:") {
		t.Fatalf("message = %q", message)
	}
	if !strings.Contains(message, "results.pdf (from eurofusion-sim)") {
		t.Fatalf("message missing linked artifact origin: %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "list_linked_artifacts" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := intFromAny(payloads[0]["count"], 0); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}

	_, _ = sourceWorkspace, artifact
}
