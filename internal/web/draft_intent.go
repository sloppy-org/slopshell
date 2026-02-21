package web

import "strings"

type draftReplyIntent string

const (
	draftReplyIntentDictation draftReplyIntent = "dictation"
	draftReplyIntentPrompt    draftReplyIntent = "prompt"
)

type draftReplyIntentDecision struct {
	Intent          draftReplyIntent
	Reason          string
	FallbackApplied bool
	FallbackPolicy  draftReplyIntent
}

var draftIntentPromptPhrases = []string{
	"draft a reply",
	"write a reply",
	"write back",
	"reply with",
	"reply saying",
	"respond with",
	"make it",
	"keep it",
	"use a",
	"mention",
	"include",
	"ask for",
	"tell them",
	"tone",
	"concise",
	"formal",
	"friendly",
}

var draftIntentDictationPhrases = []string{
	"\n\n",
	"\nbest,\n",
	"\nthanks,\n",
	"\nregards,\n",
	"\nsincerely,\n",
}

var draftIntentDictationPrefixes = []string{
	"hi ",
	"hello ",
	"dear ",
	"hey ",
}

func classifyDraftReplyIntent(transcript string) draftReplyIntentDecision {
	trimmed := strings.TrimSpace(transcript)
	if trimmed == "" {
		return ambiguousDraftReplyIntentDecision("empty_transcript")
	}
	normalized := strings.ToLower(trimmed)
	promptScore := countDraftIntentPhraseHits(normalized, draftIntentPromptPhrases)
	dictationScore := countDraftIntentPhraseHits(normalized, draftIntentDictationPhrases)
	if hasDraftIntentPrefix(normalized, draftIntentDictationPrefixes) {
		dictationScore += 2
	}
	if strings.Count(trimmed, "\n") >= 2 {
		dictationScore++
	}
	if strings.HasSuffix(normalized, "best") || strings.HasSuffix(normalized, "thanks") || strings.HasSuffix(normalized, "regards") {
		dictationScore++
	}

	switch {
	case promptScore == 0 && dictationScore >= 2:
		return draftReplyIntentDecision{
			Intent:         draftReplyIntentDictation,
			Reason:         "dictation_signals",
			FallbackPolicy: draftReplyIntentPrompt,
		}
	case dictationScore == 0 && promptScore >= 1:
		return draftReplyIntentDecision{
			Intent:         draftReplyIntentPrompt,
			Reason:         "instruction_signals",
			FallbackPolicy: draftReplyIntentPrompt,
		}
	case dictationScore >= promptScore+2 && dictationScore >= 3:
		return draftReplyIntentDecision{
			Intent:         draftReplyIntentDictation,
			Reason:         "dictation_dominant",
			FallbackPolicy: draftReplyIntentPrompt,
		}
	case promptScore >= dictationScore+1 && promptScore >= 2:
		return draftReplyIntentDecision{
			Intent:         draftReplyIntentPrompt,
			Reason:         "instruction_dominant",
			FallbackPolicy: draftReplyIntentPrompt,
		}
	default:
		return ambiguousDraftReplyIntentDecision("ambiguous_signals")
	}
}

func ambiguousDraftReplyIntentDecision(reason string) draftReplyIntentDecision {
	return draftReplyIntentDecision{
		Intent:          draftReplyIntentPrompt,
		Reason:          reason,
		FallbackApplied: true,
		FallbackPolicy:  draftReplyIntentPrompt,
	}
}

func hasDraftIntentPrefix(text string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func countDraftIntentPhraseHits(text string, phrases []string) int {
	score := 0
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			score++
		}
	}
	return score
}
