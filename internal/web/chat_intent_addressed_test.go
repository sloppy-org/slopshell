package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseIntentPlanClassificationReadsAddressedField(t *testing.T) {
	classification, err := parseIntentPlanClassification(`{"addressed":false,"action":"toggle_silent"}`)
	if err != nil {
		t.Fatalf("parseIntentPlanClassification returned error: %v", err)
	}
	if classification.Addressed == nil {
		t.Fatal("expected addressed classification")
	}
	if *classification.Addressed {
		t.Fatal("addressed = true, want false")
	}
	if len(classification.Actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(classification.Actions))
	}
	if classification.Actions[0].Action != "toggle_silent" {
		t.Fatalf("action = %q, want toggle_silent", classification.Actions[0].Action)
	}
}

func TestParseIntentPlanClassificationReadsLocalAnswer(t *testing.T) {
	classification, err := parseIntentPlanClassification(`{"kind":"local_answer","text":"Paris.","confidence":"high"}`)
	if err != nil {
		t.Fatalf("parseIntentPlanClassification returned error: %v", err)
	}
	if classification.LocalAnswer == nil {
		t.Fatal("expected local answer classification")
	}
	if classification.LocalAnswer.Text != "Paris." {
		t.Fatalf("local answer text = %q, want Paris.", classification.LocalAnswer.Text)
	}
	if classification.LocalAnswer.Confidence != "high" {
		t.Fatalf("local answer confidence = %q, want high", classification.LocalAnswer.Confidence)
	}
}

func TestParseIntentPlanClassificationReadsAckField(t *testing.T) {
	classification, err := parseIntentPlanClassification(`{"kind":"dialogue","ack":"  Checking   now.  "}`)
	if err != nil {
		t.Fatalf("parseIntentPlanClassification returned error: %v", err)
	}
	if classification.Ack != "Checking now." {
		t.Fatalf("ack = %q, want %q", classification.Ack, "Checking now.")
	}
}

func TestClassifyIntentPlanWithLLMMeetingPromptRequestsAddressedness(t *testing.T) {
	var systemPrompt string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm request: %v", err)
		}
		messages, _ := payload["messages"].([]interface{})
		if len(messages) == 0 {
			t.Fatal("missing llm messages")
		}
		first, _ := messages[0].(map[string]interface{})
		systemPrompt = strings.TrimSpace(strFromAny(first["content"]))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"addressed":true,"kind":"dialogue"}`,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL
	setLivePolicyForTest(t, app, LivePolicyMeeting)

	classification, err := app.classifyIntentPlanWithLLMResult(context.Background(), "what changed?")
	if err != nil {
		t.Fatalf("classifyIntentPlanWithLLMResult returned error: %v", err)
	}
	if classification.Addressed == nil || !*classification.Addressed {
		t.Fatalf("addressed = %#v, want true", classification.Addressed)
	}
	if !strings.Contains(systemPrompt, `include an "addressed" boolean`) {
		t.Fatalf("system prompt = %q, want addressedness instruction", systemPrompt)
	}
}

func TestClassifyIntentPlanWithLLMIncludesRuntimeContextForLocalAnswers(t *testing.T) {
	var userPrompt string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode llm request: %v", err)
		}
		messages, _ := payload["messages"].([]interface{})
		if len(messages) < 2 {
			t.Fatalf("messages length = %d, want >= 2", len(messages))
		}
		userMessage, _ := messages[1].(map[string]interface{})
		userPrompt = strings.TrimSpace(strFromAny(userMessage["content"]))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"kind":"local_answer","text":"You are in the default workspace.","confidence":"high"}`,
					},
				},
			},
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
	workspace, err := app.store.ActiveWorkspace()
	if err != nil {
		t.Fatalf("active workspace: %v", err)
	}
	focus, err := app.store.CreateWorkspace("Focused", filepath.Join(t.TempDir(), "focused"))
	if err != nil {
		t.Fatalf("CreateWorkspace(focused): %v", err)
	}
	if err := app.setFocusedWorkspace(focus.ID); err != nil {
		t.Fatalf("setFocusedWorkspace(): %v", err)
	}
	if _, err := app.store.CreateItem("Review parser follow-up", store.ItemOptions{WorkspaceID: &workspace.ID}); err != nil {
		t.Fatalf("create item: %v", err)
	}
	if _, err := app.store.CreateItem("Focused follow-up", store.ItemOptions{WorkspaceID: &focus.ID}); err != nil {
		t.Fatalf("create focused item: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "review the parser plan", "review the parser plan", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "assistant", "Which workspace do you mean?", "Which workspace do you mean?", "text"); err != nil {
		t.Fatalf("add assistant message: %v", err)
	}
	app.registerActiveChatTurn(session.ID, "run-ctx", func() {})
	defer app.unregisterActiveChatTurn(session.ID, "run-ctx")

	classification, err := app.classifyIntentPlanWithLLMResultForTurn(context.Background(), session.ID, session, "what workspace am I in?")
	if err != nil {
		t.Fatalf("classifyIntentPlanWithLLMResultForTurn returned error: %v", err)
	}
	if classification.LocalAnswer == nil || classification.LocalAnswer.Confidence != "high" {
		t.Fatalf("local answer = %#v, want high-confidence local answer", classification.LocalAnswer)
	}
	if !strings.Contains(userPrompt, "Runtime context:") {
		t.Fatalf("user prompt = %q, want runtime context header", userPrompt)
	}
	if !strings.Contains(userPrompt, workspace.Name) || !strings.Contains(userPrompt, workspace.DirPath) {
		t.Fatalf("user prompt = %q, want active workspace details", userPrompt)
	}
	if !strings.Contains(userPrompt, focus.Name) || !strings.Contains(userPrompt, focus.DirPath) {
		t.Fatalf("user prompt = %q, want focused workspace details", userPrompt)
	}
	if !strings.Contains(userPrompt, "Open items in focused workspace: 1") {
		t.Fatalf("user prompt = %q, want workspace item count", userPrompt)
	}
	if !strings.Contains(userPrompt, "Running tasks: 1 active, 0 queued") {
		t.Fatalf("user prompt = %q, want running task count", userPrompt)
	}
	if !strings.Contains(userPrompt, "USER: review the parser plan") || !strings.Contains(userPrompt, "ASSISTANT: Which workspace do you mean?") {
		t.Fatalf("user prompt = %q, want recent conversation summary", userPrompt)
	}
}

func TestRunAssistantTurnSuppressesUnaddressedMeetingTurn(t *testing.T) {
	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"addressed":false,"action":"toggle_silent"}`)
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL
	setLivePolicyForTest(t, app, LivePolicyMeeting)

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.chatSessionForProject(project)
	if err != nil {
		t.Fatalf("project session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "please summarize the budget discussion", "please summarize the budget discussion", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}

	app.runAssistantTurn(session.ID, dequeuedTurn{outputMode: turnOutputModeSilent})

	if app.silentModeEnabled() {
		t.Fatal("silent mode toggled for unaddressed meeting turn")
	}
	if got := latestAssistantMessage(t, app, session.ID); got != "" {
		t.Fatalf("assistant message = %q, want empty", got)
	}
}

func TestRunAssistantTurnMeetingDirectAddressOverridesFalseAddressedClassification(t *testing.T) {
	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"addressed":false,"action":"toggle_silent"}`)
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL
	setLivePolicyForTest(t, app, LivePolicyMeeting)

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.chatSessionForProject(project)
	if err != nil {
		t.Fatalf("project session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "Tabura, be quiet", "Tabura, be quiet", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemActionForTurn(context.Background(), session.ID, session, "Tabura, be quiet", nil, "")
	if !handled {
		t.Fatal("expected explicit direct address to be handled")
	}
	if message != "Toggled silent mode." {
		t.Fatalf("message = %q, want %q", message, "Toggled silent mode.")
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "toggle_silent" {
		t.Fatalf("payloads = %#v, want toggle_silent payload", payloads)
	}
}

func TestRunAssistantTurnDialogueIgnoresAddressedFlag(t *testing.T) {
	llm := setupMockIntentLLMServer(t, http.StatusOK, `{"addressed":false,"action":"toggle_silent"}`)
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL
	setLivePolicyForTest(t, app, LivePolicyDialogue)

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.chatSessionForProject(project)
	if err != nil {
		t.Fatalf("project session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "be quiet", "be quiet", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemActionForTurn(context.Background(), session.ID, session, "be quiet", nil, "")
	if !handled {
		t.Fatal("expected dialogue mode to ignore addressed flag")
	}
	if message != "Toggled silent mode." {
		t.Fatalf("message = %q, want %q", message, "Toggled silent mode.")
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "toggle_silent" {
		t.Fatalf("payloads = %#v, want toggle_silent payload", payloads)
	}
}
