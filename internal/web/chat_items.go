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
	itemSplitPattern       = regexp.MustCompile(`(?i)^(?:split|teile)\s+(?:this|it|das)\s+(?:into|in)\s+(.+?)\s+(?:items?|aufgaben?)$`)
	ideaPrefixPattern      = regexp.MustCompile(`(?i)^\s*(?:new\s+idea|idea|i\s+have\s+an\s+idea|capture|idee|einfall|ich\s+habe\s+eine\s+idee)\s*:\s*(.+?)\s*$`)
	ideaSentencePattern    = regexp.MustCompile(`^.*?[.!?](?:\s|$)`)
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
	BodyText      string
	WorkspaceID   *int64
	ArtifactID    *int64
}

func normalizeItemCommandText(raw string) string {
	text := normalizeDelegationCommandText(raw)
	text = strings.TrimPrefix(text, "please ")
	text = strings.TrimPrefix(text, "bitte ")
	return strings.TrimSpace(text)
}

func parseInlineItemIntent(text string, now time.Time) *SystemAction {
	return parseInlineItemIntentWithCaptureMode(text, now, chatCaptureModeText)
}

func parseInlineItemIntentWithCaptureMode(text string, now time.Time, captureMode string) *SystemAction {
	normalized := normalizeItemCommandText(text)
	if filterAction := parseInlineItemFilterIntent(text); filterAction != nil {
		return filterAction
	}
	if somedayAction := parseInlineSomedayIntent(text); somedayAction != nil {
		return somedayAction
	}
	if ideaText, ok := extractIdeaCaptureText(text); ok {
		return &SystemAction{
			Action: canonicalActionAnnotateCapture,
			Params: map[string]interface{}{
				"target":       "idea_note",
				"text":         text,
				"idea_text":    ideaText,
				"capture_mode": normalizeChatCaptureMode(captureMode),
			},
		}
	}
	if ideaRefinement := parseInlineIdeaRefinementIntent(text); ideaRefinement != nil {
		return ideaRefinement
	}
	if ideaPromotion := parseInlineIdeaPromotionIntent(text); ideaPromotion != nil {
		return ideaPromotion
	}
	if ideaPromotionApply := parseInlineIdeaPromotionApplyIntent(text); ideaPromotionApply != nil {
		return ideaPromotionApply
	}
	if reassignment := parseInlineItemReassignmentIntent(text); reassignment != nil {
		return reassignment
	}
	switch normalized {
	case "make this an item", "track this", "add to inbox", "mach das zu einem item", "mach daraus ein item", "fuege das zum posteingang hinzu":
		return &SystemAction{Action: canonicalActionTrackItem, Params: map[string]interface{}{}}
	case "print this item", "print this for me", "druck das aus", "druck dieses item aus":
		return &SystemAction{Action: canonicalActionDispatchExecute, Params: map[string]interface{}{"target": "print"}}
	case "later", "remind me later", "spaeter", "erinnere mich spaeter":
		visibleAfter := defaultReminderTime(now)
		return &SystemAction{
			Action: canonicalActionTrackItem,
			Params: map[string]interface{}{"visible_after": visibleAfter},
		}
	}

	if actor := extractInlineDelegateActor(text); actor != "" {
		return &SystemAction{
			Action: canonicalActionDelegateActor,
			Params: map[string]interface{}{"actor": actor},
		}
	}

	if visibleAfter, ok := parseReminderVisibleAfter(text, now); ok {
		return &SystemAction{
			Action: canonicalActionTrackItem,
			Params: map[string]interface{}{"visible_after": visibleAfter},
		}
	}

	if match := itemSplitPattern.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 2 {
		if count, ok := parseItemSplitCount(match[1]); ok && count > 0 {
			return &SystemAction{
				Action: canonicalActionTrackItem,
				Params: map[string]interface{}{"count": count},
			}
		}
	}
	return nil
}

func extractIdeaCaptureText(raw string) (string, bool) {
	match := ideaPrefixPattern.FindStringSubmatch(raw)
	if len(match) != 2 {
		return "", false
	}
	clean := normalizeIdeaText(match[1])
	if clean == "" {
		return "", false
	}
	return clean, true
}

func normalizeIdeaText(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}

func deriveIdeaTitle(raw string) string {
	clean := normalizeIdeaText(raw)
	if clean == "" {
		return ""
	}
	sentenceMatch := ideaSentencePattern.FindString(clean)
	title := clean
	if strings.TrimSpace(sentenceMatch) != "" {
		title = normalizeIdeaText(sentenceMatch)
	}
	runes := []rune(title)
	if len(runes) > 80 {
		title = strings.TrimSpace(string(runes[:77])) + "..."
	}
	runes = []rune(title)
	if len(runes) == 0 {
		return ""
	}
	first := strings.ToUpper(string(runes[0]))
	if len(runes) == 1 {
		return first
	}
	return first + string(runes[1:])
}

func parseReminderVisibleAfter(text string, now time.Time) (string, bool) {
	lower := normalizeItemCommandText(text)
	if strings.Contains(lower, "tomorrow") {
		return defaultReminderTime(now), true
	}
	if strings.Contains(lower, "morgen") {
		return defaultReminderTime(now), true
	}
	if strings.Contains(lower, "next week") || strings.Contains(lower, "naechste woche") {
		base := now.UTC().AddDate(0, 0, 7)
		return time.Date(base.Year(), base.Month(), base.Day(), 9, 0, 0, 0, time.UTC).Format(time.RFC3339), true
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
	for weekday, name := range map[time.Weekday]string{
		time.Monday:    "montag",
		time.Tuesday:   "dienstag",
		time.Wednesday: "mittwoch",
		time.Thursday:  "donnerstag",
		time.Friday:    "freitag",
		time.Saturday:  "samstag",
		time.Sunday:    "sonntag",
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
		"one":    1,
		"two":    2,
		"three":  3,
		"four":   4,
		"five":   5,
		"six":    6,
		"seven":  7,
		"eight":  8,
		"nine":   9,
		"ten":    10,
		"eins":   1,
		"zwei":   2,
		"drei":   3,
		"vier":   4,
		"fuenf":  5,
		"funf":   5,
		"sechs":  6,
		"sieben": 7,
		"acht":   8,
		"neun":   9,
		"zehn":   10,
	}
	n, ok := words[value]
	return n, ok
}

func isItemSystemAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case canonicalActionAnnotateCapture, canonicalActionCompose, canonicalActionBundleReview, canonicalActionDispatchExecute, canonicalActionTrackItem, canonicalActionDelegateActor, "make_item", "delegate_item", "snooze_item", "split_items", "reassign_workspace", "reassign_project", "clear_workspace", "clear_project", "capture_idea", "refine_idea_note", "promote_idea", "apply_idea_promotion", "create_github_issue", "create_github_issue_split", "print_item", "review_someday", "triage_someday", "promote_someday", "toggle_someday_review_nudge", "show_filtered_items":
		return true
	default:
		return false
	}
}

func itemActionFailurePrefix(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case canonicalActionAnnotateCapture:
		return "I couldn't capture the note: "
	case canonicalActionCompose:
		return "I couldn't update the note: "
	case canonicalActionBundleReview:
		return "I couldn't open the review bundle: "
	case canonicalActionDispatchExecute:
		return "I couldn't execute the artifact action: "
	case canonicalActionTrackItem:
		return "I couldn't track the item: "
	case canonicalActionDelegateActor:
		return "I couldn't delegate the item: "
	case "create_github_issue", "create_github_issue_split":
		return "I couldn't create the GitHub issue: "
	case "capture_idea":
		return "I couldn't capture the idea: "
	case "refine_idea_note":
		return "I couldn't update the idea note: "
	case "promote_idea":
		return "I couldn't prepare the idea promotion: "
	case "apply_idea_promotion":
		return "I couldn't promote the idea: "
	case "print_item":
		return "I couldn't prepare the print view: "
	case "review_someday":
		return "I couldn't open the someday list: "
	case "show_filtered_items":
		return "I couldn't open the filtered item list: "
	case "toggle_someday_review_nudge":
		return "I couldn't update the someday reminder setting: "
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "split_items":
		return "I couldn't create the items: "
	case "reassign_workspace", "reassign_project", "clear_workspace", "clear_project", "triage_someday", "promote_someday":
		return "I couldn't update the item: "
	}
	return "I couldn't create the item: "
}

func systemActionActorName(params map[string]interface{}) string {
	for _, key := range []string{"actor", "name", "target"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return cleanActorReference(value)
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

func (a *App) resolveConversationCanvasArtifact(project store.Workspace) *conversationCanvasArtifact {
	canvasSessionID := strings.TrimSpace(a.canvasSessionIDForWorkspace(project))
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

func (a *App) createConversationArtifact(project store.Workspace, title string, assistantText string, canvas *conversationCanvasArtifact) (*store.Artifact, error) {
	if canvas == nil && strings.TrimSpace(assistantText) == "" {
		return nil, nil
	}
	cwd := strings.TrimSpace(project.RootPath)
	if cwd == "" {
		cwd = strings.TrimSpace(a.cwdForWorkspacePath(project.WorkspacePath))
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

func (a *App) resolveConversationWorkspaceID(project store.Workspace, artifact *store.Artifact) (*int64, error) {
	if artifact != nil {
		if inferred, err := a.store.InferWorkspaceForArtifact(*artifact); err != nil {
			return nil, err
		} else if inferred != nil {
			return inferred, nil
		}
	}
	rootPath := strings.TrimSpace(project.RootPath)
	if rootPath == "" {
		rootPath = strings.TrimSpace(a.cwdForWorkspacePath(project.WorkspacePath))
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

func (a *App) buildConversationItemContext(sessionID string, project store.Workspace) (conversationItemContext, error) {
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
	bodyText := assistantText
	if canvas != nil && strings.TrimSpace(canvas.Text) != "" {
		bodyText = cleanConversationItemText(canvas.Text)
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
		BodyText:      bodyText,
		WorkspaceID:   workspaceID,
	}
	if artifact != nil {
		ctx.ArtifactID = &artifact.ID
	}
	return ctx, nil
}

func (a *App) resolveActorByName(name string) (store.Actor, error) {
	candidates := delegationActorLookupCandidates(name)
	if len(candidates) == 0 {
		return store.Actor{}, errors.New("actor name is required")
	}
	actors, err := a.store.ListActors()
	if err != nil {
		return store.Actor{}, err
	}
	findMatches := func(candidate string) ([]store.Actor, []store.Actor) {
		exact := make([]store.Actor, 0, 1)
		partial := make([]store.Actor, 0, 1)
		for _, actor := range actors {
			switch {
			case strings.EqualFold(actor.Name, candidate):
				exact = append(exact, actor)
			case strings.Contains(strings.ToLower(actor.Name), strings.ToLower(candidate)):
				partial = append(partial, actor)
			}
		}
		return exact, partial
	}
	for _, candidate := range candidates {
		exact, _ := findMatches(candidate)
		switch {
		case len(exact) == 1:
			return exact[0], nil
		case len(exact) > 1:
			return store.Actor{}, fmt.Errorf("actor name %q is ambiguous", candidate)
		}
	}
	for _, candidate := range candidates {
		_, partial := findMatches(candidate)
		switch {
		case len(partial) == 1:
			return partial[0], nil
		case len(partial) > 1:
			return store.Actor{}, fmt.Errorf("actor name %q is ambiguous", candidate)
		}
	}
	return store.Actor{}, fmt.Errorf("actor %q not found", candidates[0])
}

func ideaCaptureConfirmation(title string) string {
	clean := strings.TrimSpace(title)
	if clean == "" {
		return "Captured idea."
	}
	if strings.HasSuffix(clean, ".") || strings.HasSuffix(clean, "!") || strings.HasSuffix(clean, "?") {
		return "Captured idea: " + clean
	}
	return "Captured idea: " + clean + "."
}

func (a *App) captureIdeaItem(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	rawText := strings.TrimSpace(systemActionStringParam(action.Params, "text"))
	ideaText, ok := extractIdeaCaptureText(rawText)
	if !ok {
		ideaText = normalizeIdeaText(systemActionStringParam(action.Params, "idea_text"))
	}
	if ideaText == "" {
		return "", nil, errors.New("idea text is required after the prefix")
	}
	title := deriveIdeaTitle(ideaText)
	if title == "" {
		return "", nil, errors.New("idea title is required")
	}

	targetProject, err := a.systemActionTargetWorkspace(session)
	if err != nil {
		return "", nil, err
	}
	workspaceID, err := a.resolveConversationWorkspaceID(targetProject, nil)
	if err != nil {
		return "", nil, err
	}
	captureMode := normalizeChatCaptureMode(systemActionStringParam(action.Params, "capture_mode"))
	capturedAt := time.Now().UTC()
	workspaceName := ""
	if workspaceID != nil {
		if workspace, workspaceErr := a.store.GetWorkspace(*workspaceID); workspaceErr == nil {
			workspaceName = workspace.Name
		}
	}
	metaJSON, err := ideaArtifactMeta(title, ideaText, captureMode, workspaceName, capturedAt)
	if err != nil {
		return "", nil, err
	}
	artifact, err := a.store.CreateArtifact(store.ArtifactKindIdeaNote, nil, nil, &title, metaJSON)
	if err != nil {
		return "", nil, err
	}
	source := "idea"
	item, err := a.store.CreateItem(title, store.ItemOptions{
		WorkspaceID: workspaceID,
		ArtifactID:  &artifact.ID,
		Source:      &source,
	})
	if err != nil {
		return "", nil, err
	}
	payload := map[string]interface{}{
		"type":         "item_created",
		"item_id":      item.ID,
		"state":        item.State,
		"title":        item.Title,
		"artifact_id":  artifact.ID,
		"source":       source,
		"capture_mode": captureMode,
	}
	return ideaCaptureConfirmation(item.Title), payload, nil
}

func (a *App) createConversationItem(sessionID string, session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	targetProject, err := a.systemActionTargetWorkspace(session)
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
		if count < 0 {
			return "", nil, errors.New("split item count must be positive")
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
