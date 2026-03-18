package web

import (
	"context"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

type deterministicFastPathContext struct {
	Now         time.Time
	CaptureMode string
	Cursor      *chatCursorContext
}

type deterministicFastPathMatch struct {
	Name           string
	Actions        []*SystemAction
	TitledItem     *titledItemIntent
	FailureMessage func(userText string, enforced []*SystemAction, err error) string
}

type deterministicFastPathSpec struct {
	Name     string
	Route    string
	Actions  []string
	Triggers []string
}

type deterministicFastPathParser struct {
	Spec  deterministicFastPathSpec
	Parse func(text string, ctx deterministicFastPathContext) *deterministicFastPathMatch
}

var deterministicFastPaths = []deterministicFastPathParser{
	{
		Spec: deterministicFastPathSpec{
			Name:     "source_sync",
			Route:    "text",
			Actions:  []string{"sync_sources"},
			Triggers: []string{"sync all sources"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineSourceSyncIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("source_sync", action, fixedFastPathFailure(sourceSyncActionFailurePrefix(action.Action)))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "calendar",
			Route:    "text",
			Actions:  []string{"show_calendar", "create_calendar_event"},
			Triggers: []string{"show calendar", "create calendar event"},
		},
		Parse: func(text string, ctx deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineCalendarIntent(text, ctx.Now)
			if action == nil {
				return nil
			}
			failurePrefix := calendarActionFailurePrefix(action.Action)
			if action.Action == "create_calendar_event" {
				failurePrefix = calendarCreateActionFailurePrefix(action.Action)
			}
			return fastPathSingleAction("calendar", action, fixedFastPathFailure(failurePrefix))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "briefing",
			Route:    "text",
			Actions:  []string{"show_briefing"},
			Triggers: []string{"show briefing"},
		},
		Parse: func(text string, ctx deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineBriefingIntent(text, ctx.Now)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("briefing", action, fixedFastPathFailure(briefingActionFailurePrefix(action.Action)))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "todoist",
			Route:    "text",
			Actions:  []string{"map_todoist_project", "sync_todoist", "create_todoist_task"},
			Triggers: []string{"sync todoist"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineTodoistIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("todoist", action, fixedFastPathFailure(todoistActionFailurePrefix(action.Action)))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "evernote",
			Route:    "text",
			Actions:  []string{"sync_evernote"},
			Triggers: []string{"sync evernote"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineEvernoteIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("evernote", action, fixedFastPathFailure(evernoteActionFailurePrefix(action.Action)))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "bear",
			Route:    "text",
			Actions:  []string{"sync_bear", "promote_bear_checklist"},
			Triggers: []string{"sync bear"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineBearIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("bear", action, fixedFastPathFailure(bearActionFailurePrefix(action.Action)))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "zotero",
			Route:    "text",
			Actions:  []string{"sync_zotero"},
			Triggers: []string{"sync zotero"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineZoteroIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("zotero", action, fixedFastPathFailure(zoteroActionFailurePrefix(action.Action)))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "cursor",
			Route:    "text",
			Actions:  []string{"cursor_open_item", "cursor_triage_item", "cursor_open_path"},
			Triggers: []string{"open this"},
		},
		Parse: func(text string, ctx deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineCursorIntent(text, ctx.Cursor)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("cursor", action, fixedFastPathFailure("I couldn't resolve the pointed selection: "))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "titled_item",
			Route:    "text",
			Actions:  []string{"triage_item_by_title"},
			Triggers: []string{`move the item "Budget" back to the inbox`},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			intent := parseInlineTitledItemIntent(text)
			if intent == nil {
				return nil
			}
			return &deterministicFastPathMatch{
				Name:       "titled_item",
				TitledItem: intent,
				FailureMessage: func(_ string, _ []*SystemAction, err error) string {
					return "I couldn't resolve the named item: " + err.Error()
				},
			}
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "item",
			Route:    "text",
			Actions:  []string{canonicalActionTrackItem, "make_item", "delegate_item", "snooze_item", "split_items", "capture_idea", "refine_idea_note", "promote_idea", "apply_idea_promotion", "review_someday", "triage_someday", "promote_someday", "toggle_someday_review_nudge", "show_filtered_items", "print_item", "reassign_workspace", "reassign_project", "clear_workspace", "clear_project"},
			Triggers: []string{"idea: better swipe triage"},
		},
		Parse: func(text string, ctx deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineItemIntentWithCaptureMode(text, ctx.Now, ctx.CaptureMode)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("item", action, itemFastPathFailure)
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "github_issue",
			Route:    "text",
			Actions:  []string{canonicalActionDispatchExecute, "create_github_issue", "create_github_issue_split"},
			Triggers: []string{"create a GitHub issue from this"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			actions := parseInlineGitHubIssueActions(text)
			if len(actions) == 0 {
				return nil
			}
			return fastPathActionPlan("github_issue", actions, githubFastPathFailure)
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "artifact_link",
			Route:    "text",
			Actions:  []string{"link_workspace_artifact", "list_linked_artifacts"},
			Triggers: []string{"show linked artifacts"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineArtifactLinkIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("artifact_link", action, fixedFastPathFailure("I couldn't resolve the artifact linking request: "))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "batch",
			Route:    "text",
			Actions:  []string{"batch_work", "batch_configure", "review_policy", "batch_limit", "batch_status"},
			Triggers: []string{"show me progress"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineBatchIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("batch", action, fixedFastPathFailure("I couldn't resolve the batch request: "))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "workspace",
			Route:    "text",
			Actions:  []string{"switch_workspace", "focus_workspace", "clear_focus", "list_workspace_items", "list_workspaces", "create_workspace", "create_workspace_from_git", "rename_workspace", "delete_workspace", "show_workspace_details", "workspace_watch_start", "workspace_watch_stop", "workspace_watch_status", "show_busy_state"},
			Triggers: []string{"list workspaces"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineWorkspaceIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("workspace", action, fixedFastPathFailure("I couldn't resolve the workspace request: "))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "project",
			Route:    "text",
			Actions:  []string{"show_workspace_project"},
			Triggers: []string{"what project is this?"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineProjectFastIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("project", action, fixedFastPathFailure("I couldn't resolve the project request: "))
		},
	},
	{
		Spec: deterministicFastPathSpec{
			Name:     "runtime_control",
			Route:    "text",
			Actions:  []string{"toggle_silent", "toggle_live_dialogue", "cancel_work", "show_status"},
			Triggers: []string{"be quiet", "toggle live dialogue", "cancel work", "status?"},
		},
		Parse: func(text string, _ deterministicFastPathContext) *deterministicFastPathMatch {
			action := parseInlineRuntimeControlIntent(text)
			if action == nil {
				return nil
			}
			return fastPathSingleAction("runtime_control", action, fixedFastPathFailure("I couldn't resolve the runtime control request: "))
		},
	},
}

func parseInlineProjectFastIntent(text string) *SystemAction {
	switch normalizeItemCommandText(text) {
	case "what project is this", "what project is this?":
		return &SystemAction{Action: "show_workspace_project", Params: map[string]interface{}{}}
	default:
		return nil
	}
}

var deterministicFastPathUIControls = []deterministicFastPathSpec{
	{
		Name:     "ui_runtime_controls",
		Route:    "ui",
		Actions:  []string{"toggle_silent", "toggle_live_dialogue"},
		Triggers: []string{"system_action:toggle_silent", "system_action:toggle_live_dialogue"},
	},
	{
		Name:     "ui_push_to_talk",
		Route:    "ui",
		Triggers: []string{"ctrl_long_press", "ctrl_release"},
	},
}

func deterministicFastPathCatalog() []deterministicFastPathSpec {
	specs := make([]deterministicFastPathSpec, 0, len(deterministicFastPaths)+len(deterministicFastPathUIControls))
	for _, parser := range deterministicFastPaths {
		if strings.TrimSpace(parser.Spec.Name) == "" {
			continue
		}
		spec := parser.Spec
		spec.Actions = append([]string(nil), spec.Actions...)
		spec.Triggers = append([]string(nil), spec.Triggers...)
		specs = append(specs, spec)
	}
	for _, spec := range deterministicFastPathUIControls {
		copied := spec
		copied.Actions = append([]string(nil), copied.Actions...)
		copied.Triggers = append([]string(nil), copied.Triggers...)
		specs = append(specs, copied)
	}
	return specs
}

func fastPathSingleAction(name string, action *SystemAction, failure func(string, []*SystemAction, error) string) *deterministicFastPathMatch {
	if action == nil {
		return nil
	}
	return fastPathActionPlan(name, []*SystemAction{action}, failure)
}

func fastPathActionPlan(name string, actions []*SystemAction, failure func(string, []*SystemAction, error) string) *deterministicFastPathMatch {
	if len(actions) == 0 {
		return nil
	}
	return &deterministicFastPathMatch{
		Name:           name,
		Actions:        actions,
		FailureMessage: failure,
	}
}

func fixedFastPathFailure(prefix string) func(string, []*SystemAction, error) string {
	return func(_ string, _ []*SystemAction, err error) string {
		return prefix + err.Error()
	}
}

func itemFastPathFailure(userText string, enforced []*SystemAction, err error) string {
	action := ""
	copied := copySystemActions(enforced)
	if len(copied) == 1 {
		action = copied[0].Action
		if normalized := normalizeSystemActionForExecution(copied[0], userText); normalized != nil {
			action = normalized.Action
		}
	}
	if action == "" {
		action = "make_item"
	}
	return itemActionFailurePrefix(action) + err.Error()
}

func githubFastPathFailure(_ string, enforced []*SystemAction, err error) string {
	return githubIssueActionFailurePrefix(enforced) + err.Error()
}

func tryDeterministicFastPath(text string, ctx deterministicFastPathContext) *deterministicFastPathMatch {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	for _, parser := range deterministicFastPaths {
		if parser.Parse == nil {
			continue
		}
		if match := parser.Parse(trimmed, ctx); match != nil {
			if strings.TrimSpace(match.Name) == "" {
				match.Name = parser.Spec.Name
			}
			return match
		}
	}
	return nil
}

func (a *App) executeDeterministicFastPath(ctx context.Context, sessionID string, session store.ChatSession, userText string, match *deterministicFastPathMatch) (string, []map[string]interface{}, bool) {
	if match == nil {
		return "", nil, false
	}
	if match.TitledItem != nil {
		message, payload, err := a.executeTitledItemIntent(ctx, session, match.TitledItem)
		if err != nil {
			if match.FailureMessage != nil {
				return match.FailureMessage(userText, nil, err), nil, true
			}
			return err.Error(), nil, true
		}
		if payload == nil {
			return message, nil, true
		}
		return message, []map[string]interface{}{payload}, true
	}
	enforced := enforceRoutingPolicy(userText, match.Actions)
	if len(enforced) == 0 {
		return "", nil, false
	}
	message, payloads, err := a.executeSystemActionPlan(sessionID, session, userText, enforced)
	if err != nil {
		if match.FailureMessage != nil {
			return match.FailureMessage(userText, enforced, err), nil, true
		}
		return err.Error(), nil, true
	}
	return message, payloads, true
}
