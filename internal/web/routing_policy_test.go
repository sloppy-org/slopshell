package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/modelprofile"
)

func TestClassifyRoutingRouteCurrentInfoUsesCodexHigh(t *testing.T) {
	route := classifyRoutingRoute("Wie wird das Wetter heute in Graz?")
	if route.Domain != routingDomainCurrentInfo {
		t.Fatalf("domain = %q, want %q", route.Domain, routingDomainCurrentInfo)
	}
	if route.Complexity != routingComplexitySimple {
		t.Fatalf("complexity = %q, want %q", route.Complexity, routingComplexitySimple)
	}
	if route.Model != modelprofile.AliasCodex {
		t.Fatalf("model = %q, want %q", route.Model, modelprofile.AliasCodex)
	}
	if route.Effort != modelprofile.ReasoningHigh {
		t.Fatalf("effort = %q, want %q", route.Effort, modelprofile.ReasoningHigh)
	}
	if !route.BlockShell {
		t.Fatal("expected BlockShell for current-info route")
	}
}

func TestClassifyRoutingRouteSimpleGeneralKeepsConfiguredModel(t *testing.T) {
	route := classifyRoutingRoute("summarize this note")
	if route.Domain != routingDomainGeneral {
		t.Fatalf("domain = %q, want %q", route.Domain, routingDomainGeneral)
	}
	if route.Model != "" {
		t.Fatalf("model = %q, want empty to keep configured model", route.Model)
	}
	if route.Effort != "" {
		t.Fatalf("effort = %q, want empty to keep configured model effort", route.Effort)
	}
}

func TestClassifyRoutingRouteComplexCodingUsesCodexHigh(t *testing.T) {
	text := "Please do a deep dive root cause analysis of this timeout bug in our Go repo and propose architecture fixes."
	route := classifyRoutingRoute(text)
	if route.Domain != routingDomainCoding {
		t.Fatalf("domain = %q, want %q", route.Domain, routingDomainCoding)
	}
	if route.Complexity != routingComplexityComplex {
		t.Fatalf("complexity = %q, want %q", route.Complexity, routingComplexityComplex)
	}
	if route.Model != modelprofile.AliasCodex {
		t.Fatalf("model = %q, want %q", route.Model, modelprofile.AliasCodex)
	}
	if route.Effort != modelprofile.ReasoningHigh {
		t.Fatalf("effort = %q, want %q", route.Effort, modelprofile.ReasoningHigh)
	}
}

func TestEnforceRoutingPolicyBlocksShellForCurrentInfo(t *testing.T) {
	actions := []*SystemAction{
		{Action: "shell", Params: map[string]interface{}{"command": "date"}},
		{Action: "toggle_silent", Params: map[string]interface{}{}},
	}
	enforced := enforceRoutingPolicy("What is the weather in Graz today?", actions)
	if len(enforced) != 1 {
		t.Fatalf("enforced length = %d, want 1", len(enforced))
	}
	if got := strings.TrimSpace(enforced[0].Action); got != "toggle_silent" {
		t.Fatalf("action = %q, want toggle_silent", got)
	}
}

func TestEnforceRoutingPolicyBlocksItemActionsForCurrentInfo(t *testing.T) {
	for _, actionName := range []string{"make_item", "snooze_item", "delegate_item", "split_items", "capture_idea"} {
		t.Run(actionName, func(t *testing.T) {
			actions := []*SystemAction{
				{Action: actionName, Params: map[string]interface{}{"visible_after": "2026-03-13T09:00:00Z"}},
			}
			enforced := enforceRoutingPolicy("What will be the weather in Graz tomorrow?", actions)
			if len(enforced) != 0 {
				t.Fatalf("enforced length = %d, want 0 for current-info query with %s", len(enforced), actionName)
			}
		})
	}
}

func TestEnforceRoutingPolicyAllowsItemActionsForNonCurrentInfo(t *testing.T) {
	actions := []*SystemAction{
		{Action: "snooze_item", Params: map[string]interface{}{"visible_after": "2026-03-13T09:00:00Z"}},
	}
	enforced := enforceRoutingPolicy("remind me to call Bob tomorrow", actions)
	if len(enforced) != 1 {
		t.Fatalf("enforced length = %d, want 1 for non-current-info query", len(enforced))
	}
}

func TestRouteProfileForRoutingAppliesCodexHigh(t *testing.T) {
	base := appServerModelProfile{
		Alias: modelprofile.AliasCodex,
		Model: modelprofile.ModelForAlias(modelprofile.AliasCodex),
	}
	profile := routeProfileForRouting(routingRoute{
		Model:  modelprofile.AliasCodex,
		Effort: modelprofile.ReasoningHigh,
	}, base, modelprofile.ReasoningLow)
	if profile.Alias != modelprofile.AliasCodex {
		t.Fatalf("alias = %q, want %q", profile.Alias, modelprofile.AliasCodex)
	}
	if profile.Model != modelprofile.ModelForAlias(modelprofile.AliasCodex) {
		t.Fatalf("model = %q, want codex model", profile.Model)
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
	profile := routeProfileForRouting(routingRoute{}, base, modelprofile.ReasoningMedium)
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

func TestClassifyAndExecuteSystemActionCurrentInfoDropsShellPlan(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{{
				"message": map[string]interface{}{
					"content": `{"actions":[{"action":"shell","command":"curl -s https://wttr.in/Graz"}]}`,
				},
			}},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(
		context.Background(),
		session.ID,
		session,
		"What is the weather in Graz, Austria today?",
	)
	if handled {
		t.Fatalf("expected no system action handling, got message=%q payloads=%#v", message, payloads)
	}
}
