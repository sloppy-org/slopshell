package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/krystophny/tabula/internal/canvas"
	"github.com/krystophny/tabula/internal/mcp"
	"github.com/krystophny/tabula/internal/protocol"
	"github.com/krystophny/tabula/internal/serve"
	"github.com/krystophny/tabula/internal/voxtypemcp"
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
	case "voxtype-mcp":
		return cmdVoxTypeMCP(args[1:])
	case "canvas":
		return cmdCanvas(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printHelp()
		return 2
	}
}

func printHelp() {
	fmt.Println("tabula <command> [flags]")
	fmt.Println("commands: canvas schema bootstrap mcp-server serve web voxtype-mcp")
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
	app, err := web.New(*dataDir, res.Paths.ProjectDir, *localMCPURL, *appServerURL, *devRuntime)
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

func cmdVoxTypeMCP(args []string) int {
	fs := flag.NewFlagSet("voxtype-mcp", flag.ContinueOnError)
	bind := fs.String("bind", "127.0.0.1", "bind address")
	port := fs.Int("port", 8091, "port")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	server := voxtypemcp.NewServer(*bind, *port)
	if err := server.Start(); err != nil {
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
	app, err := web.New(*dataDir, res.Paths.ProjectDir, "", web.DefaultAppServerURL, false)
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
