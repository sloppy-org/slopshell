package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/krystophny/tabura/internal/store"
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
	if !strings.Contains(prompt, "Voice mode is chat-only") {
		t.Error("prompt should define chat-only voice mode")
	}
	if !strings.Contains(prompt, "Do not emit :::file blocks.") {
		t.Error("prompt should explicitly disallow :::file blocks")
	}
	if !strings.Contains(prompt, "Do not emit :::canvas blocks.") {
		t.Error("prompt should explicitly disallow :::canvas blocks")
	}
	if !strings.Contains(prompt, "Do not render chat output on canvas.") {
		t.Error("prompt should explicitly disallow rendering chat output on canvas")
	}
	if !strings.Contains(prompt, "show/open an existing file") {
		t.Error("prompt should define existing-file canvas behavior")
	}
	if !strings.Contains(prompt, "do NOT paste that file body into chat") {
		t.Error("prompt should forbid inlining existing file bodies")
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

func TestBuildPromptFromHistoryForMode_SilentUsesToolOnlyPreamble(t *testing.T) {
	prompt := buildPromptFromHistoryForMode("chat", nil, nil, turnOutputModeSilent, "")
	if strings.Contains(prompt, "You are Tabura") {
		t.Error("silent prompt should not include identity preamble")
	}
	if strings.Contains(prompt, "spoken via TTS") {
		t.Error("silent prompt should not include voice TTS instructions")
	}
	if strings.Contains(prompt, "Spoken chat must be one paragraph max.") {
		t.Error("silent prompt should not include spoken chat paragraph limits")
	}
	if strings.Contains(prompt, "Use [lang:de]") {
		t.Error("silent prompt should not include voice language tag guidance")
	}
	if !strings.Contains(prompt, "delegate_to_model") {
		t.Error("silent prompt should include delegation section")
	}
	if strings.Contains(prompt, "Reply as ASSISTANT.") {
		t.Error("silent prompt should not include assistant-style reply directive")
	}
}

func TestBuildPromptFromHistoryForMode_SparkIncludesModelHints(t *testing.T) {
	prompt := buildPromptFromHistoryForMode("chat", nil, nil, turnOutputModeVoice, "spark")
	if !strings.Contains(prompt, "merge conflicts") {
		t.Error("spark prompt should include git conflict delegation hints")
	}
	if !strings.Contains(prompt, "delegate to codex") {
		t.Error("spark prompt should instruct delegation to codex for conflicts")
	}
}

func TestBuildPromptFromHistoryForMode_CodexOmitsSparkHints(t *testing.T) {
	prompt := buildPromptFromHistoryForMode("chat", nil, nil, turnOutputModeVoice, "codex")
	if strings.Contains(prompt, "Model-Specific Rules (spark)") {
		t.Error("codex prompt should not include spark-specific hints")
	}
}

func TestBuildTurnPromptForMode_SparkIncludesModelHints(t *testing.T) {
	prompt := buildTurnPromptForMode([]store.ChatMessage{{
		Role:         "user",
		ContentPlain: "fix the merge",
	}}, nil, turnOutputModeSilent, "spark")
	if !strings.Contains(prompt, "merge conflicts") {
		t.Error("spark turn prompt should include git conflict delegation hints")
	}
}

func TestBuildPromptFromHistoryForMode_SilentSkipsCanvasContext(t *testing.T) {
	ctx := &canvasContext{HasArtifact: true, ArtifactTitle: "Report.md", ArtifactKind: "text_artifact"}
	prompt := buildPromptFromHistoryForMode("chat", nil, ctx, turnOutputModeSilent, "")
	if strings.Contains(prompt, "Report.md") {
		t.Error("silent prompt should not include canvas context")
	}
	if strings.Contains(prompt, "Current Artifact") {
		t.Error("silent prompt should not include artifact section")
	}
}

func TestBuildPromptFromHistory_WithCanvasContext(t *testing.T) {
	ctx := &canvasContext{HasArtifact: true, ArtifactTitle: "Report.md", ArtifactKind: "text_artifact"}
	prompt := buildPromptFromHistory("chat", nil, ctx)
	if !strings.Contains(prompt, "Report.md") {
		t.Error("prompt should include artifact title")
	}
}

func TestAssistantFinalChatContent_AutoCanvasRetainsCompanionChat(t *testing.T) {
	markdown, plain, format := assistantFinalChatContent("Long answer", true, true)
	if markdown != "Long answer" || plain != "Long answer" {
		t.Fatalf("expected companion chat for auto canvas, got markdown=%q plain=%q", markdown, plain)
	}
	if format != "markdown" {
		t.Fatalf("format = %q, want markdown", format)
	}
}

func TestAssistantRenderPlan_AutoCanvasForMultiParagraph(t *testing.T) {
	text := "Paragraph one.\n\nParagraph two."
	plan := assistantRenderPlan(text)
	if plan.RenderOnCanvas {
		t.Fatal("expected renderOnCanvas=false")
	}
	if plan.AutoCanvas {
		t.Fatal("expected autoCanvas=false")
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
	if plan.RenderOnCanvas {
		t.Fatal("expected renderOnCanvas=false for file block")
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
	if plan.RenderOnCanvas {
		t.Fatal("expected renderOnCanvas=false")
	}
	if plan.AutoCanvas {
		t.Fatal("expected autoCanvas=false for long spoken companion")
	}
}

func TestAssistantRenderPlanForMode_SilentAlwaysReturnsFalse(t *testing.T) {
	text := "Paragraph one.\n\nParagraph two."
	plan := assistantRenderPlanForMode(text, turnOutputModeSilent)
	if plan.AutoCanvas {
		t.Fatal("expected autoCanvas=false in silent mode")
	}
	if plan.RenderOnCanvas {
		t.Fatal("expected renderOnCanvas=false in silent mode")
	}

	textWithFile := ":::file{path=\"notes.md\"}\nFile body.\n:::"
	plan2 := assistantRenderPlanForMode(textWithFile, turnOutputModeSilent)
	if plan2.AutoCanvas {
		t.Fatal("expected autoCanvas=false in silent mode with file block")
	}
	if plan2.RenderOnCanvas {
		t.Fatal("expected renderOnCanvas=false in silent mode with file block")
	}
}

func TestAssistantSnapshotContent_AutoCanvasRetainsCompanionChat(t *testing.T) {
	markdown, plain, format := assistantSnapshotContent("Paragraph one.\n\nParagraph two.", true, true)
	expected := "Paragraph one.\n\nParagraph two."
	if markdown != expected || plain != expected {
		t.Fatalf("expected companion snapshot for auto canvas, got markdown=%q plain=%q", markdown, plain)
	}
	if format != "markdown" {
		t.Fatalf("format = %q, want markdown", format)
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

func TestBuildTurnPromptForMode_SilentUsesToolOnlyPreamble(t *testing.T) {
	prompt := buildTurnPromptForMode([]store.ChatMessage{{
		Role:         "user",
		ContentPlain: "Please summarize this module.",
	}}, nil, turnOutputModeSilent, "")
	if strings.Contains(prompt, "Reply as ASSISTANT.") {
		t.Error("silent turn prompt should not include assistant reply style")
	}
	if !strings.Contains(prompt, "delegate_to_model") {
		t.Error("silent turn prompt should include delegation section")
	}
	if strings.Contains(prompt, "Spoken chat must be one paragraph max.") {
		t.Error("silent turn prompt should not include spoken paragraph limits")
	}
	if !strings.Contains(prompt, "Please summarize this module.") {
		t.Error("silent turn prompt should include user message")
	}
}

func TestBuildTurnPromptForMode_SilentSkipsCanvasContext(t *testing.T) {
	ctx := &canvasContext{HasArtifact: true, ArtifactTitle: "Summary.md", ArtifactKind: "text_artifact"}
	prompt := buildTurnPromptForMode([]store.ChatMessage{{
		Role:         "user",
		ContentPlain: "hello",
	}}, ctx, turnOutputModeSilent, "")
	if strings.Contains(prompt, "Summary.md") {
		t.Error("silent turn prompt should not include canvas context")
	}
	if strings.Contains(prompt, "Active artifact") {
		t.Error("silent turn prompt should not include artifact info")
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

func TestIsCanvasScratchArtifactTitle(t *testing.T) {
	if !isCanvasScratchArtifactTitle(".tabura/artifacts/tmp/reply.md") {
		t.Fatal("expected relative tmp artifact title to be detected")
	}
	if !isCanvasScratchArtifactTitle("/home/u/proj/.tabura/artifacts/tmp/reply.md") {
		t.Fatal("expected absolute tmp artifact title to be detected")
	}
	if isCanvasScratchArtifactTitle(".tabura/artifacts/pr/pr-12.diff") {
		t.Fatal("did not expect PR artifact title to be detected as scratch")
	}
	if isCanvasScratchArtifactTitle("README.md") {
		t.Fatal("did not expect workspace file title to be detected as scratch")
	}
}

func TestCanOverwriteSilentAutoCanvasArtifact(t *testing.T) {
	if !canOverwriteSilentAutoCanvasArtifact(&canvasContext{
		HasArtifact:   true,
		ArtifactTitle: ".tabura/artifacts/tmp/reply.md",
		ArtifactKind:  "text_artifact",
	}) {
		t.Fatal("expected text tmp artifact to be overwriteable")
	}
	if canOverwriteSilentAutoCanvasArtifact(&canvasContext{
		HasArtifact:   true,
		ArtifactTitle: "notes.md",
		ArtifactKind:  "text_artifact",
	}) {
		t.Fatal("did not expect workspace text file artifact to be overwriteable")
	}
	if canOverwriteSilentAutoCanvasArtifact(&canvasContext{
		HasArtifact:   true,
		ArtifactTitle: ".tabura/artifacts/tmp/reply.md",
		ArtifactKind:  "image_artifact",
	}) {
		t.Fatal("did not expect non-text artifact to be overwriteable")
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

type canvasMCPMock struct {
	mu               sync.Mutex
	artifactTitle    string
	artifactKind     string
	artifactText     string
	lastShownTitle   string
	lastShownContent string
	artifactShow     int32
}

func (m *canvasMCPMock) setupServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || strings.TrimSpace(r.URL.Path) != "/mcp" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		params, _ := payload["params"].(map[string]interface{})
		name := strings.TrimSpace(fmt.Sprint(params["name"]))
		args, _ := params["arguments"].(map[string]interface{})

		var structured map[string]interface{}
		switch name {
		case "canvas_status":
			m.mu.Lock()
			active := map[string]interface{}{
				"title": m.artifactTitle,
				"kind":  m.artifactKind,
				"text":  m.artifactText,
			}
			m.mu.Unlock()
			structured = map[string]interface{}{"active_artifact": active}
		case "canvas_artifact_show":
			atomic.AddInt32(&m.artifactShow, 1)
			title := strings.TrimSpace(fmt.Sprint(args["title"]))
			kind := strings.TrimSpace(fmt.Sprint(args["kind"]))
			content := fmt.Sprint(args["markdown_or_text"])
			m.mu.Lock()
			m.artifactTitle = title
			if kind != "" {
				m.artifactKind = kind
			}
			m.artifactText = content
			m.lastShownTitle = title
			m.lastShownContent = content
			m.mu.Unlock()
			structured = map[string]interface{}{"ok": true}
		default:
			http.Error(w, "unknown tool", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      payload["id"],
			"result": map[string]interface{}{
				"structuredContent": structured,
				"isError":           false,
			},
		})
	}))
}

func TestFinalizeAssistantResponse_SilentOverwritesScratchArtifact(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: ".tabura/artifacts/tmp/reply.md",
		artifactKind:  "text_artifact",
		artifactText:  "old content",
	}
	server := mock.setupServer(t)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract port: %v", err)
	}
	app.mu.Lock()
	app.tunnelPorts[app.canvasSessionIDForProject(project)] = port
	app.mu.Unlock()
	ctx := app.resolveCanvasContext(project.ProjectKey)
	if ctx == nil {
		t.Fatal("expected canvas context")
	}
	if !canOverwriteSilentAutoCanvasArtifact(ctx) {
		t.Fatalf("expected scratch artifact to be overwritable, got %+v", *ctx)
	}

	var persistedID int64
	var persistedText string
	_ = app.finalizeAssistantResponse(
		session.ID,
		project.ProjectKey,
		"second response",
		&persistedID,
		&persistedText,
		"",
		"",
		"",
		turnOutputModeSilent,
	)

	if got := atomic.LoadInt32(&mock.artifactShow); got == 0 {
		t.Fatal("expected silent response to push canvas_artifact_show for scratch artifact")
	}
	if strings.TrimSpace(mock.lastShownTitle) != ".tabura/artifacts/tmp/reply.md" {
		t.Fatalf("expected overwrite same scratch artifact title, got %q", mock.lastShownTitle)
	}
	if strings.TrimSpace(mock.lastShownContent) != "second response" {
		t.Fatalf("expected updated canvas content, got %q", mock.lastShownContent)
	}
	scratchPath := filepath.Join(project.RootPath, filepath.FromSlash(".tabura/artifacts/tmp/reply.md"))
	b, err := os.ReadFile(scratchPath)
	if err != nil {
		t.Fatalf("expected scratch file written: %v", err)
	}
	if strings.TrimSpace(string(b)) != "second response" {
		t.Fatalf("expected scratch file content updated, got %q", string(b))
	}
}

func TestFinalizeAssistantResponse_SilentFallsBackToScratchForWorkspaceArtifact(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	workspaceFile := filepath.Join(project.RootPath, "docs", "notes.md")
	if err := os.MkdirAll(filepath.Dir(workspaceFile), 0o755); err != nil {
		t.Fatalf("mkdir workspace file dir: %v", err)
	}
	if err := os.WriteFile(workspaceFile, []byte("user-owned"), 0o644); err != nil {
		t.Fatalf("seed workspace file: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: "docs/notes.md",
		artifactKind:  "text_artifact",
		artifactText:  "user-owned",
	}
	server := mock.setupServer(t)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract port: %v", err)
	}
	app.mu.Lock()
	app.tunnelPorts[app.canvasSessionIDForProject(project)] = port
	app.mu.Unlock()

	var persistedID int64
	var persistedText string
	_ = app.finalizeAssistantResponse(
		session.ID,
		project.ProjectKey,
		"assistant follow-up",
		&persistedID,
		&persistedText,
		"",
		"",
		"",
		turnOutputModeSilent,
	)

	if got := atomic.LoadInt32(&mock.artifactShow); got != 1 {
		t.Fatalf("expected one canvas_artifact_show for workspace artifact fallback, got %d", got)
	}
	if strings.TrimSpace(mock.lastShownTitle) == "docs/notes.md" {
		t.Fatalf("expected fallback scratch artifact title, got %q", mock.lastShownTitle)
	}
	if !strings.HasPrefix(strings.TrimSpace(mock.lastShownTitle), ".tabura/artifacts/tmp/") {
		t.Fatalf("expected scratch artifact title, got %q", mock.lastShownTitle)
	}
	if strings.TrimSpace(mock.lastShownContent) != "assistant follow-up" {
		t.Fatalf("expected fallback scratch artifact content, got %q", mock.lastShownContent)
	}
	b, err := os.ReadFile(workspaceFile)
	if err != nil {
		t.Fatalf("read workspace file: %v", err)
	}
	if strings.TrimSpace(string(b)) != "user-owned" {
		t.Fatalf("workspace file should remain untouched, got %q", string(b))
	}
	scratchPath := filepath.Join(project.RootPath, filepath.FromSlash(strings.TrimSpace(mock.lastShownTitle)))
	scratchBytes, err := os.ReadFile(scratchPath)
	if err != nil {
		t.Fatalf("read scratch artifact file: %v", err)
	}
	if strings.TrimSpace(string(scratchBytes)) != "assistant follow-up" {
		t.Fatalf("expected scratch artifact file content updated, got %q", string(scratchBytes))
	}
}

func TestFinalizeAssistantResponse_SilentFallsBackWhenOverwritePathEscapesProject(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	mock := &canvasMCPMock{
		artifactTitle: "/tmp/other-project/.tabura/artifacts/tmp/reply.md",
		artifactKind:  "text_artifact",
		artifactText:  "old",
	}
	server := mock.setupServer(t)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract port: %v", err)
	}
	app.mu.Lock()
	app.tunnelPorts[app.canvasSessionIDForProject(project)] = port
	app.mu.Unlock()

	var persistedID int64
	var persistedText string
	_ = app.finalizeAssistantResponse(
		session.ID,
		project.ProjectKey,
		"fresh response",
		&persistedID,
		&persistedText,
		"",
		"",
		"",
		turnOutputModeSilent,
	)

	if got := atomic.LoadInt32(&mock.artifactShow); got != 1 {
		t.Fatalf("expected one successful fallback canvas_artifact_show call, got %d", got)
	}
	if strings.TrimSpace(mock.lastShownTitle) == "/tmp/other-project/.tabura/artifacts/tmp/reply.md" {
		t.Fatalf("expected fallback to new local scratch artifact, got %q", mock.lastShownTitle)
	}
	if !strings.HasPrefix(strings.TrimSpace(mock.lastShownTitle), ".tabura/artifacts/tmp/") {
		t.Fatalf("expected local scratch artifact title, got %q", mock.lastShownTitle)
	}
	if strings.TrimSpace(mock.lastShownContent) != "fresh response" {
		t.Fatalf("expected fallback scratch artifact content, got %q", mock.lastShownContent)
	}
}
