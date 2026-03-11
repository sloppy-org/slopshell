package web

import (
	"errors"
	"net/http"
	"strings"
)

const appStateLivePolicyKey = "runtime.live_policy"

type LivePolicy string

const (
	LivePolicyDialogue LivePolicy = "dialogue"
	LivePolicyMeeting  LivePolicy = "meeting"
)

type LivePolicyConfig struct {
	AssumeAddressed     bool `json:"assume_addressed"`
	ProactiveSpeech     bool `json:"proactive_speech"`
	CaptureDecisions    bool `json:"capture_decisions"`
	CaptureActionItems  bool `json:"capture_action_items"`
	InterruptionAllowed bool `json:"interruption_allowed"`
}

func (c LivePolicyConfig) RequiresExplicitAddress() bool {
	return !c.AssumeAddressed
}

func (c LivePolicyConfig) CapturesMeetingNotes() bool {
	return c.CaptureDecisions || c.CaptureActionItems
}

type livePolicyRequest struct {
	Policy string `json:"policy"`
}

func parseLivePolicy(raw string) (LivePolicy, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(LivePolicyDialogue):
		return LivePolicyDialogue, true
	case string(LivePolicyMeeting):
		return LivePolicyMeeting, true
	default:
		return "", false
	}
}

func normalizeLivePolicy(raw string) LivePolicy {
	policy, ok := parseLivePolicy(raw)
	if !ok {
		return LivePolicyDialogue
	}
	return policy
}

func (p LivePolicy) String() string {
	return string(normalizeLivePolicy(string(p)))
}

func (p LivePolicy) Config() LivePolicyConfig {
	switch normalizeLivePolicy(string(p)) {
	case LivePolicyMeeting:
		return LivePolicyConfig{
			AssumeAddressed:     false,
			ProactiveSpeech:     false,
			CaptureDecisions:    true,
			CaptureActionItems:  true,
			InterruptionAllowed: false,
		}
	default:
		return LivePolicyConfig{
			AssumeAddressed:     true,
			ProactiveSpeech:     true,
			CaptureDecisions:    false,
			CaptureActionItems:  false,
			InterruptionAllowed: true,
		}
	}
}

func (p LivePolicy) RequiresExplicitAddress() bool {
	return p.Config().RequiresExplicitAddress()
}

func (p LivePolicy) CapturesMeetingNotes() bool {
	return p.Config().CapturesMeetingNotes()
}

func (p LivePolicy) UsesParticipantCapture() bool {
	return p.RequiresExplicitAddress() || p.CapturesMeetingNotes()
}

func (a *App) initializeLivePolicy() error {
	if a == nil || a.store == nil {
		return nil
	}
	raw, err := a.store.AppState(appStateLivePolicyKey)
	if err != nil {
		return err
	}
	policy := normalizeLivePolicy(raw)
	if strings.TrimSpace(raw) == "" {
		if err := a.store.SetAppState(appStateLivePolicyKey, policy.String()); err != nil {
			return err
		}
	}
	a.livePolicyMu.Lock()
	a.livePolicy = policy
	a.livePolicyMu.Unlock()
	return nil
}

func (a *App) LivePolicy() LivePolicy {
	if a == nil {
		return LivePolicyDialogue
	}
	a.livePolicyMu.Lock()
	defer a.livePolicyMu.Unlock()
	if a.livePolicy == "" {
		a.livePolicy = LivePolicyDialogue
	}
	return normalizeLivePolicy(a.livePolicy.String())
}

func (a *App) livePolicyConfig() LivePolicyConfig {
	return a.LivePolicy().Config()
}

func (a *App) setLivePolicy(policy LivePolicy) (bool, error) {
	if a == nil || a.store == nil {
		return false, nil
	}
	normalized := normalizeLivePolicy(policy.String())
	a.livePolicyMu.Lock()
	current := normalizeLivePolicy(a.livePolicy.String())
	if current == normalized {
		a.livePolicyMu.Unlock()
		return false, nil
	}
	if err := a.store.SetAppState(appStateLivePolicyKey, normalized.String()); err != nil {
		a.livePolicyMu.Unlock()
		return false, err
	}
	a.livePolicy = normalized
	a.livePolicyMu.Unlock()
	return true, nil
}

func (a *App) broadcastLivePolicyChanged(policy LivePolicy) {
	if a == nil || a.hub == nil {
		return
	}
	payload := map[string]interface{}{
		"type":   "live_policy_changed",
		"policy": normalizeLivePolicy(policy.String()).String(),
	}
	a.hub.forEachChatConn(func(conn *chatWSConn) {
		_ = conn.writeJSON(payload)
	})
}

func (a *App) handleLivePolicyGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	writeJSON(w, map[string]interface{}{
		"policy": a.LivePolicy().String(),
	})
}

func decodeLivePolicyRequest(r *http.Request) (LivePolicy, error) {
	var req livePolicyRequest
	if err := decodeJSON(r, &req); err != nil {
		return "", err
	}
	if strings.TrimSpace(req.Policy) == "" {
		return "", errors.New("live policy is required")
	}
	policy, ok := parseLivePolicy(req.Policy)
	if !ok {
		return "", errors.New("invalid live policy")
	}
	return policy, nil
}

func (a *App) handleLivePolicyPost(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	policy, err := decodeLivePolicyRequest(r)
	if err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	current := a.LivePolicy()
	changed, err := a.setLivePolicy(policy)
	if err != nil {
		http.Error(w, "failed to save live policy", http.StatusInternalServerError)
		return
	}
	if changed {
		if current.UsesParticipantCapture() && !policy.UsesParticipantCapture() {
			a.disableLiveMeetingCapture()
		}
		a.broadcastLivePolicyChanged(policy)
	}
	writeJSON(w, map[string]interface{}{
		"policy": policy.String(),
	})
}
