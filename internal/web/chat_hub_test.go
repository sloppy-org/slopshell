package web

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/modelprofile"
)

func TestParseHubAction(t *testing.T) {
	plain, err := parseHubAction("hello world")
	if err != nil {
		t.Fatalf("plain parse returned error: %v", err)
	}
	if plain != nil {
		t.Fatalf("expected nil action for plain text")
	}

	action, err := parseHubAction(`{"action":"switch_project","name":"docs"}`)
	if err != nil {
		t.Fatalf("json parse returned error: %v", err)
	}
	if action == nil {
		t.Fatalf("expected parsed action")
	}
	if action.Action != "switch_project" {
		t.Fatalf("action = %q, want switch_project", action.Action)
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

	msg, payload, err := app.executeHubAction(session.ID, session, &hubAction{
		Action: "switch_model",
		Raw: map[string]interface{}{
			"action": "switch_model",
			"alias":  "gpt",
			"effort": "extra_high",
		},
	})
	if err != nil {
		t.Fatalf("execute switch_model: %v", err)
	}
	if payload != nil {
		t.Fatalf("switch_model payload = %#v, want nil", payload)
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

	_, payload, err := app.executeHubAction(session.ID, session, &hubAction{
		Action: "switch_project",
		Raw: map[string]interface{}{
			"action": "switch_project",
			"name":   "note",
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
