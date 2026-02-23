package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fsnotify/fsnotify"
)

type canvasBlock struct {
	Title   string
	Content string
}

type fileBlock struct {
	Path    string
	Content string
}

var canvasBlockRe = regexp.MustCompile(`(?s):::canvas\{([^}]*)\}\n?(.*?):::`)
var fileBlockRe = regexp.MustCompile(`(?s):::file\{([^}]*)\}\n?(.*?):::`)
var langTagRe = regexp.MustCompile(`\[lang:[a-z]{2}\]`)
var canvasFileMarkerRe = regexp.MustCompile(`\[(?:canvas|file):[^\]]*\]`)
var canvasFileDirectiveOpenRe = regexp.MustCompile(`(?m)^\\s*:::(?:canvas|file)\{[^}]*\}\s*$`)
var canvasFileDirectiveCloseRe = regexp.MustCompile(`(?m)^\\s*:::\s*$`)

var attrRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

func extractAttr(attrs, name string) string {
	for _, m := range attrRe.FindAllStringSubmatch(attrs, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

func parseCanvasBlocks(text string) ([]canvasBlock, string) {
	matches := canvasBlockRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, text
	}
	var blocks []canvasBlock
	var cleaned strings.Builder
	lastEnd := 0
	for _, m := range matches {
		title := extractAttr(text[m[2]:m[3]], "title")
		blocks = append(blocks, canvasBlock{
			Title:   title,
			Content: strings.TrimSpace(text[m[4]:m[5]]),
		})
		cleaned.WriteString(text[lastEnd:m[0]])
		fmt.Fprintf(&cleaned, "[canvas: %s]", title)
		lastEnd = m[1]
	}
	cleaned.WriteString(text[lastEnd:])
	return blocks, strings.TrimSpace(cleaned.String())
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

func (a *App) executeCanvasBlocks(canvasSessionID string, blocks []canvasBlock) {
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return
	}
	for _, block := range blocks {
		_, _ = a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
			"session_id":       canvasSessionID,
			"kind":             "text",
			"title":            block.Title,
			"markdown_or_text": block.Content,
		})
	}
}

func (a *App) executeAssistantTextBlock(canvasSessionID, title, text string) {
	canvasSessionID = strings.TrimSpace(canvasSessionID)
	text = strings.TrimSpace(text)
	if canvasSessionID == "" || text == "" {
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Assistant Output"
	}
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return
	}
	_, _ = a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
		"session_id":       canvasSessionID,
		"kind":             "text",
		"title":            title,
		"markdown_or_text": text,
	})
}

func (a *App) executeFileBlocks(canvasSessionID string, blocks []fileBlock) {
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return
	}
	for _, block := range blocks {
		_, _ = a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
			"session_id":       canvasSessionID,
			"kind":             "text",
			"title":            block.Path,
			"markdown_or_text": block.Content,
		})
	}
}

// resolveArtifactFilePath maps an artifact title to an absolute file path.
// Returns "" if title has no file-like indicator (dot or separator) or the
// resolved file does not exist on disk.
func resolveArtifactFilePath(cwd, title string) string {
	t := strings.TrimSpace(title)
	if t == "" {
		return ""
	}
	if !strings.Contains(t, ".") && !strings.Contains(t, "/") {
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
	a.mu.Lock()
	port, ok := a.tunnelPorts[sid]
	a.mu.Unlock()
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
