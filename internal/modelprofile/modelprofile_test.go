package modelprofile

import "testing"

func TestResolveModel(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		fallback     string
		wantModel    string
		wantResolved string
	}{
		{name: "alias spark", raw: "spark", fallback: "", wantModel: ModelSpark, wantResolved: AliasSpark},
		{name: "full model", raw: ModelGPT, fallback: "", wantModel: ModelGPT, wantResolved: AliasGPT},
		{name: "default alias", raw: "", fallback: AliasSpark, wantModel: ModelSpark, wantResolved: AliasSpark},
		{name: "custom passthrough", raw: "my-custom-model", fallback: "", wantModel: "my-custom-model", wantResolved: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveModel(tc.raw, tc.fallback); got != tc.wantModel {
				t.Fatalf("ResolveModel(%q, %q) = %q, want %q", tc.raw, tc.fallback, got, tc.wantModel)
			}
			if got := ResolveAlias(tc.raw, tc.fallback); got != tc.wantResolved {
				t.Fatalf("ResolveAlias(%q, %q) = %q, want %q", tc.raw, tc.fallback, got, tc.wantResolved)
			}
		})
	}
}

func TestMainThreadReasoningEffort(t *testing.T) {
	if got := MainThreadReasoningEffort(AliasSpark); got != ReasoningLow {
		t.Fatalf("spark effort = %q, want %q", got, ReasoningLow)
	}
	if got := MainThreadReasoningEffort(AliasGPT); got != ReasoningHigh {
		t.Fatalf("gpt effort = %q, want %q", got, ReasoningHigh)
	}
}

func TestAvailableReasoningEffortsByAlias(t *testing.T) {
	efforts := AvailableReasoningEffortsByAlias()
	if len(efforts) == 0 {
		t.Fatalf("expected efforts map")
	}
	for alias, expectation := range map[string][]string{
		AliasSpark: {ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh},
		AliasGPT:   {ReasoningLow, ReasoningMedium, ReasoningHigh, ReasoningExtraHigh},
	} {
		options, ok := efforts[alias]
		if !ok {
			t.Fatalf("missing alias %q", alias)
		}
		if len(options) != len(expectation) {
			t.Fatalf("alias %q option count = %d, want %d", alias, len(options), len(expectation))
		}
		for i := range expectation {
			if options[i] != expectation[i] {
				t.Fatalf("alias %q option[%d] = %q, want %q", alias, i, options[i], expectation[i])
			}
		}
	}
}

func TestNormalizeReasoningEffortLegacyExtraHigh(t *testing.T) {
	if got := NormalizeReasoningEffort(AliasSpark, "extra_high"); got != ReasoningExtraHigh {
		t.Fatalf("legacy effort normalize = %q, want %q", got, ReasoningExtraHigh)
	}
}
