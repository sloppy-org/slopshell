# タブラ tabura

Core paradigm:
- Full-viewport zen canvas: blank screen (tabula rasa) or artifact fills the view.
- Tap to talk, right-click to type, keyboard auto-activates. No visible chrome.
- Responses stream as ephemeral overlays; document edits update in place with diff highlighting.
- Edge panels (hover/swipe to reveal) for project switching and diagnostics.

License: MIT (`LICENSE`)
Risk notice: see [`DISCLAIMER.md`](DISCLAIMER.md)

## Start Here

- **Spec hub**: [`docs/spec-index.md`](docs/spec-index.md)
- **UI paradigm**: [`docs/object-scoped-intent-ui.md`](docs/object-scoped-intent-ui.md)
- **HTTP/MCP interface inventory**: [`docs/interfaces.md`](docs/interfaces.md)
- **Integrated handoff protocol spec**: [`docs/handoff-protocol/README.md`](docs/handoff-protocol/README.md)
- **System architecture**: [`docs/architecture.md`](docs/architecture.md)
- **Codex app-server pivot notes**: [`docs/codex-app-server-pivot.md`](docs/codex-app-server-pivot.md)
- **Published release (v0.0.8)**: [`docs/release-v0.0.8.md`](docs/release-v0.0.8.md)
- **Previous release (v0.0.7)**: [`docs/release-v0.0.7.md`](docs/release-v0.0.7.md)
- **Published baseline (v0.0.1)**: [`docs/release-v0.0.1.md`](docs/release-v0.0.1.md)

## Install

```bash
go build ./cmd/tabura
go install ./cmd/tabura
```

Requirements:
- Go 1.24+

## Core Commands

```bash
tabura bootstrap --project-dir .
tabura mcp-server --project-dir .
tabura serve --project-dir . --host 127.0.0.1 --port 9420
tabura web --data-dir ~/.tabura-web --project-dir . --host 127.0.0.1 --port 8420 --app-server-url ws://127.0.0.1:8787
tabura voxtype-mcp --bind 127.0.0.1 --port 8091
tabura canvas
```

## Local Integration Defaults

- Web UI: `http://localhost:8420`
- MCP HTTP: `http://127.0.0.1:9420/mcp`
- Canvas websocket (internal relay source): `ws://127.0.0.1:9420/ws/canvas`
- Codex app-server websocket: `ws://127.0.0.1:8787`
- VoxType MCP bridge: `http://127.0.0.1:8091/mcp`
- Local canvas session id: `local`

Zen canvas behavior:
- Browser opens to tabula rasa (blank white screen) or last artifact.
- Tap anywhere to start/stop voice recording. Right-click to type. Keyboard auto-activates.
- Responses stream as ephemeral overlays. Click outside to dismiss.
- Edge panels: hover near top edge for projects, right edge for chat log.
- Slash commands: `/plan`, `/plan on`, `/plan off`, `/clear`, `/compact`.
- Artifacts render Markdown + LaTeX.

## Push To Prompt

Tabura uses the term **Push To Prompt** (coined in this project) for voice-driven intent capture, analogous to Push To Talk.  
In `v0.0.5`, STT is routed through VoxType MCP (`/api/stt/push-to-prompt`) and no Helpy STT provider is used by Tabura.

For always-on local usage, run the user `systemd` bridge service `tabura-voxtype-mcp.service`.
It bridges browser audio capture to the `voxtype` transcription CLI.

## Markdown LaTeX Rendering

Markdown text artifacts support TeX math rendering via MathJax.
- Inline math: `$...$` or `\(...\)`
- Display math: `$$...$$` or `\[...\]`

## Novel UI Focus (What To Evaluate First)

1. Zen canvas: invisible chrome, full-viewport document surface.
2. Tap-to-talk voice capture with recording dot indicator.
3. Ephemeral response overlays (no persistent chat panel).
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
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"handoff.create","arguments":{"kind":"mail_headers","selector":{"provider":"work","folder":"INBOX","limit":20}}}}' \
  | jq -r '.result.structuredContent.handoff_id'
)

curl -sS -X POST "$CONSUMER" -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"canvas_session_open","arguments":{"session_id":"local"}}}'

curl -sS -X POST "$CONSUMER" -H 'content-type: application/json' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"canvas_import_handoff\",\"arguments\":{\"session_id\":\"local\",\"handoff_id\":\"$handoff_id\",\"producer_mcp_url\":\"$PRODUCER\",\"title\":\"Inbox (20)\"}}}"
```

## Tests

```bash
./scripts/sync-surface.sh --check
go test ./...
npm run test:reports
```

Test report artifacts are written under `.tabura/artifacts/test-reports/`.

## Citation and Archival Metadata

- Citation metadata: `CITATION.cff`
- Zenodo metadata: `.zenodo.json`
