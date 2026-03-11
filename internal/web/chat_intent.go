package web

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const (
	DefaultIntentLLMURL            = "http://127.0.0.1:8426"
	DefaultIntentLLMModel          = "local"
	DefaultIntentLLMProfile        = "qwen3.5-9b"
	DefaultIntentLLMProfileOptions = "qwen3.5-9b,qwen3.5-4b"
	intentLLMRequestTimeout        = 900 * time.Millisecond
	intentLLMResponseLimit         = 128 * 1024
	systemActionShellTimeout       = 8 * time.Second
	systemActionShellOutputLimit   = 16 * 1024
	systemActionOpenFileSizeLimit  = 256 * 1024
)

type SystemAction struct {
	Action string                 `json:"action"`
	Params map[string]interface{} `json:"-"`
}

type intentPlanClassification struct {
	Actions     []*SystemAction
	Addressed   *bool
	LocalAnswer *intentLocalAnswer
	Ack         string
}

type localTurnEvaluation struct {
	handled               bool
	text                  string
	payloads              []map[string]interface{}
	localAnswerConfidence string
	ack                   string
}

func (e localTurnEvaluation) suppressesResponse() bool {
	return suppressLocalAssistantResponse(e.payloads)
}

func (e localTurnEvaluation) isCommand() bool {
	return e.handled && len(e.payloads) > 0 && !e.suppressesResponse()
}

func (e localTurnEvaluation) isHighConfidenceLocalAnswer() bool {
	return e.handled &&
		len(e.payloads) == 0 &&
		strings.TrimSpace(e.text) != "" &&
		e.localAnswerConfidence == "high"
}

func (e localTurnEvaluation) fallbackText() string {
	if strings.TrimSpace(e.text) == "" {
		return ""
	}
	return strings.TrimSpace(e.text)
}

const systemActionLastShellPathPlaceholder = "$last_shell_path"

func extractEmbeddedJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	start := -1
	for idx, r := range trimmed {
		if r == '{' || r == '[' {
			start = idx
			break
		}
	}
	if start < 0 {
		return ""
	}
	for idx := start; idx < len(trimmed); idx++ {
		candidate := strings.TrimSpace(trimmed[start : idx+1])
		if candidate == "" {
			continue
		}
		var decoded interface{}
		if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
			return candidate
		}
	}
	return ""
}

func repairMalformedCommandQuotes(raw string) string {
	const marker = `"command":"`
	if !strings.Contains(raw, marker) {
		return raw
	}
	var out strings.Builder
	out.Grow(len(raw))
	cursor := 0
	for cursor < len(raw) {
		rel := strings.Index(raw[cursor:], marker)
		if rel < 0 {
			out.WriteString(raw[cursor:])
			break
		}
		start := cursor + rel
		endStart := start + len(marker)
		out.WriteString(raw[cursor:endStart])
		cursor = endStart
		for cursor < len(raw) {
			ch := raw[cursor]
			if ch == '\\' && cursor+1 < len(raw) {
				out.WriteByte(raw[cursor])
				out.WriteByte(raw[cursor+1])
				cursor += 2
				continue
			}
			if ch == '"' {
				lookahead := cursor + 1
				for lookahead < len(raw) {
					next := raw[lookahead]
					if next == ' ' || next == '\n' || next == '\r' || next == '\t' {
						lookahead++
						continue
					}
					break
				}
				if lookahead >= len(raw) || raw[lookahead] == ',' || raw[lookahead] == '}' {
					out.WriteByte('"')
					cursor++
					break
				}
				out.WriteByte('\'')
				cursor++
				continue
			}
			out.WriteByte(ch)
			cursor++
		}
	}
	return out.String()
}

func parseSystemAction(raw string) (*SystemAction, error) {
	return parseSystemActionJSON(raw)
}

func parseSystemActions(raw string) ([]*SystemAction, error) {
	return parseSystemActionsJSON(raw)
}

func parseSystemActionObject(obj map[string]interface{}) *SystemAction {
	if obj == nil {
		return nil
	}
	kind := normalizeIntentResponseKind(fmt.Sprint(obj["kind"]))
	if kind == "" {
		kind = normalizeIntentResponseKind(fmt.Sprint(obj["type"]))
	}
	if kind == intentKindDialogue {
		return nil
	}
	if kind == intentKindCanonicalAction {
		action := normalizeCanonicalActionName(fmt.Sprint(obj["action"]))
		if action == "" {
			action = normalizeCanonicalActionName(fmt.Sprint(obj["canonical_action"]))
		}
		if action == "" {
			return nil
		}
		params := make(map[string]interface{}, len(obj))
		for key, value := range obj {
			trimmed := strings.TrimSpace(key)
			if strings.EqualFold(trimmed, "action") || strings.EqualFold(trimmed, "canonical_action") || strings.EqualFold(trimmed, "kind") || strings.EqualFold(trimmed, "type") {
				continue
			}
			params[key] = value
		}
		return &SystemAction{Action: action, Params: params}
	}
	action := normalizeSystemActionName(fmt.Sprint(obj["action"]))
	if action == "" {
		action = normalizeSystemActionName(fmt.Sprint(obj["intent"]))
	}
	if action == "" && kind == intentKindSystemCommand {
		action = normalizeSystemActionName(fmt.Sprint(obj["command"]))
	}
	if action == "" {
		return nil
	}
	params := make(map[string]interface{}, len(obj))
	for key, value := range obj {
		if strings.EqualFold(strings.TrimSpace(key), "action") || strings.EqualFold(strings.TrimSpace(key), "intent") {
			continue
		}
		params[key] = value
	}
	return &SystemAction{Action: action, Params: params}
}

func parseSystemActionJSON(raw string) (*SystemAction, error) {
	actions, err := parseSystemActionsJSON(raw)
	if err != nil {
		return nil, err
	}
	if len(actions) == 0 {
		return nil, nil
	}
	return actions[0], nil
}

func decodeSystemActionCandidate(candidate string) (interface{}, bool) {
	var decoded interface{}
	if err := json.Unmarshal([]byte(candidate), &decoded); err == nil {
		return decoded, true
	}
	repaired := repairMalformedCommandQuotes(candidate)
	if repaired != candidate {
		if err := json.Unmarshal([]byte(repaired), &decoded); err == nil {
			return decoded, true
		}
	}
	return nil, false
}

func decodeSystemActionJSON(raw string) (interface{}, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	decoded, ok := decodeSystemActionCandidate(trimmed)
	if !ok {
		embedded := extractEmbeddedJSON(trimmed)
		if embedded == "" {
			return nil, false
		}
		decoded, ok = decodeSystemActionCandidate(embedded)
		if !ok {
			return nil, false
		}
	}
	return decoded, true
}

func collectSystemActionsFromDecoded(decoded interface{}) []*SystemAction {
	collect := func(values []interface{}) []*SystemAction {
		actions := make([]*SystemAction, 0, len(values))
		for _, value := range values {
			obj, _ := value.(map[string]interface{})
			if action := parseSystemActionObject(obj); action != nil {
				actions = append(actions, action)
			}
		}
		return actions
	}
	switch typed := decoded.(type) {
	case map[string]interface{}:
		if rawActions, ok := typed["actions"]; ok {
			items, _ := rawActions.([]interface{})
			return collect(items)
		}
		action := parseSystemActionObject(typed)
		if action == nil {
			return nil
		}
		return []*SystemAction{action}
	case []interface{}:
		return collect(typed)
	default:
		return nil
	}
}

func parseSystemActionsJSON(raw string) ([]*SystemAction, error) {
	decoded, ok := decodeSystemActionJSON(raw)
	if !ok {
		return nil, nil
	}
	actions := collectSystemActionsFromDecoded(decoded)
	if len(actions) == 0 {
		return nil, nil
	}
	return actions, nil
}

func systemActionStringParam(params map[string]interface{}, key string) string {
	return strings.TrimSpace(fmt.Sprint(params[key]))
}

func normalizeSystemActionName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "switch_project", "switch_workspace", "focus_workspace", "clear_focus", "list_workspace_items", "list_workspaces", "create_workspace", "create_workspace_from_git", "rename_workspace", "delete_workspace", "show_workspace_details", "workspace_watch_start", "workspace_watch_stop", "workspace_watch_status", "batch_work", "batch_configure", "review_policy", "batch_limit", "batch_status", "assign_workspace_project", "show_workspace_project", "create_project", "list_project_workspaces", "link_workspace_artifact", "list_linked_artifacts", "switch_model", "toggle_silent", "toggle_live_dialogue", "cancel_work", "show_status", "shell", "open_file_canvas", "show_calendar", "show_briefing", "make_item", "delegate_item", "snooze_item", "split_items", "reassign_workspace", "reassign_project", "clear_workspace", "clear_project", "capture_idea", "refine_idea_note", "promote_idea", "apply_idea_promotion", "create_github_issue", "create_github_issue_split", "print_item", "review_someday", "triage_someday", "promote_someday", "toggle_someday_review_nudge", "show_filtered_items", "sync_project", "sync_sources", "map_todoist_project", "sync_todoist", "create_todoist_task", "sync_evernote", "sync_bear", "promote_bear_checklist", "sync_zotero", "cursor_open_item", "cursor_triage_item", "cursor_open_path":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func systemActionShellCommand(params map[string]interface{}) string {
	for _, key := range []string{"command", "cmd", "text"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func systemActionOpenPath(params map[string]interface{}) string {
	for _, key := range []string{"path", "file", "target"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func mergeSystemActionParams(target map[string]interface{}, source map[string]interface{}) {
	if source == nil {
		return
	}
	for key, value := range source {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		target[trimmed] = value
	}
}

func stripCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 {
		return trimmed
	}
	if !strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		return trimmed
	}
	end := len(lines)
	if end > 1 && strings.HasPrefix(strings.TrimSpace(lines[end-1]), "```") {
		end--
	}
	if end <= 1 {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[1:end], "\n"))
}

func normalizeSystemActionForExecution(action *SystemAction, fallbackText string) *SystemAction {
	if action == nil {
		return nil
	}
	action = translateCanonicalActionForExecution(action)
	if action == nil {
		return nil
	}
	if normalizeSystemActionName(action.Action) == "" {
		return nil
	}
	if action.Params == nil {
		action.Params = map[string]interface{}{}
	}
	if strings.EqualFold(strings.TrimSpace(action.Action), "capture_idea") {
		if strings.TrimSpace(systemActionStringParam(action.Params, "text")) == "" {
			action.Params["text"] = strings.TrimSpace(fallbackText)
		}
		if strings.TrimSpace(systemActionStringParam(action.Params, "capture_mode")) == "" {
			action.Params["capture_mode"] = chatCaptureModeText
		}
	}
	if strings.EqualFold(strings.TrimSpace(action.Action), "refine_idea_note") {
		if strings.TrimSpace(systemActionStringParam(action.Params, "text")) == "" {
			action.Params["text"] = strings.TrimSpace(fallbackText)
		}
	}
	return action
}

func requestRequiresOpenCanvasAction(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	prefixes := []string{"open ", "show ", "display "}
	hasPrefix := false
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			hasPrefix = true
			break
		}
	}
	if !hasPrefix {
		return false
	}
	if strings.Contains(lower, " file") || strings.Contains(lower, " document") || strings.Contains(lower, " diff") || strings.Contains(lower, " path") {
		return true
	}
	for _, rawHint := range extractOpenRequestHints(text) {
		hint := normalizeOpenHintToken(rawHint)
		if hint == "" {
			continue
		}
		if strings.Contains(hint, "/") || strings.Contains(hint, ".") {
			return true
		}
		base := filepath.Base(hint)
		if len(base) >= 4 && base == strings.ToUpper(base) {
			return true
		}
		switch base {
		case "readme", "license", "makefile", "dockerfile", "compose", "claude", "agents":
			return true
		}
	}
	return false
}

func planContainsAction(actions []*SystemAction, actionName string) bool {
	needle := strings.ToLower(strings.TrimSpace(actionName))
	if needle == "" {
		return false
	}
	for _, action := range actions {
		if action == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(action.Action), needle) {
			return true
		}
	}
	return false
}

func safeFindToken(raw string) string {
	token := strings.TrimSpace(strings.ToLower(raw))
	if token == "" {
		return ""
	}
	var out strings.Builder
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == '/' {
			out.WriteRune(r)
		}
	}
	clean := strings.TrimSpace(out.String())
	return strings.Trim(clean, "/")
}

func buildOpenCanvasFallbackPlan(text string) []*SystemAction {
	if !requestRequiresOpenCanvasAction(text) {
		return nil
	}
	hints := extractOpenRequestHints(text)
	patterns := make([]string, 0, 16)
	addPattern := func(pattern string) {
		p := strings.TrimSpace(pattern)
		if p == "" {
			return
		}
		for _, existing := range patterns {
			if strings.EqualFold(existing, p) {
				return
			}
		}
		patterns = append(patterns, p)
	}
	for _, rawHint := range hints {
		hint := safeFindToken(normalizeOpenHintToken(rawHint))
		if hint == "" {
			continue
		}
		base := safeFindToken(filepath.Base(hint))
		stem := safeFindToken(strings.TrimSuffix(base, filepath.Ext(base)))
		if base != "" {
			addPattern(base)
			addPattern(base + ".*")
			addPattern("*" + base + "*")
		}
		if stem != "" && stem != base {
			addPattern(stem)
			addPattern(stem + ".*")
			addPattern("*" + stem + "*")
		}
	}
	if len(patterns) == 0 {
		return nil
	}
	parts := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		parts = append(parts, fmt.Sprintf("-iname '%s'", pattern))
	}
	command := "find . -maxdepth 8 -type f \\( " + strings.Join(parts, " -o ") + " \\) | head -n 80"
	return []*SystemAction{
		{
			Action: "shell",
			Params: map[string]interface{}{"command": command},
		},
		{
			Action: "open_file_canvas",
			Params: map[string]interface{}{"path": systemActionLastShellPathPlaceholder},
		},
	}
}

func ensureOpenCanvasTerminalAction(actions []*SystemAction) []*SystemAction {
	if len(actions) == 0 || planContainsAction(actions, "open_file_canvas") {
		return actions
	}
	hasShell := false
	for _, action := range actions {
		if action == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(action.Action), "shell") {
			continue
		}
		if strings.TrimSpace(systemActionShellCommand(action.Params)) == "" {
			continue
		}
		hasShell = true
		break
	}
	if !hasShell {
		return actions
	}
	repaired := make([]*SystemAction, 0, len(actions)+1)
	repaired = append(repaired, actions...)
	repaired = append(repaired, &SystemAction{
		Action: "open_file_canvas",
		Params: map[string]interface{}{"path": systemActionLastShellPathPlaceholder},
	})
	return repaired
}

func (a *App) classifyAndExecuteSystemAction(ctx context.Context, sessionID string, session store.ChatSession, text string) (string, []map[string]interface{}, bool) {
	return a.classifyAndExecuteSystemActionWithCursor(ctx, sessionID, session, text, nil)
}

func (a *App) classifyAndExecuteSystemActionWithCursor(ctx context.Context, sessionID string, session store.ChatSession, text string, cursor *chatCursorContext) (string, []map[string]interface{}, bool) {
	return a.classifyAndExecuteSystemActionForTurn(ctx, sessionID, session, text, cursor, "")
}

func (a *App) classifyAndExecuteSystemActionForTurn(ctx context.Context, sessionID string, session store.ChatSession, text string, cursor *chatCursorContext, captureMode string) (string, []map[string]interface{}, bool) {
	evaluation := a.evaluateLocalTurn(ctx, sessionID, session, text, cursor, captureMode)
	return evaluation.text, evaluation.payloads, evaluation.handled
}

func (a *App) evaluateLocalTurn(ctx context.Context, sessionID string, session store.ChatSession, text string, cursor *chatCursorContext, captureMode string) localTurnEvaluation {
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return localTurnEvaluation{}
	}
	intentText := trimmedText
	livePolicy := a.LivePolicy()
	assumeAddressed := livePolicy.Config().AssumeAddressed
	tryExecutePlan := func(actions []*SystemAction, ack string) localTurnEvaluation {
		enforced := enforceRoutingPolicy(trimmedText, actions)
		if len(enforced) == 0 {
			return localTurnEvaluation{}
		}
		message, payloads, err := a.executeSystemActionPlan(sessionID, session, trimmedText, enforced)
		if err != nil {
			return localTurnEvaluation{}
		}
		return localTurnEvaluation{
			handled:  true,
			text:     message,
			payloads: payloads,
			ack:      strings.TrimSpace(ack),
		}
	}

	if pending := a.popPendingActionConfirmation(sessionID); pending != nil {
		if isExplicitDangerConfirm(trimmedText) {
			message, payloads, err := a.executeSystemActionPlanUnsafe(sessionID, session, pending.UserText, pending.Actions)
			if err != nil {
				return localTurnEvaluation{
					handled: true,
					text:    fmt.Sprintf("Confirmation failed: %v", err),
				}
			}
			return localTurnEvaluation{
				handled:  true,
				text:     message,
				payloads: payloads,
			}
		}
		if isExplicitDangerDecline(trimmedText) {
			return localTurnEvaluation{
				handled:  true,
				text:     pendingConfirmationCanceledMessage(pending.Kind),
				payloads: []map[string]interface{}{{"type": "confirmation_canceled"}},
			}
		}
	}

	captureMode = normalizeChatCaptureMode(firstNonEmptyCursorText(captureMode, a.chatCaptureModes.consume(sessionID)))
	now := time.Now().UTC()
	if a != nil && a.calendarNow != nil {
		now = a.calendarNow().UTC()
	}
	pendingAck := ""
	tryDeterministicPlan := func() (string, []map[string]interface{}, bool) {
		match := tryDeterministicFastPath(trimmedText, deterministicFastPathContext{
			Now:         now,
			CaptureMode: captureMode,
			Cursor:      cursor,
		})
		if match == nil {
			return "", nil, false
		}
		return a.executeDeterministicFastPath(ctx, sessionID, session, trimmedText, match)
	}
	if assumeAddressed {
		if message, payloads, handled := tryDeterministicPlan(); handled {
			return localTurnEvaluation{
				handled:  true,
				text:     message,
				payloads: payloads,
			}
		}
	}
	intentText = a.contextualizeClarificationReplyForSession(sessionID, trimmedText)
	if strings.TrimSpace(a.intentLLMURL) != "" {
		classification, llmErr := a.classifyIntentPlanWithLLMResultForTurn(ctx, sessionID, session, intentText)
		if llmErr == nil {
			pendingAck = classification.Ack
			if addressed, known := resolveIntentAddressedness(livePolicy, intentText, classification.Addressed); known && !addressed {
				return localTurnEvaluation{
					handled: true,
					payloads: []map[string]interface{}{{
						"type":              "meeting_capture",
						"addressed":         false,
						"suppress_response": true,
					}},
				}
			}
			if classification.LocalAnswer != nil && strings.TrimSpace(classification.LocalAnswer.Text) != "" {
				return localTurnEvaluation{
					handled:               true,
					text:                  classification.LocalAnswer.Text,
					localAnswerConfidence: classification.LocalAnswer.Confidence,
					ack:                   classification.Ack,
				}
			}
			if evaluation := tryExecutePlan(classification.Actions, classification.Ack); evaluation.handled {
				return evaluation
			}
			if !assumeAddressed {
				if message, payloads, handled := tryDeterministicPlan(); handled {
					return localTurnEvaluation{
						handled:  true,
						text:     message,
						payloads: payloads,
					}
				}
			}
		}
		if requestRequiresOpenCanvasAction(intentText) {
			if fallbackPlan := buildOpenCanvasFallbackPlan(intentText); len(fallbackPlan) > 0 {
				if evaluation := tryExecutePlan(fallbackPlan, pendingAck); evaluation.handled {
					return evaluation
				}
			}
			return localTurnEvaluation{
				handled: true,
				text:    "I couldn't open that file on canvas. Please provide an exact relative path (for example: docs/CLAUDE.md).",
			}
		}
	}
	if !assumeAddressed && isCompanionDirectAddress(intentText) {
		if message, payloads, handled := tryDeterministicPlan(); handled {
			return localTurnEvaluation{
				handled:  true,
				text:     message,
				payloads: payloads,
			}
		}
	}

	if cursor != nil && cursor.hasPointedItem() && looksLikeStandaloneSystemRequest(trimmedText) {
		if message, payloads, ok := a.suggestCanonicalActionsForCursorItem(cursor); ok {
			return localTurnEvaluation{
				handled:  true,
				text:     message,
				payloads: payloads,
			}
		}
	}
	return localTurnEvaluation{ack: pendingAck}
}

func resolveIntentAddressedness(policy LivePolicy, text string, addressed *bool) (bool, bool) {
	if normalizeLivePolicy(policy.String()) != LivePolicyMeeting {
		return true, true
	}
	if isCompanionDirectAddress(text) {
		return true, true
	}
	if addressed == nil {
		return false, false
	}
	return *addressed, true
}

func (a *App) suggestCanonicalActionsForCursorItem(cursor *chatCursorContext) (string, []map[string]interface{}, bool) {
	if a == nil || a.store == nil || cursor == nil || cursor.ItemID <= 0 {
		return "", nil, false
	}
	item, err := a.store.GetItem(cursor.ItemID)
	if err != nil {
		return "", nil, false
	}
	artifactKind := ""
	artifactTitle := firstNonEmptyCursorText(cursor.ItemTitle, item.Title)
	if item.ArtifactID != nil && *item.ArtifactID > 0 {
		artifact, artifactErr := a.store.GetArtifact(*item.ArtifactID)
		if artifactErr == nil {
			artifactKind = string(artifact.Kind)
			artifactTitle = firstNonEmptyCursorText(optionalStringValue(artifact.Title), artifactTitle)
		}
	}
	if artifactKind == "" {
		linkedArtifacts, listErr := a.store.ListItemArtifacts(item.ID)
		if listErr == nil && len(linkedArtifacts) > 0 {
			artifactKind = string(linkedArtifacts[0].Artifact.Kind)
			artifactTitle = firstNonEmptyCursorText(optionalStringValue(linkedArtifacts[0].Artifact.Title), artifactTitle)
		}
	}
	artifactKind = normalizedArtifactKind(artifactKind)
	if artifactKind == "" {
		return "", nil, false
	}
	spec := lookupArtifactKindSpec(artifactKind)
	if len(spec.Actions) == 0 {
		return "", nil, false
	}
	actionLabels := artifactPromptActions(artifactKind)
	target := firstNonEmptyCursorText(artifactTitle, "this item")
	kindLabel := strings.ReplaceAll(artifactKind, "_", " ")
	message := fmt.Sprintf(
		"I wasn't confident enough to guess. Use one of the %s actions for %q: %s.",
		kindLabel,
		target,
		strings.Join(actionLabels, ", "),
	)
	return message, []map[string]interface{}{{
		"type":          "suggest_canonical_actions",
		"actions":       append([]string(nil), spec.Actions...),
		"item_id":       item.ID,
		"item_state":    item.State,
		"artifact_kind": artifactKind,
		"message":       message,
	}}, true
}
