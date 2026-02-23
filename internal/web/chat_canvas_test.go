package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCanvasBlocks_NoMarkers(t *testing.T) {
	blocks, cleaned := parseCanvasBlocks("Hello world, no markers here.")
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}
	if cleaned != "Hello world, no markers here." {
		t.Fatalf("cleaned text should be unchanged, got %q", cleaned)
	}
}

func TestParseCanvasBlocks_SingleBlock(t *testing.T) {
	input := `Here is some analysis:

:::canvas{title="Performance Analysis"}
## Results

The system is performing well.
:::

Let me know if you need more.`

	blocks, cleaned := parseCanvasBlocks(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Title != "Performance Analysis" {
		t.Errorf("title = %q, want %q", blocks[0].Title, "Performance Analysis")
	}
	if blocks[0].Content == "" {
		t.Error("content should not be empty")
	}
	if cleaned == input {
		t.Error("cleaned should differ from input (markers stripped)")
	}
	if !strings.Contains(cleaned, "[canvas: Performance Analysis]") {
		t.Errorf("cleaned should contain reference, got %q", cleaned)
	}
}

func TestParseCanvasBlocks_MultipleBlocks(t *testing.T) {
	input := `First:

:::canvas{title="Part 1"}
Content A
:::

Second:

:::canvas{title="Part 2"}
Content B
:::

Done.`

	blocks, cleaned := parseCanvasBlocks(input)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Title != "Part 1" {
		t.Errorf("blocks[0].Title = %q, want %q", blocks[0].Title, "Part 1")
	}
	if blocks[1].Title != "Part 2" {
		t.Errorf("blocks[1].Title = %q, want %q", blocks[1].Title, "Part 2")
	}
	if cleaned == "" {
		t.Error("cleaned should not be empty")
	}
}

func TestParseCanvasBlocks_CleanedContainsReference(t *testing.T) {
	input := `:::canvas{title="Report"}
Full report here.
:::`
	_, cleaned := parseCanvasBlocks(input)
	want := "[canvas: Report]"
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
}

func TestParseFileBlocks_NoMarkers(t *testing.T) {
	blocks, cleaned := parseFileBlocks("Hello world, no markers here.")
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}
	if cleaned != "Hello world, no markers here." {
		t.Fatalf("cleaned text should be unchanged, got %q", cleaned)
	}
}

func TestParseFileBlocks_SingleBlock(t *testing.T) {
	input := `Here is the code:

:::file{path="server.go"}
package main

func main() {
	println("hello")
}
:::

Let me know if you need changes.`

	blocks, cleaned := parseFileBlocks(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Path != "server.go" {
		t.Errorf("path = %q, want %q", blocks[0].Path, "server.go")
	}
	if blocks[0].Content == "" {
		t.Error("content should not be empty")
	}
	if !strings.Contains(cleaned, "[file: server.go]") {
		t.Errorf("cleaned should contain file reference, got %q", cleaned)
	}
}

func TestParseFileBlocks_MultipleBlocks(t *testing.T) {
	input := `:::file{path="main.go"}
package main
:::

:::file{path="util.go"}
package main
:::`

	blocks, _ := parseFileBlocks(input)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Path != "main.go" {
		t.Errorf("blocks[0].Path = %q, want %q", blocks[0].Path, "main.go")
	}
	if blocks[1].Path != "util.go" {
		t.Errorf("blocks[1].Path = %q, want %q", blocks[1].Path, "util.go")
	}
}

func TestParseCanvasBlocks_ContentWithCodeFences(t *testing.T) {
	input := ":::canvas{title=\"Code Review\"}\nHere is the code:\n```go\nfunc main() {}\n```\nLooks good.\n:::\n"

	blocks, cleaned := parseCanvasBlocks(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 canvas block, got %d", len(blocks))
	}
	if blocks[0].Title != "Code Review" {
		t.Errorf("title = %q, want %q", blocks[0].Title, "Code Review")
	}
	if !strings.Contains(blocks[0].Content, "```go") {
		t.Errorf("content should preserve code fences, got %q", blocks[0].Content)
	}
	if !strings.Contains(cleaned, "[canvas: Code Review]") {
		t.Errorf("cleaned should contain reference, got %q", cleaned)
	}
}

func TestParseFileBlocks_ContentWithCodeFences(t *testing.T) {
	input := ":::file{path=\"README.md\"}\n# Title\n```bash\necho hello\n```\n:::\n"

	blocks, cleaned := parseFileBlocks(input)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 file block, got %d", len(blocks))
	}
	if blocks[0].Path != "README.md" {
		t.Errorf("path = %q, want %q", blocks[0].Path, "README.md")
	}
	if !strings.Contains(blocks[0].Content, "```bash") {
		t.Errorf("content should preserve code fences, got %q", blocks[0].Content)
	}
	if !strings.Contains(cleaned, "[file: README.md]") {
		t.Errorf("cleaned should contain reference, got %q", cleaned)
	}
}

func TestStripLangTags(t *testing.T) {
	input := "[lang:de] Hallo, ich habe die Datei aktualisiert."
	got := stripLangTags(input)
	if strings.Contains(got, "[lang:") {
		t.Errorf("lang tags should be stripped, got %q", got)
	}
	if !strings.Contains(got, "Hallo") {
		t.Errorf("content should be preserved, got %q", got)
	}
}

func TestStripLangTags_NoTags(t *testing.T) {
	input := "No lang tags here."
	got := stripLangTags(input)
	if got != input {
		t.Errorf("expected %q, got %q", input, got)
	}
}

func TestMixedCanvasAndFileBlocks(t *testing.T) {
	input := `[lang:en] Here is the summary and the code.

:::canvas{title="Summary"}
Everything looks good.
:::

:::file{path="main.go"}
package main
:::`

	cBlocks, afterCanvas := parseCanvasBlocks(input)
	fBlocks, afterFile := parseFileBlocks(afterCanvas)
	final := stripLangTags(afterFile)

	if len(cBlocks) != 1 {
		t.Fatalf("expected 1 canvas block, got %d", len(cBlocks))
	}
	if len(fBlocks) != 1 {
		t.Fatalf("expected 1 file block, got %d", len(fBlocks))
	}
	if strings.Contains(final, "[lang:") {
		t.Errorf("lang tags should be stripped from final, got %q", final)
	}
	if !strings.Contains(final, "[canvas: Summary]") {
		t.Errorf("canvas reference missing, got %q", final)
	}
	if !strings.Contains(final, "[file: main.go]") {
		t.Errorf("file reference missing, got %q", final)
	}
}

func TestBuildPromptFromHistory_IncludesSystemPrompt(t *testing.T) {
	prompt := buildPromptFromHistory("chat", nil, nil)
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	if !strings.Contains(prompt, "You are Tabura") {
		t.Error("prompt should contain system identity")
	}
	if !strings.Contains(prompt, ":::canvas{") {
		t.Error("prompt should mention :::canvas{ markers")
	}
	if !strings.Contains(prompt, ":::file{") {
		t.Error("prompt should mention :::file{ markers")
	}
	if !strings.Contains(prompt, "[lang:") {
		t.Error("prompt should mention [lang:] markers")
	}
	if !strings.Contains(prompt, "Reply as ASSISTANT.") {
		t.Error("prompt should end with Reply as ASSISTANT")
	}
}

func TestBuildPromptFromHistory_PlanMode(t *testing.T) {
	prompt := buildPromptFromHistory("plan", nil, nil)
	if !strings.Contains(prompt, "plan mode") {
		t.Error("prompt should mention plan mode")
	}
}

func TestBuildPromptFromHistory_WithCanvasContext(t *testing.T) {
	ctx := &canvasContext{HasArtifact: true, ArtifactTitle: "Report.md", ArtifactKind: "text_artifact"}
	prompt := buildPromptFromHistory("chat", nil, ctx)
	if !strings.Contains(prompt, "Report.md") {
		t.Error("prompt should include artifact title")
	}
}

func TestResolveArtifactFilePath_AbsoluteExists(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "main.go")
	if err := os.WriteFile(f, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	got := resolveArtifactFilePath(tmp, f)
	if got != f {
		t.Fatalf("expected %q, got %q", f, got)
	}
}

func TestResolveArtifactFilePath_RelativeExists(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "src", "app.go")
	if err := os.MkdirAll(filepath.Join(tmp, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("package app"), 0644); err != nil {
		t.Fatal(err)
	}
	got := resolveArtifactFilePath(tmp, "src/app.go")
	if got != f {
		t.Fatalf("expected %q, got %q", f, got)
	}
}

func TestResolveArtifactFilePath_NoExtension(t *testing.T) {
	got := resolveArtifactFilePath("/tmp", "Meeting Notes")
	if got != "" {
		t.Fatalf("expected empty for non-file title, got %q", got)
	}
}

func TestResolveArtifactFilePath_MissingFile(t *testing.T) {
	got := resolveArtifactFilePath("/tmp", "nonexistent.go")
	if got != "" {
		t.Fatalf("expected empty for missing file, got %q", got)
	}
}

func TestResolveArtifactFilePath_EmptyTitle(t *testing.T) {
	got := resolveArtifactFilePath("/tmp", "")
	if got != "" {
		t.Fatalf("expected empty for empty title, got %q", got)
	}
}

func TestResolveArtifactFilePath_Directory(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "subdir.d")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	got := resolveArtifactFilePath(tmp, "subdir.d")
	if got != "" {
		t.Fatalf("expected empty for directory, got %q", got)
	}
}
