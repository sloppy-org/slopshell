package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chzyer/readline"
)

const (
	defaultBaseURL = "http://127.0.0.1:8420"
	defaultTimeout = 10 * time.Minute
	envBaseURL     = "SLOPSHELL_BASE_URL"
	envTokenFile   = "SLOPSHELL_CLI_TOKEN_FILE"
)

type cliOptions struct {
	baseURL    string
	projectDir string
	resumeID   string
	prompt     string
	model      string
	gpt        bool
	think      string
	timeout    time.Duration
	tokenFile  string
	jsonOut    bool
	noColor    bool
	verbose    bool
	stdinPrompt bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	opts, tail, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	// Positional args after the flags are concatenated as an implicit prompt.
	if opts.prompt == "" && len(tail) > 0 {
		opts.prompt = strings.Join(tail, " ")
	}

	// Stdin pipe: if stdin is not a terminal and --prompt is empty, consume
	// the whole input as the one-shot prompt.
	if opts.prompt == "" && isPipedStdin(stdin) {
		body, readErr := io.ReadAll(stdin)
		if readErr != nil {
			fmt.Fprintf(stderr, "slsh: reading stdin: %v\n", readErr)
			return 1
		}
		trimmed := strings.TrimSpace(string(body))
		if trimmed != "" {
			opts.prompt = trimmed
			opts.stdinPrompt = true
		}
	}

	resolved := opts.resolveBaseURL()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := newClient(ctx, clientConfig{
		baseURL:   resolved,
		tokenFile: opts.effectiveTokenFile(),
		verbose:   opts.verbose,
		stderr:    stderr,
	})
	if err != nil {
		fmt.Fprintf(stderr, "slsh: %v\n", err)
		return 1
	}

	session, err := client.startChatSession(ctx, opts.projectDir, opts.resumeID)
	if err != nil {
		fmt.Fprintf(stderr, "slsh: %v\n", err)
		return 1
	}
	if err := persistSessionForWorkspace(session.WorkspacePath, session.ID); err != nil && opts.verbose {
		fmt.Fprintf(stderr, "slsh: warning: persist session: %v\n", err)
	}

	renderer := newRenderer(stdout, opts.jsonOut, !opts.noColor && isTerminal(stdout))

	if opts.prompt != "" {
		return runOneShot(ctx, client, session, opts, renderer, stderr)
	}
	return runREPL(ctx, client, session, opts, renderer, stdin, stdout, stderr)
}

func parseFlags(args []string) (cliOptions, []string, error) {
	fs := flag.NewFlagSet("slsh", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cwd, _ := os.Getwd()
	opts := cliOptions{
		baseURL:    os.Getenv(envBaseURL),
		projectDir: cwd,
		timeout:    defaultTimeout,
		tokenFile:  os.Getenv(envTokenFile),
	}
	fs.StringVar(&opts.baseURL, "base-url", opts.baseURL, "slopshell server base URL")
	fs.StringVar(&opts.projectDir, "project-dir", opts.projectDir, "workspace directory (defaults to cwd)")
	fs.StringVar(&opts.resumeID, "resume", "", "resume a prior chat session by id instead of starting fresh")
	fs.StringVar(&opts.prompt, "prompt", "", "one-shot prompt (also -p)")
	fs.StringVar(&opts.prompt, "p", "", "one-shot prompt (shorthand for --prompt)")
	fs.StringVar(&opts.model, "model", "", "model alias: local|spark|gpt|mini")
	fs.BoolVar(&opts.gpt, "gpt", false, "shorthand for --model gpt")
	fs.StringVar(&opts.think, "think", "", "reasoning-effort hint: low|medium|high")
	fs.DurationVar(&opts.timeout, "timeout", opts.timeout, "per-turn timeout (e.g. 2m, 10m)")
	fs.StringVar(&opts.tokenFile, "token-file", opts.tokenFile, "override CLI token file path")
	fs.BoolVar(&opts.jsonOut, "json", false, "emit NDJSON events instead of pretty output")
	fs.BoolVar(&opts.noColor, "no-color", false, "disable ANSI colors")
	fs.BoolVar(&opts.verbose, "verbose", false, "log websocket events to stderr")
	help := fs.Bool("help", false, "show usage")
	fs.BoolVar(help, "h", false, "show usage")

	if err := fs.Parse(args); err != nil {
		return cliOptions{}, nil, err
	}
	if *help {
		return cliOptions{}, nil, errHelp{Usage: usage()}
	}
	if opts.gpt {
		opts.model = "gpt"
	}
	if strings.TrimSpace(opts.projectDir) == "" {
		opts.projectDir = cwd
	}
	if abs, err := filepath.Abs(opts.projectDir); err == nil {
		opts.projectDir = abs
	}
	model := strings.ToLower(strings.TrimSpace(opts.model))
	if model != "" {
		switch model {
		case "local", "spark", "gpt", "mini", "codex":
		default:
			return cliOptions{}, nil, fmt.Errorf("unsupported --model %q (want local|spark|gpt|mini)", opts.model)
		}
		opts.model = model
	}
	think := strings.ToLower(strings.TrimSpace(opts.think))
	if think != "" {
		switch think {
		case "low", "medium", "high":
		default:
			return cliOptions{}, nil, fmt.Errorf("unsupported --think %q (want low|medium|high)", opts.think)
		}
		opts.think = think
	}
	return opts, fs.Args(), nil
}

type errHelp struct{ Usage string }

func (e errHelp) Error() string { return e.Usage }

func usage() string {
	return strings.TrimSpace(`
slsh — SlopShell terminal chat client

usage:
  slsh [flags] [prompt words...]
  echo "..." | slsh [flags]

flags:
  --base-url URL        slopshell server (default http://127.0.0.1:8420 or $SLOPSHELL_BASE_URL)
  --project-dir DIR     workspace directory (default cwd)
  --resume SESSION_ID   resume a prior chat session instead of starting fresh
  -p, --prompt STRING   one-shot mode: send this prompt and exit
  --model ALIAS         local|spark|gpt|mini (default local)
  --gpt                 shorthand for --model gpt
  --think LEVEL         low|medium|high reasoning-effort hint
  --timeout DURATION    per-turn timeout (default 10m)
  --token-file PATH     override CLI token file path
  --json                emit NDJSON events instead of pretty output
  --no-color            disable ANSI colors
  --verbose             log websocket events to stderr
  -h, --help            show this message

REPL commands (type inside slsh):
  /help                 show this help
  /exit, /quit          leave
  /clear                wipe all chat state on the server
  /compact              drop just the app-server thread for this session
  /new                  alias for /clear
  /resume ID            reattach to a previous session id
  /sessions             list session ids this CLI has seen per workspace
  /model ALIAS          switch model for the next turn
  /think LEVEL          set reasoning-effort hint for the next turn
  /stop                 cancel the running assistant turn

Anything that does not start with '/' is sent as a chat message.
`)
}

func (o cliOptions) resolveBaseURL() string {
	raw := strings.TrimSpace(o.baseURL)
	if raw == "" {
		return defaultBaseURL
	}
	return raw
}

func (o cliOptions) effectiveTokenFile() string {
	if strings.TrimSpace(o.tokenFile) != "" {
		return strings.TrimSpace(o.tokenFile)
	}
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
		return filepath.Join(runtimeDir, "slopshell", "cli-token")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".local", "share", "slopshell-web", "cli-token")
	}
	return ""
}

func buildPromptWithDirectives(raw string, opts cliOptions) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	var prefix []string
	switch opts.model {
	case "gpt":
		prefix = append(prefix, "use gpt")
	case "spark", "codex":
		prefix = append(prefix, "use spark")
	case "mini":
		prefix = append(prefix, "use mini")
	}
	switch opts.think {
	case "low":
		prefix = append(prefix, "think quickly")
	case "medium":
		prefix = append(prefix, "think a bit")
	case "high":
		prefix = append(prefix, "think hard")
	}
	if len(prefix) == 0 {
		return text
	}
	return strings.Join(prefix, ", ") + " to " + text
}

func runOneShot(ctx context.Context, client *chatClient, session *chatSessionInfo, opts cliOptions, renderer *renderer, stderr io.Writer) int {
	prompt := buildPromptWithDirectives(opts.prompt, opts)
	if prompt == "" {
		fmt.Fprintln(stderr, "slsh: empty prompt")
		return 2
	}
	turnCtx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()
	final, err := client.sendAndWaitForFinal(turnCtx, session.ID, prompt, renderer)
	if err != nil {
		if errors.Is(err, errAssistantError) {
			fmt.Fprintf(stderr, "slsh: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "slsh: %v\n", err)
		return 1
	}
	if !opts.jsonOut {
		trimmed := strings.TrimSpace(final)
		if trimmed != "" && !renderer.didEmitFinalText() {
			fmt.Fprintln(renderer.out, trimmed)
		}
	}
	return 0
}

func runREPL(ctx context.Context, client *chatClient, session *chatSessionInfo, opts cliOptions, renderer *renderer, stdin io.Reader, stdout, stderr io.Writer) int {
	info := renderer.colorize(colorDim, fmt.Sprintf("slsh session %s @ %s", session.ID, session.WorkspacePath))
	fmt.Fprintln(stdout, info)
	fmt.Fprintln(stdout, renderer.colorize(colorDim, "type /help for commands, /exit to leave"))

	if strings.TrimSpace(opts.resumeID) != "" {
		if err := client.printRecentHistory(ctx, session.ID, renderer); err != nil && opts.verbose {
			fmt.Fprintf(stderr, "slsh: history: %v\n", err)
		}
	} else {
		// Fresh context: drop the app-server thread so the next turn starts clean.
		if _, err := client.sendCommand(ctx, session.ID, "/compact"); err != nil && opts.verbose {
			fmt.Fprintf(stderr, "slsh: compact: %v\n", err)
		}
	}

	rl, err := newReadline(stdin, stdout, stderr, renderer.colorize(colorCyan, "› "), opts.verbose)
	if err != nil {
		fmt.Fprintf(stderr, "slsh: input: %v\n", err)
		return 1
	}
	defer rl.Close()

	activeOpts := opts
	for {
		line, readErr := rl.Readline()
		if readErr != nil {
			if errors.Is(readErr, readline.ErrInterrupt) || errors.Is(readErr, io.EOF) {
				fmt.Fprintln(stdout)
				return 0
			}
			fmt.Fprintf(stderr, "slsh: input: %v\n", readErr)
			return 1
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			stop, newOpts, err := handleReplCommand(ctx, client, &session, &activeOpts, renderer, line, stdout, stderr)
			if err != nil {
				fmt.Fprintln(stderr, renderer.colorize(colorRed, fmt.Sprintf("error: %v", err)))
				continue
			}
			if stop {
				return 0
			}
			if newOpts != nil {
				activeOpts = *newOpts
			}
			continue
		}
		prompt := buildPromptWithDirectives(line, activeOpts)
		turnCtx, cancel := context.WithTimeout(ctx, activeOpts.timeout)
		_, err := client.sendAndWaitForFinal(turnCtx, session.ID, prompt, renderer)
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, renderer.colorize(colorRed, fmt.Sprintf("turn failed: %v", err)))
			continue
		}
	}
}

// newReadline builds a readline.Instance with persistent history, arrow-key
// recall, Ctrl+R reverse search, and tab completion for slash commands.
// readline handles non-tty stdin internally (used in tests and piped input).
func newReadline(stdin io.Reader, stdout, stderr io.Writer, prompt string, verbose bool) (*readline.Instance, error) {
	historyPath := historyFilePath()
	if historyPath != "" {
		if err := os.MkdirAll(filepath.Dir(historyPath), 0o700); err != nil && verbose {
			fmt.Fprintf(stderr, "slsh: history dir: %v\n", err)
		}
	}
	cfg := &readline.Config{
		Prompt:            prompt,
		HistoryFile:       historyPath,
		HistoryLimit:      10000,
		HistorySearchFold: true,
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		AutoComplete: readline.NewPrefixCompleter(
			readline.PcItem("/help"),
			readline.PcItem("/exit"),
			readline.PcItem("/quit"),
			readline.PcItem("/clear"),
			readline.PcItem("/new"),
			readline.PcItem("/compact"),
			readline.PcItem("/resume"),
			readline.PcItem("/sessions"),
			readline.PcItem("/model",
				readline.PcItem("local"),
				readline.PcItem("spark"),
				readline.PcItem("gpt"),
				readline.PcItem("mini"),
			),
			readline.PcItem("/think",
				readline.PcItem("low"),
				readline.PcItem("medium"),
				readline.PcItem("high"),
				readline.PcItem("off"),
			),
			readline.PcItem("/stop"),
		),
		Stdout: stdout,
		Stderr: stderr,
	}
	if f, ok := stdin.(*os.File); ok {
		cfg.Stdin = f
	}
	return readline.NewEx(cfg)
}

func handleReplCommand(ctx context.Context, client *chatClient, sessionPtr **chatSessionInfo, optsPtr *cliOptions, renderer *renderer, line string, stdout, stderr io.Writer) (bool, *cliOptions, error) {
	parts := strings.Fields(line)
	name := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	rest := parts[1:]
	switch name {
	case "help", "?":
		fmt.Fprintln(stdout, usage())
		return false, nil, nil
	case "exit", "quit", "q":
		return true, nil, nil
	case "clear", "new", "reset":
		if _, err := client.sendCommand(ctx, (*sessionPtr).ID, "/clear"); err != nil {
			return false, nil, err
		}
		fmt.Fprintln(stdout, renderer.colorize(colorDim, "context cleared"))
		return false, nil, nil
	case "compact":
		if _, err := client.sendCommand(ctx, (*sessionPtr).ID, "/compact"); err != nil {
			return false, nil, err
		}
		fmt.Fprintln(stdout, renderer.colorize(colorDim, "thread compacted"))
		return false, nil, nil
	case "stop", "cancel":
		if _, err := client.sendCommand(ctx, (*sessionPtr).ID, "/stop"); err != nil {
			return false, nil, err
		}
		return false, nil, nil
	case "resume":
		if len(rest) == 0 {
			return false, nil, errors.New("usage: /resume SESSION_ID")
		}
		next, err := client.attachSession(ctx, rest[0])
		if err != nil {
			return false, nil, err
		}
		*sessionPtr = next
		fmt.Fprintln(stdout, renderer.colorize(colorDim, fmt.Sprintf("resumed %s @ %s", next.ID, next.WorkspacePath)))
		_ = client.printRecentHistory(ctx, next.ID, renderer)
		return false, nil, nil
	case "sessions":
		entries, err := listKnownSessions()
		if err != nil {
			return false, nil, err
		}
		if len(entries) == 0 {
			fmt.Fprintln(stdout, renderer.colorize(colorDim, "(no saved sessions)"))
			return false, nil, nil
		}
		for _, entry := range entries {
			fmt.Fprintf(stdout, "  %s\t%s\t%s\n", entry.SessionID, entry.WorkspacePath, entry.UpdatedAt.Format(time.RFC3339))
		}
		return false, nil, nil
	case "model":
		if len(rest) == 0 {
			fmt.Fprintf(stdout, "current model: %s\n", firstNonEmpty(optsPtr.model, "local"))
			return false, nil, nil
		}
		newOpts := *optsPtr
		newOpts.model = strings.ToLower(rest[0])
		newOpts.gpt = newOpts.model == "gpt"
		switch newOpts.model {
		case "local", "spark", "gpt", "mini", "codex":
		default:
			return false, nil, fmt.Errorf("unsupported model %q", rest[0])
		}
		fmt.Fprintln(stdout, renderer.colorize(colorDim, "model set to "+newOpts.model))
		return false, &newOpts, nil
	case "think":
		if len(rest) == 0 {
			fmt.Fprintf(stdout, "current think level: %s\n", firstNonEmpty(optsPtr.think, "(default)"))
			return false, nil, nil
		}
		level := strings.ToLower(rest[0])
		switch level {
		case "low", "medium", "high", "off", "none":
		default:
			return false, nil, fmt.Errorf("unsupported think level %q (want low|medium|high)", rest[0])
		}
		newOpts := *optsPtr
		if level == "off" || level == "none" {
			newOpts.think = ""
		} else {
			newOpts.think = level
		}
		fmt.Fprintln(stdout, renderer.colorize(colorDim, "think level set to "+firstNonEmpty(newOpts.think, "(default)")))
		return false, &newOpts, nil
	default:
		// Unknown slash command — let the server decide. Some commands like
		// /plan, /pr, /status are handled server-side.
		result, err := client.sendCommand(ctx, (*sessionPtr).ID, line)
		if err != nil {
			return false, nil, err
		}
		if message, ok := result["message"].(string); ok && strings.TrimSpace(message) != "" {
			fmt.Fprintln(stdout, message)
		}
		return false, nil, nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func isPipedStdin(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) == 0
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
