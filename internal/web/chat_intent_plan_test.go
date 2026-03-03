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

func TestClassifyIntentPlanWithLLMMultiAction(t *testing.T) {
	llm := setupMockIntentLLMServer(
		t,
		200,
		`{"actions":[{"action":"shell","command":"ls -1"},{"action":"open_file_canvas","path":"$last_shell_path"}]}`,
	)
	defer llm.Close()

	app := newAuthedTestApp(t)
	app.intentClassifierURL = ""
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

func TestDetectQuickOpenToken(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{name: "open bare token", text: "Open README", want: "README"},
		{name: "show quoted file", text: "show `README.md` on canvas", want: "README.md"},
		{name: "display with please", text: "display notes.txt please", want: "notes.txt"},
		{name: "non-open request", text: "what is in README", want: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := detectQuickOpenToken(tc.text); got != tc.want {
				t.Fatalf("detectQuickOpenToken(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
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

	message, payloads, err := app.executeSystemActionPlan(session.ID, session, []*SystemAction{
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

func TestClassifyAndExecuteSystemActionWithoutIntentLLMDoesNotAutoOpen(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentClassifierURL = ""
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

	_, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "Open README")
	if handled {
		t.Fatal("expected request to remain unhandled without intent LLM")
	}
	if len(payloads) != 0 {
		t.Fatalf("payloads length = %d, want 0", len(payloads))
	}
	if showCalls != 0 {
		t.Fatalf("canvas_artifact_show calls = %d, want 0", showCalls)
	}
}

func TestClassifyAndExecuteSystemActionToolRequestsUseQwenPlan(t *testing.T) {
	classifier := setupMockIntentClassifierServer(t, http.StatusOK, map[string]interface{}{
		"intent":     "shell",
		"confidence": 0.99,
		"entities": map[string]interface{}{
			"command": "printf './README.md\\n'",
		},
	})
	defer classifier.Close()

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
	app.intentClassifierURL = classifier.URL
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
