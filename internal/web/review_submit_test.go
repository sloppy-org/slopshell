package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewSubmitWritesMarkdownArtifact(t *testing.T) {
	app := newAuthedTestApp(t)

	rrProjects := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces", nil)
	if rrProjects.Code != http.StatusOK {
		t.Fatalf("projects status=%d body=%s", rrProjects.Code, rrProjects.Body.String())
	}
	var listPayload projectsListResponse
	if err := json.Unmarshal(rrProjects.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	if len(listPayload.Projects) == 0 {
		t.Fatalf("expected at least one project")
	}
	workspaceID := listPayload.Projects[0].ID

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/review/submit", map[string]any{
		"workspace_id":   workspaceID,
		"artifact_kind":  "text",
		"artifact_title": "README.md",
		"artifact_path":  "README.md",
		"comments": []map[string]any{
			{
				"text": "Tighten this explanation.",
				"anchor": map[string]any{
					"title":         "README.md",
					"line":          12,
					"selected_text": "Current text",
				},
			},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("review submit status=%d body=%s", rr.Code, rr.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode review submit response: %v", err)
	}
	reviewPath := strFromAny(payload["review_markdown_path"])
	manifestPath := strFromAny(payload["revision_manifest_path"])
	historyPath := strFromAny(payload["revision_history_path"])
	if reviewPath == "" {
		t.Fatalf("expected review_markdown_path in response")
	}
	if manifestPath == "" || historyPath == "" {
		t.Fatalf("expected revision paths in response: %v", payload)
	}

	project, err := app.store.GetProject(workspaceID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(project.RootPath, filepath.FromSlash(reviewPath)))
	if err != nil {
		t.Fatalf("read review artifact: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "Tighten this explanation.") {
		t.Fatalf("review artifact missing comment: %s", text)
	}
	if !strings.Contains(text, "Line: `12`") {
		t.Fatalf("review artifact missing line anchor: %s", text)
	}

	manifestContent, err := os.ReadFile(filepath.Join(project.RootPath, filepath.FromSlash(manifestPath)))
	if err != nil {
		t.Fatalf("read revision manifest: %v", err)
	}
	if !strings.Contains(string(manifestContent), "\"kind\": \"review\"") {
		t.Fatalf("revision manifest missing review entry: %s", string(manifestContent))
	}

	historyContent, err := os.ReadFile(filepath.Join(project.RootPath, filepath.FromSlash(historyPath)))
	if err != nil {
		t.Fatalf("read revision history: %v", err)
	}
	if !strings.Contains(string(historyContent), "Local Revision History: README.md") {
		t.Fatalf("revision history missing heading: %s", string(historyContent))
	}
}

func TestReviewSubmitClearsReviewPendingUnread(t *testing.T) {
	app := newAuthedTestApp(t)

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	if _, err := app.store.UpdateChatSessionMode(session.ID, "review"); err != nil {
		t.Fatalf("set review mode: %v", err)
	}

	app.markProjectOutput(project.WorkspacePath)

	readActivity := func() projectsActivityResponse {
		t.Helper()
		rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces/activity", nil)
		if rr.Code != http.StatusOK {
			t.Fatalf("activity status=%d body=%s", rr.Code, rr.Body.String())
		}
		var payload projectsActivityResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode activity response: %v", err)
		}
		return payload
	}

	assertState := func(unread, reviewPending bool) {
		t.Helper()
		payload := readActivity()
		for _, item := range payload.Projects {
			if item.WorkspaceID != projectIDString(project.ID) {
				continue
			}
			if item.ChatMode != "review" {
				t.Fatalf("chat_mode = %q, want review", item.ChatMode)
			}
			if item.Unread != unread {
				t.Fatalf("unread = %v, want %v", item.Unread, unread)
			}
			if item.ReviewPending != reviewPending {
				t.Fatalf("review_pending = %v, want %v", item.ReviewPending, reviewPending)
			}
			return
		}
		t.Fatalf("expected project %q in activity response", projectIDString(project.ID))
	}

	assertState(true, true)

	rrActivate := doAuthedJSONRequest(
		t,
		app.Router(),
		http.MethodPost,
		"/api/runtime/workspaces/"+projectIDString(project.ID)+"/activate",
		map[string]any{},
	)
	if rrActivate.Code != http.StatusOK {
		t.Fatalf("activate status=%d body=%s", rrActivate.Code, rrActivate.Body.String())
	}
	assertState(true, true)

	rrSubmit := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/review/submit", map[string]any{
		"workspace_id":   project.ID,
		"artifact_kind":  "text",
		"artifact_title": "README.md",
		"artifact_path":  "README.md",
		"comments": []map[string]any{
			{
				"text": "Resolve before merge.",
				"anchor": map[string]any{
					"title": "README.md",
					"line":  3,
				},
			},
		},
	})
	if rrSubmit.Code != http.StatusOK {
		t.Fatalf("review submit status=%d body=%s", rrSubmit.Code, rrSubmit.Body.String())
	}
	assertState(false, false)

	app.markProjectOutput(project.WorkspacePath)
	assertState(true, true)
}
