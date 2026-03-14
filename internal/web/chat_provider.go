package web

import (
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

const (
	assistantProviderLocal  = "local"
	assistantProviderFast   = "fast"
	assistantProviderOpenAI = "openai"
	assistantProviderSpark  = "spark"
	assistantProviderGPT    = "gpt"
)

type assistantResponseMetadata struct {
	Provider        string
	ProviderModel   string
	ProviderLatency int
}

func newAssistantResponseMetadata(provider, model string, latency time.Duration) assistantResponseMetadata {
	latencyMS := int(latency / time.Millisecond)
	if latencyMS < 0 {
		latencyMS = 0
	}
	return assistantResponseMetadata{
		Provider:        normalizeAssistantProvider(provider),
		ProviderModel:   strings.TrimSpace(model),
		ProviderLatency: latencyMS,
	}
}

func normalizeAssistantProvider(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case assistantProviderLocal:
		return assistantProviderLocal
	case assistantProviderFast:
		return assistantProviderFast
	case assistantProviderOpenAI:
		return assistantProviderOpenAI
	case assistantProviderSpark:
		return assistantProviderSpark
	case assistantProviderGPT:
		return assistantProviderGPT
	default:
		return ""
	}
}

func assistantProviderDisplayLabel(provider, model string) string {
	switch assistantProviderBadgeKey(provider, model) {
	case assistantProviderLocal:
		return "Local"
	case assistantProviderFast:
		return "Fast"
	case assistantProviderSpark:
		return "Spark"
	case assistantProviderGPT:
		return "GPT"
	case assistantProviderOpenAI:
		return "OpenAI"
	default:
		return "Assistant"
	}
}

func assistantProviderBadgeKey(provider, model string) string {
	normalizedProvider := normalizeAssistantProvider(provider)
	switch normalizedProvider {
	case assistantProviderSpark, assistantProviderGPT, assistantProviderFast:
		return normalizedProvider
	case assistantProviderLocal:
		if inferred := inferLocalAssistantProvider(model); inferred != "" {
			return inferred
		}
		return assistantProviderLocal
	case assistantProviderOpenAI:
		if alias := modelprofile.ResolveAlias(model, ""); alias != "" {
			return alias
		}
		return assistantProviderOpenAI
	case "":
		if alias := modelprofile.ResolveAlias(model, ""); alias != "" {
			return alias
		}
		if inferred := inferLocalAssistantProvider(model); inferred != "" {
			return inferred
		}
		return ""
	default:
		return normalizedProvider
	}
}

func inferLocalAssistantProvider(model string) string {
	lower := strings.ToLower(strings.TrimSpace(model))
	if lower == "" {
		return assistantProviderFast
	}
	switch {
	case strings.Contains(lower, "fast"),
		strings.Contains(lower, "9b"),
		strings.Contains(lower, "4b"),
		strings.Contains(lower, "mini"),
		strings.Contains(lower, "small"):
		return assistantProviderFast
	default:
		return assistantProviderLocal
	}
}

func (m assistantResponseMetadata) storeOptions() []store.ChatMessageOption {
	return []store.ChatMessageOption{
		store.WithProviderMetadata(m.Provider, m.ProviderModel, m.ProviderLatency),
	}
}

func (m assistantResponseMetadata) applyToPayload(payload map[string]interface{}) {
	payload["provider"] = m.Provider
	payload["provider_label"] = assistantProviderDisplayLabel(m.Provider, m.ProviderModel)
	payload["provider_model"] = m.ProviderModel
	payload["provider_latency_ms"] = m.ProviderLatency
}

func providerForAppServerProfile(profile appServerModelProfile) string {
	switch modelprofile.ResolveAlias(profile.Alias, profile.Model) {
	case modelprofile.AliasGPT:
		return assistantProviderGPT
	case modelprofile.AliasSpark:
		return assistantProviderSpark
	default:
		return ""
	}
}

func (a *App) localAssistantProvider() string {
	if a == nil {
		return assistantProviderFast
	}
	return inferLocalAssistantProvider(a.localAssistantModelLabel())
}

func (a *App) localAssistantModelLabel() string {
	if a == nil {
		return DefaultIntentLLMProfile
	}
	if profile := strings.TrimSpace(a.intentLLMProfile); profile != "" {
		return profile
	}
	if model := strings.TrimSpace(a.localIntentLLMModel()); model != "" {
		return model
	}
	return DefaultIntentLLMProfile
}
