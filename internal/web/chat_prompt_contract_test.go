package web

import (
	"context"
	"testing"
)

func TestEnsurePromptContractFresh_FirstWriteDoesNotClearMessages(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "hello", "hello", "markdown"); err != nil {
		t.Fatalf("add chat message: %v", err)
	}
	if err := app.store.SetAppState(promptContractStateKey, ""); err != nil {
		t.Fatalf("clear prompt digest state: %v", err)
	}

	if err := app.ensurePromptContractFresh(); err != nil {
		t.Fatalf("ensurePromptContractFresh: %v", err)
	}

	messages, err := app.store.ListChatMessages(session.ID, 100)
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected message to remain on first digest write, got %d", len(messages))
	}
	digest, err := app.store.AppState(promptContractStateKey)
	if err != nil {
		t.Fatalf("read prompt digest state: %v", err)
	}
	if digest == "" {
		t.Fatal("expected stored prompt digest")
	}
}

func TestEnsurePromptContractFresh_ChangedDigestClearsMessages(t *testing.T) {
	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("create chat session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "assistant", "reply", "reply", "markdown"); err != nil {
		t.Fatalf("add chat message: %v", err)
	}
	if err := app.store.SetAppState(promptContractStateKey, "outdated-digest"); err != nil {
		t.Fatalf("set outdated prompt digest: %v", err)
	}

	if err := app.ensurePromptContractFresh(); err != nil {
		t.Fatalf("ensurePromptContractFresh: %v", err)
	}

	messages, err := app.store.ListChatMessages(session.ID, 100)
	if err != nil {
		t.Fatalf("list chat messages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("expected messages cleared after digest change, got %d", len(messages))
	}
}
