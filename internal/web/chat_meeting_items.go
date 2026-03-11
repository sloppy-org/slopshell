package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

const meetingSummaryItemSource = "meeting_summary"

var (
	meetingItemExplicitPrefixPattern  = regexp.MustCompile(`(?i)^(?:action(?:\s+item)?|todo|follow[- ]?up|next\s+step|owner)\s*[:\-]\s*(.+)$`)
	meetingItemActorActionPattern     = regexp.MustCompile(`^([A-Z][\pL0-9'’.-]*(?:\s+[A-Z][\pL0-9'’.-]*){0,2})\s+(?:will|should|can|must|needs?\s+to|is\s+going\s+to|to)\s+(.+)$`)
	meetingItemActorLabelPattern      = regexp.MustCompile(`^([A-Z][\pL0-9'’.-]*(?:\s+[A-Z][\pL0-9'’.-]*){0,2})\s*[:\-]\s*(.+)$`)
	meetingItemAssigneeRequestPattern = regexp.MustCompile(`^([A-Z][\pL0-9'’.-]*(?:\s+[A-Z][\pL0-9'’.-]*){0,2})\s*,\s*(?:can|could|will|would)\s+you\s+(.+)$`)
)

type proposedMeetingItem struct {
	Index     int    `json:"index"`
	Title     string `json:"title"`
	ActorName string `json:"actor_name,omitempty"`
	Evidence  string `json:"evidence,omitempty"`
}

type projectMeetingItemsResponse struct {
	OK            bool                       `json:"ok"`
	ProjectID     string                     `json:"project_id"`
	ProjectKey    string                     `json:"project_key"`
	Sessions      []store.ParticipantSession `json:"sessions"`
	Session       *store.ParticipantSession  `json:"session,omitempty"`
	SummaryText   string                     `json:"summary_text"`
	ProposedItems []proposedMeetingItem      `json:"proposed_items"`
}

type createdMeetingItem struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	State     string `json:"state"`
	ActorName string `json:"actor_name,omitempty"`
}

type createMeetingItemsRequest struct {
	Selected []int `json:"selected"`
}

type createMeetingItemsResponse struct {
	OK            bool                      `json:"ok"`
	ProjectID     string                    `json:"project_id"`
	ProjectKey    string                    `json:"project_key"`
	Session       *store.ParticipantSession `json:"session,omitempty"`
	CreatedItems  []createdMeetingItem      `json:"created_items"`
	ProposedItems []proposedMeetingItem     `json:"proposed_items"`
}

func meetingItemActionVerbs() []string {
	return []string{
		"add", "book", "clean", "close", "collect", "confirm", "contact", "coordinate",
		"create", "decide", "deliver", "document", "draft", "follow up", "follow-up",
		"fix", "implement", "investigate", "move", "open", "plan", "prepare", "publish",
		"review", "schedule", "send", "set up", "setup", "share", "summarize",
		"sync", "test", "triage", "update", "write",
	}
}

func splitMeetingSummaryCandidates(summary string) []string {
	text := strings.ReplaceAll(summary, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		start := 0
		for i, r := range line {
			switch r {
			case '.', '!', '?', ';':
				segment := strings.TrimSpace(line[start : i+1])
				if segment != "" {
					out = append(out, segment)
				}
				start = i + 1
			}
		}
		if tail := strings.TrimSpace(line[start:]); tail != "" {
			out = append(out, tail)
		}
	}
	return out
}

func normalizeMeetingItemTitle(raw string) string {
	title := strings.TrimSpace(raw)
	title = strings.Trim(title, " \t\r\n-:;,.!?")
	if strings.HasPrefix(strings.ToLower(title), "to ") {
		title = strings.TrimSpace(title[3:])
	}
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return ""
	}
	runes := []rune(title)
	first := strings.ToUpper(string(runes[0]))
	if len(runes) == 1 {
		return first
	}
	return first + string(runes[1:])
}

func looksLikeMeetingAction(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, prefix := range []string{
		"meeting summary", "summary", "decisions", "decision", "references",
		"agenda", "notes", "discussion", "context",
	} {
		if lower == prefix {
			return false
		}
	}
	for _, verb := range meetingItemActionVerbs() {
		if strings.HasPrefix(lower, verb+" ") {
			return true
		}
	}
	return false
}

func parseMeetingItemCandidate(raw string) (proposedMeetingItem, bool) {
	evidence := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if evidence == "" || strings.HasPrefix(evidence, "#") {
		return proposedMeetingItem{}, false
	}
	text := strings.TrimSpace(itemTitlePrefixPattern.ReplaceAllString(evidence, ""))
	if text == "" {
		return proposedMeetingItem{}, false
	}
	explicit := false
	if match := meetingItemExplicitPrefixPattern.FindStringSubmatch(text); len(match) == 2 {
		text = strings.TrimSpace(match[1])
		explicit = true
	}

	actorName := ""
	if match := meetingItemAssigneeRequestPattern.FindStringSubmatch(text); len(match) == 3 {
		actorName = strings.TrimSpace(match[1])
		text = strings.TrimSpace(match[2])
	} else if match := meetingItemActorActionPattern.FindStringSubmatch(text); len(match) == 3 {
		actorName = strings.TrimSpace(match[1])
		text = strings.TrimSpace(match[2])
	} else if match := meetingItemActorLabelPattern.FindStringSubmatch(text); len(match) == 3 && looksLikeMeetingAction(match[2]) {
		actorName = strings.TrimSpace(match[1])
		text = strings.TrimSpace(match[2])
	}

	if !explicit && !looksLikeMeetingAction(text) {
		return proposedMeetingItem{}, false
	}

	title := normalizeMeetingItemTitle(text)
	if title == "" {
		return proposedMeetingItem{}, false
	}
	return proposedMeetingItem{
		Title:     title,
		ActorName: actorName,
		Evidence:  evidence,
	}, true
}

func (a *App) extractMeetingItems(summary string) []proposedMeetingItem {
	candidates := splitMeetingSummaryCandidates(summary)
	if len(candidates) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]proposedMeetingItem, 0, len(candidates))
	for _, candidate := range candidates {
		item, ok := parseMeetingItemCandidate(candidate)
		if !ok {
			continue
		}
		key := strings.ToLower(item.Title) + "\n" + strings.ToLower(item.ActorName)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		item.Index = len(out)
		out = append(out, item)
	}
	return out
}

func meetingPayloadProject(workspace store.Workspace, project *store.Project) (string, string) {
	projectID := ""
	projectKey := strings.TrimSpace(workspace.DirPath)
	if project != nil {
		projectID = project.ID
		projectKey = strings.TrimSpace(project.ProjectKey)
	}
	return projectID, projectKey
}

func (a *App) loadWorkspaceMeetingItems(w http.ResponseWriter, r *http.Request) (store.Workspace, *store.Project, []store.ParticipantSession, *store.ParticipantSession, string, []proposedMeetingItem, bool) {
	workspace, project, sessions, session, ok := a.resolveWorkspaceCompanionArtifact(w, r)
	if !ok {
		return store.Workspace{}, nil, nil, nil, "", nil, false
	}
	summaryText := ""
	if session != nil {
		memory, err := a.loadCompanionRoomMemory(session.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return store.Workspace{}, nil, nil, nil, "", nil, false
		}
		summaryText = strings.TrimSpace(memory.SummaryText)
	}
	return workspace, project, sessions, session, summaryText, a.extractMeetingItems(summaryText), true
}

func (a *App) handleWorkspaceMeetingItemsGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, project, sessions, session, summaryText, proposed, ok := a.loadWorkspaceMeetingItems(w, r)
	if !ok {
		return
	}
	projectID, projectKey := meetingPayloadProject(workspace, project)
	writeJSON(w, projectMeetingItemsResponse{
		OK:            true,
		ProjectID:     projectID,
		ProjectKey:    projectKey,
		Sessions:      sessions,
		Session:       session,
		SummaryText:   summaryText,
		ProposedItems: proposed,
	})
}

func (a *App) ensureMeetingSummaryArtifact(workspace store.Workspace, project *store.Project, session *store.ParticipantSession, summaryText string) (store.Artifact, error) {
	if session == nil {
		return store.Artifact{}, errors.New("meeting session is required")
	}
	if err := a.syncProjectCompanionArtifacts(workspace, session); err != nil {
		return store.Artifact{}, err
	}
	summaryPath := filepath.Join(companionArtifactDir(workspace, session), "summary.md")
	projectID, projectKey := meetingPayloadProject(workspace, project)
	title := "Meeting Summary"
	metaPayload := map[string]any{
		"source":      meetingSummaryItemSource,
		"summary":     strings.TrimSpace(summaryText),
		"session_id":  session.ID,
		"project_id":  projectID,
		"project_key": projectKey,
	}
	raw, err := json.Marshal(metaPayload)
	if err != nil {
		return store.Artifact{}, err
	}
	metaJSON := string(raw)
	return a.store.CreateArtifact(store.ArtifactKindMarkdown, &summaryPath, nil, &title, &metaJSON)
}

func (a *App) resolveMeetingItemActor(name string) (*store.Actor, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return nil, nil
	}
	actors, err := a.store.ListActors()
	if err != nil {
		return nil, err
	}
	var exact *store.Actor
	for i := range actors {
		if strings.EqualFold(actors[i].Name, cleanName) {
			if exact != nil {
				return nil, nil
			}
			actor := actors[i]
			exact = &actor
		}
	}
	if exact != nil {
		return exact, nil
	}
	created, err := a.store.CreateActor(cleanName, store.ActorKindHuman)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func normalizeSelectedMeetingItems(selected []int, limit int) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, len(selected))
	for _, index := range selected {
		if index < 0 || index >= limit {
			continue
		}
		if _, exists := seen[index]; exists {
			continue
		}
		seen[index] = struct{}{}
		out = append(out, index)
	}
	return out
}

func (a *App) handleCreateMeetingItems(workspace store.Workspace, project *store.Project, session *store.ParticipantSession, summaryText string, proposed []proposedMeetingItem, selected []int) ([]createdMeetingItem, error) {
	chosen := normalizeSelectedMeetingItems(selected, len(proposed))
	if len(chosen) == 0 {
		return nil, errors.New("at least one proposed item must be selected")
	}
	artifact, err := a.ensureMeetingSummaryArtifact(workspace, project, session, summaryText)
	if err != nil {
		return nil, err
	}
	workspaceID := &workspace.ID
	if project != nil {
		resolvedID, resolveErr := a.resolveConversationWorkspaceID(*project, &artifact)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if resolvedID != nil {
			workspaceID = resolvedID
		}
	}
	created := make([]createdMeetingItem, 0, len(chosen))
	for _, index := range chosen {
		proposal := proposed[index]
		opts := store.ItemOptions{
			WorkspaceID: workspaceID,
			ArtifactID:  &artifact.ID,
		}
		if actor, err := a.resolveMeetingItemActor(proposal.ActorName); err != nil {
			return nil, err
		} else if actor != nil {
			opts.ActorID = &actor.ID
		}
		sourceRef := fmt.Sprintf("%s:%d", session.ID, index)
		opts.Source = stringPtr(meetingSummaryItemSource)
		opts.SourceRef = &sourceRef
		item, err := a.store.CreateItem(proposal.Title, opts)
		if err != nil {
			return nil, err
		}
		createdItem := createdMeetingItem{
			ID:    item.ID,
			Title: item.Title,
			State: item.State,
		}
		if proposal.ActorName != "" {
			createdItem.ActorName = proposal.ActorName
		}
		created = append(created, createdItem)
	}
	return created, nil
}

func (a *App) handleWorkspaceMeetingItemsCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, project, _, session, summaryText, proposed, ok := a.loadWorkspaceMeetingItems(w, r)
	if !ok {
		return
	}
	if session == nil || strings.TrimSpace(summaryText) == "" {
		http.Error(w, "meeting summary not available", http.StatusBadRequest)
		return
	}
	var req createMeetingItemsRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	created, err := a.handleCreateMeetingItems(workspace, project, session, summaryText, proposed, req.Selected)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	projectID, projectKey := meetingPayloadProject(workspace, project)
	writeJSON(w, createMeetingItemsResponse{
		OK:            true,
		ProjectID:     projectID,
		ProjectKey:    projectKey,
		Session:       session,
		CreatedItems:  created,
		ProposedItems: proposed,
	})
}

func stringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	clean := strings.TrimSpace(value)
	return &clean
}
