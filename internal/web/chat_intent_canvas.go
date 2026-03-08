package web

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func firstShellPathFromOutput(output string) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		candidate := strings.TrimSpace(line)
		if candidate == "" || candidate == "(no output)" {
			continue
		}
		candidate = strings.TrimPrefix(candidate, "./")
		if candidate == "." || candidate == ".." {
			continue
		}
		return candidate
	}
	return ""
}

type shellPathCandidate struct {
	Title         string
	HintScore     int
	HiddenPenalty int
	NoisyPenalty  int
	Depth         int
	Length        int
}

var quotedTextPattern = regexp.MustCompile(`"([^"]+)"|'([^']+)'`)

func normalizeOpenHintToken(raw string) string {
	token := strings.ToLower(strings.TrimSpace(raw))
	token = strings.Trim(token, " \t\r\n`'\".,:;!?()[]{}<>")
	token = strings.TrimPrefix(token, "./")
	token = strings.Trim(token, "/")
	token = strings.ReplaceAll(token, "\\", "/")
	return token
}

func extractOpenRequestHints(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	stopwords := map[string]struct{}{
		"open": {}, "show": {}, "display": {}, "read": {}, "view": {}, "edit": {},
		"the": {}, "a": {}, "an": {}, "please": {}, "file": {}, "files": {},
		"on": {}, "in": {}, "at": {}, "to": {}, "from": {}, "for": {},
		"canvas": {}, "project": {}, "this": {}, "that": {}, "my": {},
	}
	addable := func(token string, seen map[string]struct{}, out *[]string) {
		token = normalizeOpenHintToken(token)
		if token == "" {
			return
		}
		if _, blocked := stopwords[token]; blocked {
			return
		}
		if len(token) < 3 && !strings.Contains(token, ".") && !strings.Contains(token, "/") {
			return
		}
		if _, exists := seen[token]; exists {
			return
		}
		seen[token] = struct{}{}
		*out = append(*out, token)
	}

	hints := make([]string, 0, 8)
	seen := map[string]struct{}{}
	for _, match := range quotedTextPattern.FindAllStringSubmatch(trimmed, -1) {
		for _, group := range match[1:] {
			if strings.TrimSpace(group) == "" {
				continue
			}
			addable(group, seen, &hints)
		}
	}

	fields := strings.Fields(strings.ToLower(trimmed))
	verbs := map[string]struct{}{"open": {}, "show": {}, "display": {}, "read": {}, "view": {}, "edit": {}}
	for i, field := range fields {
		verb := normalizeOpenHintToken(field)
		if _, isVerb := verbs[verb]; !isVerb {
			continue
		}
		for j := i + 1; j < len(fields) && j <= i+6; j++ {
			addable(fields[j], seen, &hints)
		}
	}

	for _, field := range strings.Fields(trimmed) {
		token := normalizeOpenHintToken(field)
		if token == "" {
			continue
		}
		if strings.Contains(token, ".") || strings.Contains(token, "/") {
			addable(token, seen, &hints)
			base := normalizeOpenHintToken(filepath.Base(token))
			addable(base, seen, &hints)
			stem := normalizeOpenHintToken(strings.TrimSuffix(base, filepath.Ext(base)))
			addable(stem, seen, &hints)
		}
	}
	return hints
}

func scoreShellPathCandidate(title string, hints []string) int {
	if len(hints) == 0 {
		return 0
	}
	cleanTitle := filepath.ToSlash(strings.ToLower(strings.TrimSpace(title)))
	if cleanTitle == "" {
		return 0
	}
	base := strings.ToLower(strings.TrimSpace(filepath.Base(cleanTitle)))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	score := 0
	for _, rawHint := range hints {
		hint := normalizeOpenHintToken(rawHint)
		if hint == "" {
			continue
		}
		hintBase := strings.ToLower(strings.TrimSpace(filepath.Base(hint)))
		hintStem := strings.TrimSuffix(hintBase, filepath.Ext(hintBase))
		switch {
		case cleanTitle == hint || base == hintBase:
			score += 120
		case stem != "" && (stem == hintBase || stem == hintStem):
			score += 90
		case strings.HasSuffix(cleanTitle, "/"+hintBase):
			score += 70
		case strings.Contains(base, hintBase):
			score += 55
		case strings.Contains(cleanTitle, hint):
			score += 35
		}
	}
	return score
}

func shellPathNoisyPenalty(title string) int {
	clean := filepath.ToSlash(strings.ToLower(strings.TrimSpace(title)))
	if clean == "" {
		return 0
	}
	penalty := 0
	segments := strings.Split(clean, "/")
	for _, segment := range segments {
		switch strings.TrimSpace(segment) {
		case "node_modules", ".venv", "vendor", "dist", "build", "target", "gcc-build", "__pycache__":
			penalty += 2
		}
	}
	return penalty
}

func selectBestShellPathFromOutput(cwd, output string, hints []string) string {
	root := strings.TrimSpace(cwd)
	if root == "" {
		return firstShellPathFromOutput(output)
	}
	lines := strings.Split(output, "\n")
	candidates := make([]shellPathCandidate, 0, len(lines))
	for _, line := range lines {
		candidate := strings.TrimSpace(line)
		if candidate == "" || candidate == "(no output)" {
			continue
		}
		candidate = strings.TrimPrefix(candidate, "./")
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == "." || candidate == ".." {
			continue
		}
		absPath, canvasTitle, err := resolveCanvasFilePath(root, candidate)
		if err != nil {
			continue
		}
		info, statErr := os.Stat(absPath)
		if statErr != nil || info.IsDir() {
			continue
		}
		title := filepath.ToSlash(strings.TrimSpace(canvasTitle))
		if title == "" {
			continue
		}
		segments := strings.Split(title, "/")
		hiddenPenalty := 0
		for _, segment := range segments {
			if strings.HasPrefix(strings.TrimSpace(segment), ".") {
				hiddenPenalty++
			}
		}
		candidates = append(candidates, shellPathCandidate{
			Title:         title,
			HintScore:     scoreShellPathCandidate(title, hints),
			HiddenPenalty: hiddenPenalty,
			NoisyPenalty:  shellPathNoisyPenalty(title),
			Depth:         strings.Count(title, "/"),
			Length:        len(title),
		})
	}
	if len(candidates) == 0 {
		return firstShellPathFromOutput(output)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.HintScore != right.HintScore {
			return left.HintScore > right.HintScore
		}
		if left.HiddenPenalty != right.HiddenPenalty {
			return left.HiddenPenalty < right.HiddenPenalty
		}
		if left.NoisyPenalty != right.NoisyPenalty {
			return left.NoisyPenalty < right.NoisyPenalty
		}
		if left.Depth != right.Depth {
			return left.Depth < right.Depth
		}
		return left.Length < right.Length
	})
	return candidates[0].Title
}

func resolveRootTopLevelFile(root, rel string) (string, bool) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(rel) == "" {
		return "", false
	}
	cleanRel := strings.TrimSpace(filepath.Base(filepath.Clean(rel)))
	if cleanRel == "" || cleanRel == "." || cleanRel == ".." {
		return "", false
	}
	abs := filepath.Clean(filepath.Join(root, cleanRel))
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		return filepath.ToSlash(cleanRel), true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if name == "" {
			continue
		}
		if strings.EqualFold(name, cleanRel) {
			return filepath.ToSlash(name), true
		}
	}
	return "", false
}

func preferTopLevelSiblingPath(cwd, candidate string) string {
	cleanCandidate := filepath.ToSlash(strings.TrimSpace(candidate))
	if cleanCandidate == "" {
		return cleanCandidate
	}
	base := strings.TrimSpace(filepath.Base(cleanCandidate))
	if base == "" || base == "." || base == ".." {
		return cleanCandidate
	}
	root := strings.TrimSpace(cwd)
	if root == "" {
		return cleanCandidate
	}
	if resolved, ok := resolveRootTopLevelFile(root, base); ok {
		return resolved
	}
	if ext := strings.TrimSpace(filepath.Ext(base)); ext == "" {
		stem := strings.TrimSpace(base)
		for _, variant := range []string{stem + ".md", stem + ".markdown", stem + ".txt", stem + ".rst", stem + ".adoc"} {
			if resolved, ok := resolveRootTopLevelFile(root, variant); ok {
				return resolved
			}
		}
	}
	return cleanCandidate
}
