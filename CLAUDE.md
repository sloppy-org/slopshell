# CLAUDE

## Fast Path Rule

For direct runtime requests, run the obvious command first, then verify.
Do not scan source/docs unless the command fails.

## Unified Canvas

Chat is the default pane. Artifacts (text, image, PDF) appear as closeable tabs in the canvas tab bar. A single prompt bar (`#prompt-input` + `#prompt-send`) serves all modes. No dual-mode switching.

## Artifact Interaction (Tap-to-Reference)

Tap/click on artifact text sets a transient marker and location context badge in the prompt bar (`Line N of "title"`). Long-press starts PTT voice recording with location context. Text selection captures the selected text as context. Context is prepended to the chat message on send and cleared after. No persistent marks, overlays, popovers, or commit lifecycle.

Key selectors: `.transient-marker` (pulsing dot), `.prompt-context` (badge chip), `.prompt-context-dismiss` (X button).

JS modules: `canvas.js` (core rendering + location capture, <500 lines), `canvas-mail.js` (mail triage UI), `app.js` (prompt context state + artifact interaction listeners).

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
1. After release: `scripts/bump-version.sh v0.0.X-dev` (next version with -dev)
2. Develop on `-dev` version (all commits during development carry this)
3. To release: `scripts/bump-version.sh v0.0.X` (strip -dev suffix)
4. Create `docs/release-v0.0.X.md`, update README.md and docs/spec-index.md release links
5. Tag and push: `git tag v0.0.X && git push origin v0.0.X`
6. Create GitHub release: `gh release create v0.0.X --title "v0.0.X" --notes-file docs/release-v0.0.X.md`
7. Immediately after: `scripts/bump-version.sh v0.0.Y-dev` (next cycle)

The bump script updates: `.zenodo.json`, `CITATION.cff`, `internal/mcp/server.go`, `internal/web/server.go`, `internal/appserver/client.go`, `internal/voxtypemcp/server.go`.

A pre-commit hook (`scripts/check-version-consistency.sh`) blocks commits when version strings are inconsistent.

Never edit historical release notes. They document what happened in that release. Document new changes in a new release file.

## Playwright Testing Policy

Every UI interaction flow must have a Playwright test. Never skip tests.

- New UI features require corresponding Playwright tests before merge.
- Touch event flows (touchstart/touchend) must be tested alongside mouse flows (mousedown/mouseup).
- Async flows (mic capture, STT, WebSocket) must use mock harnesses (see `tests/playwright/chat-harness.html` and `tests/playwright/harness.html`).
- Key selectors: `#prompt-input` (textarea), `#prompt-send` (send button), `#prompt-bar` (form), `#canvas-tab-bar` (tab bar), `.canvas-pane` (panes).
- Run `npx playwright test` locally and verify 100% pass before push.
- Existing tests: `tests/playwright/artifact-context.spec.ts`, `tests/playwright/mail-actions.spec.ts`, `tests/playwright/chat-voice-send.spec.ts`.

## Cross-Repo Protocol

The generic handoff protocol is maintained in:

- `../handoff-protocol`
- GitHub: `github.com/krystophny/handoff-protocol`

Use that repo as the source of truth for handoff envelope/schema/lifecycle (`handoff.create|peek|consume|revoke|status`).
