package web

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/krystophny/tabura/internal/store"
)

//go:embed testdata/conference_replay_corpus.json
var conferenceReplayCorpusJSON []byte

type conferenceReplaySummary struct {
	CorpusVersion string                      `json:"corpus_version"`
	Profile       string                      `json:"profile"`
	Metrics       conferenceReplayMetrics     `json:"metrics"`
	Cases         []conferenceReplayCaseState `json:"cases,omitempty"`
}

type conferenceReplayMetrics struct {
	Cases               int   `json:"cases"`
	Passing             int   `json:"passing"`
	FalseBargeIns       int   `json:"false_barge_ins"`
	MissedSpeakerStarts int   `json:"missed_speaker_starts"`
	OverlapYields       int   `json:"overlap_yields"`
	ResumeLatencyMSAvg  int64 `json:"resume_latency_ms_avg"`
	ResumeLatencyMSMax  int64 `json:"resume_latency_ms_max"`
}

type conferenceReplayCaseState struct {
	Name                string `json:"name"`
	Triggered           bool   `json:"triggered"`
	TriggerExpected     bool   `json:"trigger_expected"`
	GateDecision        string `json:"gate_decision"`
	GateReason          string `json:"gate_reason"`
	InteractionDecision string `json:"interaction_decision"`
	InteractionReason   string `json:"interaction_reason"`
	FalseBargeIn        bool   `json:"false_barge_in,omitempty"`
	MissedSpeakerStart  bool   `json:"missed_speaker_start,omitempty"`
	OverlapYield        bool   `json:"overlap_yield,omitempty"`
	ResumeLatencyMS     int64  `json:"resume_latency_ms,omitempty"`
	Pass                bool   `json:"pass"`
}

type conferenceDecisionSummary struct {
	Pickup  string `json:"pickup"`
	Overlap string `json:"overlap"`
}

type conferenceReplayCorpus struct {
	Version string                 `json:"version"`
	Cases   []conferenceReplayCase `json:"cases"`
}

type conferenceReplayCase struct {
	Name           string                     `json:"name"`
	Session        store.ParticipantSession   `json:"session"`
	Segments       []store.ParticipantSegment `json:"segments"`
	Events         []store.ParticipantEvent   `json:"events"`
	PendingSinceMS int64                      `json:"pending_since_ms,omitempty"`
	EvaluatedAtMS  int64                      `json:"evaluated_at_ms,omitempty"`
	Expect         conferenceReplayExpect     `json:"expect"`
}

type conferenceReplayExpect struct {
	TriggerExpected     bool   `json:"trigger_expected"`
	GateDecision        string `json:"gate_decision"`
	GateReason          string `json:"gate_reason"`
	InteractionDecision string `json:"interaction_decision"`
	InteractionReason   string `json:"interaction_reason"`
	Overlap             bool   `json:"overlap"`
}

var (
	conferenceReplayCorpusOnce sync.Once
	conferenceReplayLoaded     conferenceReplayCorpus
	conferenceReplayLoadErr    error
)

func loadConferenceReplayCorpus() (conferenceReplayCorpus, error) {
	conferenceReplayCorpusOnce.Do(func() {
		if err := json.Unmarshal(conferenceReplayCorpusJSON, &conferenceReplayLoaded); err != nil {
			conferenceReplayLoadErr = fmt.Errorf("decode conference replay corpus: %w", err)
			return
		}
		if strings.TrimSpace(conferenceReplayLoaded.Version) == "" {
			conferenceReplayLoadErr = fmt.Errorf("conference replay corpus version is required")
			return
		}
	})
	return conferenceReplayLoaded, conferenceReplayLoadErr
}

func buildConferenceReplaySummary(profile string) conferenceReplaySummary {
	corpus, err := loadConferenceReplayCorpus()
	if err != nil {
		return conferenceReplaySummary{
			CorpusVersion: "invalid",
			Profile:       normalizeRuntimeTurnPolicyProfile(profile),
		}
	}
	summary := conferenceReplaySummary{
		CorpusVersion: corpus.Version,
		Profile:       normalizeRuntimeTurnPolicyProfile(profile),
		Metrics: conferenceReplayMetrics{
			Cases: len(corpus.Cases),
		},
		Cases: make([]conferenceReplayCaseState, 0, len(corpus.Cases)),
	}
	var resumeLatencyTotal int64
	var resumeLatencyCount int64
	for _, replayCase := range corpus.Cases {
		cfg := defaultCompanionConfig()
		cfg.CompanionEnabled = true
		cfg.DirectedSpeechGateEnabled = true
		session := replayCase.Session
		if strings.TrimSpace(session.ID) == "" {
			session.ID = replayCase.Name
		}
		if strings.TrimSpace(session.WorkspacePath) == "" {
			session.WorkspacePath = "conference-replay"
		}
		gate := evaluateCompanionDirectedSpeechGate(cfg, &session, replayCase.Segments, replayCase.Events)
		policy := evaluateCompanionInteractionPolicy(cfg, &session, replayCase.Segments, replayCase.Events)
		triggered := companionDecisionTriggersAssistant(policy.Decision)
		falseBargeIn := triggered && !replayCase.Expect.TriggerExpected
		missedSpeakerStart := !triggered && replayCase.Expect.TriggerExpected
		overlapYield := replayCase.Expect.Overlap && (policy.Decision == companionInteractionDecisionInterrupt || policy.Decision == companionInteractionDecisionSuppressed)
		resumeLatencyMS := int64(0)
		if replayCase.PendingSinceMS > 0 && replayCase.EvaluatedAtMS > replayCase.PendingSinceMS && triggered {
			resumeLatencyMS = replayCase.EvaluatedAtMS - replayCase.PendingSinceMS
			resumeLatencyTotal += resumeLatencyMS
			resumeLatencyCount++
			if resumeLatencyMS > summary.Metrics.ResumeLatencyMSMax {
				summary.Metrics.ResumeLatencyMSMax = resumeLatencyMS
			}
		}
		pass := triggered == replayCase.Expect.TriggerExpected &&
			gate.Decision == replayCase.Expect.GateDecision &&
			gate.Reason == replayCase.Expect.GateReason &&
			policy.Decision == replayCase.Expect.InteractionDecision &&
			policy.Reason == replayCase.Expect.InteractionReason
		if pass {
			summary.Metrics.Passing++
		}
		if falseBargeIn {
			summary.Metrics.FalseBargeIns++
		}
		if missedSpeakerStart {
			summary.Metrics.MissedSpeakerStarts++
		}
		if overlapYield {
			summary.Metrics.OverlapYields++
		}
		summary.Cases = append(summary.Cases, conferenceReplayCaseState{
			Name:                replayCase.Name,
			Triggered:           triggered,
			TriggerExpected:     replayCase.Expect.TriggerExpected,
			GateDecision:        gate.Decision,
			GateReason:          gate.Reason,
			InteractionDecision: policy.Decision,
			InteractionReason:   policy.Reason,
			FalseBargeIn:        falseBargeIn,
			MissedSpeakerStart:  missedSpeakerStart,
			OverlapYield:        overlapYield,
			ResumeLatencyMS:     resumeLatencyMS,
			Pass:                pass,
		})
	}
	if resumeLatencyCount > 0 {
		summary.Metrics.ResumeLatencyMSAvg = resumeLatencyTotal / resumeLatencyCount
	}
	return summary
}

func companionDecisionTriggersAssistant(decision string) bool {
	switch strings.TrimSpace(decision) {
	case companionInteractionDecisionRespond, companionInteractionDecisionInterrupt:
		return true
	default:
		return false
	}
}

func summarizeConferenceDecision(gate companionDirectedSpeechGate, policy companionInteractionPolicyState) conferenceDecisionSummary {
	return conferenceDecisionSummary{
		Pickup:  summarizeConferencePickup(gate),
		Overlap: summarizeConferenceOverlap(policy),
	}
}

func summarizeConferencePickup(gate companionDirectedSpeechGate) string {
	switch strings.TrimSpace(gate.Decision) {
	case companionGateDecisionDirect:
		if gate.Reason == "target_speaker_follow_up" {
			return "Picked up because the tracked speaker continued the request."
		}
		return "Picked up because the latest utterance addressed Tabura directly."
	case companionGateDecisionUncertain:
		if gate.Reason == "request_without_assistant_name" {
			return "Not picked up because the latest request did not address Tabura and did not match the tracked speaker."
		}
		return "Not picked up because there is not enough meeting transcript context yet."
	case companionGateDecisionNotAddressed:
		return "Not picked up because the latest utterance looked like room speech, not an assistant request."
	default:
		if gate.Reason == "gate_disabled" {
			return "Meeting pickup is disabled because the directed-speech gate is off."
		}
		if gate.Reason == "companion_disabled" {
			return "Meeting pickup is disabled because meeting capture is off."
		}
		return "Meeting pickup is currently disabled."
	}
}

func summarizeConferenceOverlap(policy companionInteractionPolicyState) string {
	switch strings.TrimSpace(policy.Decision) {
	case companionInteractionDecisionInterrupt:
		if policy.Reason == "target_speaker_overlap" {
			return "The pending response will yield because the tracked speaker kept talking and replaced the earlier request."
		}
		return "The pending response will yield because a new addressed request arrived during playback."
	case companionInteractionDecisionSuppressed:
		if policy.Reason == "overlap_other_speaker" {
			return "Wrong-speaker overlap is suppressed while another speaker owns the pending turn."
		}
		return "The overlap is suppressed instead of triggering a new assistant turn."
	case companionInteractionDecisionRespond:
		return "No overlap guard is blocking the next assistant turn."
	case companionInteractionDecisionCooldown:
		return "The previous response is still in cooldown, so the meeting turn is deferred."
	default:
		return "No overlap handoff is active yet."
	}
}
