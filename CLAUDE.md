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

No separate `tabura-mcp.service` or `tabura-voxtype-mcp.service` sidecars are used.
No Helpy integration/runtime is active in Tabura.
Tabura keeps four local sidecars:
- `tabura-codex-app-server.service` for Codex app-server
- `tabura-piper-tts.service` for Piper TTS over loopback HTTP
- `tabura-intent.service` for local intent classification (`/classify` on `127.0.0.1:8425`)
- `tabura-llm.service` for Qwen3 0.6B via llama.cpp (`/v1/chat/completions` on `127.0.0.1:8426`)

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

## Zen Interaction

- Tap/left-click toggles voice recording.
- Right-click opens floating text input (`#zen-input`).
- Keyboard typing auto-activates input when nothing is focused.
- Enter sends and clears input.
- Ctrl long-press starts push-to-talk; release stops/sends.
- Escape dismisses overlay/input; if nothing is open and artifact is visible, clears to tabula rasa.

Key selectors:
- `#workspace`, `#canvas-column`, `.canvas-pane`
- `#zen-input`, `#zen-overlay`, `#zen-indicator`
- `#edge-top`, `#edge-right`

## Local Services (systemd --user)

Primary units:
- `tabura-web.service`
- `tabura-codex-app-server.service`
- `tabura-piper-tts.service`
- `tabura-intent.service` (optional but recommended)
- `tabura-llm.service` (optional for ambiguous-intent fallback)

Quick status:

```bash
systemctl --user status tabura-web.service tabura-codex-app-server.service tabura-piper-tts.service tabura-intent.service tabura-llm.service --no-pager -n 40
```

Restart core stack:

```bash
systemctl --user restart tabura-codex-app-server.service tabura-piper-tts.service tabura-intent.service tabura-llm.service tabura-web.service
```

## Endpoints

- Web: `http://127.0.0.1:8420`
- MCP: `http://127.0.0.1:9420/mcp`
- MCP canvas WS: `ws://127.0.0.1:9420/ws/canvas`
- App-server: `ws://127.0.0.1:8787`
- TTS (Piper): `http://127.0.0.1:8424`
- Intent classifier: `http://127.0.0.1:8425/classify` (`TABURA_INTENT_CLASSIFIER_URL`, use `off` to disable)
- Intent LLM fallback: `http://127.0.0.1:8426/v1/chat/completions` (`TABURA_INTENT_LLM_URL`, use `off` to disable)
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

## Testing Policy

Every UI interaction flow must have a Playwright test.

Run before push:

```bash
./scripts/sync-surface.sh --check
go test ./...
npx playwright test
```

Current core specs include:
- `tests/playwright/zen-canvas.spec.ts`
- `tests/playwright/chat-voice-send.spec.ts`
- `tests/playwright/artifact-context.spec.ts`
- `tests/playwright/review-mode.spec.ts`
- `tests/playwright/canvas-refresh.spec.ts`
