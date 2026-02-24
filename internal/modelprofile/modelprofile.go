package modelprofile

import "strings"

const (
	AliasSpark = "spark"
	AliasCodex = "codex"
	AliasGPT   = "gpt"

	ModelSpark = "gpt-5.3-codex-spark"
	ModelCodex = "gpt-5.3-codex"
	ModelGPT   = "gpt-5.2"

	ReasoningLow    = "low"
	ReasoningMedium = "medium"
	ReasoningHigh   = "high"
)

var aliasToModel = map[string]string{
	AliasSpark: ModelSpark,
	AliasCodex: ModelCodex,
	AliasGPT:   ModelGPT,
}

var modelToAlias = map[string]string{
	strings.ToLower(ModelSpark): AliasSpark,
	strings.ToLower(ModelCodex): AliasCodex,
	strings.ToLower(ModelGPT):   AliasGPT,
}

func SupportedAliases() []string {
	return []string{AliasCodex, AliasGPT, AliasSpark}
}

func SupportedModels() []string {
	return []string{ModelSpark, ModelCodex, ModelGPT}
}

func NormalizeAlias(raw string) string {
	key := strings.TrimSpace(strings.ToLower(raw))
	if _, ok := aliasToModel[key]; ok {
		return key
	}
	return ""
}

func AliasForModel(raw string) string {
	if alias := NormalizeAlias(raw); alias != "" {
		return alias
	}
	key := strings.TrimSpace(strings.ToLower(raw))
	if key == "" {
		return ""
	}
	if alias, ok := modelToAlias[key]; ok {
		return alias
	}
	return ""
}

func ResolveAlias(raw, fallback string) string {
	if alias := AliasForModel(raw); alias != "" {
		return alias
	}
	if alias := AliasForModel(fallback); alias != "" {
		return alias
	}
	return ""
}

func ModelForAlias(alias string) string {
	clean := NormalizeAlias(alias)
	if clean == "" {
		return ""
	}
	return aliasToModel[clean]
}

func ResolveModel(raw, fallbackAlias string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if model := ModelForAlias(fallbackAlias); model != "" {
			return model
		}
		return ""
	}
	if model := ModelForAlias(trimmed); model != "" {
		return model
	}
	if alias := AliasForModel(trimmed); alias != "" {
		return aliasToModel[alias]
	}
	return trimmed
}

func MainThreadReasoningEffort(alias string) string {
	switch NormalizeAlias(alias) {
	case AliasSpark:
		return ReasoningLow
	case AliasCodex, AliasGPT:
		return ReasoningHigh
	default:
		return ""
	}
}

func ReasoningParamsForEffort(effort string) map[string]interface{} {
	switch strings.TrimSpace(strings.ToLower(effort)) {
	case ReasoningLow, ReasoningMedium, ReasoningHigh:
		return map[string]interface{}{"model_reasoning_effort": strings.TrimSpace(strings.ToLower(effort))}
	default:
		return nil
	}
}

func MainThreadReasoningParams(alias string) map[string]interface{} {
	return ReasoningParamsForEffort(MainThreadReasoningEffort(alias))
}

func DelegateReasoningParams(model string) map[string]interface{} {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return nil
	}
	if alias := AliasForModel(trimmed); alias != "" {
		if alias == AliasSpark {
			return nil
		}
		return ReasoningParamsForEffort(ReasoningHigh)
	}
	if strings.Contains(strings.ToLower(trimmed), "spark") {
		return nil
	}
	return ReasoningParamsForEffort(ReasoningHigh)
}
