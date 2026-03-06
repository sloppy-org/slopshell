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
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/krystophny/tabura/internal/canvas"
	"github.com/krystophny/tabura/internal/mcp"
	"github.com/krystophny/tabura/internal/protocol"
	"github.com/krystophny/tabura/internal/ptt"
	"github.com/krystophny/tabura/internal/serve"
	"github.com/krystophny/tabura/internal/store"
	updater "github.com/krystophny/tabura/internal/update"
	"github.com/krystophny/tabura/internal/web"
)

const defaultBinaryVersion = "0.1.8"

var (
	version   = defaultBinaryVersion
	commit    = "dev"
	runUpdate = updater.Run
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
	case "version":
		return cmdVersion()
	case "update":
		return cmdUpdate(args[1:])
	case "ptt-daemon":
		return cmdPTTDaemon(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printHelp()
		return 2
	}
}

func printHelp() {
	fmt.Println("tabura <command> [flags]")
	fmt.Println("commands: schema bootstrap server mcp-server set-password version update ptt-daemon")
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

type serverConfig struct {
	dataDir              string
	projectDir           string
	webHost              string
	webPort              int
	webHTTPSPort         int
	webCertFile          string
	webKeyFile           string
	mcpHost              string
	mcpPort              int
	unsafePublicMCP      bool
	appServerURL         string
	model                string
	sparkReasoningEffort string
	ttsURL               string
	devRuntime           bool
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
	fmt.Printf("mcp config snippet: %s\n", res.Paths.MCPConfigPath)
	fmt.Println("project AGENTS.md files are left untouched")
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
	cfg, status := parseServerConfig(args)
	if status != 0 {
		return status
	}
	return runServer(cfg)
}

func parseServerConfig(args []string) (*serverConfig, int) {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	cfg := &serverConfig{
		dataDir: filepath.Join(os.Getenv("HOME"), ".tabura-web"),
	}
	projectDir := fs.String("project-dir", ".", "project dir")
	fs.StringVar(&cfg.dataDir, "data-dir", cfg.dataDir, "data dir")
	fs.StringVar(&cfg.webHost, "web-host", "127.0.0.1", "web listener host")
	fs.IntVar(&cfg.webPort, "web-port", web.DefaultPort, "web listener port")
	fs.IntVar(&cfg.webHTTPSPort, "web-https-port", 8443, "HTTPS web listener port (requires --web-cert-file and --web-key-file)")
	fs.StringVar(&cfg.webCertFile, "web-cert-file", "", "TLS certificate path for HTTPS web listener")
	fs.StringVar(&cfg.webKeyFile, "web-key-file", "", "TLS private key path for HTTPS web listener")
	fs.StringVar(&cfg.mcpHost, "mcp-host", "127.0.0.1", "mcp listener host")
	fs.IntVar(&cfg.mcpPort, "mcp-port", serve.DefaultPort, "mcp listener port")
	fs.BoolVar(&cfg.unsafePublicMCP, "unsafe-public-mcp", false, "allow non-loopback MCP bind (unsafe)")
	fs.StringVar(&cfg.appServerURL, "app-server-url", web.DefaultAppServerURL, "Codex app-server websocket URL")
	fs.StringVar(&cfg.model, "model", "", "LLM model for chat (default: env TABURA_APP_SERVER_MODEL or "+web.DefaultModel+")")
	fs.StringVar(&cfg.sparkReasoningEffort, "spark-reasoning-effort", "", "Spark thinking budget, e.g. low|medium|high (default: env TABURA_APP_SERVER_SPARK_REASONING_EFFORT or low)")
	fs.StringVar(&cfg.ttsURL, "tts-url", "", "TTS server URL (default: env TABURA_TTS_URL or "+web.DefaultTTSURL+")")
	fs.BoolVar(&cfg.devRuntime, "dev-runtime", false, "dev runtime endpoint")
	if err := fs.Parse(args); err != nil {
		return nil, 2
	}
	cfg.projectDir = *projectDir
	if !cfg.unsafePublicMCP && !isLoopbackOnlyHost(cfg.mcpHost) {
		fmt.Fprintln(os.Stderr, "refusing non-loopback MCP bind; use --unsafe-public-mcp to override")
		return nil, 2
	}
	hasCert := strings.TrimSpace(cfg.webCertFile) != ""
	hasKey := strings.TrimSpace(cfg.webKeyFile) != ""
	if hasCert != hasKey {
		fmt.Fprintln(os.Stderr, "HTTPS requires both --web-cert-file and --web-key-file")
		return nil, 2
	}
	return cfg, 0
}

func runServer(cfg *serverConfig) int {
	res, err := protocol.BootstrapProject(cfg.projectDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	mcpApp, mcpErrCh, mcpURL := startMCPListener(cfg, res.Paths.ProjectDir)
	if err := waitForMCPReady(cfg.mcpHost, cfg.mcpPort, 10*time.Second, mcpErrCh); err != nil {
		_ = mcpApp.Stop(context.Background())
		fmt.Fprintf(os.Stderr, "failed to start local MCP listener: %v\n", err)
		return 1
	}
	app, err := web.New(
		cfg.dataDir,
		res.Paths.ProjectDir,
		mcpURL,
		cfg.appServerURL,
		cfg.model,
		cfg.ttsURL,
		cfg.sparkReasoningEffort,
		cfg.devRuntime,
	)
	if err != nil {
		_ = mcpApp.Stop(context.Background())
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	hasTLS := strings.TrimSpace(cfg.webCertFile) != "" && strings.TrimSpace(cfg.webKeyFile) != ""
	if hasTLS {
		go func() {
			if err := app.ListenTLS(cfg.webHost, cfg.webHTTPSPort, cfg.webCertFile, cfg.webKeyFile); err != nil {
				fmt.Fprintf(os.Stderr, "HTTPS listener failed: %v\n", err)
			}
		}()
	}
	startErr := app.Start(cfg.webHost, cfg.webPort)
	if startErr != nil {
		_ = mcpApp.Stop(context.Background())
		fmt.Fprintln(os.Stderr, startErr)
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

func startMCPListener(cfg *serverConfig, projectDir string) (*serve.App, chan error, string) {
	mcpApp := serve.NewApp(projectDir)
	mcpErrCh := make(chan error, 1)
	go func() {
		mcpErrCh <- mcpApp.Start(cfg.mcpHost, cfg.mcpPort)
	}()
	mcpURL := (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(cfg.mcpHost, fmt.Sprintf("%d", cfg.mcpPort)),
		Path:   "/mcp",
	}).String()
	return mcpApp, mcpErrCh, mcpURL
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

func waitForMCPReady(host string, port int, timeout time.Duration, mcpErrCh <-chan error) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://%s/health", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	client := &http.Client{Timeout: 750 * time.Millisecond}
	for time.Now().Before(deadline) {
		select {
		case err := <-mcpErrCh:
			if err == nil {
				return errors.New("mcp listener exited before becoming healthy")
			}
			return fmt.Errorf("mcp listener failed to start: %w", err)
		default:
		}
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	select {
	case err := <-mcpErrCh:
		if err == nil {
			return errors.New("mcp listener exited before becoming healthy")
		}
		return fmt.Errorf("mcp listener failed to start: %w", err)
	default:
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

func cmdPTTDaemon(args []string) int {
	fs := flag.NewFlagSet("ptt-daemon", flag.ContinueOnError)
	cfg := ptt.DefaultConfig()
	fs.StringVar(&cfg.DevicePath, "device", cfg.DevicePath, "evdev device path (auto-detected if empty)")
	keyCode := fs.Int("key", int(cfg.KeyCode), "evdev key code to listen for (183=F13)")
	fs.StringVar(&cfg.STTURL, "stt-url", cfg.STTURL, "STT sidecar URL")
	fs.StringVar(&cfg.WebAPIURL, "web-api-url", cfg.WebAPIURL, "tabura web API URL for STT replacements")
	fs.StringVar(&cfg.OutputMode, "output", cfg.OutputMode, "output mode: type (ydotool) or clipboard (wl-copy)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg.KeyCode = uint16(*keyCode)
	if err := ptt.Run(context.Background(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func cmdVersion() int {
	fmt.Println(formatVersionLine(version, commit, runtime.GOOS, runtime.GOARCH))
	return 0
}

func cmdUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := runUpdate(updater.Options{CurrentVersion: version})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if res.Updated {
		fmt.Printf("Updated tabura %s -> %s. Restart service to apply.\n", res.CurrentVersion, res.LatestVersion)
		return 0
	}
	fmt.Printf("Already up to date (%s)\n", res.CurrentVersion)
	return 0
}

func formatVersionLine(rawVersion, rawCommit, goos, goarch string) string {
	release := strings.TrimSpace(rawVersion)
	if release == "" {
		release = "0.0.0"
	}
	if !strings.HasPrefix(strings.ToLower(release), "v") {
		release = "v" + release
	}
	shortCommit := strings.TrimSpace(rawCommit)
	if shortCommit == "" {
		shortCommit = "unknown"
	}
	return fmt.Sprintf("tabura %s (%s) %s/%s", release, shortCommit, goos, goarch)
}
