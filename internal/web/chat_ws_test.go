package web

import "testing"

func TestTTSSequenceOrdering(t *testing.T) {
	conn := newChatWSConn(nil)

	seq0 := conn.reserveTTSSeq()
	seq1 := conn.reserveTTSSeq()
	seq2 := conn.reserveTTSSeq()

	if got := conn.completeTTSSeq(seq1, []byte("b"), ""); len(got) != 0 {
		t.Fatalf("completeTTSSeq(seq1) emitted %d items, want 0", len(got))
	}
	got := conn.completeTTSSeq(seq0, []byte("a"), "")
	if len(got) != 2 {
		t.Fatalf("completeTTSSeq(seq0) emitted %d items, want 2", len(got))
	}
	if got[0].seq != seq0 || string(got[0].audio) != "a" {
		t.Fatalf("first emit = seq %d audio %q, want seq %d audio %q", got[0].seq, string(got[0].audio), seq0, "a")
	}
	if got[1].seq != seq1 || string(got[1].audio) != "b" {
		t.Fatalf("second emit = seq %d audio %q, want seq %d audio %q", got[1].seq, string(got[1].audio), seq1, "b")
	}

	got = conn.completeTTSSeq(seq2, []byte("c"), "")
	if len(got) != 1 || got[0].seq != seq2 || string(got[0].audio) != "c" {
		t.Fatalf("final emit = %#v, want seq %d audio %q", got, seq2, "c")
	}
}

func TestTTSSequenceErrorDoesNotBlock(t *testing.T) {
	conn := newChatWSConn(nil)

	seq0 := conn.reserveTTSSeq()
	seq1 := conn.reserveTTSSeq()

	if got := conn.completeTTSSeq(seq1, []byte("later"), ""); len(got) != 0 {
		t.Fatalf("completeTTSSeq(seq1) emitted %d items, want 0", len(got))
	}
	got := conn.completeTTSSeq(seq0, nil, "boom")
	if len(got) != 2 {
		t.Fatalf("completeTTSSeq(seq0) emitted %d items, want 2", len(got))
	}
	if got[0].seq != seq0 || got[0].err != "boom" {
		t.Fatalf("first emit = seq %d err %q, want seq %d err %q", got[0].seq, got[0].err, seq0, "boom")
	}
	if got[1].seq != seq1 || string(got[1].audio) != "later" {
		t.Fatalf("second emit = seq %d audio %q, want seq %d audio %q", got[1].seq, string(got[1].audio), seq1, "later")
	}
}
