package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/krystophny/tabura/internal/canvas"
	"github.com/krystophny/tabura/internal/mcp"
	"github.com/krystophny/tabura/internal/protocol"
	"github.com/krystophny/tabura/internal/serve"
	"github.com/krystophny/tabura/internal/store"
	"github.com/krystophny/tabura/internal/web"
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
	case "server":
		return cmdServer(args[1:])
	case "mcp-server":
		return cmdMCPServer(args[1:])
	case "set-password":
		return cmdSetPassword(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printHelp()
		return 2
	}
}

func printHelp() {
	fmt.Println("tabura <command> [flags]")
	fmt.Println("commands: schema bootstrap server mcp-server set-password")
}

func cmdSchema() int {
	schema := map[string]interface{}{
		"title": "TaburaCanvasEvent",
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
	fmt.Printf("tabura sidecar protocol: %s\n", filepath.Join(res.Paths.ProjectDir, ".tabura", "AGENTS.tabura.md"))
	fmt.Printf("mcp config snippet: %s\n", res.Paths.MCPConfigPath)
	if res.AgentsPreserved {
		fmt.Println("existing AGENTS.md is preserved; tabura protocol is in sidecar")
	}
	if res.GitInitialized {
		fmt.Println("git initialized")
	}
	return 0
}

func cmdMCPServer(args []string) int {
	fs := flag.NewFlagSet("mcp-server", flag.ContinueOnError)
	projectDir := fs.String("project-dir", ".", "project dir")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	adapter := canvas.NewAdapter(res.Paths.ProjectDir, nil)
	return mcp.RunStdio(adapter)
}

func cmdServer(args []string) int {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	dataDir := fs.String("data-dir", filepath.Join(os.Getenv("HOME"), ".tabura-web"), "data dir")
	projectDir := fs.String("project-dir", ".", "project dir")
	webHost := fs.String("web-host", "0.0.0.0", "web listener host")
	webPort := fs.Int("web-port", web.DefaultPort, "web listener port")
	mcpHost := fs.String("mcp-host", "127.0.0.1", "mcp listener host")
	mcpPort := fs.Int("mcp-port", serve.DefaultPort, "mcp listener port")
	unsafePublicMCP := fs.Bool("unsafe-public-mcp", false, "allow non-loopback MCP bind (unsafe)")
	appServerURL := fs.String("app-server-url", web.DefaultAppServerURL, "Codex app-server websocket URL")
	model := fs.String("model", "", "LLM model for chat (default: env TABURA_APP_SERVER_MODEL or "+web.DefaultModel+")")
	sparkReasoningEffort := fs.String("spark-reasoning-effort", "", "Spark thinking budget, e.g. low|medium|high (default: env TABURA_APP_SERVER_SPARK_REASONING_EFFORT or low)")
	ttsURL := fs.String("tts-url", "", "TTS server URL (default: env TABURA_TTS_URL or "+web.DefaultTTSURL+")")
	devRuntime := fs.Bool("dev-runtime", false, "dev runtime endpoint")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !*unsafePublicMCP && !isLoopbackOnlyHost(*mcpHost) {
		fmt.Fprintln(os.Stderr, "refusing non-loopback MCP bind; use --unsafe-public-mcp to override")
		return 2
	}
	res, err := protocol.BootstrapProject(*projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	mcpApp := serve.NewApp(res.Paths.ProjectDir)
	mcpErrCh := make(chan error, 1)
	go func() {
		mcpErrCh <- mcpApp.Start(*mcpHost, *mcpPort)
	}()
	mcpURL := (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(*mcpHost, fmt.Sprintf("%d", *mcpPort)),
		Path:   "/mcp",
	}).String()
	if err := waitForMCPHealth(*mcpHost, *mcpPort, 10*time.Second); err != nil {
		_ = mcpApp.Stop(context.Background())
		fmt.Fprintf(os.Stderr, "failed to start local MCP listener: %v\n", err)
		return 1
	}
	app, err := web.New(*dataDir, res.Paths.ProjectDir, mcpURL, *appServerURL, *model, *ttsURL, *sparkReasoningEffort, *devRuntime)
	if err != nil {
		_ = mcpApp.Stop(context.Background())
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := app.Start(*webHost, *webPort); err != nil {
		_ = mcpApp.Stop(context.Background())
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	select {
	case mcpErr := <-mcpErrCh:
		if mcpErr != nil {
			fmt.Fprintf(os.Stderr, "mcp listener failed: %v\n", mcpErr)
			return 1
		}
	default:
	}
	return 0
}

func isLoopbackOnlyHost(host string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(host))
	trimmed = strings.Trim(trimmed, "[]")
	if trimmed == "localhost" {
		return true
	}
	switch trimmed {
	case "127.0.0.1", "::1":
		return true
	case "", "0.0.0.0", "::":
		return false
	}
	ip := net.ParseIP(trimmed)
	return ip != nil && ip.IsLoopback()
}

func waitForMCPHealth(host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://%s/health", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("mcp health check timeout")
}

func cmdSetPassword(args []string) int {
	fs := flag.NewFlagSet("set-password", flag.ContinueOnError)
	dataDir := fs.String("data-dir", filepath.Join(os.Getenv("HOME"), ".local/share/tabura-web"), "data dir")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dbPath := filepath.Join(*dataDir, "tabura.db")
	s, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open store %s: %v\n", dbPath, err)
		return 1
	}
	defer s.Close()
	var pw []byte
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Enter password: ")
		pw, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
	} else {
		pw, err = os.ReadFile("/dev/stdin")
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "read password: %v\n", err)
		return 1
	}
	pw = []byte(strings.TrimRight(string(pw), "\n\r"))
	if err := s.SetAdminPassword(string(pw)); err != nil {
		fmt.Fprintf(os.Stderr, "set password: %v\n", err)
		return 1
	}
	fmt.Println("Password set.")
	return 0
}
