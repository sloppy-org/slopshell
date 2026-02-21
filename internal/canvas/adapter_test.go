package canvas

import (
	"testing"
)

func marksFromResult(t *testing.T, resp map[string]interface{}) []*Mark {
	t.Helper()
	list, ok := resp["marks"].([]*Mark)
	if !ok {
		t.Fatalf("expected []*Mark in marks result, got %T", resp["marks"])
	}
	return list
}

func TestClearDraftRemovesFocusedMarkAndDraftIndex(t *testing.T) {
	a := NewAdapter(t.TempDir(), nil, true)
	sessionID := "s-clear-draft"

	_, err := a.CanvasMarkSet(
		sessionID,
		"draft-a",
		"artifact-a",
		IntentDraft,
		MarkHighlight,
		TargetTextRange,
		map[string]interface{}{"line_start": 1, "line_end": 1},
		"",
		"",
	)
	if err != nil {
		t.Fatalf("set draft mark a: %v", err)
	}
	_, err = a.CanvasMarkSet(
		sessionID,
		"draft-b",
		"artifact-b",
		IntentDraft,
		MarkHighlight,
		TargetTextRange,
		map[string]interface{}{"line_start": 2, "line_end": 2},
		"",
		"",
	)
	if err != nil {
		t.Fatalf("set draft mark b: %v", err)
	}
	if _, err := a.CanvasMarkFocus(sessionID, "draft-a"); err != nil {
		t.Fatalf("focus draft mark: %v", err)
	}

	a.HandleFeedback(`{"kind":"mark_clear_draft","session_id":"s-clear-draft","artifact_id":"artifact-a"}`)

	marks := marksFromResult(t, a.CanvasMarksList(sessionID, "", "", 0))
	if len(marks) != 1 || marks[0].MarkID != "draft-b" {
		t.Fatalf("expected only draft-b to remain, got %#v", marks)
	}
	status := a.CanvasStatus(sessionID)
	if status["focused_mark_id"] != nil {
		t.Fatalf("expected focused_mark_id to clear after draft deletion, got %#v", status["focused_mark_id"])
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	record := a.sessions[sessionID]
	if record == nil {
		t.Fatalf("missing session record")
	}
	if _, ok := record.DraftByArtifactID["artifact-a"]; ok {
		t.Fatalf("artifact-a draft index should be removed")
	}
	if got := record.DraftByArtifactID["artifact-b"]; got != "draft-b" {
		t.Fatalf("expected artifact-b draft index to remain draft-b, got %q", got)
	}
}

func TestCanvasCommitConvertsDraftAndCleansDraftIndex(t *testing.T) {
	a := NewAdapter(t.TempDir(), nil, true)
	sessionID := "s-commit"

	_, err := a.CanvasMarkSet(
		sessionID,
		"draft-a",
		"artifact-a",
		IntentDraft,
		MarkHighlight,
		TargetTextRange,
		map[string]interface{}{"line_start": 1, "line_end": 1},
		"",
		"",
	)
	if err != nil {
		t.Fatalf("set draft mark: %v", err)
	}
	_, err = a.CanvasMarkSet(
		sessionID,
		"persist-a",
		"artifact-a",
		IntentPersistent,
		MarkCommentPoint,
		TargetTextRange,
		map[string]interface{}{"line_start": 1, "line_end": 1},
		"keep",
		"",
	)
	if err != nil {
		t.Fatalf("set persistent mark: %v", err)
	}

	resp, err := a.CanvasCommit(sessionID, "artifact-a", true)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got, _ := resp["converted_to_persistent"].(int); got != 1 {
		t.Fatalf("expected converted_to_persistent=1, got %#v", resp["converted_to_persistent"])
	}
	if got, _ := resp["persistent_count"].(int); got != 2 {
		t.Fatalf("expected persistent_count=2, got %#v", resp["persistent_count"])
	}
	sidecarPath, _ := resp["sidecar_path"].(string)
	if sidecarPath == "" {
		t.Fatalf("expected sidecar path from commit response")
	}

	a.mu.RLock()
	record := a.sessions[sessionID]
	a.mu.RUnlock()
	if record == nil {
		t.Fatalf("missing session record")
	}
	if _, ok := record.DraftByArtifactID["artifact-a"]; ok {
		t.Fatalf("artifact-a should not remain in draft index after commit")
	}

	marks := marksFromResult(t, a.CanvasMarksList(sessionID, "artifact-a", "", 0))
	if len(marks) != 2 {
		t.Fatalf("expected 2 marks for artifact-a, got %d", len(marks))
	}
	for _, m := range marks {
		if m.Intent != IntentPersistent {
			t.Fatalf("expected mark %s to be persistent after commit, got %q", m.MarkID, m.Intent)
		}
	}
}

func TestCanvasSessionOpenLoadsPersistedAnnotations(t *testing.T) {
	tmpDir := t.TempDir()
	const sessionID = "s-reload"

	writer := NewAdapter(tmpDir, nil, true)
	if _, err := writer.CanvasMarkSet(
		sessionID,
		"draft-to-persist",
		"artifact-1",
		IntentDraft,
		MarkHighlight,
		TargetTextRange,
		map[string]interface{}{"line_start": 3, "line_end": 3},
		"",
		"",
	); err != nil {
		t.Fatalf("set draft mark: %v", err)
	}
	if _, err := writer.CanvasMarkSet(
		sessionID,
		"persist-existing",
		"artifact-2",
		IntentPersistent,
		MarkCommentPoint,
		TargetTextRange,
		map[string]interface{}{"line_start": 4, "line_end": 4},
		"saved",
		"",
	); err != nil {
		t.Fatalf("set persistent mark: %v", err)
	}
	if _, err := writer.CanvasCommit(sessionID, "", true); err != nil {
		t.Fatalf("commit persisted annotations: %v", err)
	}

	reloaded := NewAdapter(tmpDir, nil, true)
	openResp := reloaded.CanvasSessionOpen(sessionID, "")
	if got, _ := openResp["marks_total"].(int); got != 2 {
		t.Fatalf("expected marks_total=2 on open, got %#v", openResp["marks_total"])
	}

	marks := marksFromResult(t, reloaded.CanvasMarksList(sessionID, "", "", 0))
	if len(marks) != 2 {
		t.Fatalf("expected 2 reloaded marks, got %d", len(marks))
	}
	byID := map[string]*Mark{}
	for _, m := range marks {
		byID[m.MarkID] = m
	}
	if byID["draft-to-persist"] == nil || byID["persist-existing"] == nil {
		t.Fatalf("reloaded mark ids mismatch: %#v", byID)
	}
	for id, m := range byID {
		if m.Intent != IntentPersistent {
			t.Fatalf("expected reloaded mark %s to be persistent, got %q", id, m.Intent)
		}
	}

	reloaded.mu.RLock()
	defer reloaded.mu.RUnlock()
	record := reloaded.sessions[sessionID]
	if record == nil {
		t.Fatalf("missing reloaded session record")
	}
	if len(record.DraftByArtifactID) != 0 {
		t.Fatalf("expected empty draft index after reload, got %#v", record.DraftByArtifactID)
	}
}

func TestCanvasMarkLifecycleForPDFAndImageArtifacts(t *testing.T) {
	a := NewAdapter(t.TempDir(), nil, true)
	const sessionID = "s-media"

	pdfShown, err := a.CanvasArtifactShow(sessionID, "pdf", "Doc", "", "/tmp/doc.pdf", 0, "", nil)
	if err != nil {
		t.Fatalf("show pdf artifact: %v", err)
	}
	pdfArtifactID, _ := pdfShown["artifact_id"].(string)
	if pdfArtifactID == "" {
		t.Fatalf("expected pdf artifact_id")
	}
	if _, err := a.CanvasMarkSet(
		sessionID,
		"pdf-draft",
		pdfArtifactID,
		IntentDraft,
		MarkHighlight,
		TargetPDFQuads,
		map[string]interface{}{"page": 1, "quads": []interface{}{map[string]interface{}{"x1": 1.0}}},
		"",
		"",
	); err != nil {
		t.Fatalf("set pdf draft mark: %v", err)
	}
	if _, err := a.CanvasCommit(sessionID, pdfArtifactID, true); err != nil {
		t.Fatalf("commit pdf mark: %v", err)
	}
	pdfMarks := marksFromResult(t, a.CanvasMarksList(sessionID, pdfArtifactID, "", 0))
	if len(pdfMarks) != 1 {
		t.Fatalf("expected 1 pdf mark after commit, got %d", len(pdfMarks))
	}
	if pdfMarks[0].Intent != IntentPersistent {
		t.Fatalf("expected committed pdf mark to be persistent, got %q", pdfMarks[0].Intent)
	}

	imageShown, err := a.CanvasArtifactShow(sessionID, "image", "Image", "", "/tmp/image.png", 0, "", nil)
	if err != nil {
		t.Fatalf("show image artifact: %v", err)
	}
	imageArtifactID, _ := imageShown["artifact_id"].(string)
	if imageArtifactID == "" {
		t.Fatalf("expected image artifact_id")
	}
	if _, err := a.CanvasMarkSet(
		sessionID,
		"image-draft",
		imageArtifactID,
		IntentDraft,
		MarkCommentPoint,
		TargetPDFPoint,
		map[string]interface{}{"x": 12.5, "y": 24.0},
		"point",
		"",
	); err != nil {
		t.Fatalf("set image draft mark: %v", err)
	}
	if _, err := a.CanvasMarkDelete(sessionID, "image-draft"); err != nil {
		t.Fatalf("delete image draft mark: %v", err)
	}
	imageMarks := marksFromResult(t, a.CanvasMarksList(sessionID, imageArtifactID, "", 0))
	if len(imageMarks) != 0 {
		t.Fatalf("expected deleted image mark to be gone, got %#v", imageMarks)
	}
}
