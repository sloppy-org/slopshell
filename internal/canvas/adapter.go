package canvas

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type MarkIntent string

type MarkType string

type TargetKind string

const (
	IntentEphemeral  MarkIntent = "ephemeral"
	IntentDraft      MarkIntent = "draft"
	IntentPersistent MarkIntent = "persistent"
)

const (
	MarkHighlight    MarkType = "highlight"
	MarkUnderline    MarkType = "underline"
	MarkStrikeout    MarkType = "strikeout"
	MarkSquiggly     MarkType = "squiggly"
	MarkCommentPoint MarkType = "comment_point"
)

const (
	TargetTextRange TargetKind = "text_range"
	TargetPDFQuads  TargetKind = "pdf_quads"
	TargetPDFPoint  TargetKind = "pdf_point"
)

type Mark struct {
	MarkID     string                 `json:"mark_id"`
	SessionID  string                 `json:"session_id"`
	ArtifactID string                 `json:"artifact_id"`
	Intent     MarkIntent             `json:"intent"`
	Type       MarkType               `json:"type"`
	TargetKind TargetKind             `json:"target_kind"`
	Target     map[string]interface{} `json:"target"`
	Comment    string                 `json:"comment,omitempty"`
	Author     string                 `json:"author,omitempty"`
	State      string                 `json:"state"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
}

type Selection struct {
	HasSelection bool   `json:"has_selection"`
	LineStart    int    `json:"line_start,omitempty"`
	LineEnd      int    `json:"line_end,omitempty"`
	StartOffset  int    `json:"start_offset,omitempty"`
	EndOffset    int    `json:"end_offset,omitempty"`
	Text         string `json:"text,omitempty"`
}

type SessionRecord struct {
	Opened            bool
	Mode              string
	ActiveArtifact    *Event
	History           []Event
	Marks             map[string]*Mark
	FocusedMarkID     string
	Selection         Selection
	DraftByArtifactID map[string]string
}

type Adapter struct {
	mu                sync.RWMutex
	projectDir        string
	onEvent           func(Event)
	sessions          map[string]*SessionRecord
	headless          bool
	canvasProcessLive bool
	launchErr         string
}

func newSessionRecord(opened bool) *SessionRecord {
	return &SessionRecord{
		Opened:            opened,
		Mode:              "prompt",
		Marks:             map[string]*Mark{},
		History:           []Event{},
		DraftByArtifactID: map[string]string{},
	}
}

func NewAdapter(projectDir string, onEvent func(Event), headless bool) *Adapter {
	return &Adapter{
		projectDir: projectDir,
		onEvent:    onEvent,
		sessions:   map[string]*SessionRecord{},
		headless:   headless,
	}
}

func (a *Adapter) ProjectDir() string {
	return a.projectDir
}

func (a *Adapter) listSessions() []string {
	ids := make([]string, 0, len(a.sessions))
	for id := range a.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (a *Adapter) ensureSession(sessionID string) *SessionRecord {
	r, ok := a.sessions[sessionID]
	if ok {
		return r
	}
	r = newSessionRecord(true)
	a.loadPersistedAnnotationsLocked(sessionID, r)
	a.sessions[sessionID] = r
	return r
}

func (a *Adapter) sessionForRead(sessionID string) *SessionRecord {
	r, ok := a.sessions[sessionID]
	if ok {
		return r
	}
	return newSessionRecord(false)
}

func (a *Adapter) emit(ev Event) {
	if a.onEvent != nil {
		a.onEvent(ev)
	}
}

func (a *Adapter) CanvasSessionOpen(sessionID, modeHint string) map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)
	if modeHint != "" {
		r.Mode = modeHint
	}
	return map[string]interface{}{
		"active":               true,
		"mode":                 r.Mode,
		"mode_hint":            modeHint,
		"active_artifact_id":   activeArtifactID(r),
		"active_artifact_kind": activeArtifactKind(r),
		"marks_total":          len(r.Marks),
		"focused_mark_id":      r.FocusedMarkID,
		"headless":             a.headless,
		"canvas_process_alive": a.canvasProcessLive,
		"canvas_launch_error":  a.launchErrOrNil(),
	}
}

func activeArtifactID(r *SessionRecord) interface{} {
	if r.ActiveArtifact == nil {
		return nil
	}
	return r.ActiveArtifact.EventID
}

func activeArtifactKind(r *SessionRecord) interface{} {
	if r.ActiveArtifact == nil {
		return nil
	}
	return r.ActiveArtifact.Kind
}

func (a *Adapter) launchErrOrNil() interface{} {
	if a.launchErr == "" {
		return nil
	}
	return a.launchErr
}

func (a *Adapter) CanvasArtifactShow(sessionID, kind, title, markdownOrText, path string, page int, reason string, meta map[string]interface{}) (map[string]interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)

	var ev Event
	switch kind {
	case "text":
		ev = NewEvent(EventText)
		ev.Title = title
		ev.Text = markdownOrText
	case "image":
		ev = NewEvent(EventImage)
		ev.Title = title
		ev.Path = path
	case "pdf":
		ev = NewEvent(EventPDF)
		ev.Title = title
		ev.Path = path
		ev.Page = page
	case "clear":
		ev = NewEvent(EventClear)
		ev.Reason = reason
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
	ev.Meta = cloneMeta(meta)

	if ev.Kind == EventClear {
		r.Mode = "prompt"
		r.ActiveArtifact = nil
	} else {
		r.Mode = "review"
		r.ActiveArtifact = &ev
	}
	r.History = append(r.History, ev)
	a.emit(ev)

	resp := map[string]interface{}{
		"artifact_id": ev.EventID,
		"kind":        ev.Kind,
		"mode":        r.Mode,
		"artifact":    ev,
	}
	return resp, nil
}

func cloneMeta(meta map[string]interface{}) map[string]interface{} {
	if len(meta) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(meta))
	for k, v := range meta {
		cloned[k] = v
	}
	return cloned
}

func (a *Adapter) CanvasMarkSet(sessionID, markID, artifactID string, intent MarkIntent, markType MarkType, targetKind TargetKind, target map[string]interface{}, comment, author string) (map[string]interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)

	if artifactID == "" {
		if r.ActiveArtifact == nil {
			return nil, errors.New("artifact_id required when no active artifact")
		}
		artifactID = r.ActiveArtifact.EventID
	}
	if markID == "" {
		markID = newID()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	m, exists := r.Marks[markID]
	oldArtifactID := ""
	oldIntent := MarkIntent("")
	if !exists {
		m = &Mark{MarkID: markID, CreatedAt: now, State: "active"}
		r.Marks[markID] = m
	} else {
		oldArtifactID = m.ArtifactID
		oldIntent = m.Intent
	}
	m.SessionID = sessionID
	m.ArtifactID = artifactID
	m.Intent = intent
	m.Type = markType
	m.TargetKind = targetKind
	m.Target = target
	m.Comment = comment
	m.Author = author
	m.UpdatedAt = now

	if oldIntent == IntentDraft && (intent != IntentDraft || oldArtifactID != artifactID) {
		if r.DraftByArtifactID[oldArtifactID] == markID {
			delete(r.DraftByArtifactID, oldArtifactID)
		}
	}
	if intent == IntentDraft {
		r.DraftByArtifactID[artifactID] = markID
	} else {
		removeDraftIndexForMarkLocked(r, markID)
	}
	pruneDraftIndexLocked(r)

	return map[string]interface{}{"mark": m}, nil
}

func (a *Adapter) CanvasMarkDelete(sessionID, markID string) (map[string]interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)
	if !deleteMarkLocked(r, markID) {
		return nil, fmt.Errorf("mark not found: %s", markID)
	}
	return map[string]interface{}{"deleted": true, "mark_id": markID}, nil
}

func (a *Adapter) CanvasMarksList(sessionID, artifactID string, intent MarkIntent, limit int) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r := a.sessionForRead(sessionID)
	out := make([]*Mark, 0, len(r.Marks))
	for _, m := range r.Marks {
		if artifactID != "" && m.ArtifactID != artifactID {
			continue
		}
		if intent != "" && m.Intent != intent {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt == out[j].UpdatedAt {
			return out[i].MarkID < out[j].MarkID
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return map[string]interface{}{"marks": out}
}

func (a *Adapter) CanvasMarkFocus(sessionID, markID string) (map[string]interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)
	if markID != "" {
		if _, ok := r.Marks[markID]; !ok {
			return nil, fmt.Errorf("mark not found: %s", markID)
		}
	}
	r.FocusedMarkID = markID
	var m *Mark
	if markID != "" {
		m = r.Marks[markID]
	}
	return map[string]interface{}{"focused_mark_id": emptyToNil(markID), "focused_mark": m}, nil
}

func emptyToNil(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

func (a *Adapter) CanvasCommit(sessionID, artifactID string, includeDraft bool) (map[string]interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)

	persistent := 0
	for _, m := range r.Marks {
		if artifactID != "" && m.ArtifactID != artifactID {
			continue
		}
		if includeDraft && m.Intent == IntentDraft {
			m.Intent = IntentPersistent
			m.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			removeDraftIndexForMarkLocked(r, m.MarkID)
			persistent++
		}
	}
	pruneDraftIndexLocked(r)
	path, err := a.writeAnnotationsFile(sessionID, r)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"session_id":              sessionID,
		"artifact_id":             emptyToNil(artifactID),
		"converted_to_persistent": persistent,
		"persistent_count":        countPersistent(r, artifactID),
		"sidecar_path":            path,
		"pdf_annotations_written": 0,
	}, nil
}

func countPersistent(r *SessionRecord, artifactID string) int {
	t := 0
	for _, m := range r.Marks {
		if artifactID != "" && m.ArtifactID != artifactID {
			continue
		}
		if m.Intent == IntentPersistent {
			t++
		}
	}
	return t
}

func (a *Adapter) writeAnnotationsFile(sessionID string, r *SessionRecord) (string, error) {
	path := a.annotationsPath(sessionID)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	marks := make([]*Mark, 0, len(r.Marks))
	for _, m := range r.Marks {
		marks = append(marks, m)
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i].MarkID < marks[j].MarkID })
	payload := map[string]interface{}{
		"session_id": sessionID,
		"marks":      marks,
		"written_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	buf, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func hex8(b []byte) string {
	s := fmt.Sprintf("%x", b)
	if len(s) > 16 {
		return s[:16]
	}
	return s
}

func (a *Adapter) CanvasStatus(sessionID string) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r := a.sessionForRead(sessionID)
	active := map[string]interface{}(nil)
	if r.ActiveArtifact != nil {
		buf, _ := json.Marshal(r.ActiveArtifact)
		_ = json.Unmarshal(buf, &active)
	}
	return map[string]interface{}{
		"mode":                 r.Mode,
		"active":               r.Opened,
		"active_artifact_id":   activeArtifactID(r),
		"active_artifact_kind": activeArtifactKind(r),
		"active_artifact":      active,
		"history_size":         len(r.History),
		"marks_total":          len(r.Marks),
		"marks_for_active":     countMarksForActive(r),
		"focused_mark_id":      emptyToNil(r.FocusedMarkID),
		"focused_mark":         focusedMark(r),
		"headless":             a.headless,
		"canvas_process_alive": a.canvasProcessLive,
		"canvas_launch_error":  a.launchErrOrNil(),
		"selection":            r.Selection,
	}
}

func focusedMark(r *SessionRecord) interface{} {
	if r.FocusedMarkID == "" {
		return nil
	}
	m, ok := r.Marks[r.FocusedMarkID]
	if !ok {
		return nil
	}
	return m
}

func countMarksForActive(r *SessionRecord) int {
	if r.ActiveArtifact == nil {
		return 0
	}
	n := 0
	for _, m := range r.Marks {
		if m.ArtifactID == r.ActiveArtifact.EventID {
			n++
		}
	}
	return n
}

func (a *Adapter) CanvasHistory(sessionID string, limit int) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r := a.sessionForRead(sessionID)
	if limit <= 0 || limit > len(r.History) {
		limit = len(r.History)
	}
	start := len(r.History) - limit
	if start < 0 {
		start = 0
	}
	h := append([]Event(nil), r.History[start:]...)
	return map[string]interface{}{"history": h}
}

func (a *Adapter) CanvasSelection(sessionID string) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r := a.sessionForRead(sessionID)
	return map[string]interface{}{"selection": r.Selection}
}

func (a *Adapter) HandleFeedback(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return
	}
	kind, _ := payload["kind"].(string)
	sessionID, _ := payload["session_id"].(string)
	if sessionID == "" {
		sessionID = "local"
	}

	switch kind {
	case "text_selection":
		a.handleTextSelection(sessionID, payload)
	case "mark_set":
		a.handleMarkSetFeedback(sessionID, payload)
	case "mark_commit":
		_, _ = a.CanvasCommit(sessionID, asString(payload["artifact_id"]), asBool(payload["include_draft"], true))
	case "mark_clear_draft":
		a.clearDraft(sessionID, asString(payload["artifact_id"]))
	}
}

func (a *Adapter) handleTextSelection(sessionID string, payload map[string]interface{}) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)
	r.Selection = Selection{
		HasSelection: true,
		LineStart:    asInt(payload["line_start"]),
		LineEnd:      asInt(payload["line_end"]),
		StartOffset:  asInt(payload["start_offset"]),
		EndOffset:    asInt(payload["end_offset"]),
		Text:         asString(payload["text"]),
	}
}

func (a *Adapter) handleMarkSetFeedback(sessionID string, payload map[string]interface{}) {
	target, _ := payload["target"].(map[string]interface{})
	_, _ = a.CanvasMarkSet(
		sessionID,
		asString(payload["mark_id"]),
		asString(payload["artifact_id"]),
		MarkIntent(asString(payload["intent"])),
		MarkType(asString(payload["type"])),
		TargetKind(asString(payload["target_kind"])),
		target,
		asString(payload["comment"]),
		asString(payload["author"]),
	)
}

func (a *Adapter) clearDraft(sessionID, artifactID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)
	var toDelete []string
	for id, m := range r.Marks {
		if m.Intent != IntentDraft {
			continue
		}
		if artifactID != "" && m.ArtifactID != artifactID {
			continue
		}
		toDelete = append(toDelete, id)
	}
	for _, id := range toDelete {
		deleteMarkLocked(r, id)
	}
	if artifactID != "" {
		if markID, ok := r.DraftByArtifactID[artifactID]; ok {
			if _, exists := r.Marks[markID]; !exists {
				delete(r.DraftByArtifactID, artifactID)
			}
		}
	}
	pruneDraftIndexLocked(r)
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func asInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return 0
	}
}

func asBool(v interface{}, d bool) bool {
	b, ok := v.(bool)
	if !ok {
		return d
	}
	return b
}

func (a *Adapter) SetProcessState(headless, live bool, launchErr string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.headless = headless
	a.canvasProcessLive = live
	a.launchErr = launchErr
}

func (a *Adapter) ListSessions() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.listSessions()
}

func (a *Adapter) annotationsPath(sessionID string) string {
	h := sha256.Sum256([]byte(sessionID))
	name := hex8(h[:]) + ".annotations.json"
	return filepath.Join(a.projectDir, ".tabula", "artifacts", "annotations", name)
}

func (a *Adapter) loadPersistedAnnotationsLocked(sessionID string, r *SessionRecord) {
	path := a.annotationsPath(sessionID)
	buf, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var payload struct {
		Marks []*Mark `json:"marks"`
	}
	if err := json.Unmarshal(buf, &payload); err != nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, m := range payload.Marks {
		if m == nil || strings.TrimSpace(m.MarkID) == "" {
			continue
		}
		if m.SessionID == "" {
			m.SessionID = sessionID
		}
		if m.State == "" {
			m.State = "active"
		}
		if m.CreatedAt == "" {
			m.CreatedAt = now
		}
		if m.UpdatedAt == "" {
			m.UpdatedAt = m.CreatedAt
		}
		r.Marks[m.MarkID] = m
		if m.Intent == IntentDraft && m.ArtifactID != "" {
			r.DraftByArtifactID[m.ArtifactID] = m.MarkID
		}
	}
	pruneDraftIndexLocked(r)
}

func removeDraftIndexForMarkLocked(r *SessionRecord, markID string) {
	for artifactID, draftID := range r.DraftByArtifactID {
		if draftID == markID {
			delete(r.DraftByArtifactID, artifactID)
		}
	}
}

func pruneDraftIndexLocked(r *SessionRecord) {
	for artifactID, draftID := range r.DraftByArtifactID {
		m, ok := r.Marks[draftID]
		if !ok || m.Intent != IntentDraft || m.ArtifactID != artifactID {
			delete(r.DraftByArtifactID, artifactID)
		}
	}
}

func deleteMarkLocked(r *SessionRecord, markID string) bool {
	if _, ok := r.Marks[markID]; !ok {
		return false
	}
	delete(r.Marks, markID)
	removeDraftIndexForMarkLocked(r, markID)
	if r.FocusedMarkID == markID {
		r.FocusedMarkID = ""
	}
	return true
}
