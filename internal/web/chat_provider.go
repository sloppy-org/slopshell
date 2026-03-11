package web

import (
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

const (
	assistantProviderLocal    = "local"
	assistantProviderCerebras = "cerebras"
	assistantProviderGoogle   = "google"
	assistantProviderOpenAI   = "openai"
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
	case assistantProviderCerebras:
		return assistantProviderCerebras
	case assistantProviderGoogle:
		return assistantProviderGoogle
	case assistantProviderOpenAI:
		return assistantProviderOpenAI
	default:
		return ""
	}
}

func assistantProviderDisplayLabel(provider string) string {
	switch normalizeAssistantProvider(provider) {
	case assistantProviderLocal:
		return "Local"
	case assistantProviderCerebras:
		return "Cerebras"
	case assistantProviderGoogle:
		return "Google"
	case assistantProviderOpenAI:
		return "OpenAI"
	default:
		return "Assistant"
	}
}

func (m assistantResponseMetadata) storeOptions() []store.ChatMessageOption {
	return []store.ChatMessageOption{
		store.WithProviderMetadata(m.Provider, m.ProviderModel, m.ProviderLatency),
	}
}

func (m assistantResponseMetadata) applyToPayload(payload map[string]interface{}) {
	payload["provider"] = m.Provider
	payload["provider_label"] = assistantProviderDisplayLabel(m.Provider)
	payload["provider_model"] = m.ProviderModel
	payload["provider_latency_ms"] = m.ProviderLatency
}

func providerForAppServerProfile(profile appServerModelProfile) string {
	switch modelprofile.ResolveAlias(profile.Alias, profile.Model) {
	case modelprofile.AliasCodex, modelprofile.AliasGPT, modelprofile.AliasSpark:
		return assistantProviderOpenAI
	default:
		return ""
	}
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
