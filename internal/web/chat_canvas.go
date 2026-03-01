package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type fileBlock struct {
	Path    string
	Content string
}

var fileBlockRe = regexp.MustCompile(`(?s):::file\{([^}]*)\}\n?(.*?):::`)
var langTagRe = regexp.MustCompile(`\[lang:[a-z]{2}\]`)
var canvasFileMarkerRe = regexp.MustCompile(`\[file:[^\]]*\]`)
var canvasFileDirectiveOpenRe = regexp.MustCompile(`(?m)^\s*:::file\{[^}]*\}\s*$`)
var canvasFileDirectiveCloseRe = regexp.MustCompile(`(?m)^\s*:::\s*$`)
var canvasTempFileStemRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
var canvasScratchTitlePrefix = filepath.ToSlash(filepath.Join(".tabura", "artifacts", "tmp")) + "/"

var attrRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

func extractAttr(attrs, name string) string {
	for _, m := range attrRe.FindAllStringSubmatch(attrs, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

func parseFileBlocks(text string) ([]fileBlock, string) {
	matches := fileBlockRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, text
	}
	var blocks []fileBlock
	var cleaned strings.Builder
	lastEnd := 0
	for _, m := range matches {
		path := extractAttr(text[m[2]:m[3]], "path")
		blocks = append(blocks, fileBlock{
			Path:    path,
			Content: strings.TrimSpace(text[m[4]:m[5]]),
		})
		cleaned.WriteString(text[lastEnd:m[0]])
		fmt.Fprintf(&cleaned, "[file: %s]", path)
		lastEnd = m[1]
	}
	cleaned.WriteString(text[lastEnd:])
	return blocks, strings.TrimSpace(cleaned.String())
}

func stripLangTags(text string) string {
	return strings.TrimSpace(langTagRe.ReplaceAllString(text, ""))
}

func stripCanvasFileMarkers(text string) string {
	text = strings.TrimSpace(text)
	text = canvasFileMarkerRe.ReplaceAllString(text, " ")
	text = canvasFileDirectiveOpenRe.ReplaceAllString(text, " ")
	text = canvasFileDirectiveCloseRe.ReplaceAllString(text, " ")
	return strings.TrimSpace(text)
}

func defaultCanvasTempFilePath(seed string) string {
	stem := strings.TrimSpace(seed)
	if stem == "" {
		stem = "canvas"
	}
	stem = strings.ToLower(stem)
	stem = canvasTempFileStemRe.ReplaceAllString(stem, "-")
	stem = strings.Trim(stem, "-.")
	if stem == "" {
		stem = "canvas"
	}
	name := fmt.Sprintf("%s-%d.md", stem, time.Now().UnixNano())
	return filepath.ToSlash(filepath.Join(".tabura", "artifacts", "tmp", name))
}

func isCanvasScratchArtifactTitle(title string) bool {
	t := filepath.ToSlash(strings.TrimSpace(title))
	if t == "" {
		return false
	}
	t = strings.TrimPrefix(t, "./")
	if strings.HasPrefix(t, canvasScratchTitlePrefix) {
		return true
	}
	return strings.Contains(t, "/"+canvasScratchTitlePrefix)
}

func canOverwriteSilentAutoCanvasArtifact(ctx *canvasContext) bool {
	if ctx == nil || !ctx.HasArtifact {
		return false
	}
	kind := strings.TrimSpace(ctx.ArtifactKind)
	if kind != "text" && kind != "text_artifact" {
		return false
	}
	return isCanvasScratchArtifactTitle(ctx.ArtifactTitle)
}

func resolveCanvasFilePath(cwd, requested string) (absolutePath, canvasTitle string, err error) {
	root := strings.TrimSpace(cwd)
	if root == "" {
		root = "."
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	raw := strings.TrimSpace(requested)
	if raw == "" {
		raw = defaultCanvasTempFilePath("")
	}
	var abs string
	if filepath.IsAbs(raw) {
		abs = filepath.Clean(raw)
	} else {
		abs = filepath.Clean(filepath.Join(rootAbs, raw))
	}
	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return "", "", err
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path escapes project root: %s", requested)
	}
	return abs, filepath.ToSlash(rel), nil
}

func (a *App) executeFileBlocks(projectKey, canvasSessionID string, blocks []fileBlock) {
	for _, block := range blocks {
		_ = a.writeCanvasFileBlock(projectKey, canvasSessionID, block)
	}
}

func (a *App) writeCanvasFileBlock(projectKey, canvasSessionID string, block fileBlock) bool {
	cwd := a.cwdForProjectKey(projectKey)
	port, ok := a.tunnels.getPort(canvasSessionID)
	if !ok {
		return false
	}
	absPath, title, err := resolveCanvasFilePath(cwd, block.Path)
	if err != nil {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return false
	}
	if err := os.WriteFile(absPath, []byte(block.Content), 0644); err != nil {
		return false
	}
	if _, err := a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
		"session_id":       canvasSessionID,
		"kind":             "text",
		"title":            title,
		"markdown_or_text": block.Content,
	}); err != nil {
		return false
	}
	return true
}

// resolveArtifactFilePath maps an artifact title to an absolute file path.
// Returns "" when the resolved file does not exist on disk.
func resolveArtifactFilePath(cwd, title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}
	var abs string
	if filepath.IsAbs(t) {
		abs = t
	} else {
		abs = filepath.Join(cwd, t)
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return ""
	}
	return abs
}

// canvasFileTarget holds the resolved file-to-canvas binding for refresh.
type canvasFileTarget struct {
	sessionID string
	port      int
	title     string
	filePath  string
}

// resolveCanvasFileTarget resolves the active canvas artifact to a disk file.
// Returns nil if the artifact is not a text file or the title doesn't map to
// an existing file on disk.
func (a *App) resolveCanvasFileTarget(projectKey string) *canvasFileTarget {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return nil
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return nil
	}
	sid := a.canvasSessionIDForProject(project)
	port, ok := a.tunnels.getPort(sid)
	if !ok {
		return nil
	}
	status, err := a.mcpToolsCall(port, "canvas_status", map[string]interface{}{"session_id": sid})
	if err != nil {
		return nil
	}
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		return nil
	}
	kind, _ := active["kind"].(string)
	if kind != "text_artifact" && kind != "text" {
		return nil
	}
	title, _ := active["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	cwd := a.cwdForProjectKey(key)
	filePath := resolveArtifactFilePath(cwd, title)
	if filePath == "" {
		return nil
	}
	return &canvasFileTarget{sessionID: sid, port: port, title: title, filePath: filePath}
}

// refreshCanvasFromDisk does a single check: reads the file, compares with
// the canvas text, and pushes if different. Returns true if an update was pushed.
func (a *App) refreshCanvasFromDisk(projectKey string) bool {
	t := a.resolveCanvasFileTarget(projectKey)
	if t == nil {
		return false
	}
	return a.pushCanvasFileIfChanged(t)
}

func (a *App) pushCanvasFileIfChanged(t *canvasFileTarget) bool {
	diskBytes, err := os.ReadFile(t.filePath)
	if err != nil {
		return false
	}
	diskContent := string(diskBytes)
	status, err := a.mcpToolsCall(t.port, "canvas_status", map[string]interface{}{"session_id": t.sessionID})
	if err != nil {
		return false
	}
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		return false
	}
	currentText, _ := active["text"].(string)
	if strings.TrimSpace(diskContent) == strings.TrimSpace(currentText) {
		return false
	}
	_, _ = a.mcpToolsCall(t.port, "canvas_artifact_show", map[string]interface{}{
		"session_id":       t.sessionID,
		"kind":             "text",
		"title":            t.title,
		"markdown_or_text": diskContent,
	})
	return true
}

// watchCanvasFile uses fsnotify to watch the disk file backing the active
// canvas artifact. On every write, it reads the new content and pushes it
// to the canvas via MCP. Blocks until ctx is cancelled.
func (a *App) watchCanvasFile(ctx context.Context, projectKey string) {
	t := a.resolveCanvasFileTarget(projectKey)
	if t == nil {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()
	dir := filepath.Dir(t.filePath)
	if err := watcher.Add(dir); err != nil {
		return
	}
	base := filepath.Base(t.filePath)
	lastContent := ""
	if b, err := os.ReadFile(t.filePath); err == nil {
		lastContent = string(b)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != base {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			b, err := os.ReadFile(t.filePath)
			if err != nil {
				continue
			}
			content := string(b)
			if content == lastContent {
				continue
			}
			lastContent = content
			_, _ = a.mcpToolsCall(t.port, "canvas_artifact_show", map[string]interface{}{
				"session_id":       t.sessionID,
				"kind":             "text",
				"title":            t.title,
				"markdown_or_text": content,
			})
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}
