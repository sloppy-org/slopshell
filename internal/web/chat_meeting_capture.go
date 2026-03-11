package web

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

const meetingCaptureItemSource = "meeting_capture"

type meetingNotesSnapshot struct {
	Participants []string
	Decisions    []string
	ActionItems  []meetingNotesActionItem
	KeyTopics    []string
}

type meetingNotesActionItem struct {
	ActorName string
	ItemTitle string
	ItemID    int64
}

var (
	meetingDecisionGoWithPattern   = regexp.MustCompile(`(?i)^(?:ok(?:ay)?[, ]+)?(?:let us|let's)\s+go with\s+(.+)$`)
	meetingDecisionDecidedTo       = regexp.MustCompile(`(?i)^(?:we\s+)?decided\s+to\s+(.+)$`)
	meetingDecisionDecidedOn       = regexp.MustCompile(`(?i)^(?:we\s+)?decided\s+on\s+(.+)$`)
	meetingDecisionAgreement       = regexp.MustCompile(`(?i)^(?:we\s+)?agreed\s+to\s+(.+)$`)
	meetingDecisionExplicitPattern = regexp.MustCompile(`(?i)^decision\s*[:\-]\s*(.+)$`)
)

var ignoredMeetingTopics = map[string]struct{}{
	"assistant response cancelled":   {},
	"assistant response completed":   {},
	"assistant response failed":      {},
	"assistant response interrupted": {},
	"assistant response triggered":   {},
	"session started":                {},
	"session stopped":                {},
}

func meetingCaptureKey(raw string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(raw))), " ")
}

func normalizeMeetingDecisionText(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.Trim(text, " \t\r\n-:;,.!?")
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	runes := []rune(text)
	first := strings.ToUpper(string(runes[0]))
	if len(runes) == 1 {
		return first
	}
	return first + string(runes[1:])
}

func parseMeetingDecisionCandidate(raw string) (string, bool) {
	evidence := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if evidence == "" || strings.HasPrefix(evidence, "#") {
		return "", false
	}
	text := strings.TrimSpace(itemTitlePrefixPattern.ReplaceAllString(evidence, ""))
	switch {
	case meetingDecisionGoWithPattern.MatchString(text):
		match := meetingDecisionGoWithPattern.FindStringSubmatch(text)
		return normalizeMeetingDecisionText("Go with " + match[1]), true
	case meetingDecisionDecidedTo.MatchString(text):
		match := meetingDecisionDecidedTo.FindStringSubmatch(text)
		return normalizeMeetingDecisionText("Decided to " + match[1]), true
	case meetingDecisionDecidedOn.MatchString(text):
		match := meetingDecisionDecidedOn.FindStringSubmatch(text)
		return normalizeMeetingDecisionText("Decided on " + match[1]), true
	case meetingDecisionAgreement.MatchString(text):
		match := meetingDecisionAgreement.FindStringSubmatch(text)
		return normalizeMeetingDecisionText("Agreed to " + match[1]), true
	case meetingDecisionExplicitPattern.MatchString(text):
		match := meetingDecisionExplicitPattern.FindStringSubmatch(text)
		return normalizeMeetingDecisionText(match[1]), true
	default:
		return "", false
	}
}

func extractMeetingDecisions(text string) []string {
	candidates := splitMeetingSummaryCandidates(text)
	if len(candidates) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		decision, ok := parseMeetingDecisionCandidate(candidate)
		if !ok {
			continue
		}
		key := meetingCaptureKey(decision)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, decision)
	}
	return out
}

func meetingCaptureItemTitle(item proposedMeetingItem) string {
	if strings.TrimSpace(item.ActorName) == "" {
		return item.Title
	}
	return fmt.Sprintf("%s: %s", item.ActorName, item.Title)
}

func meetingCaptureSourceRef(sessionID string, item proposedMeetingItem) string {
	return strings.TrimSpace(sessionID) + ":" + meetingCaptureKey(item.ActorName) + ":" + meetingCaptureKey(item.Title)
}

func parseMeetingCapturePayload(raw string) map[string]any {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(clean), &payload); err != nil {
		return nil
	}
	return payload
}

func meetingCapturePayloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value := strings.TrimSpace(fmt.Sprint(payload[key]))
	if value == "<nil>" {
		return ""
	}
	return value
}

func (a *App) captureMeetingNotesForSegment(participantSessionID string, seg store.ParticipantSegment) {
	if a == nil || a.store == nil || seg.ID == 0 {
		return
	}
	if !a.LivePolicy().CapturesMeetingNotes() {
		return
	}
	text := strings.TrimSpace(seg.Text)
	if text == "" || isCompanionDirectAddress(text) {
		return
	}
	session, err := a.store.GetParticipantSession(participantSessionID)
	if err != nil {
		log.Printf("meeting capture session lookup error: %v", err)
		return
	}
	events, err := a.store.ListParticipantEvents(participantSessionID)
	if err != nil {
		log.Printf("meeting capture event lookup error: %v", err)
		return
	}

	decisionKeys := map[string]struct{}{}
	actionRefs := map[string]struct{}{}
	for _, event := range events {
		payload := parseMeetingCapturePayload(event.PayloadJSON)
		switch strings.TrimSpace(event.EventType) {
		case "meeting_decision_captured":
			if key := meetingCaptureKey(meetingCapturePayloadString(payload, "decision")); key != "" {
				decisionKeys[key] = struct{}{}
			}
		case "meeting_action_item_captured":
			if sourceRef := strings.TrimSpace(meetingCapturePayloadString(payload, "source_ref")); sourceRef != "" {
				actionRefs[sourceRef] = struct{}{}
			}
		}
	}

	if a.livePolicyConfig().CaptureDecisions {
		for _, decision := range extractMeetingDecisions(text) {
			key := meetingCaptureKey(decision)
			if _, exists := decisionKeys[key]; exists {
				continue
			}
			payload, err := json.Marshal(map[string]any{
				"decision": decision,
				"text":     decision,
			})
			if err != nil {
				continue
			}
			if err := a.store.AddParticipantEvent(participantSessionID, seg.ID, "meeting_decision_captured", string(payload)); err == nil {
				decisionKeys[key] = struct{}{}
			}
		}
	}

	if !a.livePolicyConfig().CaptureActionItems || session.WorkspaceID <= 0 {
		return
	}
	workspaceID := session.WorkspaceID
	for _, item := range a.extractMeetingItems(text) {
		sourceRef := meetingCaptureSourceRef(participantSessionID, item)
		if _, exists := actionRefs[sourceRef]; exists {
			continue
		}
		created, err := a.store.UpsertItemFromSource(meetingCaptureItemSource, sourceRef, meetingCaptureItemTitle(item), &workspaceID)
		if err != nil {
			log.Printf("meeting capture item upsert error: %v", err)
			continue
		}
		payload, err := json.Marshal(map[string]any{
			"actor_name": item.ActorName,
			"item_id":    created.ID,
			"item_title": created.Title,
			"source_ref": sourceRef,
			"text":       created.Title,
			"title":      item.Title,
		})
		if err != nil {
			continue
		}
		if err := a.store.AddParticipantEvent(participantSessionID, seg.ID, "meeting_action_item_captured", string(payload)); err == nil {
			actionRefs[sourceRef] = struct{}{}
		}
	}
}

func participantsFromSegments(segments []store.ParticipantSegment) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		name := strings.TrimSpace(seg.Speaker)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func decisionsFromParticipantEvents(events []store.ParticipantEvent) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.EventType) != "meeting_decision_captured" {
			continue
		}
		payload := parseMeetingCapturePayload(event.PayloadJSON)
		decision := normalizeMeetingDecisionText(meetingCapturePayloadString(payload, "decision"))
		if decision == "" {
			decision = normalizeMeetingDecisionText(meetingCapturePayloadString(payload, "text"))
		}
		if decision == "" {
			continue
		}
		key := meetingCaptureKey(decision)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, decision)
	}
	return out
}

func actionItemsFromParticipantEvents(events []store.ParticipantEvent) []meetingNotesActionItem {
	seen := map[string]struct{}{}
	out := make([]meetingNotesActionItem, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.EventType) != "meeting_action_item_captured" {
			continue
		}
		payload := parseMeetingCapturePayload(event.PayloadJSON)
		itemTitle := strings.TrimSpace(meetingCapturePayloadString(payload, "item_title"))
		if itemTitle == "" {
			itemTitle = meetingCaptureItemTitle(proposedMeetingItem{
				ActorName: meetingCapturePayloadString(payload, "actor_name"),
				Title:     meetingCapturePayloadString(payload, "title"),
			})
		}
		if itemTitle == "" {
			continue
		}
		key := meetingCaptureKey(itemTitle)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		item := meetingNotesActionItem{
			ActorName: strings.TrimSpace(meetingCapturePayloadString(payload, "actor_name")),
			ItemTitle: itemTitle,
		}
		if rawID := meetingCapturePayloadString(payload, "item_id"); rawID != "" {
			fmt.Sscan(rawID, &item.ItemID)
		}
		out = append(out, item)
	}
	return out
}

func distinctMeetingTopics(items []any, limit int) []string {
	if limit <= 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, limit)
	for _, item := range items {
		topic := strings.TrimSpace(formatCompanionTopicTimelineItem(item))
		if topic == "" {
			continue
		}
		key := strings.ToLower(topic)
		if _, ignore := ignoredMeetingTopics[key]; ignore {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, topic)
		if len(out) == limit {
			break
		}
	}
	return out
}

func buildMeetingNotesSnapshot(segments []store.ParticipantSegment, events []store.ParticipantEvent, memory companionRoomMemory) meetingNotesSnapshot {
	return meetingNotesSnapshot{
		Participants: participantsFromSegments(segments),
		Decisions:    decisionsFromParticipantEvents(events),
		ActionItems:  actionItemsFromParticipantEvents(events),
		KeyTopics:    distinctMeetingTopics(memory.TopicTimeline, 5),
	}
}

func (a *App) loadMeetingNotesSnapshot(sessionID string, memory companionRoomMemory) (meetingNotesSnapshot, error) {
	if a == nil || a.store == nil || strings.TrimSpace(sessionID) == "" {
		return meetingNotesSnapshot{}, nil
	}
	segments, err := a.store.ListParticipantSegments(sessionID, 0, 0)
	if err != nil {
		return meetingNotesSnapshot{}, err
	}
	events, err := a.store.ListParticipantEvents(sessionID)
	if err != nil {
		return meetingNotesSnapshot{}, err
	}
	return buildMeetingNotesSnapshot(segments, events, memory), nil
}
