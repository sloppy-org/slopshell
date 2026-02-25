package web

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/modelprofile"
)

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
	app.runHubTurn(session.ID, session, messages, turnOutputModeSilent)

	updatedMessages, err := app.store.ListChatMessages(session.ID, 100)
	if err != nil {
		t.Fatalf("list updated messages: %v", err)
	}
	lastAssistant := ""
	for i := len(updatedMessages) - 1; i >= 0; i-- {
		if strings.EqualFold(strings.TrimSpace(updatedMessages[i].Role), "assistant") {
			lastAssistant = strings.TrimSpace(updatedMessages[i].ContentPlain)
			break
		}
	}
	if lastAssistant != assistantReply {
		t.Fatalf("assistant plain text = %q, want %q", lastAssistant, assistantReply)
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
