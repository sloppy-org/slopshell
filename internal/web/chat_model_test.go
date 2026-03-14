package web

import (
	"testing"

	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/store"
)

func TestEffectiveProjectChatModelFallsBackToSpark(t *testing.T) {
	app := newAuthedTestApp(t)
	app.appServerModel = modelprofile.ModelSpark

	project := store.Project{}
	if got := app.effectiveProjectChatModelAlias(project); got != modelprofile.AliasSpark {
		t.Fatalf("effectiveProjectChatModelAlias() = %q, want %q", got, modelprofile.AliasSpark)
	}
	if got := app.effectiveProjectChatModelReasoningEffort(project); got != modelprofile.ReasoningLow {
		t.Fatalf("effectiveProjectChatModelReasoningEffort() = %q, want %q", got, modelprofile.ReasoningLow)
	}
}

func TestEffectiveProjectChatModelDefaultsToSpark(t *testing.T) {
	app := newAuthedTestApp(t)
	app.appServerModel = ""

	project := store.Project{}
	if got := app.effectiveProjectChatModelAlias(project); got != modelprofile.AliasSpark {
		t.Fatalf("effectiveProjectChatModelAlias() = %q, want %q", got, modelprofile.AliasSpark)
	}
	if got := app.effectiveProjectChatModelReasoningEffort(project); got != modelprofile.ReasoningLow {
		t.Fatalf("effectiveProjectChatModelReasoningEffort() = %q, want %q", got, modelprofile.ReasoningLow)
	}
}

func TestAppServerModelProfileForProjectUsesStoredAliasAndNormalizesReasoning(t *testing.T) {
	app := newAuthedTestApp(t)

	profile := app.appServerModelProfileForProject(store.Project{
		ChatModel:                modelprofile.AliasGPT,
		ChatModelReasoningEffort: "minimal",
	})
	if profile.Alias != modelprofile.AliasGPT {
		t.Fatalf("profile.Alias = %q, want %q", profile.Alias, modelprofile.AliasGPT)
	}
	if profile.Model != modelprofile.ModelGPT {
		t.Fatalf("profile.Model = %q, want %q", profile.Model, modelprofile.ModelGPT)
	}
	if profile.ThreadParams != nil {
		t.Fatalf("profile.ThreadParams = %#v, want nil", profile.ThreadParams)
	}
	if got := profile.TurnParams["effort"]; got != modelprofile.ReasoningHigh {
		t.Fatalf("profile.TurnParams[effort] = %#v, want %q", got, modelprofile.ReasoningHigh)
	}
}

func TestAppServerModelProfileForProjectKeyFallsBackWhenProjectMissing(t *testing.T) {
	app := newAuthedTestApp(t)
	app.appServerModel = modelprofile.ModelSpark

	profile := app.appServerModelProfileForProjectKey("missing-project")
	if profile.Alias != modelprofile.AliasSpark {
		t.Fatalf("profile.Alias = %q, want %q", profile.Alias, modelprofile.AliasSpark)
	}
	if profile.Model != modelprofile.ModelSpark {
		t.Fatalf("profile.Model = %q, want %q", profile.Model, modelprofile.ModelSpark)
	}
	if got := profile.TurnParams["effort"]; got != modelprofile.ReasoningLow {
		t.Fatalf("profile.TurnParams[effort] = %#v, want %q", got, modelprofile.ReasoningLow)
	}
}
