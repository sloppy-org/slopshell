package web

import (
	"regexp"
	"strings"
)

var (
	itemDelegatePattern  = regexp.MustCompile(`(?i)^(?:delegate|assign|delegiere|übergib|uebergib|übergebe|uebergebe|übertrage|uebertrage)(?:\s+(?:this|it|das))?\s+(?:to|an)\s+(.+?)$`)
	delegateAskLeadWords = map[string]struct{}{
		"ask":   {},
		"tell":  {},
		"have":  {},
		"sag":   {},
		"frag":  {},
		"bitte": {},
	}
	delegateLetLeadWords = map[string]struct{}{
		"let":  {},
		"lass": {},
	}
	delegateSubjectLeadWords = map[string]struct{}{
		"soll":   {},
		"mach":   {},
		"mache":  {},
		"should": {},
		"do":     {},
		"can":    {},
		"could":  {},
		"kann":   {},
		"muss":   {},
	}
)

func normalizeDelegationCommandText(raw string) string {
	text := strings.ToLower(strings.TrimSpace(raw))
	replacer := strings.NewReplacer(
		"’", "'",
		"‘", "'",
		"ä", "ae",
		"ö", "oe",
		"ü", "ue",
		"ß", "ss",
	)
	text = replacer.Replace(text)
	text = strings.Trim(text, " \t\r\n.!?,:;\"'")
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func cleanActorReference(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.Trim(text, " \t\r\n.!?,:;")
	for _, suffix := range []string{" please", " thanks", " thank you", " bitte", " danke"} {
		if strings.HasSuffix(strings.ToLower(text), suffix) {
			text = strings.TrimSpace(text[:len(text)-len(suffix)])
		}
	}
	if normalized := normalizeKnownDelegationActorName(text); normalized != "" {
		return normalized
	}
	return strings.TrimSpace(text)
}

func normalizeKnownDelegationActorName(raw string) string {
	normalized := normalizeItemCommandText(raw)
	if normalized == "" {
		return ""
	}
	compact := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(normalized)
	switch compact {
	case "codex":
		return "Codex"
	case "gpt", "chatgpt", "gpt5", "gpt54", "gpt53", "gpt52", "gpt51":
		return "GPT"
	case "sloppy":
		return "Sloppy"
	default:
		return ""
	}
}

func matchKnownDelegationActorAlias(fields []string) (string, []string) {
	if len(fields) == 0 {
		return "", nil
	}
	if actor := normalizeKnownDelegationActorName(fields[0]); actor != "" {
		return actor, fields[1:]
	}
	if len(fields) >= 2 {
		if actor := normalizeKnownDelegationActorName(fields[0] + " " + fields[1]); actor != "" {
			return actor, fields[2:]
		}
	}
	return "", fields
}

func extractInlineDelegateActor(text string) string {
	trimmed := strings.TrimSpace(text)
	if match := itemDelegatePattern.FindStringSubmatch(trimmed); len(match) == 2 {
		return cleanActorReference(match[1])
	}
	normalized := normalizeDelegationCommandText(text)
	if normalized == "" {
		return ""
	}
	fields := strings.Fields(normalized)
	if len(fields) < 2 {
		return ""
	}
	if _, ok := delegateAskLeadWords[fields[0]]; ok {
		if actor, rest := matchKnownDelegationActorAlias(fields[1:]); actor != "" && len(rest) > 0 {
			return actor
		}
	}
	if _, ok := delegateLetLeadWords[fields[0]]; ok {
		if actor, rest := matchKnownDelegationActorAlias(fields[1:]); actor != "" && len(rest) > 0 {
			return actor
		}
	}
	if actor, rest := matchKnownDelegationActorAlias(fields); actor != "" && len(rest) > 0 {
		lead := strings.Trim(rest[0], ",")
		if _, ok := delegateSubjectLeadWords[lead]; ok {
			return actor
		}
	}
	return ""
}

func delegationActorLookupCandidates(name string) []string {
	primary := cleanActorReference(name)
	if primary == "" {
		return nil
	}
	candidates := make([]string, 0, 3)
	seen := map[string]struct{}{}
	add := func(value string) {
		clean := strings.TrimSpace(value)
		if clean == "" {
			return
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, clean)
	}
	add(primary)
	switch primary {
	case "GPT":
		add("Codex")
	case "Codex":
		add("GPT")
	}
	add(strings.TrimSpace(name))
	return candidates
}
