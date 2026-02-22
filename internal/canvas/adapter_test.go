package canvas

import (
	"testing"
)

func TestCanvasSessionOpenAndStatus(t *testing.T) {
	a := NewAdapter(t.TempDir(), nil)
	resp := a.CanvasSessionOpen("s1", "review")
	if resp["active"] != true {
		t.Fatalf("expected active=true, got %v", resp["active"])
	}
	if resp["mode"] != "review" {
		t.Fatalf("expected mode=review, got %v", resp["mode"])
	}

	status := a.CanvasStatus("s1")
	if status["mode"] != "review" {
		t.Fatalf("expected status mode=review, got %v", status["mode"])
	}
}

func TestCanvasArtifactShowAndHistory(t *testing.T) {
	a := NewAdapter(t.TempDir(), nil)
	resp, err := a.CanvasArtifactShow("s1", "text", "Doc", "# Hello", "", 0, "", nil)
	if err != nil {
		t.Fatalf("show text: %v", err)
	}
	if resp["kind"] != EventText {
		t.Fatalf("expected kind=text_artifact, got %v", resp["kind"])
	}
	artifactID, _ := resp["artifact_id"].(string)
	if artifactID == "" {
		t.Fatalf("expected artifact_id")
	}

	status := a.CanvasStatus("s1")
	if status["active_artifact_id"] != artifactID {
		t.Fatalf("expected active_artifact_id=%s, got %v", artifactID, status["active_artifact_id"])
	}

	hist := a.CanvasHistory("s1", 10)
	events, _ := hist["history"].([]Event)
	if len(events) != 1 {
		t.Fatalf("expected 1 history event, got %d", len(events))
	}
}

func TestCanvasArtifactClear(t *testing.T) {
	a := NewAdapter(t.TempDir(), nil)
	_, _ = a.CanvasArtifactShow("s1", "text", "Doc", "body", "", 0, "", nil)
	_, err := a.CanvasArtifactShow("s1", "clear", "", "", "", 0, "done", nil)
	if err != nil {
		t.Fatalf("clear: %v", err)
	}

	status := a.CanvasStatus("s1")
	if status["mode"] != "prompt" {
		t.Fatalf("expected mode=prompt after clear, got %v", status["mode"])
	}
	if status["active_artifact_id"] != nil {
		t.Fatalf("expected nil active_artifact_id after clear, got %v", status["active_artifact_id"])
	}
}

func TestHandleFeedbackNoOp(t *testing.T) {
	a := NewAdapter(t.TempDir(), nil)
	a.HandleFeedback(`{"kind":"mark_set","session_id":"s1"}`)
	a.HandleFeedback(`{"kind":"text_selection","session_id":"s1"}`)
	a.HandleFeedback("")
	a.HandleFeedback("{invalid")
}

func TestListSessions(t *testing.T) {
	a := NewAdapter(t.TempDir(), nil)
	a.CanvasSessionOpen("b", "")
	a.CanvasSessionOpen("a", "")
	sessions := a.ListSessions()
	if len(sessions) != 2 || sessions[0] != "a" || sessions[1] != "b" {
		t.Fatalf("expected sorted [a, b], got %v", sessions)
	}
}

