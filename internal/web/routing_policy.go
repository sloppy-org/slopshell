package web

import (
	"regexp"
	"strings"

	"github.com/krystophny/tabura/internal/modelprofile"
)

var explicitGPTTurnPattern = regexp.MustCompile(`(?i)\b(?:use|delegate|ask|let|have|run|solve|handle|do|switch)\b.{0,24}\b(?:gpt(?:[- ]?5(?:\.\d+)?)?)\b|\bwith\s+gpt(?:[- ]?5(?:\.\d+)?)?\b`)

func explicitTurnModelAlias(text string) string {
	if explicitGPTTurnPattern.MatchString(strings.TrimSpace(text)) {
		return modelprofile.AliasGPT
	}
	return ""
}

func routeProfileForRouting(requestedAlias string, fallback appServerModelProfile, sparkEffort string) appServerModelProfile {
	alias := modelprofile.ResolveAlias(requestedAlias, modelprofile.AliasSpark)
	if alias == "" {
		alias = modelprofile.AliasSpark
	}
	model := modelprofile.ModelForAlias(alias)
	if model == "" {
		model = modelprofile.ModelForAlias(modelprofile.AliasSpark)
	}
	effortInput := ""
	if effortInput == "" && alias == modelprofile.AliasSpark {
		effortInput = strings.TrimSpace(sparkEffort)
	}
	effort := modelprofile.NormalizeReasoningEffort(alias, effortInput)
	if effort == "" {
		effort = strings.TrimSpace(modelprofile.MainThreadReasoningEffort(alias))
	}
	return appServerModelProfile{
		Alias:        alias,
		Model:        model,
		ThreadParams: fallback.ThreadParams,
		TurnParams:   modelprofile.MainThreadReasoningParamsForEffort(alias, effort),
	}
}

func enforceRoutingPolicy(userText string, actions []*SystemAction) []*SystemAction {
	if len(actions) == 0 {
		return nil
	}
	out := make([]*SystemAction, 0, len(actions))
	for _, action := range actions {
		if normalized := normalizeSystemActionForExecution(action, userText); normalized != nil {
			out = append(out, normalized)
		}
	}
	return out
}
