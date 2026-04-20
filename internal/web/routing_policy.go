package web

import (
	"regexp"
	"strings"

	"github.com/sloppy-org/slopshell/internal/modelprofile"
)

type turnRoutingDirectives struct {
	ModelAlias         string
	ModelAliasExplicit bool
	ReasoningEffort    string
	DetailRequested    bool
	PromptText         string
	DirectiveApplied   bool
}

type directivePattern struct {
	regex  *regexp.Regexp
	alias  string
	effort string
}

var verbosityDetailPattern = regexp.MustCompile(`(?i)\b(?:` +
	`in\s+detail|explain\s+(?:in\s+)?detail|long\s+answer|be\s+thorough|` +
	`elaborate|full\s+explanation|verbose|go\s+deep|all\s+the\s+details|` +
	`ausführlich|ausfuehrlich|im\s+detail|in\s+allen?\s+details|` +
	`sag(?:\s+mir)?\s+(?:es\s+)?(?:im|in)\s+detail|` +
	`erklär(?:\s+(?:es|mir))?\s+(?:im|in)\s+detail|` +
	`erklaer(?:\s+(?:es|mir))?\s+(?:im|in)\s+detail|` +
	`ganz\s+genau|in\s+aller\s+ausführlichkeit|in\s+aller\s+ausfuehrlichkeit` +
	`)\b`)

var turnDirectivePatterns = []directivePattern{
	{
		regex:  regexp.MustCompile(`(?i)\bthink\s+quick(?:ly)?\b|\bdenk\s+kurz\b|\bdenke\s+kurz\b|\büberleg\s+kurz\b|\bueberleg\s+kurz\b`),
		effort: modelprofile.ReasoningLow,
	},
	{
		regex:  regexp.MustCompile(`(?i)\bthink\s+hard\b|\bthink\s+deep(?:ly)?\b|\bdenk\s+tief(?:\s+nach)?\b|\bdenke\s+tief(?:\s+nach)?\b|\büberleg\s+tief\b|\bueberleg\s+tief\b`),
		effort: modelprofile.ReasoningHigh,
	},
	{
		regex:  regexp.MustCompile(`(?i)\bthink(?:\s+a\s+bit)?\b|\bdenk\s+nach\b|\bdenke\b|\büberleg\b|\bueberleg\b`),
		effort: modelprofile.ReasoningMedium,
	},
	{
		regex: regexp.MustCompile(`(?i)\b(?:use|ask|let|have|run|solve|handle|do|switch(?:\s+to)?|delegate|frag|lass|benutz(?:e)?|verwende)\b(?:[\s,:-]+(?:the|das|den|die|dem|mal|bitte|doch|einfach|modell|model|to|mit))*[\s,:-]+(?:spark|codex)\b|\b(?:with|mit)\s+(?:spark|codex)\b`),
		alias: modelprofile.AliasSpark,
	},
	{
		regex: regexp.MustCompile(`(?i)\b(?:use|ask|let|have|run|solve|handle|do|switch(?:\s+to)?|delegate|frag|lass|benutz(?:e)?|verwende)\b(?:[\s,:-]+(?:the|das|den|die|dem|mal|bitte|doch|einfach|modell|model|to|mit))*[\s,:-]+gpt\b|\b(?:with|mit)\s+gpt\b`),
		alias: modelprofile.AliasGPT,
	},
	{
		regex: regexp.MustCompile(`(?i)\b(?:use|ask|let|have|run|solve|handle|do|switch(?:\s+to)?|delegate|frag|lass|benutz(?:e)?|verwende)\b(?:[\s,:-]+(?:the|das|den|die|dem|mal|bitte|doch|einfach|modell|model|to|mit))*[\s,:-]+mini\b|\b(?:with|mit)\s+mini\b`),
		alias: modelprofile.AliasMini,
	},
}

func parseTurnRoutingDirectives(text string) turnRoutingDirectives {
	original := strings.TrimSpace(text)
	if original == "" {
		return turnRoutingDirectives{}
	}
	if strings.HasPrefix(original, "/") {
		return turnRoutingDirectives{PromptText: original}
	}
	working := original
	directives := turnRoutingDirectives{PromptText: original}
	for _, pattern := range turnDirectivePatterns {
		matches := pattern.regex.FindAllStringIndex(working, -1)
		if len(matches) == 0 {
			continue
		}
		directives.DirectiveApplied = true
		if pattern.alias != "" {
			directives.ModelAlias = pattern.alias
			directives.ModelAliasExplicit = true
		}
		if pattern.effort != "" {
			directives.ReasoningEffort = pattern.effort
		}
		working = pattern.regex.ReplaceAllString(working, " ")
	}
	if verbosityDetailPattern.MatchString(original) {
		directives.DetailRequested = true
		directives.DirectiveApplied = true
	}
	cleaned := strings.Join(strings.Fields(working), " ")
	cleaned = strings.TrimSpace(strings.Trim(cleaned, ",:;-"))
	if cleaned == "" {
		cleaned = original
	}
	directives.PromptText = cleaned
	return directives
}

func routeProfileForRouting(requestedAlias string, fallback appServerModelProfile, sparkEffort string, reasoningOverride string) appServerModelProfile {
	alias := modelprofile.ResolveAlias(requestedAlias, fallback.Alias)
	if alias == "" {
		alias = modelprofile.AliasLocal
	}
	model := modelprofile.ModelForAlias(alias)
	if alias == modelprofile.AliasLocal {
		model = modelprofile.ModelLocal
	}
	if strings.TrimSpace(model) == "" {
		model = strings.TrimSpace(fallback.Model)
	}
	effortInput := strings.TrimSpace(reasoningOverride)
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
