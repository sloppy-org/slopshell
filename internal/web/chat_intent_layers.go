package web

import "strings"

const (
	intentKindSystemCommand   = "system_command"
	intentKindCanonicalAction = "canonical_action"
	intentKindLocalAnswer     = "local_answer"
	intentKindDialogue        = "dialogue"

	canonicalActionOpenShow        = "open_show"
	canonicalActionAnnotateCapture = "annotate_capture"
	canonicalActionCompose         = "compose"
	canonicalActionBundleReview    = "bundle_review"
	canonicalActionDispatchExecute = "dispatch_execute"
	canonicalActionTrackItem       = "track_item"
	canonicalActionDelegateActor   = "delegate_actor"
)

var intentPromptSystemCommands = []string{
	"switch_project",
	"switch_workspace",
	"focus_workspace",
	"clear_focus",
	"list_workspace_items",
	"list_workspaces",
	"create_workspace",
	"create_workspace_from_git",
	"rename_workspace",
	"delete_workspace",
	"show_workspace_details",
	"workspace_watch_start",
	"workspace_watch_stop",
	"workspace_watch_status",
	"batch_work",
	"batch_configure",
	"review_policy",
	"batch_limit",
	"batch_status",
	"assign_workspace_project",
	"show_workspace_project",
	"create_project",
	"list_project_workspaces",
	"link_workspace_artifact",
	"list_linked_artifacts",
	"switch_model",
	"toggle_silent",
	"toggle_live_dialogue",
	"cancel_work",
	"show_status",
	"shell",
	"open_file_canvas",
	"show_calendar",
	"show_briefing",
	"sync_project",
	"sync_sources",
	"map_todoist_project",
	"sync_todoist",
	"create_todoist_task",
	"sync_evernote",
	"sync_bear",
	"promote_bear_checklist",
	"sync_zotero",
}

var legacyArtifactIntentNames = []string{
	"make_item",
	"delegate_item",
	"snooze_item",
	"split_items",
	"capture_idea",
	"refine_idea_note",
	"promote_idea",
	"apply_idea_promotion",
	"create_github_issue",
	"create_github_issue_split",
	"review_someday",
	"triage_someday",
	"promote_someday",
	"triage_item_by_title",
}

func normalizeCanonicalActionName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case canonicalActionOpenShow,
		canonicalActionAnnotateCapture,
		canonicalActionCompose,
		canonicalActionBundleReview,
		canonicalActionDispatchExecute,
		canonicalActionTrackItem,
		canonicalActionDelegateActor:
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func normalizeIntentResponseKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case intentKindSystemCommand:
		return intentKindSystemCommand
	case intentKindCanonicalAction:
		return intentKindCanonicalAction
	case intentKindLocalAnswer:
		return intentKindLocalAnswer
	case intentKindDialogue:
		return intentKindDialogue
	default:
		return ""
	}
}

func buildIntentLLMSystemPrompt() string {
	return buildIntentLLMSystemPromptForPolicy(LivePolicyDialogue)
}

func buildIntentLLMSystemPromptForPolicy(policy LivePolicy) string {
	prompt := `You are Tabura's local router. Output JSON only.
System commands: ` + strings.Join(intentPromptSystemCommands, ", ") + `.
Canonical artifact actions: ` + strings.Join(artifactTaxonomy.CanonicalActionOrder, ", ") + `.
Return exactly one of:
- {"kind":"system_command","action":"<system_command>", ...params}
- {"kind":"canonical_action","action":"<canonical_action>", ...params}
- {"kind":"local_answer","text":"<short reply>","confidence":"high|medium|low"}
- {"actions":[{"action":"shell",...},{"action":"open_file_canvas","path":"..."}]}
- {"kind":"dialogue"}
Optional top-level field: "ack":"<short acknowledgment>" for dialogue or command turns that may need a brief provisional reply while a richer response is still running.
Use {"kind":"dialogue"} unless the user clearly requests a system command or canonical artifact action.
Use {"kind":"local_answer"} for short complete replies you can answer from the provided runtime context without tools: greetings, acknowledgments, brief social turns, and simple workspace/status questions.
Local answers must stay within 1-3 sentences.
Use confidence="high" only when the answer is complete and correct.
Use confidence="medium" when a plausible short answer exists but a richer model should answer.
Use {"kind":"dialogue"} for anything requiring file/code inspection, internet/current information, multi-step reasoning, or longer-form writing.
Use canonical_action for artifact/item work and do not emit legacy artifact intents such as ` + strings.Join(legacyArtifactIntentNames, ", ") + `.
Composite operations must be expressed as a canonical action plus parameters or as a multi-step system-command plan.
For current-information requests (weather, web search, news, prices, schedules, latest/current updates), use {"kind":"dialogue"} and MUST NOT use shell.
For shell-like requests use {"kind":"system_command","action":"shell","command":"..."}.
For open/show/display file requests, end with {"action":"open_file_canvas","path":"..."} inside an actions plan.
If exact path is uncertain, use multi-step {"actions":[...]}: shell search first, then open_file_canvas with path="$last_shell_path".
For track_item include visible_after or count when relevant.
For annotate_capture or compose on idea notes include target="idea_note".
For bundle_review on someday work include target="someday" and operation.
For dispatch_execute issue filing include target="github_issue" and mode="split" when local items are also required.
Prefer case-insensitive filename search (for example -iname) and use single quotes inside JSON command strings.`
	if normalizeLivePolicy(policy.String()) == LivePolicyMeeting {
		prompt += `
Meeting mode: include an "addressed" boolean on every JSON response indicating whether the utterance is directed at Tabura.
If the user explicitly mentions "Tabura" or "assistant", set "addressed":true.
For meeting discussion not directed at Tabura, use {"addressed":false,"kind":"dialogue"}.
For addressed commands/plans, keep the same response shape and add "addressed":true at the top level.`
	}
	return strings.TrimSpace(prompt)
}

func translateCanonicalActionForExecution(action *SystemAction) *SystemAction {
	if action == nil {
		return nil
	}
	name := normalizeCanonicalActionName(action.Action)
	if name == "" {
		return action
	}
	params := map[string]interface{}{}
	for key, value := range action.Params {
		params[key] = value
	}
	switch name {
	case canonicalActionTrackItem:
		switch {
		case paramKeyPresent(params, "count"):
			return &SystemAction{Action: "split_items", Params: params}
		case strings.TrimSpace(systemActionVisibleAfter(params)) != "":
			return &SystemAction{Action: "snooze_item", Params: params}
		default:
			return &SystemAction{Action: "make_item", Params: params}
		}
	case canonicalActionDelegateActor:
		return &SystemAction{Action: "delegate_item", Params: params}
	case canonicalActionAnnotateCapture:
		target := strings.ToLower(strings.TrimSpace(systemActionStringParam(params, "target")))
		if target == "idea_note" || strings.TrimSpace(systemActionStringParam(params, "idea_text")) != "" {
			return &SystemAction{Action: "capture_idea", Params: params}
		}
		return nil
	case canonicalActionCompose:
		target := strings.ToLower(strings.TrimSpace(systemActionStringParam(params, "target")))
		if target == "idea_note" || strings.TrimSpace(systemActionStringParam(params, "kind")) != "" {
			return &SystemAction{Action: "refine_idea_note", Params: params}
		}
		return nil
	case canonicalActionBundleReview:
		target := strings.ToLower(strings.TrimSpace(systemActionStringParam(params, "target")))
		operation := strings.ToLower(strings.TrimSpace(systemActionStringParam(params, "operation")))
		if target != "someday" {
			return nil
		}
		switch operation {
		case "", "review":
			return &SystemAction{Action: "review_someday", Params: params}
		case "triage", "someday":
			return &SystemAction{Action: "triage_someday", Params: params}
		case "promote", "inbox", "back_to_inbox":
			return &SystemAction{Action: "promote_someday", Params: params}
		default:
			return nil
		}
	case canonicalActionDispatchExecute:
		target := strings.ToLower(strings.TrimSpace(systemActionStringParam(params, "target")))
		mode := strings.ToLower(strings.TrimSpace(systemActionStringParam(params, "mode")))
		switch target {
		case "github_issue", "github":
			if mode == "split" {
				return &SystemAction{Action: "create_github_issue_split", Params: params}
			}
			return &SystemAction{Action: "create_github_issue", Params: params}
		case "idea_promotion":
			if mode == "apply" {
				if promotionTarget := strings.TrimSpace(systemActionStringParam(params, "promotion_target")); promotionTarget != "" {
					params["target"] = promotionTarget
				}
				return &SystemAction{Action: "apply_idea_promotion", Params: params}
			}
			if promotionTarget := strings.TrimSpace(systemActionStringParam(params, "promotion_target")); promotionTarget != "" {
				params["target"] = promotionTarget
			}
			return &SystemAction{Action: "promote_idea", Params: params}
		case "print":
			return &SystemAction{Action: "print_item", Params: params}
		default:
			return nil
		}
	default:
		return nil
	}
}

func paramKeyPresent(params map[string]interface{}, key string) bool {
	if params == nil {
		return false
	}
	_, ok := params[key]
	return ok
}
