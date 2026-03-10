package web

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
	tabsync "github.com/krystophny/tabura/internal/sync"
)

type stubSourceSyncRunner struct {
	runOnceCount  int
	runNowCount   int
	runOnceFn     func()
	runOnceResult tabsync.RunResult
	runNowResult  tabsync.RunResult
	runOnceErr    error
	runNowErr     error
}

func (s *stubSourceSyncRunner) RunOnce(context.Context) (tabsync.RunResult, error) {
	s.runOnceCount++
	if s.runOnceFn != nil {
		s.runOnceFn()
	}
	return s.runOnceResult, s.runOnceErr
}

func (s *stubSourceSyncRunner) RunNow(context.Context) (tabsync.RunResult, error) {
	s.runNowCount++
	return s.runNowResult, s.runNowErr
}

type scriptedSyncProvider struct {
	name   string
	syncFn func(context.Context, store.ExternalAccount, tabsync.Sink) error
}

func (p *scriptedSyncProvider) Name() string {
	return p.name
}

func (p *scriptedSyncProvider) Sync(ctx context.Context, account store.ExternalAccount, sink tabsync.Sink) error {
	if p == nil || p.syncFn == nil {
		return nil
	}
	return p.syncFn(ctx, account, sink)
}

func TestSourcePollerLoopRunsRunnerUntilCanceled(t *testing.T) {
	app := newAuthedTestApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := &stubSourceSyncRunner{
		runOnceResult: tabsync.RunResult{NextDelay: 10 * time.Millisecond},
	}
	runner.runOnceFn = func() {
		if runner.runOnceCount >= 2 {
			cancel()
		}
	}
	app.sourceSync = runner

	done := make(chan struct{})
	go func() {
		defer close(done)
		app.runSourcePoller(ctx)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runSourcePoller did not stop after cancel")
	}
	if runner.runOnceCount < 2 {
		t.Fatalf("runOnceCount = %d, want at least 2", runner.runOnceCount)
	}
}

func TestSyncNowCommandForcesImmediateRun(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession: %v", err)
	}
	app.sourceSync = &stubSourceSyncRunner{
		runNowResult: tabsync.RunResult{
			Accounts: []tabsync.AccountResult{
				{AccountID: 1, Provider: "todoist", Label: "main"},
				{AccountID: 2, Provider: "bear", Label: "notes", Skipped: true, Reason: "interval"},
			},
		},
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "sync now")
	if !handled {
		t.Fatal("expected sync now to be handled")
	}
	if message != "Polled 2 external source account(s); 1 synced, 1 skipped." {
		t.Fatalf("message = %q, want poll summary", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	if got := strFromAny(payloads[0]["type"]); got != "sync_sources" {
		t.Fatalf("payload type = %q, want sync_sources", got)
	}
	if got := intFromAny(payloads[0]["synced_accounts"], 0); got != 1 {
		t.Fatalf("synced_accounts = %d, want 1", got)
	}
	if got := intFromAny(payloads[0]["skipped_accounts"], 0); got != 1 {
		t.Fatalf("skipped_accounts = %d, want 1", got)
	}
	runner, _ := app.sourceSync.(*stubSourceSyncRunner)
	if runner.runNowCount != 1 {
		t.Fatalf("runNowCount = %d, want 1", runner.runNowCount)
	}
}

func TestSyncSourcesNowPopulatesUnifiedInboxAcrossProviders(t *testing.T) {
	app := newAuthedTestApp(t)

	workspace, err := app.store.CreateWorkspace("Ops", filepath.Join(t.TempDir(), "ops"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	project, err := app.store.CreateProject("Ops Program", "ops-program", filepath.Join(t.TempDir(), "program"), "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	todoAccount, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist Work", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(todoist) error: %v", err)
	}
	imapAccount, err := app.store.CreateExternalAccount(store.SphereWork, store.ExternalProviderIMAP, "IMAP Work", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(imap) error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderTodoist, "project", "alpha", &workspace.ID, &project.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping(todoist) error: %v", err)
	}
	if _, err := app.store.SetContainerMapping(store.ExternalProviderIMAP, "folder", "INBOX/Work", &workspace.ID, &project.ID, nil); err != nil {
		t.Fatalf("SetContainerMapping(imap) error: %v", err)
	}

	sink := tabsync.NewStoreSink(app.store)
	engine := tabsync.NewEngine(app.store, app.store, sink, tabsync.Options{})
	engine.Register(&scriptedSyncProvider{
		name: store.ExternalProviderTodoist,
		syncFn: func(ctx context.Context, account store.ExternalAccount, sink tabsync.Sink) error {
			_, err := sink.UpsertItem(ctx, store.Item{
				Title:     "Prepare incident report",
				Source:    stringPtr(store.ExternalProviderTodoist),
				SourceRef: stringPtr("task:todo-1"),
			}, store.ExternalBinding{
				AccountID:    account.ID,
				Provider:     account.Provider,
				ObjectType:   "task",
				RemoteID:     "todo-1",
				ContainerRef: stringPtr("alpha"),
			})
			return err
		},
	})
	engine.Register(&scriptedSyncProvider{
		name: store.ExternalProviderIMAP,
		syncFn: func(ctx context.Context, account store.ExternalAccount, sink tabsync.Sink) error {
			title := "Architecture review request"
			artifact, err := sink.UpsertArtifact(ctx, store.Artifact{
				Kind:  store.ArtifactKindEmail,
				Title: &title,
			}, store.ExternalBinding{
				AccountID:    account.ID,
				Provider:     account.Provider,
				ObjectType:   "message",
				RemoteID:     "msg-1",
				ContainerRef: stringPtr("INBOX/Work"),
			})
			if err != nil {
				return err
			}
			_, err = sink.UpsertItem(ctx, store.Item{
				Title:      "Reply to architecture review",
				ArtifactID: &artifact.ID,
				Source:     stringPtr(store.ExternalProviderIMAP),
				SourceRef:  stringPtr("message:msg-1"),
			}, store.ExternalBinding{
				AccountID:    account.ID,
				Provider:     account.Provider,
				ObjectType:   "message_item",
				RemoteID:     "msg-1:item",
				ContainerRef: stringPtr("INBOX/Work"),
			})
			return err
		},
	})
	app.sourceSync = engine

	result, err := app.syncSourcesNow(context.Background())
	if err != nil {
		t.Fatalf("syncSourcesNow() error: %v", err)
	}
	if len(result.Accounts) != 2 {
		t.Fatalf("len(result.Accounts) = %d, want 2", len(result.Accounts))
	}
	for _, account := range result.Accounts {
		if account.Err != nil {
			t.Fatalf("account sync error = %v", account.Err)
		}
		if account.Skipped {
			t.Fatalf("account %d skipped unexpectedly: %#v", account.AccountID, account)
		}
	}

	todoItem, err := app.store.GetItemBySource(store.ExternalProviderTodoist, "task:todo-1")
	if err != nil {
		t.Fatalf("GetItemBySource(todoist) error: %v", err)
	}
	if todoItem.WorkspaceID == nil || *todoItem.WorkspaceID != workspace.ID {
		t.Fatalf("todo workspace = %v, want %d", todoItem.WorkspaceID, workspace.ID)
	}
	if todoItem.ProjectID == nil || *todoItem.ProjectID != project.ID {
		t.Fatalf("todo project = %v, want %q", todoItem.ProjectID, project.ID)
	}
	if todoItem.Sphere != store.SphereWork {
		t.Fatalf("todo sphere = %q, want %q", todoItem.Sphere, store.SphereWork)
	}
	if _, err := app.store.GetBindingByRemote(todoAccount.ID, store.ExternalProviderTodoist, "task", "todo-1"); err != nil {
		t.Fatalf("GetBindingByRemote(todoist) error: %v", err)
	}

	imapItem, err := app.store.GetItemBySource(store.ExternalProviderIMAP, "message:msg-1")
	if err != nil {
		t.Fatalf("GetItemBySource(imap) error: %v", err)
	}
	if imapItem.ArtifactID == nil {
		t.Fatal("imap item missing linked artifact")
	}
	if imapItem.WorkspaceID == nil || *imapItem.WorkspaceID != workspace.ID {
		t.Fatalf("imap workspace = %v, want %d", imapItem.WorkspaceID, workspace.ID)
	}
	if imapItem.ProjectID == nil || *imapItem.ProjectID != project.ID {
		t.Fatalf("imap project = %v, want %q", imapItem.ProjectID, project.ID)
	}
	if _, err := app.store.GetBindingByRemote(imapAccount.ID, store.ExternalProviderIMAP, "message", "msg-1"); err != nil {
		t.Fatalf("GetBindingByRemote(imap artifact) error: %v", err)
	}
	if _, err := app.store.GetBindingByRemote(imapAccount.ID, store.ExternalProviderIMAP, "message_item", "msg-1:item"); err != nil {
		t.Fatalf("GetBindingByRemote(imap item) error: %v", err)
	}
	links, err := app.store.ListArtifactWorkspaceLinks(workspace.ID)
	if err != nil {
		t.Fatalf("ListArtifactWorkspaceLinks() error: %v", err)
	}
	if len(links) != 1 || links[0].ArtifactID != *imapItem.ArtifactID {
		t.Fatalf("workspace artifact links = %#v, want artifact %d", links, *imapItem.ArtifactID)
	}

	rrInbox := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/inbox?sphere=work", nil)
	if rrInbox.Code != http.StatusOK {
		t.Fatalf("GET /api/items/inbox status = %d, want 200: %s", rrInbox.Code, rrInbox.Body.String())
	}
	inboxPayload := decodeJSONDataResponse(t, rrInbox)
	inboxItems, ok := inboxPayload["items"].([]any)
	if !ok || len(inboxItems) != 2 {
		t.Fatalf("inbox payload = %#v, want 2 items", inboxPayload)
	}

	rrTodo := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/inbox?sphere=work&source=todoist", nil)
	if rrTodo.Code != http.StatusOK {
		t.Fatalf("GET /api/items/inbox?source=todoist status = %d, want 200: %s", rrTodo.Code, rrTodo.Body.String())
	}
	todoPayload := decodeJSONDataResponse(t, rrTodo)
	todoItems, ok := todoPayload["items"].([]any)
	if !ok || len(todoItems) != 1 {
		t.Fatalf("todoist inbox payload = %#v, want 1 item", todoPayload)
	}
	if got := strFromAny(todoItems[0].(map[string]any)["title"]); got != "Prepare incident report" {
		t.Fatalf("todoist inbox title = %q, want %q", got, "Prepare incident report")
	}

	rrIMAP := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/items/inbox?sphere=work&source=imap", nil)
	if rrIMAP.Code != http.StatusOK {
		t.Fatalf("GET /api/items/inbox?source=imap status = %d, want 200: %s", rrIMAP.Code, rrIMAP.Body.String())
	}
	imapPayload := decodeJSONDataResponse(t, rrIMAP)
	imapItems, ok := imapPayload["items"].([]any)
	if !ok || len(imapItems) != 1 {
		t.Fatalf("imap inbox payload = %#v, want 1 item", imapPayload)
	}
	if got := strFromAny(imapItems[0].(map[string]any)["title"]); got != "Reply to architecture review" {
		t.Fatalf("imap inbox title = %q, want %q", got, "Reply to architecture review")
	}
}

func TestBroadcastItemsIngestedWebsocketNotification(t *testing.T) {
	app := newAuthedTestApp(t)

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	app.broadcastItemsIngested(2, "todoist")

	payload := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "items_ingested")
	if got, ok := payload["count"].(float64); !ok || int(got) != 2 {
		t.Fatalf("items_ingested count = %#v, want 2", payload["count"])
	}
	if got := strFromAny(payload["source"]); got != "todoist" {
		t.Fatalf("items_ingested source = %q, want todoist", got)
	}
}

func TestRouterRejectsIngestEndpoint(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/ingest", map[string]any{"items": []any{}})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("POST /api/ingest status = %d, want 404", rr.Code)
	}
}
