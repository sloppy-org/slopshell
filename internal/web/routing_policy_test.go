package web

import (
	"strings"
	"testing"

	"github.com/sloppy-org/slopshell/internal/modelprofile"
)

func TestParseTurnRoutingDirectivesDefaultsToLocal(t *testing.T) {
	directives := parseTurnRoutingDirectives("summarize this note")
	if directives.ModelAlias != "" {
		t.Fatalf("ModelAlias = %q, want empty", directives.ModelAlias)
	}
	if directives.ReasoningEffort != "" {
		t.Fatalf("ReasoningEffort = %q, want empty", directives.ReasoningEffort)
	}
	if directives.PromptText != "summarize this note" {
		t.Fatalf("PromptText = %q, want original", directives.PromptText)
	}
}

func TestParseTurnRoutingDirectivesSupportsSparkCodexGPTMiniAndReasoning(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantAlias  string
		wantEffort string
	}{
		{name: "spark", text: "Use Spark for this and think hard: analyze the latest error.", wantAlias: modelprofile.AliasSpark, wantEffort: modelprofile.ReasoningHigh},
		{name: "codex alias", text: "Lass Codex denk kurz den Buildfehler ansehen.", wantAlias: modelprofile.AliasSpark, wantEffort: modelprofile.ReasoningLow},
		{name: "gpt", text: "Please use GPT and think quickly about this timeout.", wantAlias: modelprofile.AliasGPT, wantEffort: modelprofile.ReasoningLow},
		{name: "mini", text: "Let mini think a bit about this API diff.", wantAlias: modelprofile.AliasMini, wantEffort: modelprofile.ReasoningMedium},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			directives := parseTurnRoutingDirectives(tc.text)
			if directives.ModelAlias != tc.wantAlias {
				t.Fatalf("ModelAlias = %q, want %q", directives.ModelAlias, tc.wantAlias)
			}
			if directives.ReasoningEffort != tc.wantEffort {
				t.Fatalf("ReasoningEffort = %q, want %q", directives.ReasoningEffort, tc.wantEffort)
			}
			if strings.TrimSpace(directives.PromptText) == "" {
				t.Fatal("PromptText is empty")
			}
		})
	}
}

func TestParseTurnRoutingDirectivesLeavesSearchPhrasingUntouched(t *testing.T) {
	directives := parseTurnRoutingDirectives("Search the web for today's AMD news.")
	if directives.ModelAlias != "" {
		t.Fatalf("ModelAlias = %q, want empty so routing stays local by default", directives.ModelAlias)
	}
	if directives.ModelAliasExplicit {
		t.Fatal("ModelAliasExplicit = true, want false")
	}
	if directives.PromptText != "Search the web for today's AMD news." {
		t.Fatalf("PromptText = %q, want original search phrasing preserved for the model", directives.PromptText)
	}
}

func TestParseTurnRoutingDirectivesExplicitSparkMarksExplicit(t *testing.T) {
	directives := parseTurnRoutingDirectives("Use Spark to review the rollout plan.")
	if !directives.ModelAliasExplicit {
		t.Fatal("ModelAliasExplicit = false, want true for explicit 'use spark'")
	}
	if directives.ModelAlias != modelprofile.AliasSpark {
		t.Fatalf("ModelAlias = %q, want %q", directives.ModelAlias, modelprofile.AliasSpark)
	}
}

func TestParseTurnRoutingDirectivesCurrentInfoCueStaysLocal(t *testing.T) {
	directives := parseTurnRoutingDirectives("What is the latest ITER timeline?")
	if directives.ModelAlias != "" {
		t.Fatalf("ModelAlias = %q, want empty", directives.ModelAlias)
	}
	if directives.ModelAliasExplicit {
		t.Fatal("ModelAliasExplicit = true, want false: 'latest' alone must not force remote routing")
	}
}

func TestParseTurnRoutingDirectivesDetailEnglish(t *testing.T) {
	for _, text := range []string{
		"Explain in detail how quaternions work",
		"Give a long answer about tokamaks please",
		"Be thorough: walk me through the proof",
		"Go deep into the pipeline",
		"elaborate on this failure",
	} {
		directives := parseTurnRoutingDirectives(text)
		if !directives.DetailRequested {
			t.Fatalf("DetailRequested = false for %q", text)
		}
	}
}

func TestParseTurnRoutingDirectivesDetailGerman(t *testing.T) {
	for _, text := range []string{
		"sag mir im Detail wie ein Tokamak funktioniert",
		"erklär mir im Detail den Build",
		"erklaer es mir in detail",
		"ausführlich bitte die Fusionsphysik",
		"ausfuehrlich erklaeren",
		"ganz genau wie der Algorithmus arbeitet",
		"in aller Ausführlichkeit",
	} {
		directives := parseTurnRoutingDirectives(text)
		if !directives.DetailRequested {
			t.Fatalf("DetailRequested = false for %q", text)
		}
	}
}

func TestParseTurnRoutingDirectivesNoDetailByDefault(t *testing.T) {
	directives := parseTurnRoutingDirectives("what time is it")
	if directives.DetailRequested {
		t.Fatal("DetailRequested = true, want false for plain short ask")
	}
}

func TestRouteProfileForRoutingUsesMiniHighAndLocalFallback(t *testing.T) {
	base := appServerModelProfile{
		Alias: modelprofile.AliasLocal,
		Model: modelprofile.ModelLocal,
	}
	miniProfile := routeProfileForRouting(modelprofile.AliasMini, base, modelprofile.ReasoningLow, "")
	if miniProfile.Alias != modelprofile.AliasMini {
		t.Fatalf("mini alias = %q, want %q", miniProfile.Alias, modelprofile.AliasMini)
	}
	if miniProfile.Model != modelprofile.ModelMini {
		t.Fatalf("mini model = %q, want %q", miniProfile.Model, modelprofile.ModelMini)
	}
	if got := strings.TrimSpace(strFromAny(miniProfile.TurnParams["effort"])); got != modelprofile.ReasoningHigh {
		t.Fatalf("mini effort = %q, want %q", got, modelprofile.ReasoningHigh)
	}
	localProfile := routeProfileForRouting("", base, modelprofile.ReasoningMedium, "")
	if localProfile.Alias != modelprofile.AliasLocal {
		t.Fatalf("local alias = %q, want %q", localProfile.Alias, modelprofile.AliasLocal)
	}
	if got := strings.TrimSpace(strFromAny(localProfile.TurnParams["effort"])); got != modelprofile.ReasoningNone {
		t.Fatalf("local effort = %q, want %q", got, modelprofile.ReasoningNone)
	}
}

func TestEnforceRoutingPolicyNormalizesButDoesNotDropActions(t *testing.T) {
	actions := []*SystemAction{
		{Action: "toggle_silent", Params: map[string]interface{}{}},
		{Action: "show_status", Params: map[string]interface{}{}},
	}
	enforced := enforceRoutingPolicy("use spark for this", actions)
	if len(enforced) != 2 {
		t.Fatalf("enforced length = %d, want 2", len(enforced))
	}
}
