package web

import "github.com/krystophny/tabura/internal/modelprofile"

func localAssistantThinkingEnabled(req *assistantTurnRequest) bool {
	if req == nil {
		return false
	}
	effort := modelprofile.NormalizeReasoningEffort(modelprofile.AliasLocal, req.reasoningEffort)
	return effort != "" && effort != modelprofile.ReasoningNone
}

func localAssistantReasoningHint(req *assistantTurnRequest) string {
	if !localAssistantThinkingEnabled(req) {
		return ""
	}
	switch modelprofile.NormalizeReasoningEffort(modelprofile.AliasLocal, req.reasoningEffort) {
	case modelprofile.ReasoningLow:
		return "Think briefly before answering."
	case modelprofile.ReasoningHigh:
		return "Think carefully and thoroughly before answering."
	default:
		return "Think before answering."
	}
}
