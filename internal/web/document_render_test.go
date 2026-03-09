package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenFileCanvasBuildsMarkdownDocumentAsPDF(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	docPath := filepath.Join(project.RootPath, "docs", "brief.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatalf("mkdir docs dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project.RootPath, ".tabura"), 0o755); err != nil {
		t.Fatalf("mkdir .tabura: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, ".tabura", "document.json"), []byte(`{"builder":"pandoc","main_file":"docs/brief.md"}`), 0o644); err != nil {
		t.Fatalf("write document config: %v", err)
	}
	if err := os.WriteFile(docPath, []byte("# Brief\n"), 0o644); err != nil {
		t.Fatalf("write brief.md: %v", err)
	}
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "pandoc.log")
	if err := os.WriteFile(filepath.Join(binDir, "pandoc"), []byte(`#!/bin/sh
echo "pandoc $*" >> "$TEST_LOG"
out=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out="$arg"
  fi
  prev="$arg"
done
printf '%%PDF-1.4\n' > "$out"
`), 0o755); err != nil {
		t.Fatalf("write pandoc stub: %v", err)
	}
	t.Setenv("TEST_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract mock port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	msg, payload, err := app.executeSystemAction(session.ID, session, &SystemAction{
		Action: "open_file_canvas",
		Params: map[string]interface{}{
			"path": "docs/brief.md",
		},
	})
	if err != nil {
		t.Fatalf("execute open_file_canvas: %v", err)
	}
	if msg != "Opened docs/brief.md on canvas as PDF." {
		t.Fatalf("message = %q", msg)
	}
	if payload == nil {
		t.Fatal("expected payload")
	}
	renderedPath := strings.TrimSpace(strFromAny(payload["rendered_path"]))
	if !strings.HasPrefix(renderedPath, ".tabura/artifacts/documents/") {
		t.Fatalf("rendered_path = %q", renderedPath)
	}
	renderedAbs := filepath.Join(project.RootPath, filepath.FromSlash(renderedPath))
	renderedBytes, err := os.ReadFile(renderedAbs)
	if err != nil {
		t.Fatalf("read rendered pdf: %v", err)
	}
	if string(renderedBytes) != "%PDF-1.4\n" {
		t.Fatalf("rendered pdf = %q", string(renderedBytes))
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["kind"])); got != "pdf" {
		t.Fatalf("canvas kind = %q, want pdf", got)
	}
	if got := strings.TrimSpace(strFromAny(observed["title"])); got != "docs/brief.md" {
		t.Fatalf("canvas title = %q, want docs/brief.md", got)
	}
	if got := strings.TrimSpace(strFromAny(observed["path"])); got != renderedPath {
		t.Fatalf("canvas path = %q, want %q", got, renderedPath)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read pandoc log: %v", err)
	}
	if !strings.Contains(string(logBytes), "pandoc ") {
		t.Fatalf("pandoc log = %q", string(logBytes))
	}
}

func TestRenderDocumentArtifactUsesWorkspaceConfigMainFile(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project.RootPath, ".tabura"), 0o755); err != nil {
		t.Fatalf("mkdir .tabura: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, ".tabura", "document.json"), []byte(`{"main_file":"paper.tex","builder":"latex"}`), 0o644); err != nil {
		t.Fatalf("write document config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "paper.tex"), []byte("\\documentclass{article}\n\\begin{document}\nHi\n\\end{document}\n"), 0o644); err != nil {
		t.Fatalf("write paper.tex: %v", err)
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "pdflatex"), []byte(`#!/bin/sh
for last; do true; done
base="${last%.tex}"
printf '%%PDF-1.4\n' > "${base}.pdf"
printf 'aux\n' > "${base}.aux"
`), 0o755); err != nil {
		t.Fatalf("write pdflatex stub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "bibtex"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write bibtex stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	renderedPath, err := app.renderDocumentArtifact(project.RootPath, "")
	if err != nil {
		t.Fatalf("renderDocumentArtifact() error = %v", err)
	}
	if !strings.HasPrefix(renderedPath, ".tabura/artifacts/documents/") {
		t.Fatalf("renderedPath = %q", renderedPath)
	}
	renderedAbs := filepath.Join(project.RootPath, filepath.FromSlash(renderedPath))
	if _, err := os.Stat(renderedAbs); err != nil {
		t.Fatalf("stat rendered artifact: %v", err)
	}
}
