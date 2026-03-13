package web

import (
	"strings"
	"unicode"

	"github.com/krystophny/tabura/internal/modelprofile"
)

const (
	routingDomainGeneral     = "general"
	routingDomainCoding      = "coding"
	routingDomainCurrentInfo = "current_info"
)

const (
	routingComplexitySimple  = "simple"
	routingComplexityComplex = "complex"
)

type routingRoute struct {
	Domain     string
	Complexity string
	Model      string
	Effort     string
	Reason     string
	BlockShell bool
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

func classifyRoutingRoute(text string) routingRoute {
	coding := isCodingQuery(text)
	currentInfo := isCurrentInfoQuery(text)
	complex := isComplexQuery(text)

	domain := routingDomainGeneral
	if coding {
		domain = routingDomainCoding
	}
	if currentInfo {
		domain = routingDomainCurrentInfo
	}

	complexity := routingComplexitySimple
	if complex {
		complexity = routingComplexityComplex
	}

	route := routingRoute{
		Domain:     domain,
		Complexity: complexity,
		BlockShell: currentInfo,
		Reason:     "general query keeps configured project model",
	}

	switch {
	case currentInfo:
		route.Model = modelprofile.AliasCodex
		route.Effort = modelprofile.ReasoningHigh
		route.Reason = "current-info query routed to codex high; shell actions blocked"
	case coding || complex:
		route.Model = modelprofile.AliasCodex
		route.Effort = modelprofile.ReasoningHigh
		route.Reason = "coding or complex query routed to codex high"
	}
	return route
}

func routeProfileForRouting(route routingRoute, fallback appServerModelProfile, sparkEffort string) appServerModelProfile {
	alias := modelprofile.ResolveAlias(route.Model, fallback.Alias)
	if alias == "" {
		alias = modelprofile.AliasSpark
	}
	model := modelprofile.ModelForAlias(alias)
	if model == "" {
		model = strings.TrimSpace(fallback.Model)
	}
	if model == "" {
		model = modelprofile.ModelForAlias(modelprofile.AliasSpark)
	}
	effortInput := strings.TrimSpace(route.Effort)
	if effortInput == "" && alias == modelprofile.AliasSpark {
		effortInput = strings.TrimSpace(sparkEffort)
	}
	effort := modelprofile.NormalizeReasoningEffort(alias, effortInput)
	if effort == "" {
		effort = modelprofile.NormalizeReasoningEffort(alias, modelprofile.MainThreadReasoningEffort(alias))
	}
	turnParams := modelprofile.MainThreadReasoningParamsForEffort(alias, effort)
	return appServerModelProfile{
		Alias:        alias,
		Model:        model,
		ThreadParams: fallback.ThreadParams,
		TurnParams:   turnParams,
	}
}

var currentInfoBlockedActions = map[string]bool{
	"shell":         true,
	"make_item":     true,
	"snooze_item":   true,
	"delegate_item": true,
	"split_items":   true,
	"capture_idea":  true,
}

func enforceRoutingPolicy(userText string, actions []*SystemAction) []*SystemAction {
	route := classifyRoutingRoute(userText)
	if len(actions) == 0 {
		return nil
	}

	out := make([]*SystemAction, 0, len(actions))
	for _, action := range actions {
		if action == nil {
			continue
		}
		normalized := normalizeSystemActionForExecution(action, userText)
		if normalized == nil {
			continue
		}
		actionName := strings.ToLower(strings.TrimSpace(normalized.Action))
		if route.BlockShell && currentInfoBlockedActions[actionName] {
			continue
		}
		out = append(out, normalized)
	}
	return out
}
