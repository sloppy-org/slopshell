package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/krystophny/tabura/internal/store"
)

const (
	dictationTargetDocumentSection = "document_section"
	dictationTargetEmailReply      = "email_reply"
	dictationTargetReviewComment   = "review_comment"
)

type dictationSessionState struct {
	Active        bool
	TargetKind    string
	Prompt        string
	ArtifactTitle string
	Transcript    string
	DraftText     string
	ScratchPath   string
}

type dictationSessionTracker struct {
	mu        sync.Mutex
	bySession map[string]dictationSessionState
}

type dictationStartRequest struct {
	Prompt        string `json:"prompt"`
	TargetKind    string `json:"target_kind"`
	ArtifactTitle string `json:"artifact_title"`
}

type dictationAppendRequest struct {
	Text string `json:"text"`
}

type dictationDraftPutRequest struct {
	DraftText string `json:"draft_text"`
}

func newDictationSessionTracker() *dictationSessionTracker {
	return &dictationSessionTracker{bySession: map[string]dictationSessionState{}}
}

func (t *dictationSessionTracker) get(sessionID string) (dictationSessionState, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	state, ok := t.bySession[sessionID]
	return state, ok
}

func (t *dictationSessionTracker) set(sessionID string, state dictationSessionState) dictationSessionState {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bySession[sessionID] = state
	return state
}

func (t *dictationSessionTracker) delete(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.bySession, sessionID)
}

func normalizeDictationTargetKind(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case dictationTargetEmailReply:
		return dictationTargetEmailReply
	case dictationTargetReviewComment:
		return dictationTargetReviewComment
	default:
		return dictationTargetDocumentSection
	}
}

func dictationTargetLabel(kind string) string {
	switch normalizeDictationTargetKind(kind) {
	case dictationTargetEmailReply:
		return "Email Reply"
	case dictationTargetReviewComment:
		return "Review Comment"
	default:
		return "Document Section"
	}
}

func inferDictationTargetKind(prompt, artifactTitle string) string {
	combined := strings.ToLower(strings.TrimSpace(prompt + "\n" + artifactTitle))
	switch {
	case strings.Contains(combined, "review"), strings.Contains(combined, "pull request"), strings.Contains(combined, ".diff"), strings.Contains(combined, "pr "):
		return dictationTargetReviewComment
	case strings.Contains(combined, "reply"), strings.Contains(combined, "email"), strings.Contains(combined, "thread"), strings.Contains(combined, "letter"):
		return dictationTargetEmailReply
	default:
		return dictationTargetDocumentSection
	}
}

func appendDictationTranscript(existing, fragment string) string {
	base := strings.TrimSpace(existing)
	next := strings.TrimSpace(fragment)
	if next == "" {
		return base
	}
	if base == "" {
		return next
	}
	return base + "\n\n" + next
}

func dictationParagraphs(transcript string) []string {
	rawParts := strings.Split(strings.ReplaceAll(strings.TrimSpace(transcript), "\r\n", "\n"), "\n\n")
	out := make([]string, 0, len(rawParts))
	for _, raw := range rawParts {
		part := strings.Join(strings.Fields(raw), " ")
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func shapeDictationDraft(targetKind, artifactTitle, transcript string) string {
	kind := normalizeDictationTargetKind(targetKind)
	title := strings.TrimSpace(artifactTitle)
	parts := dictationParagraphs(transcript)
	if len(parts) == 0 {
		switch kind {
		case dictationTargetEmailReply:
			return "# Email Reply Draft\n\nStart speaking to build the reply."
		case dictationTargetReviewComment:
			return "# Review Comment Draft\n\nStart speaking to build the comment."
		default:
			return "# Document Section Draft\n\nStart speaking to build the section."
		}
	}
	var b strings.Builder
	switch kind {
	case dictationTargetEmailReply:
		b.WriteString("# Email Reply Draft\n\n")
		if title != "" {
			fmt.Fprintf(&b, "Thread: %s\n\n", title)
		}
		b.WriteString(strings.Join(parts, "\n\n"))
	case dictationTargetReviewComment:
		b.WriteString("# Review Comment Draft\n\n")
		if title != "" {
			fmt.Fprintf(&b, "Target: %s\n\n", title)
		}
		for _, part := range parts {
			fmt.Fprintf(&b, "- %s\n", part)
		}
	default:
		b.WriteString("# Document Section Draft\n\n")
		if title != "" {
			fmt.Fprintf(&b, "Working title: %s\n\n", title)
		}
		b.WriteString(strings.Join(parts, "\n\n"))
	}
	return strings.TrimSpace(b.String())
}

func (a *App) dictationStateResponse(state dictationSessionState) map[string]any {
	return map[string]any{
		"active":         state.Active,
		"target_kind":    normalizeDictationTargetKind(state.TargetKind),
		"target_label":   dictationTargetLabel(state.TargetKind),
		"prompt":         strings.TrimSpace(state.Prompt),
		"artifact_title": strings.TrimSpace(state.ArtifactTitle),
		"transcript":     strings.TrimSpace(state.Transcript),
		"draft_text":     strings.TrimSpace(state.DraftText),
		"scratch_path":   strings.TrimSpace(state.ScratchPath),
	}
}

func (a *App) dictationScratchPath(state dictationSessionState) string {
	if clean := strings.TrimSpace(state.ScratchPath); clean != "" {
		return clean
	}
	seed := strings.ReplaceAll(normalizeDictationTargetKind(state.TargetKind), "_", "-")
	return defaultCanvasTempFilePath("dictation-" + seed)
}

func (a *App) writeDictationDraft(projectKey string, state *dictationSessionState) error {
	if state == nil {
		return fmt.Errorf("dictation state is required")
	}
	state.ScratchPath = a.dictationScratchPath(*state)
	cwd := a.cwdForProjectKey(projectKey)
	absPath, title, err := resolveCanvasFilePath(cwd, state.ScratchPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(absPath, []byte(strings.TrimSpace(state.DraftText)+"\n"), 0644); err != nil {
		return err
	}
	state.ScratchPath = title
	return nil
}

func (a *App) loadChatSessionForDictation(w http.ResponseWriter, r *http.Request) (string, store.ChatSession, bool) {
	if !a.requireAuth(w, r) {
		return "", store.ChatSession{}, false
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return "", store.ChatSession{}, false
	}
	session, err := a.store.GetChatSession(sessionID)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return "", store.ChatSession{}, false
	}
	return sessionID, session, true
}

func (a *App) handleChatSessionDictationGet(w http.ResponseWriter, r *http.Request) {
	sessionID, _, ok := a.loadChatSessionForDictation(w, r)
	if !ok {
		return
	}
	state, exists := a.dictationSessions.get(sessionID)
	if !exists {
		state = dictationSessionState{
			Active:     false,
			TargetKind: dictationTargetDocumentSection,
		}
	}
	writeJSON(w, map[string]any{"ok": true, "dictation": a.dictationStateResponse(state)})
}

func (a *App) handleChatSessionDictationStart(w http.ResponseWriter, r *http.Request) {
	sessionID, _, ok := a.loadChatSessionForDictation(w, r)
	if !ok {
		return
	}
	var req dictationStartRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	targetKind := normalizeDictationTargetKind(req.TargetKind)
	if strings.TrimSpace(req.TargetKind) == "" {
		targetKind = inferDictationTargetKind(req.Prompt, req.ArtifactTitle)
	}
	state := dictationSessionState{
		Active:        true,
		TargetKind:    targetKind,
		Prompt:        strings.TrimSpace(req.Prompt),
		ArtifactTitle: strings.TrimSpace(req.ArtifactTitle),
	}
	state = a.dictationSessions.set(sessionID, state)
	writeJSON(w, map[string]any{"ok": true, "dictation": a.dictationStateResponse(state)})
}

func (a *App) handleChatSessionDictationAppend(w http.ResponseWriter, r *http.Request) {
	sessionID, session, ok := a.loadChatSessionForDictation(w, r)
	if !ok {
		return
	}
	var req dictationAppendRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	state, exists := a.dictationSessions.get(sessionID)
	if !exists || !state.Active {
		http.Error(w, "dictation is not active", http.StatusConflict)
		return
	}
	state.Transcript = appendDictationTranscript(state.Transcript, req.Text)
	if strings.TrimSpace(state.Transcript) == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	state.DraftText = shapeDictationDraft(state.TargetKind, state.ArtifactTitle, state.Transcript)
	if err := a.writeDictationDraft(session.ProjectKey, &state); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	state = a.dictationSessions.set(sessionID, state)
	writeJSON(w, map[string]any{"ok": true, "dictation": a.dictationStateResponse(state)})
}

func (a *App) handleChatSessionDictationDraftPut(w http.ResponseWriter, r *http.Request) {
	sessionID, session, ok := a.loadChatSessionForDictation(w, r)
	if !ok {
		return
	}
	var req dictationDraftPutRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	state, exists := a.dictationSessions.get(sessionID)
	if !exists || !state.Active {
		http.Error(w, "dictation is not active", http.StatusConflict)
		return
	}
	state.DraftText = strings.TrimSpace(req.DraftText)
	if state.DraftText == "" {
		http.Error(w, "draft_text is required", http.StatusBadRequest)
		return
	}
	if err := a.writeDictationDraft(session.ProjectKey, &state); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	state = a.dictationSessions.set(sessionID, state)
	writeJSON(w, map[string]any{"ok": true, "dictation": a.dictationStateResponse(state)})
}

func (a *App) handleChatSessionDictationDelete(w http.ResponseWriter, r *http.Request) {
	sessionID, _, ok := a.loadChatSessionForDictation(w, r)
	if !ok {
		return
	}
	state, _ := a.dictationSessions.get(sessionID)
	state.Active = false
	state = a.dictationSessions.set(sessionID, state)
	writeJSON(w, map[string]any{"ok": true, "dictation": a.dictationStateResponse(state)})
}
