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

func TestClassifyDelegationRouteCurrentInfoSimple(t *testing.T) {
	route := classifyDelegationRoute("Wie wird das Wetter heute in Graz?")
	if route.Domain != delegationDomainCurrentInfo {
		t.Fatalf("domain = %q, want %q", route.Domain, delegationDomainCurrentInfo)
	}
	if route.Complexity != delegationComplexitySimple {
		t.Fatalf("complexity = %q, want %q", route.Complexity, delegationComplexitySimple)
	}
	if route.Model != modelprofile.AliasSpark {
		t.Fatalf("model = %q, want %q", route.Model, modelprofile.AliasSpark)
	}
	if route.Effort != modelprofile.ReasoningLow {
		t.Fatalf("effort = %q, want %q", route.Effort, modelprofile.ReasoningLow)
	}
	if !route.ForceDelegate {
		t.Fatal("expected ForceDelegate for current-info route")
	}
	if !route.BlockShell {
		t.Fatal("expected BlockShell for current-info route")
	}
}

func TestClassifyDelegationRouteComplexCoding(t *testing.T) {
	text := "Please do a deep dive root cause analysis of this timeout bug in our Go repo, compare tradeoffs, and propose architecture fixes."
	route := classifyDelegationRoute(text)
	if route.Domain != delegationDomainCoding {
		t.Fatalf("domain = %q, want %q", route.Domain, delegationDomainCoding)
	}
	if route.Complexity != delegationComplexityComplex {
		t.Fatalf("complexity = %q, want %q", route.Complexity, delegationComplexityComplex)
	}
	if route.Model != modelprofile.AliasCodex {
		t.Fatalf("model = %q, want %q", route.Model, modelprofile.AliasCodex)
	}
	if route.Effort != modelprofile.ReasoningHigh {
		t.Fatalf("effort = %q, want %q", route.Effort, modelprofile.ReasoningHigh)
	}
	if route.ForceDelegate {
		t.Fatal("expected ForceDelegate=false for non-current-info coding route")
	}
}

func TestEnforceDelegationPolicyCurrentInfoBlocksShell(t *testing.T) {
	actions := []*SystemAction{
		{
			Action: "shell",
			Params: map[string]interface{}{"command": "date"},
		},
	}
	enforced := enforceDelegationPolicy("What is the weather in Graz today?", actions)
	if len(enforced) == 0 {
		t.Fatal("expected enforced actions")
	}
	for _, action := range enforced {
		if action != nil && strings.EqualFold(strings.TrimSpace(action.Action), "shell") {
			t.Fatalf("unexpected shell action after policy enforcement: %#v", action)
		}
	}
	if got := strings.TrimSpace(enforced[0].Action); got != "delegate" {
		t.Fatalf("first action = %q, want delegate", got)
	}
	if got := strings.TrimSpace(systemActionStringParam(enforced[0].Params, "model")); got != modelprofile.AliasSpark {
		t.Fatalf("delegate model = %q, want %q", got, modelprofile.AliasSpark)
	}
	if got := strings.TrimSpace(systemActionStringParam(enforced[0].Params, "reasoning_effort")); got != modelprofile.ReasoningLow {
		t.Fatalf("delegate reasoning_effort = %q, want %q", got, modelprofile.ReasoningLow)
	}
	if _, exists := enforced[0].Params["effort"]; exists {
		t.Fatalf("unexpected delegate effort alias in params: %#v", enforced[0].Params["effort"])
	}
	if got := strings.TrimSpace(systemActionStringParam(enforced[0].Params, "route_domain")); got != delegationDomainCurrentInfo {
		t.Fatalf("route_domain = %q, want %q", got, delegationDomainCurrentInfo)
	}
	if got := strings.TrimSpace(systemActionStringParam(enforced[0].Params, "policy_version")); got != delegationPolicyVersion {
		t.Fatalf("policy_version = %q, want %q", got, delegationPolicyVersion)
	}
}

func TestClassifyAndExecuteSystemActionCurrentInfoRewritesShellToDelegate(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"actions":[{"action":"shell","command":"curl -s https://wttr.in/Graz"}]}`,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentClassifierURL = ""
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	delegateCalls := 0
	var observed map[string]interface{}
	delegateServer := setupMockDelegateStartServer(t, "job-weather", &delegateCalls, &observed)
	defer delegateServer.Close()
	port, err := extractPort(delegateServer.URL)
	if err != nil {
		t.Fatalf("extract delegate port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	message, payloads, handled := app.classifyAndExecuteSystemAction(
		context.Background(),
		session.ID,
		session,
		"What is the weather in Graz, Austria today?",
	)
	if !handled {
		t.Fatal("expected request to be handled")
	}
	if delegateCalls != 1 {
		t.Fatalf("delegate calls = %d, want 1", delegateCalls)
	}
	if !strings.Contains(strings.ToLower(message), "delegated to spark") {
		t.Fatalf("message = %q, want delegated to spark", message)
	}
	if len(payloads) == 0 {
		t.Fatal("expected payloads")
	}
	delegatePayloadSeen := false
	for _, payload := range payloads {
		typ := strings.TrimSpace(strFromAny(payload["type"]))
		if typ == "shell" {
			t.Fatalf("unexpected shell payload in current-info flow: %#v", payload)
		}
		if typ != "delegate" {
			continue
		}
		delegatePayloadSeen = true
		if got := strings.TrimSpace(strFromAny(payload["model"])); got != modelprofile.AliasSpark {
			t.Fatalf("payload model = %q, want %q", got, modelprofile.AliasSpark)
		}
		if got := strings.TrimSpace(strFromAny(payload["reasoning_effort"])); got != modelprofile.ReasoningLow {
			t.Fatalf("payload reasoning_effort = %q, want %q", got, modelprofile.ReasoningLow)
		}
		if _, exists := payload["effort"]; exists {
			t.Fatalf("unexpected payload effort alias: %#v", payload["effort"])
		}
		if got := strings.TrimSpace(strFromAny(payload["route_domain"])); got != delegationDomainCurrentInfo {
			t.Fatalf("payload route_domain = %q, want %q", got, delegationDomainCurrentInfo)
		}
		if got := strings.TrimSpace(strFromAny(payload["route_complexity"])); got != delegationComplexitySimple {
			t.Fatalf("payload route_complexity = %q, want %q", got, delegationComplexitySimple)
		}
		if got := strings.TrimSpace(strFromAny(payload["policy_version"])); got != delegationPolicyVersion {
			t.Fatalf("payload policy_version = %q, want %q", got, delegationPolicyVersion)
		}
	}
	if !delegatePayloadSeen {
		t.Fatalf("expected delegate payload, got %#v", payloads)
	}

	if got := strings.TrimSpace(strFromAny(observed["model"])); got != modelprofile.AliasSpark {
		t.Fatalf("delegate arg model = %q, want %q", got, modelprofile.AliasSpark)
	}
	if got := strings.TrimSpace(strFromAny(observed["reasoning_effort"])); got != modelprofile.ReasoningLow {
		t.Fatalf("delegate arg reasoning_effort = %q, want %q", got, modelprofile.ReasoningLow)
	}
	if got := strings.TrimSpace(strFromAny(observed["prompt"])); got != "What is the weather in Graz, Austria today?" {
		t.Fatalf("delegate arg prompt = %q, want weather query", got)
	}
}
