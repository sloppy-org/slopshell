package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestContextListAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	root, err := app.store.CreateContext("Work", nil)
	if err != nil {
		t.Fatalf("CreateContext(root) error: %v", err)
	}
	if _, err := app.store.CreateContext("W7x", &root.ID); err != nil {
		t.Fatalf("CreateContext(child) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/contexts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("context list status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONDataResponse(t, rr)
	contexts, ok := payload["contexts"].([]any)
	if !ok || len(contexts) < 3 {
		t.Fatalf("context list payload = %#v", payload)
	}

	byName := map[string]map[string]any{}
	for _, entry := range contexts {
		row, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("context row = %#v", entry)
		}
		byName[strings.ToLower(strFromAny(row["name"]))] = row
	}
	work, ok := byName["work"]
	if !ok {
		t.Fatalf("context list missing work root: %#v", payload)
	}
	private, ok := byName["private"]
	if !ok {
		t.Fatalf("context list missing private root: %#v", payload)
	}
	if got := int64FromAny(private["parent_id"]); got != 0 {
		t.Fatalf("private parent_id = %d, want 0", got)
	}
	child, ok := byName["w7x"]
	if !ok {
		t.Fatalf("context list missing child context: %#v", payload)
	}
	if got := int64FromAny(child["parent_id"]); got != root.ID {
		t.Fatalf("child parent_id = %d, want %d", got, root.ID)
	}
	if got := int64FromAny(work["id"]); got != root.ID {
		t.Fatalf("work id = %d, want %d", got, root.ID)
	}
}
