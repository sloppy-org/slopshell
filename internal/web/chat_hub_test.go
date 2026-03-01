package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/modelprofile"
)

func setupMockIntentClassifierServer(t *testing.T, status int, response map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/classify" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		rawText, _ := payload["text"].(string)
		if strings.TrimSpace(rawText) == "" {
			http.Error(w, "missing text", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(response)
	}))
}

func setupMockIntentLLMServer(t *testing.T, status int, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		messages, ok := payload["messages"].([]interface{})
		if !ok || len(messages) == 0 {
			http.Error(w, "missing messages", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": content,
					},
				},
			},
		})
	}))
}

func setupMockDelegateStartServer(t *testing.T, jobID string, seen *int, observed *map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		params, _ := payload["params"].(map[string]interface{})
		if strings.TrimSpace(strFromAny(params["name"])) != "delegate_to_model" {
			http.Error(w, "unexpected tool", http.StatusBadRequest)
			return
		}
		args, _ := params["arguments"].(map[string]interface{})
		if args == nil {
			http.Error(w, "missing arguments", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(strFromAny(args["prompt"])) == "" {
			http.Error(w, "missing prompt", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(strFromAny(args["cwd"])) == "" {
			http.Error(w, "missing cwd", http.StatusBadRequest)
			return
		}
		if seen != nil {
			*seen += 1
		}
		if observed != nil {
			copied := map[string]interface{}{}
			for key, value := range args {
				copied[key] = value
			}
			*observed = copied
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"result": map[string]interface{}{
				"structuredContent": map[string]interface{}{
					"job_id": jobID,
				},
			},
		})
	}))
}

func latestAssistantMessage(t *testing.T, app *App, sessionID string) string {
	t.Helper()
	updatedMessages, err := app.store.ListChatMessages(sessionID, 100)
	if err != nil {
		t.Fatalf("list updated messages: %v", err)
	}
	for i := len(updatedMessages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(updatedMessages[i].Role), "assistant") {
			return strings.TrimSpace(updatedMessages[i].ContentPlain)
		}
	}
	return ""
}

func TestParseSystemAction(t *testing.T) {
	plain, err := parseSystemAction("hello world")
	if err != nil {
		t.Fatalf("plain parse returned error: %v", err)
	}
	if plain != nil {
		t.Fatalf("expected nil action for plain text")
	}

	cases := []struct {
		name       string
		raw        string
		wantAction string
	}{
		{name: "switch project", raw: `{"action":"switch_project","name":"docs"}`, wantAction: "switch_project"},
		{name: "switch model", raw: `{"action":"switch_model","alias":"gpt","effort":"high"}`, wantAction: "switch_model"},
		{name: "toggle silent", raw: `{"action":"toggle_silent"}`, wantAction: "toggle_silent"},
		{name: "toggle conversation", raw: `{"action":"toggle_conversation"}`, wantAction: "toggle_conversation"},
		{name: "cancel work", raw: `{"action":"cancel_work"}`, wantAction: "cancel_work"},
		{name: "show status", raw: `{"action":"show_status"}`, wantAction: "show_status"},
		{name: "delegate", raw: `{"action":"delegate","model":"codex","task":"audit tests"}`, wantAction: "delegate"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			action, parseErr := parseSystemAction(tc.raw)
			if parseErr != nil {
				t.Fatalf("parse action: %v", parseErr)
			}
			if action == nil {
				t.Fatalf("expected parsed action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
		})
	}
}

func TestClassifyIntentLocallyAcceptsDelegateAction(t *testing.T) {
	classifier := setupMockIntentClassifierServer(t, http.StatusOK, map[string]interface{}{
		"intent":     "delegate",
		"confidence": 0.92,
		"entities": map[string]interface{}{
			"model": "gpt",
			"task":  "review this repository",
		},
	})
	defer classifier.Close()

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.intentClassifierURL = classifier.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	action, confidence, err := app.classifyIntentLocally(context.Background(), "delegate repo review")
	if err != nil {
		t.Fatalf("classify intent locally: %v", err)
	}
	if action == nil {
		t.Fatalf("expected delegate action")
	}
	if action.Action != "delegate" {
		t.Fatalf("action = %q, want delegate", action.Action)
	}
	if confidence != 0.92 {
		t.Fatalf("confidence = %v, want 0.92", confidence)
	}
	if got := strings.TrimSpace(strFromAny(action.Params["model"])); got != "gpt" {
		t.Fatalf("delegate model = %q, want gpt", got)
	}
	if got := strings.TrimSpace(strFromAny(action.Params["task"])); got != "review this repository" {
		t.Fatalf("delegate task = %q, want review this repository", got)
	}
}

func TestExecuteSystemActionDelegateStartsJob(t *testing.T) {
	app := newAuthedTestApp(t)
	defaultProject, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("hub session: %v", err)
	}

	delegateCalls := 0
	var observed map[string]interface{}
	server := setupMockDelegateStartServer(t, "job-123", &delegateCalls, &observed)
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse mock url: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("parse mock port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(defaultProject), port)

	msg, payload, err := app.executeSystemAction(session.ID, session, &SystemAction{
		Action: "delegate",
		Params: map[string]interface{}{
			"model": "gpt",
			"task":  "review the current repository state",
		},
	})
	if err != nil {
		t.Fatalf("execute delegate: %v", err)
	}
	if !strings.Contains(msg, "job-123") {
		t.Fatalf("delegate message = %q, want job id", msg)
	}
	if payload == nil {
		t.Fatalf("expected delegate payload")
	}
	if got := strings.TrimSpace(strFromAny(payload["type"])); got != "delegate" {
		t.Fatalf("payload type = %q, want delegate", got)
	}
	if got := strings.TrimSpace(strFromAny(payload["job_id"])); got != "job-123" {
		t.Fatalf("payload job_id = %q, want job-123", got)
	}
	if got := strings.TrimSpace(strFromAny(payload["model"])); got != "gpt" {
		t.Fatalf("payload model = %q, want gpt", got)
	}
	if got := strings.TrimSpace(strFromAny(payload["project_id"])); got != defaultProject.ID {
		t.Fatalf("payload project_id = %q, want %q", got, defaultProject.ID)
	}
	if delegateCalls != 1 {
		t.Fatalf("delegate tool calls = %d, want 1", delegateCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["model"])); got != "gpt" {
		t.Fatalf("delegate model arg = %q, want gpt", got)
	}
	if got := strings.TrimSpace(strFromAny(observed["prompt"])); got != "review the current repository state" {
		t.Fatalf("delegate prompt arg = %q, want expected task", got)
	}
	if got := strings.TrimSpace(strFromAny(observed["cwd"])); got != strings.TrimSpace(defaultProject.RootPath) {
		t.Fatalf("delegate cwd arg = %q, want %q", got, defaultProject.RootPath)
	}
}

func TestHubSwitchModelTargetsPrimaryProject(t *testing.T) {
	app := newAuthedTestApp(t)
	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	if _, err := app.activateProject(hub.ID); err != nil {
		t.Fatalf("activate hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("hub session: %v", err)
	}

	msg, payload, err := app.executeSystemAction(session.ID, session, &SystemAction{
		Action: "switch_model",
		Params: map[string]interface{}{
			"alias":  "gpt",
			"effort": "extra_high",
		},
	})
	if err != nil {
		t.Fatalf("execute switch_model: %v", err)
	}
	if payload == nil {
		t.Fatalf("expected switch_model payload")
	}
	if got := strings.TrimSpace(payload["type"].(string)); got != "switch_model" {
		t.Fatalf("action payload type = %q, want switch_model", got)
	}
	if !strings.Contains(strings.ToLower(msg), "model") {
		t.Fatalf("expected model update message, got %q", msg)
	}

	defaultProject, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	updatedDefault, err := app.store.GetProject(defaultProject.ID)
	if err != nil {
		t.Fatalf("reload default project: %v", err)
	}
	if updatedDefault.ChatModel != "gpt" {
		t.Fatalf("default project chat model = %q, want gpt", updatedDefault.ChatModel)
	}
	if updatedDefault.ChatModelReasoningEffort != "extra_high" {
		t.Fatalf(
			"default reasoning effort = %q, want extra_high",
			updatedDefault.ChatModelReasoningEffort,
		)
	}

	updatedHub, err := app.store.GetProject(hub.ID)
	if err != nil {
		t.Fatalf("reload hub project: %v", err)
	}
	if updatedHub.ChatModel != "spark" {
		t.Fatalf("hub chat model changed to %q, want spark", updatedHub.ChatModel)
	}
}

func TestHubSwitchProjectActionReturnsActivationPayload(t *testing.T) {
	app := newAuthedTestApp(t)
	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("hub session: %v", err)
	}

	linkedDir := t.TempDir()
	target, created, err := app.createProject(projectCreateRequest{
		Name: "notes",
		Kind: "linked",
		Path: filepath.Clean(linkedDir),
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if !created {
		t.Fatalf("expected linked project to be created")
	}

	_, payload, err := app.executeSystemAction(session.ID, session, &SystemAction{
		Action: "switch_project",
		Params: map[string]interface{}{
			"name": "note",
		},
	})
	if err != nil {
		t.Fatalf("execute switch_project: %v", err)
	}
	if payload == nil {
		t.Fatalf("expected system_action payload")
	}
	if got := strings.TrimSpace(payload["type"].(string)); got != "switch_project" {
		t.Fatalf("action payload type = %q, want switch_project", got)
	}
	if got := strings.TrimSpace(payload["project_id"].(string)); got != target.ID {
		t.Fatalf("action payload project_id = %q, want %q", got, target.ID)
	}
}

func TestExecuteSystemActionRejectsUnsupportedAction(t *testing.T) {
	app := newAuthedTestApp(t)
	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("hub session: %v", err)
	}

	_, _, err = app.executeSystemAction(session.ID, session, &SystemAction{Action: "unknown"})
	if err == nil {
		t.Fatalf("expected unsupported action error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unsupported action") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHubRunTurnKeepsPlainTextAssistantOutput(t *testing.T) {
	const assistantReply = "All systems nominal."
	wsServer := setupMockAppServerStatusServer(t, assistantReply)
	defer wsServer.Close()
	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")

	app, err := New(t.TempDir(), "", "", wsURL, "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("hub session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "status?", "status?", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}

	messages, err := app.store.ListChatMessages(session.ID, 100)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	app.runHubTurn(session.ID, session, messages, turnOutputModeSilent, false)

	lastAssistant := latestAssistantMessage(t, app, session.ID)
	if lastAssistant != assistantReply {
		t.Fatalf("assistant plain text = %q, want %q", lastAssistant, assistantReply)
	}
}

func TestHubRunTurnExecutesHighConfidenceLocalIntent(t *testing.T) {
	classifier := setupMockIntentClassifierServer(t, http.StatusOK, map[string]interface{}{
		"intent":     "toggle_silent",
		"confidence": 0.95,
		"entities":   map[string]interface{}{},
	})
	defer classifier.Close()

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.intentClassifierURL = classifier.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("hub session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "be quiet", "be quiet", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	messages, err := app.store.ListChatMessages(session.ID, 100)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	app.runHubTurn(session.ID, session, messages, turnOutputModeSilent, false)

	if got := latestAssistantMessage(t, app, session.ID); got != "Toggled silent mode." {
		t.Fatalf("assistant message = %q, want %q", got, "Toggled silent mode.")
	}
}

func TestHubRunTurnFallsBackToSparkOnLowIntentConfidence(t *testing.T) {
	const assistantReply = "All systems nominal."
	wsServer := setupMockAppServerStatusServer(t, assistantReply)
	defer wsServer.Close()
	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")

	classifier := setupMockIntentClassifierServer(t, http.StatusOK, map[string]interface{}{
		"intent":     "toggle_silent",
		"confidence": 0.25,
		"entities":   map[string]interface{}{},
	})
	defer classifier.Close()
	llm := setupMockIntentLLMServer(t, http.StatusServiceUnavailable, "temporary failure")
	defer llm.Close()

	app, err := New(t.TempDir(), "", "", wsURL, "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.intentClassifierURL = classifier.URL
	app.intentLLMURL = llm.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("hub session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "be quiet", "be quiet", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	messages, err := app.store.ListChatMessages(session.ID, 100)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	app.runHubTurn(session.ID, session, messages, turnOutputModeSilent, false)

	if got := latestAssistantMessage(t, app, session.ID); got != assistantReply {
		t.Fatalf("assistant message = %q, want %q", got, assistantReply)
	}
}

func TestHubRunTurnUsesIntentLLMFallbackOnLowIntentConfidence(t *testing.T) {
	classifier := setupMockIntentClassifierServer(t, http.StatusOK, map[string]interface{}{
		"intent":     "toggle_silent",
		"confidence": 0.25,
		"entities":   map[string]interface{}{},
	})
	defer classifier.Close()
	llm := setupMockIntentLLMServer(t, http.StatusOK, "```json\n{\"action\":\"toggle_silent\"}\n```")
	defer llm.Close()

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.intentClassifierURL = classifier.URL
	app.intentLLMURL = llm.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("hub session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "be quiet", "be quiet", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	messages, err := app.store.ListChatMessages(session.ID, 100)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	app.runHubTurn(session.ID, session, messages, turnOutputModeSilent, false)

	if got := latestAssistantMessage(t, app, session.ID); got != "Toggled silent mode." {
		t.Fatalf("assistant message = %q, want %q", got, "Toggled silent mode.")
	}
}

func TestHubRunTurnFallsBackToSparkWhenLocalIntentExecutionFails(t *testing.T) {
	const assistantReply = "All systems nominal."
	wsServer := setupMockAppServerStatusServer(t, assistantReply)
	defer wsServer.Close()
	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")

	classifier := setupMockIntentClassifierServer(t, http.StatusOK, map[string]interface{}{
		"intent":     "switch_project",
		"confidence": 0.97,
		"entities":   map[string]interface{}{},
	})
	defer classifier.Close()

	app, err := New(t.TempDir(), "", "", wsURL, "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	app.intentClassifierURL = classifier.URL
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(hub.ProjectKey)
	if err != nil {
		t.Fatalf("hub session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "switch project", "switch project", "text"); err != nil {
		t.Fatalf("add user message: %v", err)
	}
	messages, err := app.store.ListChatMessages(session.ID, 100)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	app.runHubTurn(session.ID, session, messages, turnOutputModeSilent, false)

	if got := latestAssistantMessage(t, app, session.ID); got != assistantReply {
		t.Fatalf("assistant message = %q, want %q", got, assistantReply)
	}
}

func TestHubProjectProfileUsesSparkLow(t *testing.T) {
	app := newAuthedTestApp(t)
	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}

	profile := app.appServerModelProfileForProject(hub)
	if profile.Alias != modelprofile.AliasSpark {
		t.Fatalf("hub profile alias = %q, want %q", profile.Alias, modelprofile.AliasSpark)
	}
	if profile.Model != modelprofile.ModelForAlias(modelprofile.AliasSpark) {
		t.Fatalf("hub profile model = %q, want spark model", profile.Model)
	}
	if got := strings.TrimSpace(profile.ThreadParams["model_reasoning_effort"].(string)); got != modelprofile.ReasoningLow {
		t.Fatalf("hub thread reasoning = %q, want %q", got, modelprofile.ReasoningLow)
	}
	if got := strings.TrimSpace(profile.TurnParams["model_reasoning_effort"].(string)); got != modelprofile.ReasoningLow {
		t.Fatalf("hub turn reasoning = %q, want %q", got, modelprofile.ReasoningLow)
	}
}

func TestEnsureHubProjectUsesDedicatedRootWhenLocalProjectConfigured(t *testing.T) {
	dataDir := t.TempDir()
	localProjectDir := t.TempDir()
	app, err := New(dataDir, localProjectDir, "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	defaultProject, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	hub, err := app.ensureHubProject()
	if err != nil {
		t.Fatalf("ensure hub project: %v", err)
	}

	if strings.TrimSpace(hub.ProjectKey) != HubProjectKey {
		t.Fatalf("hub key = %q, want %q", hub.ProjectKey, HubProjectKey)
	}
	if filepath.Clean(hub.RootPath) == filepath.Clean(defaultProject.RootPath) {
		t.Fatalf("hub root path collides with default project root: %q", hub.RootPath)
	}

	expectedHubRoot := filepath.Join(dataDir, "projects", "hub")
	if filepath.Clean(hub.RootPath) != filepath.Clean(expectedHubRoot) {
		t.Fatalf("hub root path = %q, want %q", hub.RootPath, expectedHubRoot)
	}
}
