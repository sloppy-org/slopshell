package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/krystophny/tabura/internal/store"
)

const printableArtifactFileSizeLimit = 256 * 1024

var itemPrintTemplate = template.Must(template.New("item-print").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.PageTitle}}</title>
  <link rel="stylesheet" href="/static/print.css">
</head>
<body class="print-page">
  <main class="print-shell">
    <header class="print-cover">
      <div class="print-kicker">Tabura Item Packet</div>
      <h1>{{.Title}}</h1>
      <p class="print-subtitle">{{.Subtitle}}</p>
    </header>

    <section class="print-section">
      <h2>Cover Sheet</h2>
      <dl class="print-facts">
        {{range .Facts}}
        <div class="print-fact">
          <dt>{{.Label}}</dt>
          <dd>{{.Value}}</dd>
        </div>
        {{end}}
      </dl>
    </section>

    <section class="print-section">
      <h2>Primary Artifact</h2>
      {{if .Artifact}}
        <div class="print-artifact-head">
          <div>
            <strong>{{.Artifact.Title}}</strong>
            <span class="print-badge">{{.Artifact.Kind}}</span>
          </div>
          {{if .Artifact.RefURL}}
          <a href="{{.Artifact.RefURL}}" class="print-link">{{.Artifact.RefURL}}</a>
          {{end}}
          {{if .Artifact.RefPath}}
          <div class="print-muted">{{.Artifact.RefPath}}</div>
          {{end}}
        </div>
        {{if .Artifact.ContentHTML}}
        <div class="print-content print-content-marked">{{.Artifact.ContentHTML}}</div>
        {{else if .Artifact.Content}}
        <pre class="print-content">{{.Artifact.Content}}</pre>
        {{else}}
        <p class="print-muted">No inline artifact text was available for print preview.</p>
        {{end}}
        {{if .Artifact.Metadata}}
        <details class="print-details">
          <summary>Artifact metadata</summary>
          <pre class="print-content">{{.Artifact.Metadata}}</pre>
        </details>
        {{end}}
      {{else}}
      <p class="print-muted">No linked artifact.</p>
      {{end}}
    </section>
  </main>
  {{if .AutoPrint}}
  <script>
    window.addEventListener('load', function () {
      window.focus();
      window.print();
    });
  </script>
  {{end}}
</body>
</html>
`))

type printFact struct {
	Label string
	Value string
}

type printArtifactView struct {
	Kind        string
	Title       string
	RefPath     string
	RefURL      string
	Content     string
	ContentHTML template.HTML
	Metadata    string
}

type itemPrintView struct {
	PageTitle string
	Title     string
	Subtitle  string
	AutoPrint bool
	Facts     []printFact
	Artifact  *printArtifactView
}

func (a *App) handleItemPrint(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	itemID, err := parseItemIDParam(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	item, err := a.store.GetItem(itemID)
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	view, err := a.buildItemPrintView(item, !strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "html"))
	if err != nil {
		writeItemStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := itemPrintTemplate.Execute(w, view); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) buildItemPrintView(item store.Item, autoPrint bool) (itemPrintView, error) {
	facts := []printFact{
		{Label: "State", Value: item.State},
		{Label: "Created", Value: formatPrintableTimestamp(item.CreatedAt)},
		{Label: "Updated", Value: formatPrintableTimestamp(item.UpdatedAt)},
	}
	if item.WorkspaceID != nil {
		workspace, err := a.store.GetWorkspace(*item.WorkspaceID)
		if err != nil {
			return itemPrintView{}, err
		}
		facts = append(facts, printFact{Label: "Workspace", Value: workspace.Name})
		facts = append(facts, printFact{Label: "Workspace Path", Value: workspace.DirPath})
	}
	if item.ActorID != nil {
		actor, err := a.store.GetActor(*item.ActorID)
		if err != nil {
			return itemPrintView{}, err
		}
		facts = append(facts, printFact{Label: "Actor", Value: actor.Name})
	}
	if item.VisibleAfter != nil && strings.TrimSpace(*item.VisibleAfter) != "" {
		facts = append(facts, printFact{Label: "Visible After", Value: formatPrintableTimestamp(*item.VisibleAfter)})
	}
	if item.FollowUpAt != nil && strings.TrimSpace(*item.FollowUpAt) != "" {
		facts = append(facts, printFact{Label: "Follow Up", Value: formatPrintableTimestamp(*item.FollowUpAt)})
	}
	if item.Source != nil && strings.TrimSpace(*item.Source) != "" {
		facts = append(facts, printFact{Label: "Source", Value: strings.TrimSpace(*item.Source)})
	}
	if item.SourceRef != nil && strings.TrimSpace(*item.SourceRef) != "" {
		facts = append(facts, printFact{Label: "Source Ref", Value: strings.TrimSpace(*item.SourceRef)})
	}

	view := itemPrintView{
		PageTitle: fmt.Sprintf("Print %s", item.Title),
		Title:     item.Title,
		Subtitle:  "Printable item review packet",
		AutoPrint: autoPrint,
		Facts:     facts,
	}
	if item.ArtifactID == nil {
		return view, nil
	}
	artifact, err := a.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		return itemPrintView{}, err
	}
	view.Artifact = buildPrintArtifactView(artifact)
	return view, nil
}

func buildPrintArtifactView(artifact store.Artifact) *printArtifactView {
	metaText, metaPretty := printableArtifactMeta(artifact.MetaJSON)
	content := printableArtifactContent(artifact, metaText)
	return &printArtifactView{
		Kind:        string(artifact.Kind),
		Title:       firstPrintableValue(optionalStringValue(artifact.Title), optionalStringValue(artifact.RefPath), optionalStringValue(artifact.RefURL), string(artifact.Kind)),
		RefPath:     optionalStringValue(artifact.RefPath),
		RefURL:      optionalStringValue(artifact.RefURL),
		Content:     content,
		ContentHTML: printableArtifactContentHTML(artifact, content),
		Metadata:    metaPretty,
	}
}

type printMarkerMode string

const (
	printMarkerModeNone      printMarkerMode = ""
	printMarkerModeLine      printMarkerMode = "line"
	printMarkerModeParagraph printMarkerMode = "paragraph"
)

func printableArtifactContent(artifact store.Artifact, metaText string) string {
	if path := strings.TrimSpace(optionalStringValue(artifact.RefPath)); path != "" {
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Size() <= printableArtifactFileSizeLimit {
			if data, readErr := os.ReadFile(path); readErr == nil && utf8.Valid(data) {
				return string(data)
			}
		}
	}
	return strings.TrimSpace(metaText)
}

func printableArtifactContentHTML(artifact store.Artifact, content string) template.HTML {
	switch printableArtifactMarkerMode(artifact, content) {
	case printMarkerModeLine:
		return renderPrintableLineMarkers(content)
	case printMarkerModeParagraph:
		return renderPrintableParagraphMarkers(content)
	default:
		return ""
	}
}

func printableArtifactMarkerMode(artifact store.Artifact, content string) printMarkerMode {
	if strings.TrimSpace(content) == "" {
		return printMarkerModeNone
	}
	switch artifact.Kind {
	case store.ArtifactKindEmail, store.ArtifactKindEmailThread, artifactKindEmailDraft:
		return printMarkerModeParagraph
	default:
		return printMarkerModeLine
	}
}

func renderPrintableLineMarkers(content string) template.HTML {
	lines := strings.Split(content, "\n")
	var b strings.Builder
	for i, line := range lines {
		fmt.Fprintf(
			&b,
			`<div class="print-marker-row"><span class="print-marker" data-print-marker="line" data-print-value="%d">L%03d</span><span class="print-marker-text">%s</span></div>`,
			i+1,
			i+1,
			template.HTMLEscapeString(line),
		)
	}
	return template.HTML(b.String())
}

func renderPrintableParagraphMarkers(content string) template.HTML {
	blocks := splitPrintableParagraphs(content)
	var b strings.Builder
	for i, block := range blocks {
		fmt.Fprintf(
			&b,
			`<div class="print-marker-row print-marker-row-paragraph"><span class="print-marker" data-print-marker="paragraph" data-print-value="%d">P%02d</span><span class="print-marker-text">%s</span></div>`,
			i+1,
			i+1,
			template.HTMLEscapeString(block),
		)
	}
	return template.HTML(b.String())
}

func splitPrintableParagraphs(content string) []string {
	text := strings.ReplaceAll(content, "\r\n", "\n")
	parts := strings.Split(text, "\n\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		clean := strings.TrimSpace(part)
		if clean == "" {
			continue
		}
		out = append(out, clean)
	}
	if len(out) == 0 && strings.TrimSpace(text) != "" {
		return []string{strings.TrimSpace(text)}
	}
	return out
}

func printableArtifactMeta(raw *string) (string, string) {
	text := strings.TrimSpace(optionalStringValue(raw))
	if text == "" {
		return "", ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		return "", text
	}
	extracted := extractPrintableMetaText(decoded)
	pretty, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return extracted, text
	}
	return strings.TrimSpace(extracted), string(pretty)
}

func extractPrintableMetaText(decoded any) string {
	obj, ok := decoded.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"text", "transcript", "body", "content", "summary", "description"} {
		value := strings.TrimSpace(fmt.Sprint(obj[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func optionalStringValue(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return strings.TrimSpace(*ptr)
}

func firstPrintableValue(values ...string) string {
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean != "" {
			return clean
		}
	}
	return ""
}

func formatPrintableTimestamp(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if ts, err := parseRFC3339OrSQLite(trimmed); err == nil {
		return ts.UTC().Format("2006-01-02 15:04 UTC")
	}
	return trimmed
}

func parseRFC3339OrSQLite(raw string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, errors.New("invalid timestamp")
}

func systemActionItemID(params map[string]interface{}) int64 {
	if params == nil {
		return 0
	}
	switch value := params["item_id"].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case string:
		id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err == nil {
			return id
		}
	}
	return 0
}

func itemPrintURL(itemID int64, htmlOnly bool) string {
	path := fmt.Sprintf("/api/items/%d/print", itemID)
	if htmlOnly {
		return path + "?format=html"
	}
	return path
}

func (a *App) executePrintItemAction(sessionID string, session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	item, err := a.resolvePrintItemTarget(session, action)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("Opened print view for %q.", item.Title), map[string]interface{}{
		"type":    "print_item",
		"item_id": item.ID,
		"title":   item.Title,
		"url":     itemPrintURL(item.ID, false),
	}, nil
}

func (a *App) resolvePrintItemTarget(session store.ChatSession, action *SystemAction) (store.Item, error) {
	if action != nil {
		if itemID := systemActionItemID(action.Params); itemID > 0 {
			return a.store.GetItem(itemID)
		}
	}
	project, err := a.systemActionTargetProject(session)
	if err != nil {
		return store.Item{}, err
	}
	if item, err := a.resolveCanvasConversationItem(project); err == nil {
		return item, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Item{}, err
	}
	if workspace, err := a.fallbackWorkspaceForProjectKey(session.ProjectKey); err != nil {
		return store.Item{}, err
	} else if workspace != nil {
		items, listErr := a.listOpenWorkspaceItems(workspace.ID)
		if listErr != nil {
			return store.Item{}, listErr
		}
		if len(items) > 0 {
			return items[0], nil
		}
	}
	items, err := a.store.ListItems()
	if err != nil {
		return store.Item{}, err
	}
	if len(items) == 0 {
		return store.Item{}, errors.New("no item is available to print")
	}
	return items[0], nil
}

func (a *App) resolveCanvasConversationItem(project store.Project) (store.Item, error) {
	canvas := a.resolveConversationCanvasArtifact(project)
	if canvas == nil {
		return store.Item{}, sql.ErrNoRows
	}
	cwd := strings.TrimSpace(project.RootPath)
	if cwd == "" {
		cwd = strings.TrimSpace(a.cwdForProjectKey(project.ProjectKey))
	}
	resolvedPath := ""
	if path := resolveConversationArtifactPath(cwd, canvas); path != nil {
		resolvedPath = filepath.Clean(*path)
	}
	items, err := a.store.ListItems()
	if err != nil {
		return store.Item{}, err
	}
	for _, item := range items {
		if item.ArtifactID == nil {
			continue
		}
		artifact, getErr := a.store.GetArtifact(*item.ArtifactID)
		if getErr != nil {
			continue
		}
		if artifactMatchesCanvas(artifact, canvas, resolvedPath) {
			return item, nil
		}
	}
	return store.Item{}, sql.ErrNoRows
}

func artifactMatchesCanvas(artifact store.Artifact, canvas *conversationCanvasArtifact, resolvedPath string) bool {
	if artifact.RefPath != nil && resolvedPath != "" && filepath.Clean(strings.TrimSpace(*artifact.RefPath)) == resolvedPath {
		return true
	}
	canvasTitle := strings.TrimSpace(canvas.Title)
	if artifact.Title != nil && canvasTitle != "" && strings.EqualFold(strings.TrimSpace(*artifact.Title), canvasTitle) {
		return true
	}
	if artifact.RefPath != nil && canvasTitle != "" && strings.EqualFold(filepath.Base(strings.TrimSpace(*artifact.RefPath)), canvasTitle) {
		return true
	}
	return false
}
