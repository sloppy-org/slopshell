package web

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineItemIntentFilterCommands(t *testing.T) {
	now := time.Date(2026, time.March, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		text         string
		wantAction   string
		wantSource   string
		wantNullWork bool
		wantClear    bool
		wantAll      bool
	}{
		{text: "show inbox", wantAction: "show_filtered_items", wantClear: true},
		{text: "show all inbox", wantAction: "show_filtered_items", wantClear: true, wantAll: true},
		{text: "zeige posteingang", wantAction: "show_filtered_items", wantClear: true},
		{text: "show todoist tasks", wantAction: "show_filtered_items", wantSource: store.ExternalProviderTodoist},
		{text: "show all todoist tasks", wantAction: "show_filtered_items", wantSource: store.ExternalProviderTodoist, wantAll: true},
		{text: "show my todoist tasks", wantAction: "show_filtered_items", wantSource: store.ExternalProviderTodoist},
		{text: "zeige todoist aufgaben", wantAction: "show_filtered_items", wantSource: store.ExternalProviderTodoist},
		{text: "show unassigned items", wantAction: "show_filtered_items", wantNullWork: true},
		{text: "zeige nicht zugeordnete items", wantAction: "show_filtered_items", wantNullWork: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.text, func(t *testing.T) {
			action := parseInlineItemIntent(tc.text, now)
			if action == nil {
				t.Fatal("expected inline action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if tc.wantClear && !systemActionTruthyParam(action.Params, "clear_filters") {
				t.Fatalf("expected clear_filters in %#v", action.Params)
			}
			filters := systemActionNestedParams(action.Params, "filters")
			if tc.wantSource != "" && systemActionStringParam(filters, "source") != tc.wantSource {
				t.Fatalf("source = %q, want %q", systemActionStringParam(filters, "source"), tc.wantSource)
			}
			if tc.wantNullWork && systemActionStringParam(filters, "workspace_id") != "null" {
				t.Fatalf("workspace_id = %q, want null", systemActionStringParam(filters, "workspace_id"))
			}
			if got := systemActionAllSpheresParam(action.Params); got != tc.wantAll {
				t.Fatalf("all_spheres = %v, want %v", got, tc.wantAll)
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionShowFilteredItems(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	privateWorkspace, err := app.store.CreateWorkspace("Private", filepath.Join(t.TempDir(), "private"), store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace(private) error: %v", err)
	}
	workWorkspace, err := app.store.CreateWorkspace("Work", filepath.Join(t.TempDir(), "work"), store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(work) error: %v", err)
	}
	if err := app.store.SetActiveSphere(store.SpherePrivate); err != nil {
		t.Fatalf("SetActiveSphere() error: %v", err)
	}
	past := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	sourceTodoist := store.ExternalProviderTodoist
	sourceExchange := store.ExternalProviderExchange
	if _, err := app.store.CreateItem("Todoist follow-up", store.ItemOptions{
		State:        store.ItemStateInbox,
		WorkspaceID:  &privateWorkspace.ID,
		VisibleAfter: &past,
		Source:       &sourceTodoist,
	}); err != nil {
		t.Fatalf("CreateItem(todoist) error: %v", err)
	}
	if _, err := app.store.CreateItem("Exchange follow-up", store.ItemOptions{
		State:        store.ItemStateInbox,
		WorkspaceID:  &workWorkspace.ID,
		VisibleAfter: &past,
		Source:       &sourceExchange,
	}); err != nil {
		t.Fatalf("CreateItem(exchange) error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show todoist tasks")
	if !handled {
		t.Fatal("expected filter command to be handled")
	}
	if message != "Opened inbox filtered to todoist with 1 item(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["type"]); got != "show_item_sidebar_view" {
		t.Fatalf("payload type = %q, want show_item_sidebar_view", got)
	}
	filters, ok := payloads[0]["filters"].(map[string]interface{})
	if !ok {
		t.Fatalf("filters payload = %#v", payloads[0])
	}
	if got := strFromAny(filters["source"]); got != store.ExternalProviderTodoist {
		t.Fatalf("filters.source = %q, want %q", got, store.ExternalProviderTodoist)
	}

	message, payloads, handled = app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "show all inbox")
	if !handled {
		t.Fatal("expected all-spheres inbox command to be handled")
	}
	if message != "Opened inbox across all spheres with 2 item(s)." {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads = %#v", payloads)
	}
	filters, ok = payloads[0]["filters"].(map[string]interface{})
	if !ok {
		t.Fatalf("filters payload = %#v", payloads[0])
	}
	if !boolFromAny(filters["all_spheres"]) {
		t.Fatalf("filters = %#v, want all_spheres=true", filters)
	}
}
