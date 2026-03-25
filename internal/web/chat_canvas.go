package web

import (
	"context"
	"errors"
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
	if raw != "" && !filepath.IsAbs(raw) {
		if _, statErr := os.Stat(abs); statErr != nil && os.IsNotExist(statErr) {
			if fallback, ok := resolveCanvasFileQuery(rootAbs, raw); ok {
				abs = fallback
			}
		}
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

func resolveCanvasFileQuery(rootAbs, raw string) (string, bool) {
	query := strings.TrimSpace(raw)
	if query == "" {
		return "", false
	}
	normalizedQuery := strings.ToLower(filepath.ToSlash(query))
	bestExact := ""
	bestContains := ""
	err := filepath.WalkDir(rootAbs, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil || rel == "." {
			return nil
		}
		normalizedRel := strings.ToLower(filepath.ToSlash(rel))
		base := strings.ToLower(filepath.Base(normalizedRel))
		if normalizedRel == normalizedQuery || base == normalizedQuery || strings.HasPrefix(base, normalizedQuery+".") {
			bestExact = path
			return filepath.SkipAll
		}
		if bestContains == "" && strings.Contains(normalizedRel, normalizedQuery) {
			bestContains = path
		}
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return "", false
	}
	if bestExact != "" {
		return filepath.Clean(bestExact), true
	}
	if bestContains != "" {
		return filepath.Clean(bestContains), true
	}
	return "", false
}

func (a *App) executeFileBlocks(workspacePath, canvasSessionID string, blocks []fileBlock) bool {
	wroteAny := false
	for _, block := range blocks {
		if a.writeCanvasFileBlock(workspacePath, canvasSessionID, block) {
			wroteAny = true
		}
	}
	return wroteAny
}

func (a *App) writeCanvasFileBlock(workspacePath, canvasSessionID string, block fileBlock) bool {
	cwd := a.cwdForWorkspacePath(workspacePath)
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
	a.markWorkspaceOutput(workspacePath)
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

const (
	canvasRefreshKindText        = "text"
	canvasRefreshKindDocumentPDF = "document_pdf"
)

// canvasRefreshTarget holds the resolved disk binding for an active canvas
// artifact that can be refreshed from source changes.
type canvasRefreshTarget struct {
	sessionID       string
	port            int
	title           string
	kind            string
	sourcePath      string
	renderedPath    string
	renderedAbsPath string
}

// resolveCanvasRefreshTarget resolves the active canvas artifact to a source on
// disk. It supports plain text artifacts and document-source PDFs rendered via
// open_file_canvas.
func (a *App) resolveCanvasRefreshTarget(workspacePath string) *canvasRefreshTarget {
	key := strings.TrimSpace(workspacePath)
	if key == "" {
		return nil
	}
	project, err := a.store.GetWorkspaceByStoredPath(key)
	if err != nil {
		return nil
	}
	sid := a.canvasSessionIDForWorkspace(project)
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
	title, _ := active["title"].(string)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	cwd := a.cwdForWorkspacePath(key)
	kind, _ := active["kind"].(string)
	switch strings.TrimSpace(kind) {
	case "text_artifact", "text":
		filePath := resolveArtifactFilePath(cwd, title)
		if filePath == "" {
			return nil
		}
		return &canvasRefreshTarget{
			sessionID:  sid,
			port:       port,
			title:      title,
			kind:       canvasRefreshKindText,
			sourcePath: filePath,
		}
	case "pdf_artifact", "pdf":
		sourcePath := resolveArtifactFilePath(cwd, title)
		if sourcePath == "" || !shouldRenderDocumentArtifact(cwd, sourcePath) {
			return nil
		}
		renderedPath, _ := active["path"].(string)
		renderedPath = strings.TrimSpace(renderedPath)
		renderedAbsPath := resolveArtifactFilePath(cwd, renderedPath)
		return &canvasRefreshTarget{
			sessionID:       sid,
			port:            port,
			title:           title,
			kind:            canvasRefreshKindDocumentPDF,
			sourcePath:      sourcePath,
			renderedPath:    renderedPath,
			renderedAbsPath: renderedAbsPath,
		}
	default:
		return nil
	}
}

// refreshCanvasFromDisk does a single check: reads the file, compares with
// the canvas text, and pushes if different. Returns true if an update was pushed.
func (a *App) refreshCanvasFromDisk(workspacePath string) bool {
	t := a.resolveCanvasRefreshTarget(workspacePath)
	if t == nil {
		return false
	}
	switch t.kind {
	case canvasRefreshKindText:
		return a.pushCanvasFileIfChanged(workspacePath, t)
	case canvasRefreshKindDocumentPDF:
		return a.pushCanvasDocumentIfChanged(workspacePath, t)
	default:
		return false
	}
}

func (a *App) pushCanvasFileIfChanged(workspacePath string, t *canvasRefreshTarget) bool {
	diskBytes, err := os.ReadFile(t.sourcePath)
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
	a.markWorkspaceOutput(workspacePath)
	return true
}

func canvasDocumentNeedsRefresh(sourcePath, renderedAbsPath string) bool {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil || sourceInfo.IsDir() {
		return false
	}
	if strings.TrimSpace(renderedAbsPath) == "" {
		return true
	}
	renderedInfo, err := os.Stat(renderedAbsPath)
	if err != nil || renderedInfo.IsDir() {
		return true
	}
	return sourceInfo.ModTime().After(renderedInfo.ModTime())
}

func (a *App) pushCanvasDocumentIfChanged(workspacePath string, t *canvasRefreshTarget) bool {
	if t == nil || t.kind != canvasRefreshKindDocumentPDF {
		return false
	}
	projectRoot := strings.TrimSpace(a.cwdForWorkspacePath(workspacePath))
	if projectRoot == "" || !canvasDocumentNeedsRefresh(t.sourcePath, t.renderedAbsPath) {
		return false
	}
	renderedPath, err := a.renderDocumentArtifact(projectRoot, t.sourcePath)
	if err != nil {
		return false
	}
	if _, err := a.mcpToolsCall(t.port, "canvas_artifact_show", map[string]interface{}{
		"session_id": t.sessionID,
		"kind":       "pdf",
		"title":      t.title,
		"path":       renderedPath,
	}); err != nil {
		return false
	}
	t.renderedPath = renderedPath
	t.renderedAbsPath = resolveArtifactFilePath(projectRoot, renderedPath)
	a.markWorkspaceOutput(workspacePath)
	return true
}

// watchCanvasFile uses fsnotify to watch the disk file backing the active
// canvas artifact. On every write, it reads the new content and pushes it
// to the canvas via MCP. Blocks until ctx is cancelled.
func (a *App) watchCanvasFile(ctx context.Context, workspacePath string) {
	t := a.resolveCanvasRefreshTarget(workspacePath)
	if t == nil {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()
	dir := filepath.Dir(t.sourcePath)
	if err := watcher.Add(dir); err != nil {
		return
	}
	base := filepath.Base(t.sourcePath)
	lastContent := ""
	lastModTime := time.Time{}
	if t.kind == canvasRefreshKindText {
		if b, err := os.ReadFile(t.sourcePath); err == nil {
			lastContent = string(b)
		}
	} else if info, err := os.Stat(t.sourcePath); err == nil {
		lastModTime = info.ModTime()
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
			switch t.kind {
			case canvasRefreshKindText:
				b, err := os.ReadFile(t.sourcePath)
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
				a.markWorkspaceOutput(workspacePath)
			case canvasRefreshKindDocumentPDF:
				info, err := os.Stat(t.sourcePath)
				if err != nil || info.IsDir() {
					continue
				}
				if !info.ModTime().After(lastModTime) {
					continue
				}
				lastModTime = info.ModTime()
				_ = a.pushCanvasDocumentIfChanged(workspacePath, t)
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}
