package web

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineBatchIntent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		text        string
		wantAction  string
		wantWorker  string
		wantReview  string
		wantLimit   int
		wantLabel   string
		wantNumbers []int
	}{
		{text: "work through all open issues", wantAction: "batch_work"},
		{text: "work through P0 issues", wantAction: "batch_work", wantLabel: "p0"},
		{text: "work through 166-170", wantAction: "batch_work", wantNumbers: []int{166, 167, 168, 169, 170}},
		{text: "use claude for work, codex for review", wantAction: "batch_configure", wantWorker: "claude", wantReview: "codex"},
		{text: "stop after 3", wantAction: "batch_limit", wantLimit: 3},
		{text: "show me progress", wantAction: "batch_status"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()

			action := parseInlineBatchIntent(tc.text)
			if action == nil {
				t.Fatalf("parseInlineBatchIntent(%q) returned nil", tc.text)
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if got := optionalStringParam(action.Params, "worker"); got != tc.wantWorker {
				t.Fatalf("worker = %q, want %q", got, tc.wantWorker)
			}
			if got := optionalStringParam(action.Params, "reviewer"); got != tc.wantReview {
				t.Fatalf("reviewer = %q, want %q", got, tc.wantReview)
			}
			if got := systemActionIntParam(action.Params, "limit"); got != tc.wantLimit {
				t.Fatalf("limit = %d, want %d", got, tc.wantLimit)
			}
			if got := optionalStringParam(action.Params, "label_filter"); got != tc.wantLabel {
				t.Fatalf("label_filter = %q, want %q", got, tc.wantLabel)
			}
			if got := systemActionIntListParam(action.Params, "issue_numbers"); len(got) != len(tc.wantNumbers) {
				t.Fatalf("issue_numbers = %v, want %v", got, tc.wantNumbers)
			} else {
				for i := range got {
					if got[i] != tc.wantNumbers[i] {
						t.Fatalf("issue_numbers = %v, want %v", got, tc.wantNumbers)
					}
				}
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionBatchConfigPersistsWorkspaceSettings(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Batch", filepath.Join(t.TempDir(), "batch"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "use claude for work, codex for review")
	if !handled {
		t.Fatal("expected batch configure command to be handled")
	}
	if message != "Batch config for workspace Batch set to worker claude and reviewer codex." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "batch_status" {
		t.Fatalf("payloads = %#v", payloads)
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "stop after 3")
	if !handled {
		t.Fatal("expected batch limit command to be handled")
	}
	if message != "Batch limit for workspace Batch set to 3 item(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "batch_status" {
		t.Fatalf("payloads = %#v", payloads)
	}

	watch, err := app.store.GetWorkspaceWatch(workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceWatch() error: %v", err)
	}
	cfg, err := decodeBatchWorkConfig(watch.ConfigJSON)
	if err != nil {
		t.Fatalf("decodeBatchWorkConfig() error: %v", err)
	}
	if cfg.Worker != "claude" || cfg.Reviewer != "codex" {
		t.Fatalf("config worker/reviewer = %#v", cfg)
	}
	if cfg.Limit != 3 {
		t.Fatalf("config limit = %d, want 3", cfg.Limit)
	}
	if batchConfigMode(cfg) != batchModeRun {
		t.Fatalf("config mode = %q, want %q", batchConfigMode(cfg), batchModeRun)
	}
	if watch.Enabled {
		t.Fatal("watch enabled = true, want false for saved config")
	}
}

func TestClassifyAndExecuteSystemActionBatchWorkUsesFiltersLimitAndStatus(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Batch", filepath.Join(t.TempDir(), "batch"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}

	first := createGitHubBatchItem(t, app, workspace, 166, "Fix parser edge case", "P0")
	time.Sleep(20 * time.Millisecond)
	second := createGitHubBatchItem(t, app, workspace, 167, "Repair flaky sync test", "P0")
	time.Sleep(20 * time.Millisecond)
	third := createGitHubBatchItem(t, app, workspace, 168, "Refactor inbox styles", "P1")

	orderCh := make(chan int64, 2)
	app.workspaceWatchProcessor = func(_ context.Context, _ store.Workspace, item store.ItemSummary) error {
		orderCh <- item.ID
		return nil
	}

	if _, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "stop after 1"); !handled {
		t.Fatal("expected batch limit command to be handled")
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "work through P0 issues")
	if !handled {
		t.Fatal("expected batch work command to be handled")
	}
	if message != "Started batch for workspace Batch with 2 open item(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "batch_status" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if int64FromAny(payloads[0]["item_count"]) != 2 {
		t.Fatalf("item_count = %v, want 2", payloads[0]["item_count"])
	}

	waitForCondition(t, 2*time.Second, func() bool {
		watch, err := app.store.GetWorkspaceWatch(workspace.ID)
		if err != nil || watch.Enabled {
			return false
		}
		got, err := app.store.GetItem(first.ID)
		return err == nil && got.State == store.ItemStateDone
	})

	gotOrder := <-orderCh
	if gotOrder != first.ID {
		t.Fatalf("processed item = %d, want %d", gotOrder, first.ID)
	}
	select {
	case extra := <-orderCh:
		t.Fatalf("unexpected extra processed item %d", extra)
	default:
	}

	gotSecond, err := app.store.GetItem(second.ID)
	if err != nil {
		t.Fatalf("GetItem(second) error: %v", err)
	}
	if gotSecond.State != store.ItemStateInbox {
		t.Fatalf("second state = %q, want inbox", gotSecond.State)
	}
	gotThird, err := app.store.GetItem(third.ID)
	if err != nil {
		t.Fatalf("GetItem(third) error: %v", err)
	}
	if gotThird.State != store.ItemStateInbox {
		t.Fatalf("third state = %q, want inbox", gotThird.State)
	}

	progressMessage, progressPayloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show me progress")
	if !handled {
		t.Fatal("expected batch status command to be handled")
	}
	if !strings.Contains(progressMessage, "Latest batch for workspace Batch finished completed: 1 completed, 0 failed.") {
		t.Fatalf("progress message = %q", progressMessage)
	}
	if len(progressPayloads) != 1 || strFromAny(progressPayloads[0]["type"]) != "batch_status" {
		t.Fatalf("progress payloads = %#v", progressPayloads)
	}
	items, ok := progressPayloads[0]["items"].([]store.BatchRunItem)
	if !ok || len(items) != 1 {
		t.Fatalf("progress items = %#v", progressPayloads[0]["items"])
	}
	if items[0].ItemID != first.ID || items[0].Status != "completed" {
		t.Fatalf("progress items = %#v", items)
	}
}

func createGitHubBatchItem(t *testing.T, app *App, workspace store.Workspace, number int, title string, labels ...string) store.Item {
	t.Helper()

	source := "github"
	sourceRef := githubIssueSourceRef("owner/repo", number)
	item, err := app.store.CreateItem(title, store.ItemOptions{
		WorkspaceID: &workspace.ID,
		Source:      &source,
		SourceRef:   &sourceRef,
	})
	if err != nil {
		t.Fatalf("CreateItem(%d) error: %v", number, err)
	}
	issue := ghIssueListItem{
		Number: number,
		Title:  title,
		URL:    fmt.Sprintf("https://github.com/owner/repo/issues/%d", number),
		State:  "open",
	}
	for _, label := range labels {
		issue.Labels = append(issue.Labels, ghIssueListLabel{Name: label})
	}
	if err := app.syncGitHubIssueArtifact(item, "owner/repo", issue); err != nil {
		t.Fatalf("syncGitHubIssueArtifact(%d) error: %v", number, err)
	}
	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(%d) error: %v", number, err)
	}
	return updated
}

func optionalStringParam(params map[string]interface{}, key string) string {
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}
