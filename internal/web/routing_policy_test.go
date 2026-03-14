package web

import (
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/modelprofile"
)

func TestExplicitTurnModelAliasDefaultsToSpark(t *testing.T) {
	if got := explicitTurnModelAlias("summarize this note"); got != "" {
		t.Fatalf("explicitTurnModelAlias() = %q, want empty", got)
	}
}

func TestExplicitTurnModelAliasOnlyDelegatesOnExplicitGPTRequest(t *testing.T) {
	if got := explicitTurnModelAlias("Please use GPT for this root-cause analysis."); got != modelprofile.AliasGPT {
		t.Fatalf("explicitTurnModelAlias() = %q, want %q", got, modelprofile.AliasGPT)
	}
	if got := explicitTurnModelAlias("Please do a deep dive root cause analysis of this timeout bug."); got != "" {
		t.Fatalf("explicitTurnModelAlias() = %q, want empty without explicit GPT request", got)
	}
}

func TestRouteProfileForRoutingAppliesGPTHigh(t *testing.T) {
	base := appServerModelProfile{
		Alias: modelprofile.AliasSpark,
		Model: modelprofile.ModelForAlias(modelprofile.AliasSpark),
	}
	profile := routeProfileForRouting(modelprofile.AliasGPT, base, modelprofile.ReasoningLow)
	if profile.Alias != modelprofile.AliasGPT {
		t.Fatalf("alias = %q, want %q", profile.Alias, modelprofile.AliasGPT)
	}
	if profile.Model != modelprofile.ModelForAlias(modelprofile.AliasGPT) {
		t.Fatalf("model = %q, want gpt model", profile.Model)
	}
	if got := strings.TrimSpace(strFromAny(profile.TurnParams["effort"])); got != modelprofile.ReasoningHigh {
		t.Fatalf("effort = %q, want %q", got, modelprofile.ReasoningHigh)
	}
}

func TestRouteProfileForRoutingUsesSparkFallbackWithConfiguredEffort(t *testing.T) {
	base := appServerModelProfile{
		Alias: modelprofile.AliasSpark,
		Model: modelprofile.ModelForAlias(modelprofile.AliasSpark),
	}
	profile := routeProfileForRouting("", base, modelprofile.ReasoningMedium)
	if profile.Alias != modelprofile.AliasSpark {
		t.Fatalf("alias = %q, want %q", profile.Alias, modelprofile.AliasSpark)
	}
	if profile.Model != modelprofile.ModelForAlias(modelprofile.AliasSpark) {
		t.Fatalf("model = %q, want spark model", profile.Model)
	}
	if got := strings.TrimSpace(strFromAny(profile.TurnParams["effort"])); got != modelprofile.ReasoningMedium {
		t.Fatalf("effort = %q, want %q", got, modelprofile.ReasoningMedium)
	}
}

func TestEnforceRoutingPolicyNormalizesButDoesNotDropActions(t *testing.T) {
	actions := []*SystemAction{
		{Action: "toggle_silent", Params: map[string]interface{}{}},
		{Action: "switch_model", Params: map[string]interface{}{"alias": "gpt"}},
	}
	enforced := enforceRoutingPolicy("use GPT for this", actions)
	if len(enforced) != 2 {
		t.Fatalf("enforced length = %d, want 2", len(enforced))
	}
}
