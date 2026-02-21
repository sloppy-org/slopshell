package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestClassifyDraftReplyIntent(t *testing.T) {
	cases := []struct {
		name            string
		transcript      string
		wantIntent      draftReplyIntent
		wantFallback    bool
		wantReason      string
		wantFallbackPol draftReplyIntent
	}{
		{
			name:            "dictation style transcript is routed to dictation branch",
			transcript:      "Hi Bob,\n\nThanks for the update. I can send the draft tomorrow.\n\nBest,\nAlice",
			wantIntent:      draftReplyIntentDictation,
			wantFallback:    false,
			wantReason:      "dictation_signals",
			wantFallbackPol: draftReplyIntentPrompt,
		},
		{
			name:            "instruction transcript is routed to prompt branch",
			transcript:      "Draft a reply saying we can ship next week and keep it concise.",
			wantIntent:      draftReplyIntentPrompt,
			wantFallback:    false,
			wantReason:      "instruction_signals",
			wantFallbackPol: draftReplyIntentPrompt,
		},
		{
			name:            "ambiguous transcript uses deterministic prompt fallback",
			transcript:      "Tomorrow works.",
			wantIntent:      draftReplyIntentPrompt,
			wantFallback:    true,
			wantReason:      "ambiguous_signals",
			wantFallbackPol: draftReplyIntentPrompt,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := classifyDraftReplyIntent(tc.transcript)
			if got.Intent != tc.wantIntent {
				t.Fatalf("expected intent=%q, got %q", tc.wantIntent, got.Intent)
			}
			if got.FallbackApplied != tc.wantFallback {
				t.Fatalf("expected fallback_applied=%t, got %t", tc.wantFallback, got.FallbackApplied)
			}
			if got.Reason != tc.wantReason {
				t.Fatalf("expected reason=%q, got %q", tc.wantReason, got.Reason)
			}
			if got.FallbackPolicy != tc.wantFallbackPol {
				t.Fatalf("expected fallback_policy=%q, got %q", tc.wantFallbackPol, got.FallbackPolicy)
			}
		})
	}
}

func TestMailDraftIntentRoute(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/draft-intent", map[string]any{
		"transcript": "Draft a reply saying thank you and keep it short.",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := payload["intent"]; got != string(draftReplyIntentPrompt) {
		t.Fatalf("expected intent=prompt, got %#v", got)
	}
	if got := payload["fallback_applied"]; got != false {
		t.Fatalf("expected fallback_applied=false, got %#v", got)
	}
	if got := payload["fallback_policy"]; got != string(draftReplyIntentPrompt) {
		t.Fatalf("expected fallback_policy=prompt, got %#v", got)
	}
}

func TestMailDraftIntentRequiresTranscript(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/mail/draft-intent", map[string]any{
		"transcript": "   ",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
