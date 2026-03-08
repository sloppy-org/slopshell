package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

var (
	itemTitlePrefixPattern = regexp.MustCompile(`^\s*(?:[#>*-]+|\d+[.)]|(?:\[[ xX]\]))\s*`)
	itemDelegatePattern    = regexp.MustCompile(`(?i)^(?:delegate|assign)(?:\s+(?:this|it))?\s+to\s+(.+?)$`)
	itemSplitPattern       = regexp.MustCompile(`(?i)^split\s+(?:this|it)\s+into\s+(.+?)\s+items?$`)
)

type conversationCanvasArtifact struct {
	Kind  string
	Title string
	Path  string
	Text  string
}

type conversationItemContext struct {
	Title         string
	AssistantText string
	WorkspaceID   *int64
	ArtifactID    *int64
}

func normalizeItemCommandText(raw string) string {
	text := strings.ToLower(strings.TrimSpace(raw))
	text = strings.Trim(text, " \t\r\n.!?,:;")
	text = strings.TrimPrefix(text, "please ")
	return strings.TrimSpace(text)
}

func parseInlineItemIntent(text string, now time.Time) *SystemAction {
	normalized := normalizeItemCommandText(text)
	switch normalized {
	case "make this an item", "track this", "add to inbox":
		return &SystemAction{Action: "make_item", Params: map[string]interface{}{}}
	case "later", "remind me later":
		visibleAfter := defaultReminderTime(now)
		return &SystemAction{
			Action: "snooze_item",
			Params: map[string]interface{}{"visible_after": visibleAfter},
		}
	}

	if match := itemDelegatePattern.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 2 {
		actor := cleanActorReference(match[1])
		if actor != "" {
			return &SystemAction{
				Action: "delegate_item",
				Params: map[string]interface{}{"actor": actor},
			}
		}
	}

	if visibleAfter, ok := parseReminderVisibleAfter(text, now); ok {
		return &SystemAction{
			Action: "snooze_item",
			Params: map[string]interface{}{"visible_after": visibleAfter},
		}
	}

	if match := itemSplitPattern.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 2 {
		if count, ok := parseItemSplitCount(match[1]); ok && count > 0 {
			return &SystemAction{
				Action: "split_items",
				Params: map[string]interface{}{"count": count},
			}
		}
	}
	return nil
}

func cleanActorReference(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.Trim(text, " \t\r\n.!?,:;")
	for _, suffix := range []string{" please", " thanks", " thank you"} {
		if strings.HasSuffix(strings.ToLower(text), suffix) {
			text = strings.TrimSpace(text[:len(text)-len(suffix)])
		}
	}
	return strings.TrimSpace(text)
}

func parseReminderVisibleAfter(text string, now time.Time) (string, bool) {
	lower := normalizeItemCommandText(text)
	if strings.Contains(lower, "tomorrow") {
		return defaultReminderTime(now), true
	}
	for weekday, name := range map[time.Weekday]string{
		time.Monday:    "monday",
		time.Tuesday:   "tuesday",
		time.Wednesday: "wednesday",
		time.Thursday:  "thursday",
		time.Friday:    "friday",
		time.Saturday:  "saturday",
		time.Sunday:    "sunday",
	} {
		if strings.Contains(lower, name) {
			return nextWeekdayReminderTime(now, weekday), true
		}
	}
	return "", false
}

func defaultReminderTime(now time.Time) string {
	base := now.UTC().Add(24 * time.Hour)
	return time.Date(base.Year(), base.Month(), base.Day(), 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
}

func nextWeekdayReminderTime(now time.Time, weekday time.Weekday) string {
	base := now.UTC()
	candidate := time.Date(base.Year(), base.Month(), base.Day(), 9, 0, 0, 0, time.UTC)
	days := (int(weekday) - int(base.Weekday()) + 7) % 7
	if days == 0 && !candidate.After(base) {
		days = 7
	}
	if days == 0 {
		days = 7
	}
	candidate = candidate.AddDate(0, 0, days)
	return candidate.Format(time.RFC3339)
}

func parseItemSplitCount(raw string) (int, bool) {
	value := normalizeItemCommandText(raw)
	if value == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(value); err == nil && n > 0 {
		return n, true
	}
	words := map[string]int{
		"one":   1,
		"two":   2,
		"three": 3,
		"four":  4,
		"five":  5,
		"six":   6,
		"seven": 7,
		"eight": 8,
		"nine":  9,
		"ten":   10,
	}
	n, ok := words[value]
	return n, ok
}

func isItemSystemAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "make_item", "delegate_item", "snooze_item", "split_items":
		return true
	default:
		return false
	}
}

func itemActionFailurePrefix(action string) string {
	if strings.EqualFold(strings.TrimSpace(action), "split_items") {
		return "I couldn't create the items: "
	}
	return "I couldn't create the item: "
}

func systemActionActorName(params map[string]interface{}) string {
	for _, key := range []string{"actor", "name", "target"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func systemActionVisibleAfter(params map[string]interface{}) string {
	for _, key := range []string{"visible_after", "when"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func systemActionSplitCount(params map[string]interface{}) int {
	switch value := params["count"].(type) {
	case int:
		if value > 0 {
			return value
		}
	case int64:
		if value > 0 {
			return int(value)
		}
	case float64:
		if value > 0 {
			return int(value)
		}
	case string:
		if count, ok := parseItemSplitCount(value); ok {
			return count
		}
	}
	return 0
}

func cleanConversationItemText(raw string) string {
	clean := stripLangTags(stripCanvasFileMarkers(raw))
	clean = strings.ReplaceAll(clean, "\r\n", "\n")
	return strings.TrimSpace(clean)
}

func latestConversationAssistantText(messages []store.ChatMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if !strings.EqualFold(strings.TrimSpace(messages[i].Role), "assistant") {
			continue
		}
		content := strings.TrimSpace(messages[i].ContentPlain)
		if content == "" {
			content = strings.TrimSpace(messages[i].ContentMarkdown)
		}
		content = cleanConversationItemText(content)
		if content != "" {
			return content
		}
	}
	return ""
}

func deriveConversationItemTitle(text string, fallback string) string {
	content := cleanConversationItemText(text)
	if content == "" {
		content = strings.TrimSpace(fallback)
	}
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		title := strings.TrimSpace(itemTitlePrefixPattern.ReplaceAllString(line, ""))
		title = strings.Trim(title, " \t\r\n-:;,.")
		if title == "" {
			continue
		}
		if len(title) > 120 {
			title = strings.TrimSpace(title[:117]) + "..."
		}
		return title
	}
	return ""
}

func deriveSplitItemTitles(text string, count int) ([]string, error) {
	content := cleanConversationItemText(text)
	if content == "" {
		return nil, errors.New("no recent assistant output to split into items")
	}
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	seen := map[string]struct{}{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !itemTitlePrefixPattern.MatchString(trimmed) {
			continue
		}
		title := deriveConversationItemTitle(trimmed, "")
		if title == "" {
			continue
		}
		key := strings.ToLower(title)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, title)
	}
	if len(out) == 0 {
		return nil, errors.New("no separate items found in recent assistant output")
	}
	if count > 0 && len(out) < count {
		return nil, fmt.Errorf("found %d candidate items, need %d", len(out), count)
	}
	if count > 0 && len(out) > count {
		out = out[:count]
	}
	return out, nil
}

func (a *App) resolveConversationCanvasArtifact(project store.Project) *conversationCanvasArtifact {
	canvasSessionID := strings.TrimSpace(a.canvasSessionIDForProject(project))
	if canvasSessionID == "" {
		return nil
	}
	port, ok := a.tunnels.getPort(canvasSessionID)
	if !ok {
		return nil
	}
	status, err := a.mcpToolsCall(port, "canvas_status", map[string]interface{}{"session_id": canvasSessionID})
	if err != nil {
		return nil
	}
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		return nil
	}
	kind := strings.TrimSpace(fmt.Sprint(active["kind"]))
	title := strings.TrimSpace(fmt.Sprint(active["title"]))
	path := strings.TrimSpace(fmt.Sprint(active["path"]))
	text := strings.TrimSpace(fmt.Sprint(active["text"]))
	if kind == "" || kind == "<nil>" {
		return nil
	}
	if title == "<nil>" {
		title = ""
	}
	if path == "<nil>" {
		path = ""
	}
	if text == "<nil>" {
		text = ""
	}
	return &conversationCanvasArtifact{
		Kind:  kind,
		Title: title,
		Path:  path,
		Text:  text,
	}
}

func conversationCanvasArtifactKind(canvas *conversationCanvasArtifact) store.ArtifactKind {
	if canvas == nil {
		return store.ArtifactKindPlanNote
	}
	switch strings.ToLower(strings.TrimSpace(canvas.Kind)) {
	case "image_artifact":
		return store.ArtifactKindImage
	case "pdf_artifact":
		return store.ArtifactKindPDF
	case "text_artifact", "text":
		title := strings.ToLower(strings.TrimSpace(canvas.Title))
		switch filepath.Ext(title) {
		case ".md", ".markdown":
			return store.ArtifactKindMarkdown
		default:
			return store.ArtifactKindDocument
		}
	default:
		return store.ArtifactKindPlanNote
	}
}

func resolveConversationArtifactPath(cwd string, canvas *conversationCanvasArtifact) *string {
	if canvas == nil {
		return nil
	}
	if rawPath := strings.TrimSpace(canvas.Path); rawPath != "" {
		if absPath, _, err := resolveCanvasFilePath(cwd, rawPath); err == nil {
			return &absPath
		}
	}
	if titlePath := resolveArtifactFilePath(cwd, canvas.Title); titlePath != "" {
		return &titlePath
	}
	return nil
}

func conversationArtifactMeta(source string, text string) *string {
	payload := map[string]string{
		"source": source,
	}
	cleanText := strings.TrimSpace(text)
	if cleanText != "" {
		payload["text"] = cleanText
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	meta := string(raw)
	return &meta
}

func (a *App) createConversationArtifact(project store.Project, title string, assistantText string, canvas *conversationCanvasArtifact) (*store.Artifact, error) {
	if canvas == nil && strings.TrimSpace(assistantText) == "" {
		return nil, nil
	}
	cwd := strings.TrimSpace(project.RootPath)
	if cwd == "" {
		cwd = strings.TrimSpace(a.cwdForProjectKey(project.ProjectKey))
	}
	artifactTitle := strings.TrimSpace(title)
	if canvas != nil && strings.TrimSpace(canvas.Title) != "" {
		artifactTitle = strings.TrimSpace(canvas.Title)
	}
	var titlePtr *string
	if artifactTitle != "" {
		titlePtr = &artifactTitle
	}
	if canvas != nil {
		artifact, err := a.store.CreateArtifact(
			conversationCanvasArtifactKind(canvas),
			resolveConversationArtifactPath(cwd, canvas),
			nil,
			titlePtr,
			conversationArtifactMeta("canvas", canvas.Text),
		)
		if err != nil {
			return nil, err
		}
		return &artifact, nil
	}
	artifact, err := a.store.CreateArtifact(
		store.ArtifactKindPlanNote,
		nil,
		nil,
		titlePtr,
		conversationArtifactMeta("assistant", assistantText),
	)
	if err != nil {
		return nil, err
	}
	return &artifact, nil
}

func (a *App) resolveConversationWorkspaceID(project store.Project, artifact *store.Artifact) (*int64, error) {
	if artifact != nil {
		if inferred, err := a.store.InferWorkspaceForArtifact(*artifact); err != nil {
			return nil, err
		} else if inferred != nil {
			return inferred, nil
		}
	}
	rootPath := strings.TrimSpace(project.RootPath)
	if rootPath == "" {
		rootPath = strings.TrimSpace(a.cwdForProjectKey(project.ProjectKey))
	}
	if rootPath != "" {
		if workspaceID, err := a.store.FindWorkspaceContainingPath(rootPath); err != nil {
			return nil, err
		} else if workspaceID != nil {
			return workspaceID, nil
		}
	}
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	for _, workspace := range workspaces {
		if workspace.IsActive {
			id := workspace.ID
			return &id, nil
		}
	}
	return nil, nil
}

func (a *App) buildConversationItemContext(sessionID string, project store.Project) (conversationItemContext, error) {
	messages, err := a.store.ListChatMessages(sessionID, 200)
	if err != nil {
		return conversationItemContext{}, err
	}
	assistantText := latestConversationAssistantText(messages)
	canvas := a.resolveConversationCanvasArtifact(project)
	titleFallback := ""
	if canvas != nil {
		titleFallback = canvas.Title
	}
	title := deriveConversationItemTitle(assistantText, titleFallback)
	if title == "" {
		return conversationItemContext{}, errors.New("no recent assistant output to turn into an item")
	}
	artifact, err := a.createConversationArtifact(project, title, assistantText, canvas)
	if err != nil {
		return conversationItemContext{}, err
	}
	workspaceID, err := a.resolveConversationWorkspaceID(project, artifact)
	if err != nil {
		return conversationItemContext{}, err
	}
	ctx := conversationItemContext{
		Title:         title,
		AssistantText: assistantText,
		WorkspaceID:   workspaceID,
	}
	if artifact != nil {
		ctx.ArtifactID = &artifact.ID
	}
	return ctx, nil
}

func (a *App) resolveActorByName(name string) (store.Actor, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return store.Actor{}, errors.New("actor name is required")
	}
	actors, err := a.store.ListActors()
	if err != nil {
		return store.Actor{}, err
	}
	var exact []store.Actor
	var partial []store.Actor
	for _, actor := range actors {
		switch {
		case strings.EqualFold(actor.Name, cleanName):
			exact = append(exact, actor)
		case strings.Contains(strings.ToLower(actor.Name), strings.ToLower(cleanName)):
			partial = append(partial, actor)
		}
	}
	switch {
	case len(exact) == 1:
		return exact[0], nil
	case len(exact) > 1:
		return store.Actor{}, fmt.Errorf("actor name %q is ambiguous", cleanName)
	case len(partial) == 1:
		return partial[0], nil
	case len(partial) > 1:
		return store.Actor{}, fmt.Errorf("actor name %q is ambiguous", cleanName)
	default:
		return store.Actor{}, fmt.Errorf("actor %q not found", cleanName)
	}
}

func (a *App) createConversationItem(sessionID string, session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	targetProject, err := a.systemActionTargetProject(session)
	if err != nil {
		return "", nil, err
	}
	ctx, err := a.buildConversationItemContext(sessionID, targetProject)
	if err != nil {
		return "", nil, err
	}

	makeOpts := func() store.ItemOptions {
		return store.ItemOptions{
			WorkspaceID: ctx.WorkspaceID,
			ArtifactID:  ctx.ArtifactID,
		}
	}

	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "make_item":
		item, err := a.store.CreateItem(ctx.Title, makeOpts())
		if err != nil {
			return "", nil, err
		}
		artifactID := int64(0)
		if ctx.ArtifactID != nil {
			artifactID = *ctx.ArtifactID
		}
		return fmt.Sprintf("Created inbox item %q.", item.Title), map[string]interface{}{
			"type":        "item_created",
			"item_id":     item.ID,
			"state":       item.State,
			"title":       item.Title,
			"artifact_id": artifactID,
		}, nil
	case "delegate_item":
		actor, err := a.resolveActorByName(systemActionActorName(action.Params))
		if err != nil {
			return "", nil, err
		}
		opts := makeOpts()
		opts.State = store.ItemStateWaiting
		opts.ActorID = &actor.ID
		item, err := a.store.CreateItem(ctx.Title, opts)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Created waiting item %q for %s.", item.Title, actor.Name), map[string]interface{}{
			"type":     "item_created",
			"item_id":  item.ID,
			"state":    item.State,
			"title":    item.Title,
			"actor_id": actor.ID,
		}, nil
	case "snooze_item":
		visibleAfter := systemActionVisibleAfter(action.Params)
		if visibleAfter == "" {
			return "", nil, errors.New("visible_after is required")
		}
		opts := makeOpts()
		opts.State = store.ItemStateWaiting
		opts.VisibleAfter = &visibleAfter
		item, err := a.store.CreateItem(ctx.Title, opts)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Created waiting item %q for %s.", item.Title, visibleAfter), map[string]interface{}{
			"type":          "item_created",
			"item_id":       item.ID,
			"state":         item.State,
			"title":         item.Title,
			"visible_after": visibleAfter,
		}, nil
	case "split_items":
		count := systemActionSplitCount(action.Params)
		if count <= 0 {
			return "", nil, errors.New("split item count is required")
		}
		titles, err := deriveSplitItemTitles(ctx.AssistantText, count)
		if err != nil {
			return "", nil, err
		}
		itemIDs := make([]int64, 0, len(titles))
		for _, title := range titles {
			item, createErr := a.store.CreateItem(title, makeOpts())
			if createErr != nil {
				return "", nil, createErr
			}
			itemIDs = append(itemIDs, item.ID)
		}
		return fmt.Sprintf("Created %d inbox items.", len(itemIDs)), map[string]interface{}{
			"type":     "items_created",
			"item_ids": itemIDs,
			"count":    len(itemIDs),
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported item action: %s", action.Action)
	}
}
