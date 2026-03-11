package web

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	confirmationTTLText = "next_message"
	maxDangerSummaryLen = 140
)

type pendingActionConfirmation struct {
	Token           string
	CreatedAt       time.Time
	UserText        string
	Summary         string
	Kind            string
	CanonicalAction string
	SystemAction    string
	Actions         []*SystemAction
}

var (
	destructiveShellPattern = regexp.MustCompile(`(?i)(^|[\s;&|])(rm\s+-|rm\s+--|rmdir\s|mkfs\.|dd\s+.*\sof=|shutdown\b|reboot\b|poweroff\b|halt\b|git\s+reset\s+--hard\b|git\s+clean\s+-[a-z]*f[a-z]*\b|git\s+checkout\s+--\b|git\s+restore\s+--source\b|git\s+push\s+--force\b|chmod\s+-r\b|chmod\s+-R\b|chown\s+-R\b|truncate\s+-s\s+0\b)`) //nolint:lll
	clobberRedirectPattern  = regexp.MustCompile(`(^|[;|&])\s*[^\n]*[^>]\s>\s*[^>].*`)
)

func parseBoolString(raw string, defaultValue bool) bool {
	clean := strings.TrimSpace(strings.ToLower(raw))
	switch clean {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	default:
		return defaultValue
	}
}

func (a *App) yoloModeEnabled() bool {
	if a == nil || a.store == nil {
		return false
	}
	value, err := a.store.AppState(appStateYoloModeKey)
	if err != nil {
		return false
	}
	return parseBoolString(value, false)
}

func (a *App) setYoloModeEnabled(enabled bool) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("store is unavailable")
	}
	value := "false"
	if enabled {
		value = "true"
	}
	return a.store.SetAppState(appStateYoloModeKey, value)
}

func (a *App) disclaimerAckVersion() string {
	if a == nil || a.store == nil {
		return ""
	}
	value, err := a.store.AppState(appStateDisclaimerAckKey)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func (a *App) disclaimerAckRequired() bool {
	return !strings.EqualFold(strings.TrimSpace(a.disclaimerAckVersion()), disclaimerVersionCurrent)
}

func (a *App) setDisclaimerAckVersion(version string) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("store is unavailable")
	}
	cleanVersion := strings.TrimSpace(version)
	if cleanVersion == "" {
		cleanVersion = disclaimerVersionCurrent
	}
	if err := a.store.SetAppState(appStateDisclaimerAckKey, cleanVersion); err != nil {
		return err
	}
	return a.store.SetAppState(appStateDisclaimerAckAtKey, time.Now().UTC().Format(time.RFC3339Nano))
}

func copySystemActions(actions []*SystemAction) []*SystemAction {
	if len(actions) == 0 {
		return nil
	}
	cloned := make([]*SystemAction, 0, len(actions))
	for _, action := range actions {
		if action == nil {
			continue
		}
		next := &SystemAction{Action: action.Action, Params: map[string]interface{}{}}
		for key, value := range action.Params {
			next.Params[key] = value
		}
		cloned = append(cloned, next)
	}
	return cloned
}

func (a *App) setPendingActionConfirmation(sessionID string, pending *pendingActionConfirmation) {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	a.confirmMu.Lock()
	defer a.confirmMu.Unlock()
	if pending == nil {
		delete(a.pendingConfirmations, sessionID)
		return
	}
	a.pendingConfirmations[sessionID] = pending
}

func (a *App) popPendingActionConfirmation(sessionID string) *pendingActionConfirmation {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	a.confirmMu.Lock()
	defer a.confirmMu.Unlock()
	pending := a.pendingConfirmations[sessionID]
	delete(a.pendingConfirmations, sessionID)
	return pending
}

func isExplicitDangerConfirm(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	clean = strings.Trim(clean, " .,!?:;\"'`")
	switch clean {
	case "confirm", "yes", "yes confirm", "i confirm", "do it", "run it", "proceed", "go ahead", "execute":
		return true
	default:
		return false
	}
}

func isExplicitDangerDecline(text string) bool {
	clean := strings.ToLower(strings.TrimSpace(text))
	clean = strings.Trim(clean, " .,!?:;\"'`")
	switch clean {
	case "no", "cancel", "stop", "no thanks", "don't", "dont", "abort":
		return true
	default:
		return false
	}
}

func dangerSummaryForCommand(command string) string {
	clean := strings.TrimSpace(command)
	if clean == "" {
		return "destructive shell action"
	}
	if len(clean) <= maxDangerSummaryLen {
		return clean
	}
	return clean[:maxDangerSummaryLen] + "..."
}

func isDestructiveShellCommand(command string) bool {
	clean := strings.TrimSpace(command)
	if clean == "" {
		return false
	}
	lower := strings.ToLower(clean)
	if strings.Contains(lower, " > ") || strings.HasPrefix(lower, ">") || clobberRedirectPattern.MatchString(clean) {
		if !strings.Contains(lower, ">>") {
			return true
		}
	}
	return destructiveShellPattern.MatchString(clean)
}

func firstDestructiveShellCommand(actions []*SystemAction) string {
	for _, action := range actions {
		if action == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(action.Action), "shell") {
			continue
		}
		cmd := strings.TrimSpace(systemActionShellCommand(action.Params))
		if cmd == "" {
			continue
		}
		if isDestructiveShellCommand(cmd) {
			return cmd
		}
	}
	return ""
}

type artifactConfirmationSpec struct {
	Summary         string
	CanonicalAction string
	SystemAction    string
}

func artifactConfirmationForPlan(actions []*SystemAction) *artifactConfirmationSpec {
	if len(actions) == 0 {
		return nil
	}
	splitItems := planContainsAction(actions, "split_items")
	for _, action := range actions {
		if action == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(action.Action)) {
		case "refine_idea_note":
			heading := strings.TrimSpace(ideaRefinementHeading(systemActionStringParam(action.Params, "kind")))
			summary := "update the active idea note"
			if heading != "" {
				summary = fmt.Sprintf("update the active idea note with %s", heading)
			}
			return &artifactConfirmationSpec{
				Summary:         summary,
				CanonicalAction: "compose",
				SystemAction:    action.Action,
			}
		case "delegate_item":
			actor := strings.TrimSpace(systemActionActorName(action.Params))
			summary := "delegate this work"
			if actor != "" {
				summary = fmt.Sprintf("delegate this work to %s", actor)
			}
			return &artifactConfirmationSpec{
				Summary:         summary,
				CanonicalAction: "delegate_actor",
				SystemAction:    action.Action,
			}
		case "create_github_issue", "create_github_issue_split":
			summary := "create a GitHub issue from this conversation"
			if splitItems {
				summary = "create local items and a GitHub issue from this conversation"
			}
			return &artifactConfirmationSpec{
				Summary:         summary,
				CanonicalAction: "dispatch_execute",
				SystemAction:    action.Action,
			}
		}
	}
	return nil
}

func pendingConfirmationCanceledMessage(kind string) string {
	if strings.EqualFold(strings.TrimSpace(kind), "artifact") {
		return "Canceled pending artifact action."
	}
	return "Canceled dangerous action."
}

func (a *App) guardDangerousSystemActionPlan(sessionID, userText string, actions []*SystemAction) (string, []map[string]interface{}, bool) {
	if len(actions) == 0 {
		return "", nil, false
	}
	if a.yoloModeEnabled() {
		return "", nil, false
	}
	dangerousCommand := firstDestructiveShellCommand(actions)
	if strings.TrimSpace(dangerousCommand) == "" {
		return "", nil, false
	}
	token := randomToken()
	summary := dangerSummaryForCommand(dangerousCommand)
	a.setPendingActionConfirmation(sessionID, &pendingActionConfirmation{
		Token:     token,
		CreatedAt: time.Now().UTC(),
		UserText:  strings.TrimSpace(userText),
		Summary:   summary,
		Kind:      "dangerous",
		Actions:   copySystemActions(actions),
	})
	message := "Destructive action blocked. Reply `confirm` in your next message to run:\n\n" + summary
	payload := map[string]interface{}{
		"type":              "confirmation_required",
		"token":             token,
		"summary":           summary,
		"expires":           confirmationTTLText,
		"action":            "shell",
		"confirmation_kind": "dangerous",
		"yolo_mode":         false,
	}
	return message, []map[string]interface{}{payload}, true
}

func (a *App) guardArtifactSystemActionPlan(sessionID, userText string, actions []*SystemAction) (string, []map[string]interface{}, bool) {
	spec := artifactConfirmationForPlan(actions)
	if spec == nil {
		return "", nil, false
	}
	token := randomToken()
	a.setPendingActionConfirmation(sessionID, &pendingActionConfirmation{
		Token:           token,
		CreatedAt:       time.Now().UTC(),
		UserText:        strings.TrimSpace(userText),
		Summary:         spec.Summary,
		Kind:            "artifact",
		CanonicalAction: spec.CanonicalAction,
		SystemAction:    spec.SystemAction,
		Actions:         copySystemActions(actions),
	})
	message := "Artifact action paused. Reply `confirm` in your next message to continue:\n\n" + spec.Summary
	payload := map[string]interface{}{
		"type":              "confirmation_required",
		"token":             token,
		"summary":           spec.Summary,
		"expires":           confirmationTTLText,
		"action":            "artifact",
		"confirmation_kind": "artifact",
		"canonical_action":  spec.CanonicalAction,
		"system_action":     spec.SystemAction,
	}
	return message, []map[string]interface{}{payload}, true
}
