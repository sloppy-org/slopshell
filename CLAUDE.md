# CLAUDE

## Fast Path Rule

For direct runtime requests, run the obvious command first, then verify.
Do not scan source/docs unless the command fails.

## Canvas Layout (Zen Mode)

Full-viewport canvas with no visible chrome. Two modes: **tabula rasa** (blank white screen) and **artifact** (document fills viewport). Chat happens invisibly; responses appear as ephemeral overlays. Edge panels replace toolbar and chat column.

Key structural selectors: `#workspace` (flex column, full viewport), `#canvas-column` (fills viewport), `.canvas-pane` (artifact panes), `#zen-input` (floating text input), `#zen-overlay` (response overlay), `#zen-indicator` (recording dot + label), `#edge-top` (project panel), `#edge-right` (diagnostics/chat log panel).

Removed selectors (no longer exist): `#prompt-bar`, `#prompt-input`, `#prompt-send`, `#chat-column`, `.prompt-context`.

## Interaction Model

- **Tap/left-click** anywhere on canvas toggles voice recording. Recording symbol appears at tap position.
- **Right-click** opens floating text input (`#zen-input`) at cursor position.
- **Keyboard typing** (no input focused, rasa mode) auto-activates text input centered.
- **Enter** in text input sends message and clears input.
- **Ctrl long-press** (300ms) starts push-to-talk recording; release stops and sends.
- **Escape** dismisses overlay/input. If nothing open and artifact showing, clears to tabula rasa.
- On artifact: tap/right-click sets line context (`[Line N of "title"]`) prepended to message.

Response routing: `turn_started` shows overlay, `assistant_message` streams into overlay, canvas actions update in place with diff highlight, short text stays in overlay, errors auto-dismiss after 2s.

## Edge Panels

- **Desktop**: Mouse within 20px of edge reveals panel (300ms transition). Click to pin. Esc closes.
- **Top edge** (`#edge-top`): Project list with "Tabula Rasa" button.
- **Right edge** (`#edge-right`): Chat history / diagnostics log (`#chat-history`).

JS modules: `zen.js` (interaction state, indicator, text input, overlay), `canvas.js` (rendering + diff highlighting), `canvas-mail.js` (mail triage UI), `app.js` (orchestration, WS routing, edge panels).

## Post-Adjustment Artifact Rule

After making a UI/interaction adjustment, always render a new test artifact in the local session (`session_id: local`) so the user can immediately try the latest behavior.

## Local Services (systemd --user)

Main units:

- `tabura-web.service`
- `tabura-mcp.service`
- `tabura-codex-app-server.service`
- `tabura-voxtype-mcp.service`
- `helpy-mcp.service`

Status:

```bash
systemctl --user status tabura-web.service tabura-mcp.service tabura-codex-app-server.service tabura-voxtype-mcp.service helpy-mcp.service --no-pager -n 40
```

Restart all integration services:

```bash
systemctl --user restart helpy-mcp.service tabura-codex-app-server.service tabura-mcp.service tabura-voxtype-mcp.service tabura-web.service
```

## Handoff-First UI Testing Rule

When testing mail/file workflows intended for canvas UI:

- Do not dump artifact payloads into terminal/chat.
- Create a producer handoff in Helpy (`handoff.create`).
- Import handoff into Tabura (`canvas_import_handoff`).
- Test archive/delete/defer actions in the browser UI.

Default local session and URLs:

- Tabura MCP: `http://127.0.0.1:9420/mcp`
- Helpy MCP: `http://127.0.0.1:8090/mcp`
- Codex App Server: `ws://127.0.0.1:8787`
- Tabura session id: `local`

## Handoff Example: Archive Folder (20 Headers)

```bash
HELPY=http://127.0.0.1:8090/mcp
TAB=http://127.0.0.1:9420/mcp

handoff_id=$(
  curl -sS -X POST "$HELPY" -H 'content-type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"handoff.create","arguments":{"kind":"mail_headers","selector":{"provider":"tugraz","folder":"Archive","limit":20}}}}' \
  | jq -r '.result.structuredContent.handoff_id'
)

curl -sS -X POST "$TAB" -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"canvas_session_open","arguments":{"session_id":"local"}}}'

curl -sS -X POST "$TAB" -H 'content-type: application/json' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"canvas_import_handoff\",\"arguments\":{\"session_id\":\"local\",\"handoff_id\":\"$handoff_id\",\"producer_mcp_url\":\"http://127.0.0.1:8090/mcp\",\"title\":\"Archive (20)\"}}}"
```

## Troubleshooting

- `unknown tool` from MCP after recent changes usually means stale daemon build; restart relevant services.
- If helper wrappers do not expose newly-added generic tools, call MCP JSON-RPC directly on `/mcp`.

## Start Local Web UI In Temporary Directory

Use this exact sequence:

```bash
TMP_ROOT="$(mktemp -d -t tabura-web-XXXXXX)"
PROJECT_DIR="$TMP_ROOT/project"
DATA_DIR="$TMP_ROOT/data"
LOG_FILE="$TMP_ROOT/web.log"
nohup go run ./cmd/tabura web \
  --project-dir "$PROJECT_DIR" \
  --data-dir "$DATA_DIR" \
  --host 127.0.0.1 \
  --port 8420 >"$LOG_FILE" 2>&1 &
PID=$!
curl -fsS http://127.0.0.1:8420/api/setup
```

Report back:
- URL: `http://127.0.0.1:8420`
- PID: `$PID`
- temp root/project/data/log paths

Stop command:

```bash
kill "$PID"
```

## Version Bump Policy

Development uses `-dev` suffix: after releasing `v0.0.5`, immediately bump to `v0.0.6-dev`. On release, strip the suffix.

Workflow:
1. Develop on `-dev` version (all commits during development carry `-dev` suffix)
2. To release: `scripts/bump-version.sh v0.0.X` (strip -dev suffix)
3. Create `docs/release-v0.0.X.md`, update README.md and docs/spec-index.md release links
4. Commit release: `git add` the bump files + release docs + README + spec-index, then `git commit -m "release: v0.0.X"`
5. Bump to next dev: `scripts/bump-version.sh v0.0.Y-dev` and commit
6. Push: `git push origin main`
7. Tag the release commit (not HEAD): `git tag v0.0.X <release-commit-hash> && git push origin v0.0.X`
8. Create GitHub release: `gh release create v0.0.X --title "v0.0.X" --notes-file docs/release-v0.0.X.md`

The bump script updates: `.zenodo.json`, `CITATION.cff`, `internal/mcp/server.go`, `internal/web/server.go`, `internal/appserver/client.go`, `internal/voxtypemcp/server.go`.

A pre-commit hook (`scripts/check-version-consistency.sh`) blocks commits when version strings are inconsistent.

Never edit historical release notes. They document what happened in that release. Document new changes in a new release file.

## Playwright Testing Policy

Every UI interaction flow must have a Playwright test. Never skip tests.

- New UI features require corresponding Playwright tests before merge.
- Touch event flows (touchstart/touchend) must be tested alongside mouse flows (mousedown/mouseup).
- Async flows (mic capture, STT, WebSocket) must use mock harnesses (see `tests/playwright/zen-harness.html` and `tests/playwright/harness.html`).
- Key selectors: `#zen-input` (floating textarea), `#zen-overlay` (response overlay), `#zen-indicator` (recording indicator), `#canvas-column` (viewport), `.canvas-pane` (panes), `#edge-top`, `#edge-right` (edge panels).
- Run `npx playwright test` locally and verify 100% pass before push.
- Test files: `zen-canvas.spec.ts` (zen interactions, overlay, edge panels, diff highlight), `chat-voice-send.spec.ts` (voice recording), `artifact-context.spec.ts` (line context), `review-mode.spec.ts` (artifact rendering, mail teardown), `mail-actions.spec.ts` (mail triage), `canvas-refresh.spec.ts` (fsnotify refresh).

## Cross-Repo Protocol

The generic handoff protocol is maintained in:

- `../handoff-protocol`
- GitHub: `github.com/krystophny/handoff-protocol`

Use that repo as the source of truth for handoff envelope/schema/lifecycle (`handoff.create|peek|consume|revoke|status`).
