package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabula/internal/canvas"
	"github.com/krystophny/tabula/internal/mcp"
	"github.com/krystophny/tabula/internal/protocol"
	"github.com/krystophny/tabula/internal/ptyd"
	"github.com/krystophny/tabula/internal/serve"
	"github.com/krystophny/tabula/internal/web"
	"github.com/pkg/browser"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		printHelp()
		return 2
	}
	switch args[0] {
	case "schema":
		return cmdSchema()
	case "bootstrap":
		return cmdBootstrap(args[1:])
	case "mcp-server":
		return cmdMCPServer(args[1:])
	case "serve":
		return cmdServe(args[1:])
	case "web":
		return cmdWeb(args[1:])
	case "ptyd":
		return cmdPtyd(args[1:])
	case "canvas":
		return cmdCanvas(args[1:])
	case "run":
		return cmdRun(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printHelp()
		return 2
	}
}

func printHelp() {
	fmt.Println("tabula <command> [flags]")
	fmt.Println("commands: canvas schema bootstrap mcp-server serve web ptyd run")
}

func cmdSchema() int {
	schema := map[string]interface{}{
		"title": "TabulaCanvasEvent",
		"oneOf": []map[string]interface{}{
			{"type": "object", "properties": map[string]interface{}{"kind": map[string]interface{}{"const": "text_artifact"}}},
			{"type": "object", "properties": map[string]interface{}{"kind": map[string]interface{}{"const": "image_artifact"}}},
			{"type": "object", "properties": map[string]interface{}{"kind": map[string]interface{}{"const": "pdf_artifact"}}},
			{"type": "object", "properties": map[string]interface{}{"kind": map[string]interface{}{"const": "clear_canvas"}}},
		},
	}
	b, _ := json.MarshalIndent(schema, "", "  ")
	fmt.Println(string(b))
	return 0
}

func cmdBootstrap(args []string) int {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	projectDir := fs.String("project-dir", ".", "project dir")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("project prepared: %s\n", res.Paths.ProjectDir)
	fmt.Printf("agents protocol: %s\n", res.Paths.AgentsPath)
	fmt.Printf("tabula sidecar protocol: %s\n", filepath.Join(res.Paths.ProjectDir, ".tabula", "AGENTS.tabula.md"))
	fmt.Printf("mcp config snippet: %s\n", res.Paths.MCPConfigPath)
	if res.AgentsPreserved {
		fmt.Println("existing AGENTS.md is preserved; tabula protocol is in sidecar")
	}
	if res.GitInitialized {
		fmt.Println("git initialized")
	}
	return 0
}

func cmdMCPServer(args []string) int {
	fs := flag.NewFlagSet("mcp-server", flag.ContinueOnError)
	projectDir := fs.String("project-dir", ".", "project dir")
	headless := fs.Bool("headless", false, "headless")
	noCanvas := fs.Bool("no-canvas", false, "no canvas")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	adapter := canvas.NewAdapter(res.Paths.ProjectDir, nil, *headless || *noCanvas)
	return mcp.RunStdio(adapter)
}

func cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	projectDir := fs.String("project-dir", ".", "project dir")
	host := fs.String("host", serve.DefaultHost, "host")
	port := fs.Int("port", serve.DefaultPort, "port")
	headless := fs.Bool("headless", false, "headless")
	noCanvas := fs.Bool("no-canvas", false, "no canvas")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !*headless && !*noCanvas {
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			fmt.Fprintln(os.Stderr, "warning: no DISPLAY/WAYLAND_DISPLAY detected; tabula serve will run headless")
		}
	}
	app := serve.NewApp(res.Paths.ProjectDir, *headless || *noCanvas)
	if err := app.Start(*host, *port); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func cmdWeb(args []string) int {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	dataDir := fs.String("data-dir", filepath.Join(os.Getenv("HOME"), ".tabula-web"), "data dir")
	projectDir := fs.String("project-dir", ".", "local project dir for tabula serve")
	host := fs.String("host", web.DefaultHost, "host")
	port := fs.Int("port", web.DefaultPort, "port")
	localMCPURL := fs.String("local-mcp-url", "", "external local MCP URL")
	ptydURL := fs.String("ptyd-url", "", "external PTY daemon URL")
	appServerURL := fs.String("app-server-url", web.DefaultAppServerURL, "Codex app-server websocket URL")
	devRuntime := fs.Bool("dev-runtime", false, "dev runtime endpoint")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	app, err := web.New(*dataDir, res.Paths.ProjectDir, *localMCPURL, *ptydURL, *appServerURL, *devRuntime)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := app.Start(*host, *port); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func cmdPtyd(args []string) int {
	fs := flag.NewFlagSet("ptyd", flag.ContinueOnError)
	dataDir := fs.String("data-dir", filepath.Join(os.Getenv("HOME"), ".local", "share", "tabula-ptyd"), "data dir")
	host := fs.String("host", ptyd.DefaultHost, "host")
	port := fs.Int("port", ptyd.DefaultPort, "port")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	app := ptyd.New(*dataDir)
	if err := app.Start(*host, *port); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func cmdCanvas(args []string) int {
	fs := flag.NewFlagSet("canvas", flag.ContinueOnError)
	host := fs.String("host", "127.0.0.1", "host")
	port := fs.Int("port", 8420, "port")
	dataDir := fs.String("data-dir", filepath.Join(os.Getenv("HOME"), ".tabula-web"), "data dir")
	projectDir := fs.String("project-dir", ".", "project dir")
	noOpen := fs.Bool("no-open", false, "do not open browser")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	app, err := web.New(*dataDir, res.Paths.ProjectDir, "", "", web.DefaultAppServerURL, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !*noOpen {
		go func() {
			time.Sleep(500 * time.Millisecond)
			_ = browser.OpenURL(fmt.Sprintf("http://%s:%d/canvas", *host, *port))
		}()
	}
	if err := app.Start(*host, *port); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func cmdRun(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	projectDir := fs.String("project-dir", ".", "project dir")
	assistant := fs.String("assistant", "codex", "assistant: codex|claude")
	mcpURL := fs.String("mcp-url", "http://127.0.0.1:9420/mcp", "mcp url")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	prompt := ""
	if rest := fs.Args(); len(rest) > 0 {
		prompt = strings.Join(rest, " ")
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return dispatchAssistant(*assistant, res.Paths.ProjectDir, *mcpURL, prompt)
}

func dispatchAssistant(assistant, cwd, mcpURL, prompt string) int {
	var cmd *exec.Cmd
	switch assistant {
	case "codex":
		args := []string{"--no-alt-screen", "--yolo", "--search", "-C", cwd}
		args = append(args, "-c", fmt.Sprintf("mcp_servers.tabula.url=%q", mcpURL))
		if prompt != "" {
			args = append(args, prompt)
		}
		cmd = exec.Command("codex", args...)
	case "claude":
		cfg := map[string]interface{}{"mcpServers": map[string]interface{}{"tabula": map[string]interface{}{}}}
		m := cfg["mcpServers"].(map[string]interface{})["tabula"].(map[string]interface{})
		m["url"] = mcpURL
		b, _ := json.Marshal(cfg)
		args := []string{"--dangerously-skip-permissions", "--mcp-config", string(b)}
		if prompt != "" {
			args = append(args, prompt)
		}
		cmd = exec.Command("claude", args...)
		cmd.Dir = cwd
	default:
		fmt.Fprintf(os.Stderr, "unsupported assistant: %s\n", assistant)
		return 1
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.Error); ok && ee.Err != nil {
			fmt.Fprintf(os.Stderr, "%s CLI not found on PATH\n", assistant)
			return 1
		}
		if ex, ok := err.(*exec.ExitError); ok {
			return ex.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
