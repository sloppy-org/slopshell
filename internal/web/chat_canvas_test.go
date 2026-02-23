package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestAssistantFinalChatContent_StructuredWithCompanion(t *testing.T) {
	input := "Review ready. [file: .tabura/artifacts/tmp/diff.md] Let's discuss."
	markdown, plain, format := assistantFinalChatContent(input, true, false)
	normalized := strings.Join(strings.Fields(markdown), " ")
	if normalized != "Review ready. Let's discuss." {
		t.Fatalf("markdown = %q", markdown)
	}
	if plain != markdown {
		t.Fatalf("plain = %q, want %q", plain, markdown)
	}
	if format != "markdown" {
		t.Fatalf("format = %q, want markdown", format)
	}
}

func TestAssistantFinalChatContent_StructuredMarkersOnly(t *testing.T) {
	input := "[file: .tabura/artifacts/tmp/diff.md]"
	markdown, plain, format := assistantFinalChatContent(input, true, false)
	if markdown != "" {
		t.Fatalf("markdown = %q, want empty", markdown)
	}
	if plain != "" {
		t.Fatalf("plain = %q, want empty", plain)
	}
	if format != "markdown" {
		t.Fatalf("format = %q, want markdown", format)
	}
}

func TestAssistantFinalChatContent_RegularChatResponse(t *testing.T) {
	input := "Short spoken response."
	markdown, plain, format := assistantFinalChatContent(input, false, false)
	if markdown != input {
		t.Fatalf("markdown = %q, want %q", markdown, input)
	}
	if plain != input {
		t.Fatalf("plain = %q, want %q", plain, input)
	}
	if format != "markdown" {
		t.Fatalf("format = %q, want markdown", format)
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
	if !strings.Contains(prompt, ":::file{") {
		t.Error("prompt should mention :::file{ markers")
	}
	if !strings.Contains(prompt, "Do not use :::canvas blocks.") {
		t.Error("prompt should explicitly disallow :::canvas blocks")
	}
	if !strings.Contains(prompt, "Spoken chat must be one paragraph max.") {
		t.Error("prompt should enforce one-paragraph spoken chat limit")
	}
	if !strings.Contains(prompt, "respond with :::file block content (no chat prose)") {
		t.Error("prompt should enforce file-only mode for long responses")
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

func TestAssistantFinalChatContent_AutoCanvasSuppressesChat(t *testing.T) {
	markdown, plain, format := assistantFinalChatContent("Long answer", true, true)
	if markdown != "" || plain != "" {
		t.Fatalf("expected empty chat for auto canvas, got markdown=%q plain=%q", markdown, plain)
	}
	if format != "text" {
		t.Fatalf("format = %q, want text", format)
	}
}

func TestAssistantRenderPlan_AutoCanvasForMultiParagraph(t *testing.T) {
	text := "Paragraph one.\n\nParagraph two."
	plan := assistantRenderPlan(text)
	if !plan.RenderOnCanvas {
		t.Fatal("expected renderOnCanvas=true")
	}
	if !plan.AutoCanvas {
		t.Fatal("expected autoCanvas=true")
	}
}

func TestAssistantRenderPlan_NoAutoCanvasForSingleParagraph(t *testing.T) {
	text := "Single paragraph response."
	plan := assistantRenderPlan(text)
	if plan.RenderOnCanvas {
		t.Fatal("expected renderOnCanvas=false")
	}
	if plan.AutoCanvas {
		t.Fatal("expected autoCanvas=false")
	}
}

func TestAssistantRenderPlan_FileOnlyBlockDoesNotTriggerAutoCanvas(t *testing.T) {
	text := `:::file{path="notes.md"}
Paragraph one in file body.

Paragraph two in file body.
:::`
	plan := assistantRenderPlan(text)
	if !plan.RenderOnCanvas {
		t.Fatal("expected renderOnCanvas=true for file block")
	}
	if plan.AutoCanvas {
		t.Fatal("expected autoCanvas=false for file-only block content")
	}
}

func TestAssistantRenderPlan_FileBlockWithLongCompanionTriggersAutoCanvas(t *testing.T) {
	text := `Intro paragraph.

Second paragraph.

:::file{path="notes.md"}
File body.
:::`
	plan := assistantRenderPlan(text)
	if !plan.RenderOnCanvas {
		t.Fatal("expected renderOnCanvas=true")
	}
	if !plan.AutoCanvas {
		t.Fatal("expected autoCanvas=true for long spoken companion")
	}
}

func TestAssistantSnapshotContent_AutoCanvasSuppressesChat(t *testing.T) {
	markdown, plain, format := assistantSnapshotContent("Paragraph one.\n\nParagraph two.", true, true)
	if markdown != "" || plain != "" {
		t.Fatalf("expected empty snapshot for auto canvas, got markdown=%q plain=%q", markdown, plain)
	}
	if format != "text" {
		t.Fatalf("format = %q, want text", format)
	}
}

func TestAssistantSnapshotContent_FileOnlyBlockSuppressesChat(t *testing.T) {
	input := `:::file{path="notes.md"}
File body line 1.

File body line 2.
:::`
	markdown, plain, format := assistantSnapshotContent(input, true, false)
	if markdown != "" || plain != "" {
		t.Fatalf("expected no chat snapshot, got markdown=%q plain=%q", markdown, plain)
	}
	if format != "text" {
		t.Fatalf("format = %q, want text", format)
	}
}

func TestResolveCanvasFilePath_UsesProjectRelativeTitle(t *testing.T) {
	tmp := t.TempDir()
	abs, title, err := resolveCanvasFilePath(tmp, "notes/summary.md")
	if err != nil {
		t.Fatalf("resolveCanvasFilePath returned error: %v", err)
	}
	if !strings.HasPrefix(abs, tmp) {
		t.Fatalf("expected absolute path inside %q, got %q", tmp, abs)
	}
	if title != "notes/summary.md" {
		t.Fatalf("expected title notes/summary.md, got %q", title)
	}
}

func TestResolveCanvasFilePath_RejectsEscapingProjectRoot(t *testing.T) {
	tmp := t.TempDir()
	if _, _, err := resolveCanvasFilePath(tmp, "../outside.md"); err == nil {
		t.Fatal("expected error for escaping project root")
	}
}

func TestResolveCanvasFilePath_DefaultsToTempArtifactPath(t *testing.T) {
	tmp := t.TempDir()
	abs, title, err := resolveCanvasFilePath(tmp, "")
	if err != nil {
		t.Fatalf("resolveCanvasFilePath returned error: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(abs), "/.tabura/artifacts/tmp/") {
		t.Fatalf("expected temp artifact path, got %q", abs)
	}
	if !strings.HasPrefix(title, ".tabura/artifacts/tmp/") {
		t.Fatalf("expected temp artifact title, got %q", title)
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
