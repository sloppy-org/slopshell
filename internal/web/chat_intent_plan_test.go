package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSystemActionsJSONSupportsActionLists(t *testing.T) {
	actions, err := parseSystemActionsJSON(`{"actions":[{"action":"shell","command":"ls -1"},{"action":"open_file_canvas","path":"README.md"}]}`)
	if err != nil {
		t.Fatalf("parseSystemActionsJSON returned error: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions length = %d, want 2", len(actions))
	}
	if actions[0].Action != "shell" {
		t.Fatalf("action[0] = %q, want shell", actions[0].Action)
	}
	if got := systemActionShellCommand(actions[0].Params); got != "ls -1" {
		t.Fatalf("shell command = %q, want ls -1", got)
	}
	if actions[1].Action != "open_file_canvas" {
		t.Fatalf("action[1] = %q, want open_file_canvas", actions[1].Action)
	}
	if got := systemActionOpenPath(actions[1].Params); got != "README.md" {
		t.Fatalf("open path = %q, want README.md", got)
	}
}

func TestParseSystemActionsJSONExtractsEmbeddedPayload(t *testing.T) {
	actions, err := parseSystemActionsJSON(`Open the README file in canvas.

{"actions":[{"action":"shell","command":"ls -1"},{"action":"open_file_canvas","path":"README.md"}]}`)
	if err != nil {
		t.Fatalf("parseSystemActionsJSON returned error: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions length = %d, want 2", len(actions))
	}
	if actions[0].Action != "shell" || actions[1].Action != "open_file_canvas" {
		t.Fatalf("unexpected actions: %#v", actions)
	}
}

func TestParseSystemActionsJSONRepairsMalformedCommandQuotes(t *testing.T) {
	actions, err := parseSystemActionsJSON(`{"actions":[{"action":"shell","command":"find . -maxdepth 3 -type f -iname "README*" | head -n 1"},{"action":"open_file_canvas","path":"$last_shell_path"}]}`)
	if err != nil {
		t.Fatalf("parseSystemActionsJSON returned error: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions length = %d, want 2", len(actions))
	}
	if got := strings.TrimSpace(systemActionShellCommand(actions[0].Params)); got == "" {
		t.Fatal("expected repaired shell command")
	}
}

func TestSuggestShellCommandRetryRepairsJQTrailingBracketTypos(t *testing.T) {
	command := `curl -s 'https://example.invalid' | jq -r '.current}'`
	output := `jq: error: syntax error, unexpected INVALID_CHARACTER, expecting end of file at , line 1, column 9:
    .current}
            ^
jq: 1 compile error`

	fixed, reason, ok := suggestShellCommandRetry(command, output)
	if !ok {
		t.Fatal("expected retry suggestion for jq typo")
	}
	if strings.TrimSpace(reason) == "" {
		t.Fatal("expected non-empty retry reason")
	}
	if fixed == command {
		t.Fatal("expected fixed command to differ from original")
	}
	if strings.Contains(fixed, ".current}") {
		t.Fatalf("fixed command still contains typo: %q", fixed)
	}
	if !strings.Contains(fixed, ".current") {
		t.Fatalf("fixed command missing corrected jq filter: %q", fixed)
	}
}

func TestSuggestShellCommandRetryNoopWithoutJQSyntaxError(t *testing.T) {
	command := `echo hello`
	output := `some unrelated stderr`
	if fixed, reason, ok := suggestShellCommandRetry(command, output); ok || fixed != "" || reason != "" {
		t.Fatalf("expected no retry suggestion, got fixed=%q reason=%q ok=%v", fixed, reason, ok)
	}
}

func TestClassifyIntentPlanWithLLMMultiAction(t *testing.T) {
	llm := setupMockIntentLLMServer(
		t,
		200,
		`{"actions":[{"action":"shell","command":"ls -1"},{"action":"open_file_canvas","path":"$last_shell_path"}]}`,
	)
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	actions, err := app.classifyIntentPlanWithLLM(context.Background(), "Open README")
	if err != nil {
		t.Fatalf("classifyIntentPlanWithLLM returned error: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("actions length = %d, want 2", len(actions))
	}
	if actions[0].Action != "shell" {
		t.Fatalf("action[0] = %q, want shell", actions[0].Action)
	}
	if actions[1].Action != "open_file_canvas" {
		t.Fatalf("action[1] = %q, want open_file_canvas", actions[1].Action)
	}
}

func TestClassifyIntentPlanWithLLMCanonicalAction(t *testing.T) {
	llm := setupMockIntentLLMServer(
		t,
		200,
		`{"kind":"canonical_action","action":"delegate_actor","actor":"Codex"}`,
	)
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	actions, err := app.classifyIntentPlanWithLLM(context.Background(), "delegate this to Codex")
	if err != nil {
		t.Fatalf("classifyIntentPlanWithLLM returned error: %v", err)
	}
	if len(actions) != 1 {
		t.Fatalf("actions length = %d, want 1", len(actions))
	}
	if actions[0].Action != "delegate_item" {
		t.Fatalf("action[0] = %q, want delegate_item", actions[0].Action)
	}
	if got := systemActionActorName(actions[0].Params); got != "Codex" {
		t.Fatalf("actor = %q, want Codex", got)
	}
}

func TestFirstShellPathFromOutput(t *testing.T) {
	if got := firstShellPathFromOutput("(no output)\n./README.md\n"); got != "README.md" {
		t.Fatalf("firstShellPathFromOutput returned %q, want README.md", got)
	}
	if got := firstShellPathFromOutput("\n\n"); got != "" {
		t.Fatalf("firstShellPathFromOutput returned %q, want empty", got)
	}
}

func TestExtractOpenRequestHintsGeneric(t *testing.T) {
	hints := extractOpenRequestHints(`Please open the "docs/CLAUDE.md" file in canvas.`)
	if !stringSliceContains(hints, "docs/claude.md") {
		t.Fatalf("expected docs/claude.md in hints, got %#v", hints)
	}
	if !stringSliceContains(hints, "claude.md") {
		t.Fatalf("expected claude.md in hints, got %#v", hints)
	}
	if !stringSliceContains(hints, "claude") {
		t.Fatalf("expected claude in hints, got %#v", hints)
	}
}

func TestExecuteSystemActionPlanUsesRequestHintForPlaceholder(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "go.mod"), []byte("module test"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("hello-readme"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	_, payloads, err := app.executeSystemActionPlan(session.ID, session, "Open README file in canvas", []*SystemAction{
		{
			Action: "shell",
			Params: map[string]interface{}{
				"command": "printf './go.mod\\n./README.md\\n'",
			},
		},
		{
			Action: "open_file_canvas",
			Params: map[string]interface{}{
				"path": systemActionLastShellPathPlaceholder,
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSystemActionPlan returned error: %v", err)
	}
	if len(payloads) < 2 {
		t.Fatalf("payloads length = %d, want >= 2", len(payloads))
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["title"])); got != "README.md" {
		t.Fatalf("canvas title = %q, want README.md", got)
	}
	if got := strings.TrimSpace(strFromAny(observed["markdown_or_text"])); got != "hello-readme" {
		t.Fatalf("canvas content = %q, want hello-readme", got)
	}
}

func TestExecuteSystemActionPlanResolvesLastShellPathPlaceholder(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("hello-readme"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	message, payloads, err := app.executeSystemActionPlan(session.ID, session, "Open README", []*SystemAction{
		{
			Action: "shell",
			Params: map[string]interface{}{
				"command": "printf './README.md\\n'",
			},
		},
		{
			Action: "open_file_canvas",
			Params: map[string]interface{}{
				"path": systemActionLastShellPathPlaceholder,
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSystemActionPlan returned error: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("payloads length = %d, want 2", len(payloads))
	}
	if strings.TrimSpace(message) == "" {
		t.Fatalf("expected non-empty plan message")
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["title"])); got != "README.md" {
		t.Fatalf("canvas title = %q, want README.md", got)
	}
}

func TestExecuteSystemActionPlanPrefersRootReadmeMarkdownOverNestedExtensionlessReadme(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("root-readme"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project.RootPath, "gcc-build", "gcc", "include-fixed"), 0o755); err != nil {
		t.Fatalf("mkdir nested path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "gcc-build", "gcc", "include-fixed", "README"), []byte("nested-readme"), 0o644); err != nil {
		t.Fatalf("write nested README: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	_, payloads, err := app.executeSystemActionPlan(session.ID, session, "Open readme", []*SystemAction{
		{
			Action: "shell",
			Params: map[string]interface{}{
				"command": "printf './gcc-build/gcc/include-fixed/README\\n'",
			},
		},
		{
			Action: "open_file_canvas",
			Params: map[string]interface{}{
				"path": systemActionLastShellPathPlaceholder,
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSystemActionPlan returned error: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("payloads length = %d, want 2", len(payloads))
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["title"])); got != "README.md" {
		t.Fatalf("canvas title = %q, want README.md", got)
	}
	if got := strings.TrimSpace(strFromAny(observed["markdown_or_text"])); got != "root-readme" {
		t.Fatalf("canvas content = %q, want root-readme", got)
	}
}

func TestExecuteSystemActionPlanPrefersRootClaudeMarkdownCaseInsensitive(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "CLAUDE.md"), []byte("root-claude"), 0o644); err != nil {
		t.Fatalf("write CLAUDE.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project.RootPath, "tmp", "docs"), 0o755); err != nil {
		t.Fatalf("mkdir nested path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "tmp", "docs", "claude"), []byte("nested-claude"), 0o644); err != nil {
		t.Fatalf("write nested claude: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	_, payloads, err := app.executeSystemActionPlan(session.ID, session, "Open claude", []*SystemAction{
		{
			Action: "shell",
			Params: map[string]interface{}{
				"command": "printf './tmp/docs/claude\\n'",
			},
		},
		{
			Action: "open_file_canvas",
			Params: map[string]interface{}{
				"path": systemActionLastShellPathPlaceholder,
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSystemActionPlan returned error: %v", err)
	}
	if len(payloads) != 2 {
		t.Fatalf("payloads length = %d, want 2", len(payloads))
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["title"])); got != "CLAUDE.md" {
		t.Fatalf("canvas title = %q, want CLAUDE.md", got)
	}
	if got := strings.TrimSpace(strFromAny(observed["markdown_or_text"])); got != "root-claude" {
		t.Fatalf("canvas content = %q, want root-claude", got)
	}
}

func TestClassifyAndExecuteSystemActionWithoutIntentLLMDoesNotAutoOpen(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("hello-readme"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	_, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "Open README")
	if handled {
		t.Fatal("expected request to remain unhandled without intent LLM")
	}
	if showCalls != 0 {
		t.Fatalf("canvas_artifact_show calls = %d, want 0", showCalls)
	}
}

func TestClassifyAndExecuteSystemActionToolRequestsUseQwenPlan(t *testing.T) {
	llmCalls := 0
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		llmCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"actions":[{"action":"shell","command":"ls -1"},{"action":"shell","command":"find . -maxdepth 2 -type f -iname 'README*' | head -n 1"},{"action":"open_file_canvas","path":"$last_shell_path"}]}`,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("hello-readme"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	_, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "Open the README file.")
	if !handled {
		t.Fatal("expected request to be handled by Qwen action plan")
	}
	if llmCalls == 0 {
		t.Fatal("expected Qwen intent LLM call")
	}
	if len(payloads) < 3 {
		t.Fatalf("payloads length = %d, want >= 3", len(payloads))
	}
	if got := strings.TrimSpace(strFromAny(payloads[0]["command"])); got != "ls -1" {
		t.Fatalf("expected first action command from LLM plan, got %q", got)
	}
	if got := strings.TrimSpace(strFromAny(payloads[len(payloads)-1]["type"])); got != "open_file_canvas" {
		t.Fatalf("last payload type = %q, want open_file_canvas", got)
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["title"])); got != "README.md" {
		t.Fatalf("canvas title = %q, want README.md", got)
	}
}

func TestClassifyAndExecuteSystemActionFallsThroughWhenLLMUnavailable(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = "http://127.0.0.1:1"

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "be quiet")
	if handled {
		t.Fatalf("expected request to remain unhandled, got message %q and %d payloads", message, len(payloads))
	}
}

func TestClassifyAndExecuteSystemActionWithIntentLLMHandlesSupportedPlan(t *testing.T) {
	llmCalls := 0
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		llmCalls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"actions":[{"action":"shell","command":"find . -maxdepth 2 -type f -iname 'README*' | head -n 1"},{"action":"open_file_canvas","path":"$last_shell_path"}]}`,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("hello-readme"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	_, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "Open README")
	if !handled {
		t.Fatal("expected request to be handled")
	}
	if llmCalls == 0 {
		t.Fatal("expected Qwen call")
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
}

func TestClassifyAndExecuteSystemActionHandlesMalformedQwenJSON(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"actions":[{"action":"shell","command":"find . -maxdepth 2 -type f -iname "README*" | head -n 1"},{"action":"open_file_canvas","path":"$last_shell_path"}]}`,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("hello-readme"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	_, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "Open the README file.")
	if !handled {
		t.Fatal("expected malformed JSON plan to be repaired and executed")
	}
	if len(payloads) < 2 {
		t.Fatalf("payloads length = %d, want >= 2", len(payloads))
	}
	if got := strings.TrimSpace(strFromAny(payloads[len(payloads)-1]["type"])); got != "open_file_canvas" {
		t.Fatalf("last payload type = %q, want open_file_canvas", got)
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
}

func TestClassifyIntentPlanWithLLMRepairsMissingOpenAction(t *testing.T) {
	callCount := 0
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		callCount++
		content := `{"actions":[{"action":"shell","command":"find . -maxdepth 2 -type f -iname 'README*' | head -n 1"}]}`
		if callCount >= 2 {
			content = `{"actions":[{"action":"shell","command":"find . -maxdepth 2 -type f -iname 'README*' | head -n 1"},{"action":"open_file_canvas","path":"$last_shell_path"}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": content,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	actions, err := app.classifyIntentPlanWithLLM(context.Background(), "Open README")
	if err != nil {
		t.Fatalf("classifyIntentPlanWithLLM returned error: %v", err)
	}
	if callCount < 2 {
		t.Fatalf("expected retry call for missing open action, got %d calls", callCount)
	}
	if len(actions) < 2 {
		t.Fatalf("actions length = %d, want >= 2", len(actions))
	}
	if !planContainsAction(actions, "open_file_canvas") {
		t.Fatalf("expected repaired plan with open_file_canvas, got %#v", actions)
	}
}

func TestClassifyIntentPlanWithLLMRepairsChatResponseForOpenRequest(t *testing.T) {
	callCount := 0
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		callCount++
		content := `{"action":"chat"}`
		if callCount >= 2 {
			content = `{"actions":[{"action":"shell","command":"find . -maxdepth 3 -type f \\( -iname '*readme*' -o -iname '*.md' \\) | head -n 1"},{"action":"open_file_canvas","path":"$last_shell_path"}]}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": content,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	actions, err := app.classifyIntentPlanWithLLM(context.Background(), "Open README")
	if err != nil {
		t.Fatalf("classifyIntentPlanWithLLM returned error: %v", err)
	}
	if callCount < 2 {
		t.Fatalf("expected retry call for chat response, got %d calls", callCount)
	}
	if len(actions) < 2 {
		t.Fatalf("actions length = %d, want >= 2", len(actions))
	}
	if !planContainsAction(actions, "open_file_canvas") {
		t.Fatalf("expected repaired plan with open_file_canvas, got %#v", actions)
	}
}

func TestClassifyIntentPlanWithLLMAppendsOpenActionAfterRetryForShellOnlyPlan(t *testing.T) {
	callCount := 0
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"actions":[{"action":"shell","command":"find . -maxdepth 2 -type f -iname 'README*' | head -n 1"}]}`,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	actions, err := app.classifyIntentPlanWithLLM(context.Background(), "Open README")
	if err != nil {
		t.Fatalf("classifyIntentPlanWithLLM returned error: %v", err)
	}
	if callCount < 2 {
		t.Fatalf("expected retry call, got %d", callCount)
	}
	if len(actions) < 2 {
		t.Fatalf("actions length = %d, want >= 2", len(actions))
	}
	if !planContainsAction(actions, "open_file_canvas") {
		t.Fatalf("expected appended open_file_canvas action, got %#v", actions)
	}
}

func TestClassifyIntentPlanWithLLMRejectsOpenRequestWithoutShellOrOpenActionAfterRetry(t *testing.T) {
	callCount := 0
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"action":"chat"}`,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	actions, err := app.classifyIntentPlanWithLLM(context.Background(), "Open README")
	if err != nil {
		t.Fatalf("classifyIntentPlanWithLLM returned error: %v", err)
	}
	if callCount < 2 {
		t.Fatalf("expected retry call, got %d", callCount)
	}
	if len(actions) != 0 {
		t.Fatalf("actions length = %d, want 0", len(actions))
	}
}

func TestClassifyAndExecuteSystemActionOpenRequestUsesFallbackPlanWhenLLMPlanInvalid(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"action":"chat"}`,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("fallback-readme"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "Open README")
	if !handled {
		t.Fatal("expected handled result for invalid open-file plan")
	}
	if len(payloads) < 2 {
		t.Fatalf("payloads length = %d, want >= 2", len(payloads))
	}
	if strings.Contains(strings.ToLower(message), "couldn't open") {
		t.Fatalf("unexpected failure message: %q", message)
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["title"])); got != "README.md" {
		t.Fatalf("canvas title = %q, want README.md", got)
	}
}

func TestExecuteSystemActionPlanPrefersTopLevelSiblingForPlaceholder(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(project.RootPath, "pr", "92613"), 0o755); err != nil {
		t.Fatalf("mkdir nested path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "CLAUDE.md"), []byte("root-claude"), 0o644); err != nil {
		t.Fatalf("write root CLAUDE.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "pr", "92613", "CLAUDE.md"), []byte("nested-claude"), 0o644); err != nil {
		t.Fatalf("write nested CLAUDE.md: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	server := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer server.Close()
	port, err := extractPort(server.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	_, payloads, err := app.executeSystemActionPlan(session.ID, session, "Open CLAUDE file", []*SystemAction{
		{
			Action: "shell",
			Params: map[string]interface{}{
				"command": "printf './pr/92613/CLAUDE.md\\n'",
			},
		},
		{
			Action: "open_file_canvas",
			Params: map[string]interface{}{
				"path": systemActionLastShellPathPlaceholder,
			},
		},
	})
	if err != nil {
		t.Fatalf("executeSystemActionPlan returned error: %v", err)
	}
	if len(payloads) < 2 {
		t.Fatalf("payloads length = %d, want >= 2", len(payloads))
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["title"])); got != "CLAUDE.md" {
		t.Fatalf("canvas title = %q, want CLAUDE.md", got)
	}
	if got := strings.TrimSpace(strFromAny(observed["markdown_or_text"])); got != "root-claude" {
		t.Fatalf("canvas content = %q, want root-claude", got)
	}
}

func TestClassifyAndExecuteSystemActionUsesClarificationContextForOpenFileFollowUp(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		messages, _ := payload["messages"].([]interface{})
		if len(messages) == 0 {
			http.Error(w, "missing messages", http.StatusBadRequest)
			return
		}
		last, _ := messages[len(messages)-1].(map[string]interface{})
		content := strings.TrimSpace(strFromAny(last["content"]))
		responseContent := `{}`
		if strings.Contains(content, "Open README") && strings.Contains(content, "The root one") {
			responseContent = `{"action":"open_file_canvas","path":"README.md"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": responseContent,
					},
				},
			},
		})
	}))
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentLLMURL = llm.URL

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(project.RootPath, "README.md"), []byte("root readme"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	showCalls := 0
	var observed map[string]interface{}
	canvasServer := setupMockCanvasShowServer(t, &showCalls, &observed)
	defer canvasServer.Close()
	port, err := extractPort(canvasServer.URL)
	if err != nil {
		t.Fatalf("extract canvas port: %v", err)
	}
	app.tunnels.setPort(app.canvasSessionIDForProject(project), port)

	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "Open README", "Open README", "text"); err != nil {
		t.Fatalf("add first user message: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "assistant", "Which README should I open?", "Which README should I open?", "text"); err != nil {
		t.Fatalf("add assistant clarification: %v", err)
	}
	if _, err := app.store.AddChatMessage(session.ID, "user", "The root one", "The root one", "text"); err != nil {
		t.Fatalf("add follow-up user message: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "The root one")
	if !handled {
		t.Fatal("expected contextual follow-up to be handled")
	}
	if len(payloads) == 0 {
		t.Fatal("expected open_file_canvas payloads")
	}
	if showCalls < 1 {
		t.Fatalf("canvas_artifact_show calls = %d, want >= 1", showCalls)
	}
	if got := strings.TrimSpace(strFromAny(observed["title"])); got != "README.md" {
		t.Fatalf("canvas title = %q, want README.md", got)
	}
	if !strings.Contains(message, "README.md") {
		t.Fatalf("assistant message = %q, want README.md mention", message)
	}
}

func stringSliceContains(items []string, needle string) bool {
	target := strings.TrimSpace(strings.ToLower(needle))
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}
