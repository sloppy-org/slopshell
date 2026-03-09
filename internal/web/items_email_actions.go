package web

import (
	"database/sql"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

type extractedEmailAction struct {
	Title      string
	SourceRef  string
	FollowUpAt *string
}

type emailActionTarget struct {
	ArtifactID int64
	Action     extractedEmailAction
}

var (
	emailActionDatePhrasePattern = regexp.MustCompile(`(?i)\b(?:by|before|due|deadline:?|no later than)\s+([A-Za-z]{3,9}\s+\d{1,2}(?:,\s*\d{4})?|\d{4}-\d{2}-\d{2}|tomorrow)\b`)
	emailActionSentenceRequest   = regexp.MustCompile(`(?i)\b(?:please|kindly|can you|could you|would you|need you to|i need you to|we need to|let's|let us|can we|could we|would we)\s+(.+)$`)
	emailActionMeetingPattern    = regexp.MustCompile(`(?i)\b(schedule|meeting|meet|call|availability|calendar)\b`)
)

func (a *App) persistEmailActionItems(account store.ExternalAccount, threads []emailThreadRecord) error {
	now := time.Now().UTC()
	if a != nil && a.calendarNow != nil {
		now = a.calendarNow().UTC()
	}
	desired := make(map[string]emailActionTarget)
	for _, thread := range threads {
		if !thread.HasFollowUp {
			continue
		}
		actions := extractEmailThreadActions(now, thread)
		for _, action := range actions {
			desired[action.SourceRef] = emailActionTarget{
				ArtifactID: thread.Artifact.ID,
				Action:     action,
			}
		}
	}
	if err := a.deleteStaleEmailActionInboxItems(account.Provider, desired); err != nil {
		return err
	}
	sourceRefs := make([]string, 0, len(desired))
	for sourceRef := range desired {
		sourceRefs = append(sourceRefs, sourceRef)
	}
	sort.Strings(sourceRefs)
	for _, sourceRef := range sourceRefs {
		target := desired[sourceRef]
		if err := a.upsertEmailActionItem(account, target.ArtifactID, target.Action); err != nil {
			return err
		}
	}
	return nil
}

func extractEmailThreadActions(now time.Time, thread emailThreadRecord) []extractedEmailAction {
	seen := make(map[string]struct{})
	out := make([]extractedEmailAction, 0)
	for _, persisted := range thread.Messages {
		if persisted.Message == nil {
			continue
		}
		base := now
		if !persisted.Message.Date.IsZero() {
			base = persisted.Message.Date.UTC()
		}
		for _, candidate := range emailActionCandidatesForMessage(persisted) {
			action, ok := parseEmailActionCandidate(candidate, cleanEmailThreadSubject(persisted.Message), base, thread.ThreadID)
			if !ok {
				continue
			}
			key := strings.ToLower(action.Title)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, action)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SourceRef < out[j].SourceRef
	})
	return out
}

func emailActionCandidatesForMessage(persisted emailPersistedMessage) []string {
	message := persisted.Message
	if message == nil {
		return nil
	}
	candidates := []string{}
	if message.BodyText != nil {
		candidates = append(candidates, splitMeetingSummaryCandidates(*message.BodyText)...)
	}
	if len(candidates) == 0 && strings.TrimSpace(message.Snippet) != "" {
		candidates = append(candidates, splitMeetingSummaryCandidates(message.Snippet)...)
	}
	if len(candidates) == 0 && strings.TrimSpace(message.Subject) != "" {
		candidates = append(candidates, strings.TrimSpace(message.Subject))
	}
	return candidates
}

func parseEmailActionCandidate(raw, subject string, base time.Time, threadID string) (extractedEmailAction, bool) {
	text := strings.Join(strings.Fields(strings.TrimSpace(itemTitlePrefixPattern.ReplaceAllString(raw, ""))), " ")
	if text == "" {
		return extractedEmailAction{}, false
	}

	clause := text
	if match := emailActionSentenceRequest.FindStringSubmatch(text); len(match) == 2 {
		clause = strings.TrimSpace(match[1])
	} else if !looksLikeMeetingAction(text) {
		return extractedEmailAction{}, false
	}

	followUpAt := parseEmailFollowUpAt(base, text)
	trimmedClause := strings.TrimSpace(emailActionDatePhrasePattern.ReplaceAllString(clause, ""))
	trimmedClause = strings.Trim(trimmedClause, " \t\r\n,.;:!?")
	if trimmedClause == "" {
		trimmedClause = clause
	}

	title := normalizeMeetingItemTitle(trimmedClause)
	if title == "" {
		return extractedEmailAction{}, false
	}
	if emailActionMeetingPattern.MatchString(text) && !strings.HasPrefix(strings.ToLower(title), "schedule") {
		if cleanSubject := strings.TrimSpace(subject); cleanSubject != "" {
			title = normalizeMeetingItemTitle("Schedule meeting about " + cleanSubject)
		}
	}
	if title == "" {
		return extractedEmailAction{}, false
	}

	return extractedEmailAction{
		Title:      title,
		SourceRef:  emailActionSourceRef(threadID, title),
		FollowUpAt: followUpAt,
	}, true
}

func emailActionSourceRef(threadID, title string) string {
	return "thread:" + strings.TrimSpace(threadID) + ":action:" + slugifyEmailActionTitle(title)
}

func isEmailActionSourceRef(sourceRef string) bool {
	ref := strings.TrimSpace(sourceRef)
	return strings.HasPrefix(ref, "thread:") && strings.Contains(ref, ":action:")
}

func slugifyEmailActionTitle(title string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func parseEmailFollowUpAt(base time.Time, text string) *string {
	match := emailActionDatePhrasePattern.FindStringSubmatch(text)
	if len(match) != 2 {
		return nil
	}
	target := strings.TrimSpace(match[1])
	if target == "" {
		return nil
	}
	normalized := strings.ToLower(target)
	var due time.Time
	switch normalized {
	case "tomorrow":
		if base.IsZero() {
			base = time.Now().UTC()
		}
		due = time.Date(base.Year(), base.Month(), base.Day(), 9, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	default:
		if parsed, ok := parseEmailFollowUpDate(base, target); ok {
			due = parsed
		}
	}
	if due.IsZero() {
		return nil
	}
	value := due.UTC().Format(time.RFC3339)
	return &value
}

func parseEmailFollowUpDate(base time.Time, value string) (time.Time, bool) {
	if base.IsZero() {
		base = time.Now().UTC()
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 9, 0, 0, 0, time.UTC), true
	}
	for _, layout := range []string{"January 2, 2006", "Jan 2, 2006", "January 2", "Jan 2"} {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		year := parsed.Year()
		if year == 0 {
			year = base.Year()
			if parsed.Month() < base.Month() || (parsed.Month() == base.Month() && parsed.Day() < base.Day()) {
				year++
			}
		}
		return time.Date(year, parsed.Month(), parsed.Day(), 9, 0, 0, 0, time.UTC), true
	}
	return time.Time{}, false
}

func (a *App) deleteStaleEmailActionInboxItems(provider string, desired map[string]emailActionTarget) error {
	items, err := a.store.ListItemsByStateFiltered(store.ItemStateInbox, store.ItemListFilter{Source: provider})
	if err != nil {
		return err
	}
	for _, item := range items {
		sourceRef := strings.TrimSpace(strFromPointer(item.SourceRef))
		if !isEmailActionSourceRef(sourceRef) {
			continue
		}
		if _, ok := desired[sourceRef]; ok {
			continue
		}
		if err := a.store.DeleteItem(item.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) upsertEmailActionItem(account store.ExternalAccount, artifactID int64, action extractedEmailAction) error {
	source := account.Provider
	sourceRef := strings.TrimSpace(action.SourceRef)
	if sourceRef == "" {
		return nil
	}
	existing, err := a.store.GetItemBySource(source, sourceRef)
	switch {
	case err == nil:
		if existing.State == store.ItemStateDone {
			return nil
		}
		title := action.Title
		targetArtifactID := artifactID
		followUpAt := ""
		if action.FollowUpAt != nil {
			followUpAt = strings.TrimSpace(*action.FollowUpAt)
		}
		return a.store.UpdateItem(existing.ID, store.ItemUpdate{
			Title:      &title,
			ArtifactID: &targetArtifactID,
			FollowUpAt: &followUpAt,
		})
	case !errorsIsNoRows(err):
		return err
	}

	return createEmailActionItem(a.store, source, sourceRef, artifactID, action)
}

func createEmailActionItem(s *store.Store, source, sourceRef string, artifactID int64, action extractedEmailAction) error {
	sourceValue := source
	sourceRefValue := sourceRef
	opts := store.ItemOptions{
		State:      store.ItemStateInbox,
		ArtifactID: &artifactID,
		Source:     &sourceValue,
		SourceRef:  &sourceRefValue,
	}
	if action.FollowUpAt != nil {
		opts.FollowUpAt = action.FollowUpAt
	}
	_, err := s.CreateItem(action.Title, opts)
	return err
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}
