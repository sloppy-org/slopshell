package web

import "testing"

func TestConferenceReplaySummaryStaysStable(t *testing.T) {
	summary := buildConferenceReplaySummary("balanced")
	if summary.CorpusVersion != "meeting-v1" {
		t.Fatalf("corpus_version = %q, want meeting-v1", summary.CorpusVersion)
	}
	if summary.Profile != "balanced" {
		t.Fatalf("profile = %q, want balanced", summary.Profile)
	}
	if summary.Metrics.Cases != 4 {
		t.Fatalf("cases = %d, want 4", summary.Metrics.Cases)
	}
	if summary.Metrics.Passing != 4 {
		t.Fatalf("passing = %d, want 4", summary.Metrics.Passing)
	}
	if summary.Metrics.FalseBargeIns != 0 {
		t.Fatalf("false_barge_ins = %d, want 0", summary.Metrics.FalseBargeIns)
	}
	if summary.Metrics.MissedSpeakerStarts != 0 {
		t.Fatalf("missed_speaker_starts = %d, want 0", summary.Metrics.MissedSpeakerStarts)
	}
	if summary.Metrics.OverlapYields != 2 {
		t.Fatalf("overlap_yields = %d, want 2", summary.Metrics.OverlapYields)
	}
	if summary.Metrics.ResumeLatencyMSAvg != 280 {
		t.Fatalf("resume_latency_ms_avg = %d, want 280", summary.Metrics.ResumeLatencyMSAvg)
	}
	if summary.Metrics.ResumeLatencyMSMax != 280 {
		t.Fatalf("resume_latency_ms_max = %d, want 280", summary.Metrics.ResumeLatencyMSMax)
	}
}

func TestConferenceDecisionSummaryExplainsMissedPickupAndOverlapGuard(t *testing.T) {
	summary := summarizeConferenceDecision(
		companionDirectedSpeechGate{
			Decision: companionGateDecisionUncertain,
			Reason:   "request_without_assistant_name",
		},
		companionInteractionPolicyState{
			Decision: companionInteractionDecisionSuppressed,
			Reason:   "overlap_other_speaker",
		},
	)
	if summary.Pickup == "" || summary.Overlap == "" {
		t.Fatalf("summary = %#v, want non-empty pickup and overlap text", summary)
	}
	if summary.Pickup != "Not picked up because the latest request did not address Tabura and did not match the tracked speaker." {
		t.Fatalf("pickup = %q", summary.Pickup)
	}
	if summary.Overlap != "Wrong-speaker overlap is suppressed while another speaker owns the pending turn." {
		t.Fatalf("overlap = %q", summary.Overlap)
	}
}
