package protocol

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Paths struct {
	ProjectDir    string
	AgentsPath    string
	MCPConfigPath string
}

type Result struct {
	Paths           Paths
	GitInitialized  bool
	AgentsPreserved bool
}

func BootstrapProject(projectDir string) (Result, error) {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return Result{}, err
	}
	tabulaDir := filepath.Join(abs, ".tabula")
	if err := os.MkdirAll(tabulaDir, 0o755); err != nil {
		return Result{}, err
	}
	paths := Paths{
		ProjectDir:    abs,
		AgentsPath:    filepath.Join(abs, "AGENTS.md"),
		MCPConfigPath: filepath.Join(tabulaDir, "codex-mcp.toml"),
	}
	agentsPreserved := true
	if _, err := os.Stat(paths.AgentsPath); os.IsNotExist(err) {
		agentsPreserved = false
		_ = os.WriteFile(paths.AgentsPath, []byte(defaultAgents()), 0o644)
	}
	_ = os.WriteFile(filepath.Join(tabulaDir, "AGENTS.tabula.md"), []byte(defaultAgents()), 0o644)
	_ = os.WriteFile(filepath.Join(tabulaDir, "prompt-injection.txt"), []byte("Apply these extra instructions in all Tabula Codex prompts for this project.\n"), 0o644)
	_ = os.WriteFile(paths.MCPConfigPath, []byte(fmt.Sprintf("[mcp_servers.tabula]\ncommand = \"tabula\"\nargs = [\"mcp-server\", \"--project-dir\", \"%s\"]\n", strings.ReplaceAll(abs, "\\", "\\\\"))), 0o644)
	_ = ensureGitignore(abs)
	gitInit := false
	if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
		gitInit = true
	}
	return Result{Paths: paths, AgentsPreserved: agentsPreserved, GitInitialized: gitInit}, nil
}

func defaultAgents() string {
	return "# AGENTS\n\n<!-- TABULA_PROTOCOL:BEGIN -->\n## Tabula Codex Protocol\n\nUse this protocol for Tabula interactive sessions in this project.\n\n1. Read extra instructions from `.tabula/prompt-injection.txt` and apply them.\n2. Keep generated render/output artifacts under `.tabula/artifacts`; keep editable source files in the project workspace (not under `.tabula/artifacts`).\n3. Use MCP server `tabula` for all canvas operations; do not rely on filesystem event logs.\n4. MCP tools: `canvas_session_open`, `canvas_artifact_show`, `canvas_mark_set`, `canvas_mark_delete`, `canvas_marks_list`, `canvas_mark_focus`, `canvas_commit`, `canvas_status`.\n5. Keep interaction chat-canvas-first in the web UI; do not depend on a terminal REPL.\n6. Keep `.tabula/artifacts/` gitignored; do not commit files from it unless explicitly requested.\n\n<!-- TABULA_PROTOCOL:END -->\n"
}

func ensureGitignore(projectDir string) error {
	gitignore := filepath.Join(projectDir, ".gitignore")
	data := ""
	if b, err := os.ReadFile(gitignore); err == nil {
		data = string(b)
	}
	want := ".tabula/artifacts/\n"
	if strings.Contains(data, ".tabula/artifacts/") {
		return nil
	}
	if data != "" && !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	data += want
	return os.WriteFile(gitignore, []byte(data), 0o644)
}
