# CLAUDE

## Fast Path Rule

For direct runtime requests, run the obvious command first, then verify.
Do not scan source/docs unless the command fails.

## Runtime Model (Current)

Tabura uses one monolithic runtime command for app operation:

- `tabura server` starts both listeners in one process:
  - Web listener (public-facing)
  - MCP listener (local-only by default)
- `tabura mcp-server` remains available for stdio MCP use.

No separate `tabura-mcp.service` sidecars are used.
No Helpy integration/runtime is active in Tabura.
Tabura keeps five local sidecars:
- `tabura-codex-app-server.service` for Codex app-server
- `tabura-piper-tts.service` for Piper TTS over loopback HTTP
- `tabura-intent.service` for local intent classification (`/classify` on `127.0.0.1:8425`)
- `tabura-llm.service` for Qwen3 0.6B via llama.cpp (`/v1/chat/completions` on `127.0.0.1:8426`)
- `tabura-stt.service` for voxtype STT (`/v1/audio/transcriptions` on `127.0.0.1:8427`)

## Security Boundary

- Web UI/API listener stays on port `8420` (typically routed publicly).
- MCP listener stays on port `9420` and must bind loopback by default.
- Web router does not expose MCP routes (`/mcp` must not be served by web listener).
- Non-loopback MCP bind is blocked unless `--unsafe-public-mcp` is explicitly set.

## Canvas + Chat Contract

One canvas mode only: file-backed rendering.

Assistant output must be either:
1. chat-only text (shown in chat and spoken), or
2. two-part response where canvas content is file-backed (`:::file{path="..."}`) and rendered only on canvas.

Rules:
- Canvas content is never duplicated into chat speech.
- Ephemeral canvas content is implemented via temporary files.
- Multi-paragraph assistant output should be promoted to a temp canvas file and not shown/spoken in chat.

## Interaction

- Tap/left-click toggles voice recording.
- Right-click opens floating text input (`#floating-input`).
- Keyboard typing auto-activates input when nothing is focused.
- Enter sends and clears input.
- Ctrl long-press starts push-to-talk; release stops/sends.
- Escape dismisses overlay/input; if nothing is open and artifact is visible, clears to tabula rasa.

Key selectors:
- `#workspace`, `#canvas-column`, `.canvas-pane`
- `#floating-input`, `#overlay`, `#indicator`
- `#edge-top`, `#edge-right`

## Local Services (systemd --user)

Primary units:
- `tabura-web.service`
- `tabura-codex-app-server.service`
- `tabura-piper-tts.service`
- `tabura-stt.service` (voxtype STT sidecar)
- `tabura-intent.service` (optional but recommended)
- `tabura-llm.service` (optional for ambiguous-intent fallback)

Quick status:

```bash
systemctl --user status tabura-web.service tabura-codex-app-server.service tabura-piper-tts.service tabura-stt.service tabura-intent.service tabura-llm.service --no-pager -n 40
```

Restart core stack:

```bash
systemctl --user restart tabura-codex-app-server.service tabura-piper-tts.service tabura-stt.service tabura-intent.service tabura-llm.service tabura-web.service
```

## Endpoints

- Web: `http://127.0.0.1:8420`
- MCP: `http://127.0.0.1:9420/mcp`
- MCP canvas WS: `ws://127.0.0.1:9420/ws/canvas`
- App-server: `ws://127.0.0.1:8787`
- TTS (Piper): `http://127.0.0.1:8424`
- Intent classifier: `http://127.0.0.1:8425/classify` (`TABURA_INTENT_CLASSIFIER_URL`, use `off` to disable)
- Intent LLM fallback: `http://127.0.0.1:8426/v1/chat/completions` (`TABURA_INTENT_LLM_URL`, use `off` to disable)
- STT (voxtype): `http://127.0.0.1:8427/v1/audio/transcriptions` (`TABURA_STT_URL`, use `off` to disable)
- Local canvas session: `local`

## Start Local Web UI In Temporary Directory

```bash
TMP_ROOT="$(mktemp -d -t tabura-web-XXXXXX)"
PROJECT_DIR="$TMP_ROOT/project"
DATA_DIR="$TMP_ROOT/data"
LOG_FILE="$TMP_ROOT/web.log"
nohup go run ./cmd/tabura server \
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

## Meeting Notes Privacy

Audio may exist in RAM for processing but is never persisted to disk or database.
Full contract: `docs/meeting-notes-privacy.md`.
Enforcement tests: `TestPrivacySchema*` and `TestPrivacySTT*` in `internal/web/server_security_test.go` and `internal/stt/transcribe_test.go`.

## Version Bump Policy

Development uses `-dev` suffix; release strips suffix; then bump to next `-dev`.

The bump script updates:
- `.zenodo.json`
- `CITATION.cff`
- `internal/mcp/server.go`
- `internal/web/server.go`
- `internal/appserver/client.go`

Run consistency check:

```bash
scripts/check-version-consistency.sh
```

## Code Map

```
cmd/tabura/              CLI entry point, flag parsing, server bootstrap
internal/
  web/                   HTTP/WS server (public-facing)
    server.go            App struct, constructor, auth, router, lifecycle
    server_relay.go      Canvas WS, MCP relay, file proxy
    chat.go              Chat HTTP handlers, commands, plugin hooks
    chat_queue.go        chatTurnTracker type, cancellation, session mgmt
    chat_turn.go         Assistant turn execution, rendering decisions
    chat_prompt.go       Prompt building, delegation hints, canvas context
    chat_hub.go          Hub project orchestration
    chat_intent.go       Intent classification, system action execution
    chat_canvas.go       Canvas artifact file lifecycle, file watching
    chat_model.go        Model profile resolution
    chat_participant.go  Meeting participant capture (RAM-only audio)
    chat_tts.go          TTS synthesis
    chat_stt.go          STT WebSocket message handling
    chat_stt_http.go     STT HTTP transcribe endpoint
    chat_pr.go           GitHub PR review loading
    chat_ws.go           chatWSConn type, TTS sequencing
    ws_hub.go            wsHub type: WebSocket connection registry, broadcast
    tunnel_registry.go   tunnelRegistry type: MCP tunnel/relay/serve state
    stt_config.go        STT configuration persistence
    stt_replacements.go  STT text replacement rules
    hotword.go           Hotword detector asset status
    static/              Embedded frontend (JS/CSS)
  store/                 SQLite persistence (zero internal deps)
    store.go             Store struct, types, constructor, migrations
    store_auth.go        Admin password, auth sessions
    store_project.go     Project CRUD, app state
    store_host.go        Host CRUD, remote sessions
    store_chat.go        Chat session/message operations
    store_participant.go Participant session/segment/event tracking
  mcp/                   MCP protocol server
    server.go            Protocol dispatch, types, stdio transport
    server_delegate.go   Delegate job lifecycle (start/poll/cancel)
    server_tools.go      Tool implementations, resource reads
  appserver/             Codex app-server WebSocket client
  canvas/                In-memory canvas session/artifact state
  stt/                   STT HTTP client, VAD, hallucination detection
  plugins/               Plugin webhook manager + HookProvider interface
  extensions/            Extension host (superset of plugins)
  modelprofile/          Model alias resolution, reasoning config
  serve/                 MCP HTTP server runtime
  surface/               MCP tool/route definitions
  ptt/                   Push-to-talk daemon (Linux evdev)
  pty/                   PTY abstraction (Unix/Windows)
  ptyd/                  PTY daemon application
  update/                Binary auto-update
  protocol/              Project bootstrap, AGENTS.md
  licensing/             License compliance tests
```

## Naming and Placement Conventions

- **Package names**: lowercase, single word, noun describing the domain (`store`, `canvas`, `stt`). No `util`, `common`, `helpers`.
- **File names**: `<domain>.go` for the primary file, `<domain>_<aspect>.go` for splits (e.g., `store_chat.go`, `server_delegate.go`). Tests: `<domain>_<aspect>_test.go`.
- **web/ file naming**: HTTP handlers go in the file matching their route group (`chat.go` for `/api/chat/*`). Supporting logic gets a `_<aspect>` suffix (`chat_turn.go`, `chat_queue.go`).
- **Concurrent-state types**: unexported types (`chatTurnTracker`, `wsHub`, `tunnelRegistry`) each own their own `sync.Mutex`. Live in the file that uses them most.
- **Size limits**: files < 500 lines (hard limit 1,000), functions < 50 lines (hard limit 100).
- **Interfaces**: define in the owning package (`plugins.HookProvider`), not in the consumer. Keep narrow (2-4 methods).
- **Dependency direction**: leaf packages (`store`, `stt`, `canvas`, `appserver`, `modelprofile`) have zero internal deps. `mcp` and `serve` compose leaf packages. `web` composes everything.

## Adding a New Feature Module

1. If it needs no `web` imports: create `internal/<name>/` with its own types, tests, and zero internal deps.
2. If it's a new API surface: add handlers in `internal/web/<domain>.go`, register routes in `Router()`.
3. If it manages concurrent state: define an unexported tracker/registry type with its own mutex. Add it as a field on `App`.
4. If it integrates external HTTP services: define an interface in the relevant leaf package, implement it there, inject into `App` via the constructor.

## Testing Policy

Every UI interaction flow must have a Playwright test.

Run before push:

```bash
./scripts/sync-surface.sh --check
go test ./...
npx playwright test
```

Current core specs include:
- `tests/playwright/canvas.spec.ts`
- `tests/playwright/chat-voice-send.spec.ts`
- `tests/playwright/artifact-context.spec.ts`
- `tests/playwright/review-mode.spec.ts`
- `tests/playwright/canvas-refresh.spec.ts`
- `tests/playwright/hotword.spec.ts`
- `tests/playwright/conversation-mode.spec.ts`
- `tests/playwright/e2e-system.spec.ts`
- `tests/playwright/stt-tts-system.spec.ts`
