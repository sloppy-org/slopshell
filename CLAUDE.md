# CLAUDE

This file is the repo-local working guide for Slopshell. The repository root `AGENTS.md` is a symlink to this file for tool compatibility.

Critical boundary:
- Slopshell must not create, rewrite, or patch a project's `AGENTS.md`.
- Project-local `AGENTS.md` and `CLAUDE.md` files are user-owned workspace content.
- Slopshell-specific behavior and prompt rules belong in Slopshell's internal runtime/prompt code, not in generated project instruction files.

## Contributor Policy

See `CONTRIBUTING.md` for the standing rewrite policy.

Short version:
- do not assume external compatibility obligations without concrete evidence
- prefer radical cleanup over preserving weak legacy shapes
- optimize for UX, code quality, and maintainability

Canonical product interaction reference:
- `docs/interaction-grammar.md`

## Fast Path Rule

For direct runtime requests, run the obvious command first, then verify.
Do not scan source or docs unless the runtime command fails or the request is about code/doc changes.

## Runtime Model

Current runtime shape:
- `slopshell server` is the web/UI runtime and can reuse an existing loopback MCP via `--local-mcp-url`.
- `sloptools server` is the canonical local MCP daemon on `127.0.0.1:9420`, including canvas relay.
- `slopshell mcp-server` remains available for stdio MCP use.

Supported loopback sidecars and helpers:
- `sloptools.service` for the local MCP + canvas relay (`http://127.0.0.1:9420/mcp`)
- `slopshell-codex-app-server.service` for Codex app-server (`ws://127.0.0.1:8787`)
- `slopshell-piper-tts.service` for Piper TTS (`http://127.0.0.1:8424/v1/audio/speech`)
- `slopshell-stt.service` for voxtype daemon with STT service and push-to-talk (`/v1/audio/transcriptions` on `127.0.0.1:8427`)
- `slopshell-llm.service` for the local Qwen routing/fallback layer (`/v1/chat/completions` via base URL `http://127.0.0.1:8081`)

Non-runtime notes:
- No separate `slopshell-mcp.service` sidecar is part of the current model.
- No Helpy runtime is part of Slopshell.
- `scripts/install.sh` wires `SLOPSHELL_INTENT_LLM_URL=http://127.0.0.1:8081` for `slopshell-web.service`.
- Current Qwen profile defaults in code are `qwen3.5-9b` with profile options `qwen3.5-9b,qwen3.5-4b`.
- Keep the local `:8081` runtime reasoning-capable and WebUI-enabled; `slopshell` disables thinking per request where the fast path needs it.
- `scripts/install-slopshell-user-units.sh` enables the full local unit set, including `slopshell-llm.service` and `slopshell-stt.service`.

## Project Bootstrap Contract

`slopshell bootstrap` prepares project-local Slopshell state without taking ownership of project instructions.

What bootstrap does:
- creates `.slopshell/` if needed
- writes `.slopshell/codex-mcp.toml`
- ensures `.slopshell/artifacts/` is gitignored

What bootstrap must not do:
- create `AGENTS.md`
- overwrite `AGENTS.md`
- create `.slopshell/AGENTS.slopshell.md`
- inject protocol blocks into user docs

## Security Boundary

- Web UI/API listener stays on port `8420` by default.
- `sloptools` MCP listener stays on port `9420` and binds loopback by default.
- Web routes must not expose `/mcp`.
- Non-loopback MCP bind is blocked unless `sloptools server --unsafe-public-mcp` is explicitly set.

## Canvas + Chat Contract

One canvas mode only: file-backed rendering.

Assistant output must be either:
1. chat-only text, or
2. file-backed canvas output via `:::file{path="..."}`.

Rules:
- Canvas content is not duplicated into chat speech.
- Ephemeral canvas content uses temporary files under `.slopshell/artifacts/tmp`.
- Long-response temp-file routing is part of the prompt contract and scratch-artifact support, but it is not currently hard-enforced by the backend render-plan stub.
- Canvas operations should go through the Slopshell MCP surface, not ad hoc filesystem-event assumptions.

## Interaction Model

- Runtime input modes are `pen`, `voice`, and `keyboard`; current persisted default is `pen`.
- Tap or left-click toggles voice recording.
- Right-click opens floating text input at `#floating-input`.
- Keyboard typing auto-activates input when nothing is focused.
- Enter sends and clears input.
- Ctrl long-press starts push-to-talk; release stops and sends.
- Escape dismisses overlay or input; if nothing is open and an artifact is visible, it clears to tabula rasa.
- `#edge-left-tap` toggles the workspace/file sidebar used by PR and file-browsing flows.
- Pen mode uses `#ink-layer` and `#ink-controls`; ink submission posts to `/api/ink/submit` and writes artifacts under `.slopshell/artifacts/ink/`.

Important selectors:
- `#workspace`
- `#canvas-column`
- `.canvas-pane`
- `#ink-layer`
- `#ink-controls`
- `#pr-file-pane`
- `#pr-file-drawer-backdrop`
- `#floating-input`
- `#overlay`
- `#indicator`
- `#edge-left-tap`
- `#edge-top`
- `#edge-right`

## Local Services

Core runtime user units:
- `sloptools.service`
- `slopshell-web.service`
- `slopshell-codex-app-server.service`
- `slopshell-piper-tts.service`
- `slopshell-stt.service` (voxtype daemon with STT API and push-to-talk)
- `slopshell-llm.service`

Quick status:

```bash
systemctl --user status sloptools.service slopshell-web.service slopshell-codex-app-server.service slopshell-piper-tts.service slopshell-stt.service slopshell-llm.service --no-pager -n 40
```

Restart core stack:

```bash
systemctl --user restart sloptools.service slopshell-codex-app-server.service slopshell-piper-tts.service slopshell-stt.service slopshell-llm.service slopshell-web.service
```

## Endpoints

- Web: `http://127.0.0.1:8420`
- MCP: `http://127.0.0.1:9420/mcp`
- MCP canvas WS: `ws://127.0.0.1:9420/ws/canvas`
- App-server: `ws://127.0.0.1:8787`
- TTS base URL: `http://127.0.0.1:8424` (`/v1/audio/speech`)
- Intent LLM base URL: `http://127.0.0.1:8081` (Slopshell calls `/v1/chat/completions`)
- STT base URL: `http://127.0.0.1:8427` (`/v1/audio/transcriptions`)
- Local canvas session: `local`
- CLI login endpoint: `POST /api/cli/login` (loopback-only; consumes the token
  written to `$XDG_RUNTIME_DIR/slopshell/cli-token` at server start)

## Terminal client (`slsh`)

Source in `cmd/slsh/`. Single binary, HTTP+WS client over the same API the
browser uses. Build with `./scripts/build-slsh.sh` (or have the user-units
installer place it in `$HOME/.local/bin/slsh`). One-shot with `-p`, interactive
REPL otherwise. Routes to GPT by prefixing prompts that match the existing
`parseTurnRoutingDirectives` in `internal/web/routing_policy.go`:

- `slsh --gpt -p "…"` → "use gpt to …"
- `slsh --think high -p "…"` → "think hard, …"

E2E tests are gated with `//go:build e2e` in `cmd/slsh/e2e_test.go` and use
mock LLM + mock MCP — never real EWS/TUGonline. Run with:

```bash
go test -tags=e2e ./cmd/slsh/...
```

Environment toggles:
- `SLOPSHELL_TTS_URL` overrides the TTS base URL
- `SLOPSHELL_INTENT_LLM_URL=off` disables intent LLM fallback
- `SLOPSHELL_INTENT_LLM_MODEL` selects the local routing model id (default `local`)
- `SLOPSHELL_INTENT_LLM_PROFILE` selects the active local routing profile (default `qwen3.5-9b`)
- `SLOPSHELL_INTENT_LLM_PROFILE_OPTIONS` exposes selectable local routing profiles (default `qwen3.5-9b,qwen3.5-4b`)
- `SLOPSHELL_STT_URL=off` disables STT sidecar usage
- `SLOPSHELL_WEB_MCP_URL` points at a supplementary MCP endpoint that
  exposes `web_search` / `web_fetch` (for example a local helpy server at
  `http://127.0.0.1:8090/mcp`). When set, search-intent turns stay on the
  local assistant and call those tools instead of routing to Codex/Spark.
  Leave unset (or `off`) to keep the prior Codex fallback behavior.

## Local Dev Start

Temporary local web runtime:

```bash
TMP_ROOT="$(mktemp -d -t slopshell-web-XXXXXX)"
PROJECT_DIR="$TMP_ROOT/project"
DATA_DIR="$TMP_ROOT/data"
LOG_FILE="$TMP_ROOT/web.log"
nohup go run ./cmd/slopshell server \
  --project-dir "$PROJECT_DIR" \
  --data-dir "$DATA_DIR" \
  --web-host 127.0.0.1 \
  --web-port 8420 \
  --mcp-host 127.0.0.1 \
  --mcp-port 9420 >"$LOG_FILE" 2>&1 &
PID=$!
curl -fsS http://127.0.0.1:8420/api/setup
```

Stop:

```bash
kill "$PID"
```

## Privacy Boundary

Meeting-notes and speech handling are RAM-only for audio payloads.

Rules:
- audio may exist in memory for processing
- audio is not persisted to disk
- audio is not persisted to SQLite

Primary reference:
- `docs/meeting-notes-privacy.md`

Primary enforcement tests:
- `TestPrivacySchema*` in `internal/web/server_security_test.go`
- `TestPrivacySTT*` in `internal/stt/transcribe_test.go`

## Surface Generation

Generated surface docs are limited to interface inventory.

Current generated artifact:
- `docs/interfaces.md`

Check or refresh:

```bash
./scripts/sync-surface.sh --check
./scripts/sync-surface.sh
```

`sync-surface` must not edit project instruction files.

## Version Policy

Development versions use `-dev`.
Release flow:
1. strip `-dev`
2. release
3. bump to next `-dev`

The bump script updates:
- `.zenodo.json`
- `CITATION.cff`
- `internal/mcp/server.go`
- `internal/web/server.go`
- `internal/appserver/client.go`

Consistency check:

```bash
scripts/check-version-consistency.sh
```

## Code Map

```text
cmd/slopshell/              CLI entry point, bootstrap, server startup, stdio MCP
cmd/surfacegen/          Generated interface doc sync
internal/
  appserver/             Codex app-server websocket client/session logic
  canvas/                Canvas session/artifact state
  extensions/            Legacy manifest compatibility runtime
  licensing/             License compliance tests
  mcp/                   MCP protocol server and tool dispatch
  modelprofile/          Model alias and reasoning-effort resolution
  plugins/               Legacy webhook compatibility runtime
  protocol/              Project bootstrap (.slopshell, MCP config, gitignore)
  ptt/                   Push-to-talk daemon integration
  pty/                   PTY abstraction
  ptyd/                  PTY daemon application
  serve/                 MCP HTTP server runtime
  store/                 SQLite persistence
  stt/                   STT client, normalization, VAD/hallucination guards
  surface/               MCP/web interface inventory for docs/tests
  update/                Binary update flow
  web/                   Public HTTP/WS runtime and UI coordination
    chat.go              Chat HTTP handlers
    chat_canvas.go       Canvas artifact file lifecycle
    project_attention.go Workspace attention/activity tracking
    chat_intent.go       Intent classification and system actions
    chat_model.go        Model profile resolution for the active runtime
    chat_participant.go  Meeting participant capture
    chat_pr.go           PR review loading
    chat_prompt.go       Internal prompt construction
    chat_queue.go        Turn lifecycle and cancellation
    chat_stt.go          STT websocket message handling
    chat_stt_http.go     STT HTTP transcribe endpoint
    chat_tts.go          TTS synthesis routing
    chat_turn.go         Assistant turn execution and render routing
    chat_ws.go           Chat websocket connection behavior
    workspace_runtime.go Workspace runtime CRUD, activation, bootstrap hookup
    server.go            App wiring, router, lifecycle
    server_relay.go      Canvas relay and file proxying
    static/              Embedded frontend assets
```

## Frontend

Source is TypeScript (`.ts`) in `internal/web/static/`. JavaScript (`.js`) and source maps (`.js.map`) are build output — never edit them directly.

Build:

```bash
npm run build:frontend
```

Typecheck:

```bash
npm run typecheck:frontend
```

## Naming and Placement

- Package names: lowercase, single-word, domain-specific nouns.
- Primary files: `<domain>.go`; focused splits: `<domain>_<aspect>.go`.
- Tests: `<domain>_test.go` or `<domain>_<aspect>_test.go`.
- In `internal/web/`, route handlers belong in the file matching the route domain.
- Concurrent state owners should use unexported tracker/registry types with their own mutex.
- Keep leaf packages free of internal package dependencies where possible.

Target limits:
- files under 500 lines when practical, hard limit 1000
- functions under 50 lines when practical, hard limit 100
- interfaces narrow and owned by the defining package

## Product Direction

Active direction is a public modular core, not a private extension ecosystem.

Implications:
- new product behavior should land in normal public core packages
- do not build new feature work around `internal/plugins` or `internal/extensions` unless the task is explicitly about compatibility or removal
- internal prompt/runtime behavior should be implemented in code, not by mutating project instruction files

## Adding New Work

1. If the feature does not need `internal/web`, add a new leaf package under `internal/<name>/`.
2. If it adds HTTP or WS API surface, put handlers in `internal/web/<domain>.go` and register them in the router.
3. If it owns shared mutable state, give it a dedicated unexported tracker type with its own mutex.
4. If it integrates an external HTTP service, define the interface in the owning leaf package and inject it into `web.App`.

## Testing Policy

Every UI interaction flow needs a Playwright test.

Standard pre-push checks:

```bash
./scripts/sync-surface.sh --check
go test ./...
./scripts/playwright.sh
```

Playwright runs in the official container through `scripts/playwright.sh`.

On macOS, if the container daemon (Docker Desktop, OrbStack, colima) is not
running, the script prints a diagnostic and exits.  Set `PLAYWRIGHT_NATIVE=1`
to run natively with locally installed browsers (Chromium is usually available;
Firefox and WebKit require `npx playwright install`):

```bash
PLAYWRIGHT_NATIVE=1 ./scripts/playwright.sh
```

Current Playwright specs:
- `tests/playwright/artifact-context.spec.ts`
- `tests/playwright/canvas-refresh.spec.ts`
- `tests/playwright/canvas.spec.ts`
- `tests/playwright/chat-voice-send.spec.ts`
- `tests/playwright/conversation-mode.spec.ts`
- `tests/playwright/hotword.spec.ts`
- `tests/playwright/hub-mode.spec.ts`
- `tests/playwright/live-dialogue.spec.ts`
- `tests/playwright/live-dialogue-companion.spec.ts`
- `tests/playwright/participant-capture.spec.ts`
- `tests/playwright/pr-review-mode.spec.ts`
- `tests/playwright/review-mode.spec.ts`
- `tests/playwright/silent-mode.spec.ts`
- `tests/playwright/ui-system.spec.ts`

Real-service E2E runs through `./scripts/e2e-local.sh`.

Required services:
- `slopshell-web.service`
- `slopshell-piper-tts.service`
- `slopshell-stt.service`
- `ffmpeg`

Current E2E specs:
- `tests/e2e/app-load.spec.ts`
- `tests/e2e/dialogue-e2e.spec.ts`
- `tests/e2e/stt-http.spec.ts`
- `tests/e2e/stt-tts-roundtrip.spec.ts`
- `tests/e2e/stt-tts-system.spec.ts`
- `tests/e2e/stt-ws.spec.ts`
- `tests/e2e/tts-ws.spec.ts`
- `tests/e2e/voice-e2e.spec.ts`
