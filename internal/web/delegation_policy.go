package web

import (
	"strings"
	"unicode"

	"github.com/krystophny/tabura/internal/modelprofile"
)

const delegationPolicyVersion = "v2"

const (
	delegationDomainGeneral     = "general"
	delegationDomainCoding      = "coding"
	delegationDomainCurrentInfo = "current_info"
)

const (
	delegationComplexitySimple  = "simple"
	delegationComplexityComplex = "complex"
)

type delegationRoute struct {
	Domain        string
	Complexity    string
	Model         string
	Effort        string
	Reason        string
	ForceDelegate bool
	BlockShell    bool
}

func systemActionDelegateEffort(params map[string]interface{}) string {
	value := systemActionStringParam(params, "reasoning_effort")
	if value != "" && value != "<nil>" {
		return value
	}
	return ""
}

func normalizeDelegateEffort(modelAlias string, raw string) string {
	clean := strings.TrimSpace(strings.ToLower(raw))
	if clean == "" {
		return ""
	}
	return modelprofile.NormalizeReasoningEffort(modelAlias, clean)
}

func isCurrentInfoQuery(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	strongSignals := []string{
		"weather", "forecast", "temperature", "rain", "snow", "wind",
		"wetter", "vorhersage", "temperatur", "regen", "schnee",
		"web search", "search the web", "search web", "search online", "look up online",
		"google", "bing", "duckduckgo",
		"breaking news", "headlines", "news today", "latest news",
		"stock price", "crypto price", "exchange rate", "market price",
		"schedule today", "game schedule", "match schedule", "standings",
	}
	for _, token := range strongSignals {
		if strings.Contains(lower, token) {
			return true
		}
	}
	latestCompanions := []string{
		"news", "price", "weather", "forecast", "release", "version", "update", "today",
		"schedule", "score", "results", "market", "exchange rate",
	}
	if strings.Contains(lower, "latest") || strings.Contains(lower, "current") {
		for _, token := range latestCompanions {
			if strings.Contains(lower, token) {
				return true
			}
		}
	}
	return false
}

func isCodingQuery(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	codingSignals := []string{
		"code", "coding", "bug", "debug", "compile", "build", "test", "repo", "repository",
		"function", "api", "library", "framework", "pull request", "pr ", "commit",
		"refactor", "stack trace", "timeout", "exception", "fix",
		"go ", "golang", "python", "javascript", "typescript", "rust", "java", "c++",
	}
	for _, token := range codingSignals {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func isComplexQuery(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	complexSignals := []string{
		"analyze", "analysis", "compare", "tradeoff", "architecture", "design",
		"deep dive", "step by step", "comprehensive", "investigate", "root cause",
		"overhaul", "multi-step", "research", "detailed plan",
	}
	lower := strings.ToLower(trimmed)
	for _, token := range complexSignals {
		if strings.Contains(lower, token) {
			return true
		}
	}
	words := 0
	inWord := false
	for _, r := range trimmed {
		if unicode.IsSpace(r) {
			if inWord {
				words++
			}
			inWord = false
			continue
		}
		inWord = true
	}
	if inWord {
		words++
	}
	return words >= 28
}

func classifyDelegationRoute(text string) delegationRoute {
	coding := isCodingQuery(text)
	currentInfo := isCurrentInfoQuery(text)
	complex := isComplexQuery(text)

	domain := delegationDomainGeneral
	if coding {
		domain = delegationDomainCoding
	}
	if currentInfo {
		domain = delegationDomainCurrentInfo
	}

	complexity := delegationComplexitySimple
	if complex {
		complexity = delegationComplexityComplex
	}

	route := delegationRoute{
		Domain:        domain,
		Complexity:    complexity,
		ForceDelegate: currentInfo,
		BlockShell:    currentInfo,
	}

	switch {
	case complex && coding:
		route.Model = modelprofile.AliasCodex
		route.Effort = modelprofile.ReasoningHigh
	case complex && !coding:
		route.Model = modelprofile.AliasGPT
		route.Effort = modelprofile.ReasoningHigh
	case !complex && coding:
		route.Model = modelprofile.AliasCodex
		route.Effort = modelprofile.ReasoningLow
	case currentInfo:
		route.Model = modelprofile.AliasSpark
		route.Effort = modelprofile.ReasoningLow
	default:
		route.Model = modelprofile.AliasGPT
		route.Effort = modelprofile.ReasoningLow
	}

	if route.BlockShell {
		route.Reason = "current-info query requires delegated execution and blocks shell plans"
	} else {
		route.Reason = "delegation route selected by domain/complexity policy"
	}
	return route
}

func buildDelegateActionFromRoute(route delegationRoute, userText string) *SystemAction {
	model := normalizeDelegateModel(route.Model)
	if model == "" {
		model = modelprofile.AliasCodex
	}
	effort := normalizeDelegateEffort(model, route.Effort)
	params := map[string]interface{}{
		"model":            model,
		"task":             strings.TrimSpace(userText),
		"route_domain":     route.Domain,
		"route_complexity": route.Complexity,
		"route_reason":     route.Reason,
		"policy_version":   delegationPolicyVersion,
	}
	if effort != "" {
		params["reasoning_effort"] = effort
	}
	return &SystemAction{
		Action: "delegate",
		Params: params,
	}
}

func applyRouteToDelegateAction(action *SystemAction, route delegationRoute, userText string) *SystemAction {
	if action == nil {
		return nil
	}
	if action.Params == nil {
		action.Params = map[string]interface{}{}
	}
	model := normalizeDelegateModel(systemActionStringParam(action.Params, "model"))
	if model == "" {
		model = normalizeDelegateModel(route.Model)
		if model == "" {
			model = modelprofile.AliasCodex
		}
	}
	action.Params["model"] = model

	if strings.TrimSpace(systemActionDelegateTask(action.Params)) == "" {
		action.Params["task"] = strings.TrimSpace(userText)
	}

	effort := systemActionDelegateEffort(action.Params)
	if strings.TrimSpace(effort) == "" {
		effort = route.Effort
	}
	effort = normalizeDelegateEffort(model, effort)
	if strings.TrimSpace(effort) != "" {
		action.Params["reasoning_effort"] = effort
	}

	action.Params["route_domain"] = route.Domain
	action.Params["route_complexity"] = route.Complexity
	action.Params["route_reason"] = route.Reason
	action.Params["policy_version"] = delegationPolicyVersion
	return action
}

func enforceDelegationPolicy(userText string, actions []*SystemAction) []*SystemAction {
	route := classifyDelegationRoute(userText)
	if len(actions) == 0 {
		if route.ForceDelegate {
			return []*SystemAction{buildDelegateActionFromRoute(route, userText)}
		}
		return nil
	}

	out := make([]*SystemAction, 0, len(actions)+1)
	hasDelegate := false
	for _, action := range actions {
		if action == nil {
			continue
		}
		normalized := normalizeSystemActionForExecution(action, userText)
		if normalized == nil {
			continue
		}
		if route.BlockShell && strings.EqualFold(strings.TrimSpace(normalized.Action), "shell") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(normalized.Action), "delegate") {
			hasDelegate = true
			out = append(out, applyRouteToDelegateAction(normalized, route, userText))
			continue
		}
		out = append(out, normalized)
	}

	if route.ForceDelegate && !hasDelegate {
		out = append([]*SystemAction{buildDelegateActionFromRoute(route, userText)}, out...)
	}
	return out
}
