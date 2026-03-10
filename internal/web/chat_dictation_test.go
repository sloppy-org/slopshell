package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInferDictationTargetKind(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		prompt        string
		artifactTitle string
		want          string
	}{
		{name: "review prompt", prompt: "write my review", want: dictationTargetReviewComment},
		{name: "email reply prompt", prompt: "take a letter", want: dictationTargetEmailReply},
		{name: "email artifact", prompt: "dictate", artifactTitle: "Email Thread", want: dictationTargetEmailReply},
		{name: "fallback document", prompt: "dictate", artifactTitle: "notes.md", want: dictationTargetDocumentSection},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := inferDictationTargetKind(tc.prompt, tc.artifactTitle); got != tc.want {
				t.Fatalf("inferDictationTargetKind(%q, %q) = %q, want %q", tc.prompt, tc.artifactTitle, got, tc.want)
			}
		})
	}
}

func TestShapeDictationDraftByTarget(t *testing.T) {
	t.Parallel()

	email := shapeDictationDraft(dictationTargetEmailReply, "Customer thread", "Thanks for the update.\n\nI'll send a revision tomorrow.")
	if !strings.Contains(email, "# Email Reply Draft") || !strings.Contains(email, "Thread: Customer thread") {
		t.Fatalf("email draft = %q", email)
	}
	review := shapeDictationDraft(dictationTargetReviewComment, "PR #12", "This branch looks good.\n\nPlease tighten the nil check.")
	if !strings.Contains(review, "# Review Comment Draft") || !strings.Contains(review, "- This branch looks good.") {
		t.Fatalf("review draft = %q", review)
	}
	document := shapeDictationDraft(dictationTargetDocumentSection, "Design Notes", "Intro paragraph.\n\nFollow-up paragraph.")
	if !strings.Contains(document, "# Document Section Draft") || !strings.Contains(document, "Working title: Design Notes") {
		t.Fatalf("document draft = %q", document)
	}
}

func TestChatSessionDictationLifecycle(t *testing.T) {
	app := newAuthedTestApp(t)
	projectRoot := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		t.Fatalf("mkdir project root: %v", err)
	}
	project, err := app.store.CreateProject("Dictation", "dictation-project", projectRoot, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}

	start := doAuthedJSONRequest(t, app.Router(), "POST", "/api/chat/sessions/"+session.ID+"/dictation/start", map[string]any{
		"prompt":         "write my review",
		"artifact_title": "PR #18",
	})
	if start.Code != 200 {
		t.Fatalf("start status = %d: %s", start.Code, start.Body.String())
	}
	startPayload := decodeJSONResponse(t, start)
	dictation := startPayload["dictation"].(map[string]any)
	if got := strings.TrimSpace(dictation["target_kind"].(string)); got != dictationTargetReviewComment {
		t.Fatalf("target_kind = %q, want %q", got, dictationTargetReviewComment)
	}

	appendResp := doAuthedJSONRequest(t, app.Router(), "POST", "/api/chat/sessions/"+session.ID+"/dictation/append", map[string]any{
		"text": "Please tighten the nil guard before merge.",
	})
	if appendResp.Code != 200 {
		t.Fatalf("append status = %d: %s", appendResp.Code, appendResp.Body.String())
	}
	appendPayload := decodeJSONResponse(t, appendResp)
	dictation = appendPayload["dictation"].(map[string]any)
	scratchPath := strings.TrimSpace(dictation["scratch_path"].(string))
	if !strings.HasPrefix(scratchPath, ".tabura/artifacts/tmp/") {
		t.Fatalf("scratch_path = %q", scratchPath)
	}
	if !strings.Contains(strings.TrimSpace(dictation["draft_text"].(string)), "# Review Comment Draft") {
		t.Fatalf("draft_text = %q", dictation["draft_text"])
	}
	abs := filepath.Join(projectRoot, filepath.FromSlash(scratchPath))
	body, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", abs, err)
	}
	if !strings.Contains(string(body), "Please tighten the nil guard before merge.") {
		t.Fatalf("scratch file body = %q", string(body))
	}

	saveResp := doAuthedJSONRequest(t, app.Router(), "PUT", "/api/chat/sessions/"+session.ID+"/dictation/draft", map[string]any{
		"draft_text": "# Review Comment Draft\n\n- Updated after edit",
	})
	if saveResp.Code != 200 {
		t.Fatalf("draft put status = %d: %s", saveResp.Code, saveResp.Body.String())
	}
	updatedBody, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("ReadFile(%q) after save error: %v", abs, err)
	}
	if !strings.Contains(string(updatedBody), "Updated after edit") {
		t.Fatalf("updated scratch body = %q", string(updatedBody))
	}

	stopResp := doAuthedJSONRequest(t, app.Router(), "DELETE", "/api/chat/sessions/"+session.ID+"/dictation", nil)
	if stopResp.Code != 200 {
		t.Fatalf("delete status = %d: %s", stopResp.Code, stopResp.Body.String())
	}
	stopPayload := decodeJSONResponse(t, stopResp)
	dictation = stopPayload["dictation"].(map[string]any)
	if active, _ := dictation["active"].(bool); active {
		t.Fatalf("active = true, want false")
	}
}
