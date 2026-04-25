# スロップシェル Slopshell

Core paradigm:
- Full-viewport zen canvas: blank screen (tabula rasa) or artifact fills the view.
- Tap to talk, right-click to type, keyboard auto-activates. No visible chrome.
- Responses stream as ephemeral overlays; document edits update in place with diff highlighting.
- Edge panels (hover/swipe to reveal) for workspace/file browsing and chat panel access.
- Live sessions are split into `Dialogue` and `Meeting`, with one shared audio runtime and built-in `Alexa` hotword behavior.

License: MIT (`LICENSE`)
Legal notice: Slopshell is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](DISCLAIMER.md).

## Start Here

- **Spec hub**: [`docs/spec-index.md`](docs/spec-index.md)
- **System architecture**: [`docs/architecture.md`](docs/architecture.md)
- **Live session architecture**: [`docs/architecture.md`](docs/architecture.md)
- **Codex app-server integration**: [`docs/codex-app-server-pivot.md`](docs/codex-app-server-pivot.md)
- **HTTP/MCP interface inventory**: [`docs/interfaces.md`](docs/interfaces.md)
- **UI paradigm**: [`docs/object-scoped-intent-ui.md`](docs/object-scoped-intent-ui.md)
- **Model download policy**: [`docs/model-download-policy.md`](docs/model-download-policy.md)
- **Meeting notes privacy**: [`docs/meeting-notes-privacy.md`](docs/meeting-notes-privacy.md)
- **Third-party licenses**: [`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md)
- **Current release notes (v0.2.1)**: [`docs/release-v0.2.1.md`](docs/release-v0.2.1.md)

## Install

Universal installers:

```bash
curl -fsSL https://github.com/sloppy-org/slopshell/releases/latest/download/install.sh | bash
```

```powershell
irm https://github.com/sloppy-org/slopshell/releases/latest/download/install.ps1 | iex
```

Package managers:

```bash
brew install sloppy-org/tap/slopshell
```

```bash
paru -S slopshell-bin
# or
yay -S slopshell-bin
```

```powershell
winget install sloppy-org.slopshell
```

Package-manager installs provide the `slopshell` binary only. For full local setup, run `slopshell server` or the installer scripts above.

Uninstall:

```bash
./scripts/install.sh --uninstall
```

```powershell
./scripts/install.ps1 -Uninstall
```

Manual build:

```bash
npm run build:frontend
go build ./cmd/slopshell
go install ./cmd/slopshell
```

`npm run build:frontend` auto-fetches the browser VAD runtime assets into
`internal/web/static/vad/` when they are missing, so source builds retain live
dialogue, hotword, and tap-to-talk browser VAD on a fresh checkout.

Requirements:
- Go 1.24+

## Core Commands

```bash
slopshell bootstrap --project-dir .
slopshell mcp-server --project-dir .
sloptools server --project-dir . --data-dir ~/.local/share/sloptools --mcp-host 127.0.0.1 --mcp-port 9420
slopshell server --project-dir . --data-dir ~/.slopshell-web --local-mcp-url http://127.0.0.1:9420/mcp --web-host 0.0.0.0 --web-port 8420 --app-server-url ws://127.0.0.1:8787 --tts-url http://127.0.0.1:8424
slopshell server --project-dir . --data-dir ~/.slopshell-web --local-mcp-url http://127.0.0.1:9420/mcp --web-host 0.0.0.0 --web-port 8443 --web-cert-file ~/.config/slopshell/certs/slopshell.pem --web-key-file ~/.config/slopshell/certs/slopshell-key.pem --app-server-url ws://127.0.0.1:8787 --tts-url http://127.0.0.1:8424
```

## Terminal client: `slsh`

`slsh` is a minimal terminal chat client that talks to the running
`slopshell server` over the same HTTP/WS API the browser uses. It reuses the
existing tool surface (local LLM, shell tool, MCP mail/calendar/items) and the
remote Codex app-server for GPT/Spark routing — no extra services required.

Build and install to `$HOME/.local/bin/slsh` (or set `SLOPSHELL_BIN_DIR`):

```bash
./scripts/build-slsh.sh
```

`scripts/install-slopshell-user-units.sh` installs `slsh` automatically
alongside the web server when you install the user units from source.

Authentication is loopback-only: `web.New` writes a random 32-byte token to
`$XDG_RUNTIME_DIR/slopshell/cli-token` (falling back to
`~/.local/share/slopshell-web/cli-token`) with `0600` perms; `slsh` reads it
and POSTs to `/api/cli/login`, which refuses non-loopback peers and ignores
forwarded-for headers.

Usage:

```bash
slsh -p "run echo hello from slsh"          # one-shot, local + shell tool
slsh -p "list my email accounts briefly"    # routes through sloptools MCP
slsh --gpt -p "solve this design question"  # routes to Codex app-server
slsh --think high -p "explain this plan"    # raises reasoning effort
slsh                                        # REPL; /clear, /resume, /exit, /model, /think
slsh --resume <session-id>                  # reattach to a prior chat
```

Fresh sessions are the default: `slsh` creates the chat session for the
current workspace and calls the existing `/compact` command so the next turn
starts without old thread context. `/clear` inside the REPL wipes all chat
state via the existing `clearAllAgentsAndContexts` handler. Session ids for
each workspace are cached under `$XDG_STATE_HOME/slopshell/slsh-sessions.json`
so `/sessions` can list them.

Smoke the live stack with `scripts/slsh-smoke.sh`. Go e2e tests run against
mock LLM and mock MCP (never real EWS/TUGonline) and are gated:

```bash
go test -tags=e2e ./cmd/slsh/...
```

## Runtime Stack (Canonical)

Slopshell runs with one web runtime plus a dedicated local MCP daemon:

1. `sloptools.service` (`sloptools server`, loopback MCP and canvas relay)
2. `slopshell-web.service` (`slopshell server`, consuming `--local-mcp-url`)
3. `slopshell-codex-app-server.service`
4. TTS sidecar on `127.0.0.1:8424/v1/audio/speech`
   - default: Piper
5. `slopshell-stt.service` (voxtype daemon with STT API and push-to-talk, `/v1/audio/transcriptions`)
6. `slopshell-llm.service` (local OpenAI-compatible LLM endpoint at `127.0.0.1:8081/v1/chat/completions`)
   - macOS default: `vllm-mlx` serving `mlx-community/Qwen3.5-9B-4bit`
   - Linux default: `llama.cpp` serving Qwen3.5 9B GGUF
   - keep the server reasoning-capable and WebUI-enabled; fast non-thinking paths disable thinking per request

Voice commit still uses built-in browser VAD auto-stop, then sends audio to the local voxtype STT service.

Why TTS remains an HTTP sidecar:
- Piper `libpiper` linking is GPL-governed; direct linking would change distribution obligations.
- A local loopback HTTP sidecar keeps integration simple and license boundaries clear.

## Local Integration Defaults

- Web UI/API listener: `http://localhost:8420` (public-facing)
- Optional HTTPS listener: add `--web-cert-file <cert.pem> --web-key-file <key.pem>` (for example on `8443`)
- Local MCP listener: `http://127.0.0.1:9420/mcp` (served by `sloptools`, loopback-only)
- Canvas websocket relay source: `ws://127.0.0.1:9420/ws/canvas`
- Codex app-server websocket: `ws://127.0.0.1:8787`
- TTS endpoint: `http://127.0.0.1:8424/v1/audio/speech`
- Voxtype STT endpoint: `http://127.0.0.1:8427/v1/audio/transcriptions`
- Intent LLM endpoint: `http://127.0.0.1:8081/v1/chat/completions` (`SLOPSHELL_INTENT_LLM_URL`, set `off` to disable)
- Codex local profile endpoint on macOS: `http://127.0.0.1:8081/v1/responses`
- Codex local profile endpoint on Linux: `http://127.0.0.1:8080/v1/responses`
- Intent/delegator request model id: `SLOPSHELL_INTENT_LLM_MODEL` (default `local`)
- Intent/delegator profile selection: `SLOPSHELL_INTENT_LLM_PROFILE` (default `qwen3.5-9b`)
- Intent/delegator profile options: `SLOPSHELL_INTENT_LLM_PROFILE_OPTIONS` (macOS unplugged default: `qwen3.5-9b`)
- local fast requests that should skip chain-of-thought send `chat_template_kwargs.enable_thinking=false` instead of globally disabling reasoning on the server
- Assistant routing mode: `SLOPSHELL_ASSISTANT_MODE` (macOS unplugged default: `local`)
- Codex local profiles written by `scripts/setup-codex-mcp.sh`: `local` and `fast`
- Codex local wrapper for current CLI builds: `scripts/codex-local.sh fast ...` or `scripts/codex-local.sh local ...`
- Local canvas session id: `local`
- Spark thinking budget for Spark model (fast path): `SLOPSHELL_APP_SERVER_SPARK_REASONING_EFFORT=low` (`low`/`medium`/`high`/`xhigh`)

Security model:
- MCP routes are intentionally not exposed on the web listener.
- `slopshell server` can reuse an existing loopback MCP with `--local-mcp-url`.
- `sloptools server` rejects non-loopback MCP bind unless `--unsafe-public-mcp` is explicitly set.

## Temporary Voxtype Branch Pin

Until upstream release catches up, Slopshell docs and service integration assume:

- Repo: `https://github.com/peteonrails/voxtype`
- Branch: `feature/single-daemon-openai-stt-api`

If you build voxtype from source for Slopshell STT, use that branch.

On macOS, build voxtype from source using the provided script:

```bash
scripts/build-voxtype-macos.sh
```

This clones the pinned branch and builds with Metal GPU support on
Apple Silicon. The resulting binary must expose `--service`, `GET /healthz`,
and `POST /v1/audio/transcriptions`. Requires Rust (`rustup`) and `cmake`.

## LAN HTTPS For Voice Capture

Some browsers (especially on macOS/iOS) block microphone features on insecure LAN origins.
Run the web listener with TLS and open `https://<your-lan-ip>:8443`.

Example with `mkcert`:

```bash
mkdir -p ~/.config/slopshell/certs
mkcert -install
mkcert -cert-file ~/.config/slopshell/certs/slopshell.pem -key-file ~/.config/slopshell/certs/slopshell-key.pem localhost 127.0.0.1 ::1 192.168.1.50
slopshell server --project-dir . --data-dir ~/.slopshell-web --local-mcp-url http://127.0.0.1:9420/mcp --web-host 0.0.0.0 --web-port 8443 --web-cert-file ~/.config/slopshell/certs/slopshell.pem --web-key-file ~/.config/slopshell/certs/slopshell-key.pem --app-server-url ws://127.0.0.1:8787 --tts-url http://127.0.0.1:8424
```

If a second device (for example a Mac) connects to this server, trust the same local CA on that device too.

Zen canvas behavior:
- Browser opens to tabula rasa (blank white screen) or last artifact.
- Tap anywhere to start/stop voice recording. Right-click to type. Keyboard auto-activates.
- Built-in VAD auto-stop detects utterance end and commits speech.
- Live sessions are local-first and Whisper-backed by default.
- The main canvas stays empty; live controls live in the hidden top edge panel.
- Meetings and long-running jobs default to temporary workspaces with persisted text artifacts only.
- Assistant output follows one path only:
  - chat-only (spoken), or
  - file-backed canvas (`:::file`) with canvas content rendered only on canvas.
- Multi-paragraph assistant output is auto-promoted to a temp canvas file and not shown/spoken in chat.
- Responses stream as ephemeral overlays. Click outside to dismiss.
- Edge panels: left edge reveals the workspace/file sidebar, right edge reveals chat log.
- Slash commands: `/plan`, `/plan on`, `/plan off`, `/pr [selector]`, `/status`, `/stop`, `/clear`, `/compact`.
- Artifacts render Markdown + LaTeX.

## Markdown LaTeX Rendering

Markdown text artifacts support TeX math rendering via MathJax.
- Inline math: `$...$` or `\(...\)`
- Display math: `$$...$$` or `\[...\]`

## Novel UI Focus (What To Evaluate First)

1. Zen canvas: invisible chrome, full-viewport document surface.
2. Tap-to-talk voice capture with recording dot indicator.
3. Ephemeral response overlays with hidden chat history in the edge panel.
4. Edge-reveal panels for hidden project/diagnostics chrome.
5. E-ink-friendly: no animations for functional elements, static indicators.

See:
- [`docs/object-scoped-intent-ui.md`](docs/object-scoped-intent-ui.md)
- [`docs/interfaces.md`](docs/interfaces.md)

## Integration Example (Optional)

```bash
PRODUCER=http://127.0.0.1:8090/mcp
CONSUMER=http://127.0.0.1:9420/mcp

handoff_id=$(
  curl -sS -X POST "$PRODUCER" -H 'content-type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"handoff.create","arguments":{"kind":"file","selector":{"path":"README.md"}}}}' \
  | jq -r '.result.structuredContent.handoff_id'
)

curl -sS -X POST "$CONSUMER" -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"canvas_session_open","arguments":{"session_id":"local"}}}'

curl -sS -X POST "$CONSUMER" -H 'content-type: application/json' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"canvas_import_handoff\",\"arguments\":{\"session_id\":\"local\",\"handoff_id\":\"$handoff_id\",\"producer_mcp_url\":\"$PRODUCER\",\"title\":\"Imported File\"}}}"
```

## Tests

```bash
./scripts/sync-surface.sh --check
go test ./...
./scripts/playwright.sh
```

Test report artifacts are written under `.slopshell/artifacts/test-reports/`.

## Citation and Archival Metadata

- Citation metadata: `CITATION.cff`
- Zenodo metadata: `.zenodo.json`
