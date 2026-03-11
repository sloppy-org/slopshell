package web

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func stringSliceFromAny(v any) []string {
	switch typed := v.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			text := strings.TrimSpace(strFromAny(value))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func TestClassifyAndExecuteSystemActionForTurnSuggestsCanonicalActionsForLowConfidenceCursorItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensureDefaultProjectRecord() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}

	artifactTitle := "PR 144"
	artifact, err := app.store.CreateArtifact(store.ArtifactKindGitHubPR, nil, nil, &artifactTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	item, err := app.store.CreateItem("Await review feedback", store.ItemOptions{
		State:      store.ItemStateWaiting,
		ArtifactID: &artifact.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemActionForTurn(
		context.Background(),
		session.ID,
		session,
		"review this",
		&chatCursorContext{ItemID: item.ID, ItemTitle: item.Title},
		"",
	)
	if !handled {
		t.Fatal("expected low-confidence cursor request to be handled")
	}
	if !strings.Contains(message, "wasn't confident enough to guess") {
		t.Fatalf("message = %q, want low-confidence guidance", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payload count = %d, want 1", len(payloads))
	}
	payload := payloads[0]
	if got := strFromAny(payload["type"]); got != "suggest_canonical_actions" {
		t.Fatalf("payload type = %q, want suggest_canonical_actions", got)
	}
	if got := int64FromAny(payload["item_id"]); got != item.ID {
		t.Fatalf("payload item_id = %d, want %d", got, item.ID)
	}
	if got := strFromAny(payload["item_state"]); got != store.ItemStateWaiting {
		t.Fatalf("payload item_state = %q, want %q", got, store.ItemStateWaiting)
	}
	if got := strFromAny(payload["artifact_kind"]); got != string(store.ArtifactKindGitHubPR) {
		t.Fatalf("payload artifact_kind = %q, want %q", got, store.ArtifactKindGitHubPR)
	}
	gotActions := stringSliceFromAny(payload["actions"])
	wantActions := []string{"open_show", "annotate_capture", "bundle_review", "dispatch_execute", "track_item", "delegate_actor"}
	if !reflect.DeepEqual(gotActions, wantActions) {
		t.Fatalf("actions = %#v, want %#v", gotActions, wantActions)
	}
}
