//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sloppy-org/slopshell/internal/web"
)

// buildSlshBinary compiles cmd/slsh into a tempdir and returns the absolute path.
func buildSlshBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "slsh-e2e")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/slsh")
	cmd.Dir = projectRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/slsh: %v\n%s", err, out)
	}
	return bin
}

func projectRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(cwd, "..", ".."))
}

type fakeLLM struct {
	server *httptest.Server
	calls  atomic.Int32
}

// newFakeShellLLM returns a mock OpenAI-style /v1/chat/completions server
// that first replies with a `shell` tool call, then answers with a final
// message on the follow-up turn.
func newFakeShellLLM(t *testing.T, command, finalReply string) *fakeLLM {
	t.Helper()
	llm := &fakeLLM{}
	llm.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		call := llm.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "",
						"tool_calls": []map[string]any{{
							"id":   "call_shell_1",
							"type": "function",
							"function": map[string]any{
								"name":      "shell",
								"arguments": fmt.Sprintf(`{"command":%q}`, command),
							},
						}},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": finalReply,
				},
			}},
		})
	}))
	t.Cleanup(llm.server.Close)
	return llm
}

// newFakeMailLLM returns a mock LLM that first emits an mcp__mail_account_list
// tool call, then returns a human-readable summary after receiving the tool
// result. The Mail family is activated by keywords in the user prompt.
func newFakeMailLLM(t *testing.T, finalReply string) *fakeLLM {
	t.Helper()
	llm := &fakeLLM{}
	llm.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.URL.Path) != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		call := llm.calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "",
						"tool_calls": []map[string]any{{
							"id":   "call_mail_1",
							"type": "function",
							"function": map[string]any{
								"name":      "mcp__mail_account_list",
								"arguments": `{}`,
							},
						}},
					},
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"content": finalReply,
				},
			}},
		})
	}))
	t.Cleanup(llm.server.Close)
	return llm
}

// newFakeMCPMailServer mocks just enough of the MCP JSON-RPC surface for
// the Mail family: tools/list advertises mail_account_list, and tools/call
// returns a synthetic account. No real EWS/sloptools traffic.
func newFakeMCPMailServer(t *testing.T, address string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		method, _ := payload["method"].(string)
		w.Header().Set("Content-Type", "application/json")
		switch strings.TrimSpace(method) {
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"tools": []map[string]any{{
						"name":        "mail_account_list",
						"description": "List enabled email accounts available through Sloppy.",
						"inputSchema": map[string]any{
							"type":       "object",
							"properties": map[string]any{},
						},
					}},
				},
			})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"structuredContent": map[string]any{
						"ok": true,
						"accounts": []map[string]any{{
							"id":      42,
							"address": address,
							"enabled": true,
						}},
					},
				},
			})
		default:
			http.Error(w, "unexpected mcp method", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupE2EHarness spins up a slopshell web.App backed by the provided mock
// LLM and MCP URLs, writes a CLI token file, and returns everything needed
// to exercise the slsh binary against it.
func setupE2EHarness(t *testing.T, llmURL, mcpURL string) (webBaseURL, tokenFile string) {
	t.Helper()

	workspaceDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("SLOPSHELL_BACKGROUND_SYNC", "off")
	// Pin the CLI token path into the per-test tempdir so web.New does not
	// overwrite a running system server's token at $XDG_RUNTIME_DIR/slopshell.
	t.Setenv("SLOPSHELL_CLI_TOKEN_FILE", filepath.Join(dataDir, "cli-token"))

	app, err := web.New(dataDir, workspaceDir, mcpURL, "", "", "", "", false)
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	tokenPath := filepath.Join(dataDir, "cli-token")
	token := "e2e-cli-token-" + randToken(t)
	if err := os.WriteFile(tokenPath, []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write cli token: %v", err)
	}
	web.TestingOverrideCLIAuth(app, token, tokenPath)

	web.TestingForceLocalAssistantLLM(app, llmURL)

	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	return srv.URL, tokenPath
}

// runSlsh invokes the prebuilt slsh binary and returns stdout+stderr.
func runSlsh(t *testing.T, bin string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(),
		// Ensure no ambient token file path leaks in; tests always pass --token-file.
		"SLOPSHELL_CLI_TOKEN_FILE=",
		"SLOPSHELL_CLI_STATE_FILE="+filepath.Join(t.TempDir(), "slsh-state.json"),
	)
	outBuf, errBuf := &strings.Builder{}, &strings.Builder{}
	cmd.Stdout = outBuf
	cmd.Stderr = errBuf
	runErr := cmd.Run()
	stdout = outBuf.String()
	stderr = errBuf.String()
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exit = exitErr.ExitCode()
			return stdout, stderr, exit
		}
		t.Fatalf("run slsh: %v\nstderr=%s", runErr, stderr)
	}
	return stdout, stderr, 0
}

func TestSlshOneShotShellToolProducesExpectedFinalText(t *testing.T) {
	bin := buildSlshBinary(t)
	llm := newFakeShellLLM(t, "printf e2e-shell-hello", "Shell tool printed: e2e-shell-hello")
	// MCP server is never contacted for shell family, but the app requires a URL.
	mcp := newFakeMCPMailServer(t, "never-used@example.test")
	baseURL, tokenFile := setupE2EHarness(t, llm.server.URL, mcp.URL)

	stdout, stderr, exit := runSlsh(t, bin,
		"--base-url", baseURL,
		"--token-file", tokenFile,
		"--no-color",
		"--timeout", "45s",
		"-p", "Use shell to print e2e-shell-hello",
	)
	if exit != 0 {
		t.Fatalf("slsh exit = %d\nstderr:\n%s\nstdout:\n%s", exit, stderr, stdout)
	}
	if !strings.Contains(stdout, "Shell tool printed: e2e-shell-hello") {
		t.Fatalf("stdout missing final reply; got:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if llm.calls.Load() < 2 {
		t.Fatalf("llm calls = %d, want >= 2", llm.calls.Load())
	}
}

func TestSlshOneShotMailAccountListRoutesThroughMCP(t *testing.T) {
	bin := buildSlshBinary(t)
	llm := newFakeMailLLM(t, "Account fake@e2e.test is enabled.")
	mcp := newFakeMCPMailServer(t, "fake@e2e.test")
	baseURL, tokenFile := setupE2EHarness(t, llm.server.URL, mcp.URL)

	stdout, stderr, exit := runSlsh(t, bin,
		"--base-url", baseURL,
		"--token-file", tokenFile,
		"--no-color",
		"--timeout", "45s",
		"-p", "List my email accounts briefly",
	)
	if exit != 0 {
		t.Fatalf("slsh exit = %d\nstderr:\n%s\nstdout:\n%s", exit, stderr, stdout)
	}
	if !strings.Contains(stdout, "Account fake@e2e.test is enabled.") {
		t.Fatalf("stdout missing final reply; got:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if llm.calls.Load() < 2 {
		t.Fatalf("llm calls = %d, want >= 2", llm.calls.Load())
	}
}

func TestSlshOneShotJSONModeEmitsNDJSON(t *testing.T) {
	bin := buildSlshBinary(t)
	llm := newFakeShellLLM(t, "printf json-ok", "Done.")
	mcp := newFakeMCPMailServer(t, "unused@example.test")
	baseURL, tokenFile := setupE2EHarness(t, llm.server.URL, mcp.URL)

	stdout, stderr, exit := runSlsh(t, bin,
		"--base-url", baseURL,
		"--token-file", tokenFile,
		"--no-color",
		"--json",
		"--timeout", "45s",
		"-p", "Use shell to print json-ok",
	)
	if exit != 0 {
		t.Fatalf("slsh exit = %d\nstderr:\n%s\nstdout:\n%s", exit, stderr, stdout)
	}
	sawFinal := false
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
			t.Fatalf("non-NDJSON line %q: %v", trimmed, err)
		}
		if event["type"] == "assistant_output" {
			if role, _ := event["role"].(string); role == "assistant" {
				sawFinal = true
			}
		}
	}
	if !sawFinal {
		t.Fatalf("no assistant_output event in NDJSON stream:\n%s", stdout)
	}
}

// --- test scaffolding -------------------------------------------------------

func randToken(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
