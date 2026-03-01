package store

import (
	"testing"
)

func TestParticipantSessionLifecycle(t *testing.T) {
	s := newTestStore(t)

	sess, err := s.AddParticipantSession("proj-1", `{"language":"en"}`)
	if err != nil {
		t.Fatalf("add session: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("session id is empty")
	}
	if sess.ProjectKey != "proj-1" {
		t.Fatalf("project key = %q, want proj-1", sess.ProjectKey)
	}
	if sess.StartedAt == 0 {
		t.Fatal("started_at is zero")
	}
	if sess.EndedAt != 0 {
		t.Fatalf("ended_at = %d, want 0", sess.EndedAt)
	}

	got, err := s.GetParticipantSession(sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("get returned id = %q, want %q", got.ID, sess.ID)
	}

	sess2, err := s.AddParticipantSession("proj-1", "{}")
	if err != nil {
		t.Fatalf("add second session: %v", err)
	}

	list, err := s.ListParticipantSessions("proj-1")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list length = %d, want 2", len(list))
	}

	allList, err := s.ListParticipantSessions("")
	if err != nil {
		t.Fatalf("list all sessions: %v", err)
	}
	if len(allList) != 2 {
		t.Fatalf("all list length = %d, want 2", len(allList))
	}

	if err := s.EndParticipantSession(sess2.ID); err != nil {
		t.Fatalf("end session: %v", err)
	}
	ended, err := s.GetParticipantSession(sess2.ID)
	if err != nil {
		t.Fatalf("get ended session: %v", err)
	}
	if ended.EndedAt == 0 {
		t.Fatal("ended_at should be non-zero after end")
	}
}

func TestParticipantSegmentCRUD(t *testing.T) {
	s := newTestStore(t)

	sess, err := s.AddParticipantSession("proj-seg", "{}")
	if err != nil {
		t.Fatalf("add session: %v", err)
	}

	seg1, err := s.AddParticipantSegment(ParticipantSegment{
		SessionID: sess.ID,
		StartTS:   100,
		EndTS:     110,
		Speaker:   "user-a",
		Text:      "hello meeting",
		Model:     "whisper-1",
		LatencyMS: 200,
	})
	if err != nil {
		t.Fatalf("add segment: %v", err)
	}
	if seg1.ID == 0 {
		t.Fatal("segment id is zero")
	}
	if seg1.Status != "final" {
		t.Fatalf("status = %q, want final", seg1.Status)
	}

	seg2, err := s.AddParticipantSegment(ParticipantSegment{
		SessionID: sess.ID,
		StartTS:   200,
		EndTS:     210,
		Speaker:   "user-b",
		Text:      "world response",
	})
	if err != nil {
		t.Fatalf("add second segment: %v", err)
	}

	all, err := s.ListParticipantSegments(sess.ID, 0, 0)
	if err != nil {
		t.Fatalf("list segments: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("segment count = %d, want 2", len(all))
	}

	filtered, err := s.ListParticipantSegments(sess.ID, 150, 0)
	if err != nil {
		t.Fatalf("list segments with from: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(filtered))
	}
	if filtered[0].ID != seg2.ID {
		t.Fatalf("filtered segment id = %d, want %d", filtered[0].ID, seg2.ID)
	}

	results, err := s.SearchParticipantSegments(sess.ID, "meeting")
	if err != nil {
		t.Fatalf("search segments: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("search count = %d, want 1", len(results))
	}
	if results[0].Text != "hello meeting" {
		t.Fatalf("search text = %q", results[0].Text)
	}
}

func TestParticipantEventCRUD(t *testing.T) {
	s := newTestStore(t)

	sess, err := s.AddParticipantSession("proj-ev", "{}")
	if err != nil {
		t.Fatalf("add session: %v", err)
	}

	if err := s.AddParticipantEvent(sess.ID, 0, "session_started", `{"reason":"manual"}`); err != nil {
		t.Fatalf("add event: %v", err)
	}
	if err := s.AddParticipantEvent(sess.ID, 1, "segment_committed", `{"seg_id":1}`); err != nil {
		t.Fatalf("add event 2: %v", err)
	}

	events, err := s.ListParticipantEvents(sess.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].EventType != "session_started" {
		t.Fatalf("event type = %q, want session_started", events[0].EventType)
	}
}

func TestParticipantRoomStateUpsert(t *testing.T) {
	s := newTestStore(t)

	sess, err := s.AddParticipantSession("proj-room", "{}")
	if err != nil {
		t.Fatalf("add session: %v", err)
	}

	if err := s.UpsertParticipantRoomState(sess.ID, "initial summary", `["entity-a"]`, `["topic-1"]`); err != nil {
		t.Fatalf("upsert room state: %v", err)
	}

	state, err := s.GetParticipantRoomState(sess.ID)
	if err != nil {
		t.Fatalf("get room state: %v", err)
	}
	if state.SummaryText != "initial summary" {
		t.Fatalf("summary = %q", state.SummaryText)
	}
	if state.EntitiesJSON != `["entity-a"]` {
		t.Fatalf("entities = %q", state.EntitiesJSON)
	}

	if err := s.UpsertParticipantRoomState(sess.ID, "updated summary", `["entity-b"]`, `["topic-2"]`); err != nil {
		t.Fatalf("upsert overwrite: %v", err)
	}

	state2, err := s.GetParticipantRoomState(sess.ID)
	if err != nil {
		t.Fatalf("get updated room state: %v", err)
	}
	if state2.SummaryText != "updated summary" {
		t.Fatalf("updated summary = %q", state2.SummaryText)
	}
	if state2.ID != state.ID {
		t.Fatalf("upsert should keep same id: got %d, want %d", state2.ID, state.ID)
	}
}

func TestParticipantSessionValidation(t *testing.T) {
	s := newTestStore(t)

	_, err := s.AddParticipantSession("", "{}")
	if err == nil {
		t.Fatal("expected error for empty project key")
	}

	_, err = s.AddParticipantSegment(ParticipantSegment{SessionID: ""})
	if err == nil {
		t.Fatal("expected error for empty session id in segment")
	}

	err = s.AddParticipantEvent("", 0, "test", "{}")
	if err == nil {
		t.Fatal("expected error for empty session id in event")
	}

	err = s.UpsertParticipantRoomState("", "summary", "[]", "[]")
	if err == nil {
		t.Fatal("expected error for empty session id in room state")
	}
}
