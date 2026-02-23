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
- **Published release (v0.0.9)**: [`docs/release-v0.0.9.md`](docs/release-v0.0.9.md)
- **Previous release (v0.0.8)**: [`docs/release-v0.0.8.md`](docs/release-v0.0.8.md)
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
tabura server --project-dir . --data-dir ~/.tabura-web --web-host 0.0.0.0 --web-port 8420 --mcp-host 127.0.0.1 --mcp-port 9420 --app-server-url ws://127.0.0.1:8787
```

## Local Integration Defaults

- Web UI/API listener: `http://localhost:8420` (public-facing)
- MCP listener: `http://127.0.0.1:9420/mcp` (loopback-only)
- Canvas websocket relay source: `ws://127.0.0.1:9420/ws/canvas`
- Codex app-server websocket: `ws://127.0.0.1:8787`
- Local canvas session id: `local`
- Spark thinking budget for Spark model (fast path): `TABURA_APP_SERVER_SPARK_REASONING_EFFORT=low` (low|medium|high)

Security model:
- MCP routes are intentionally not exposed on the web listener.
- By default, non-loopback MCP bind is rejected unless `--unsafe-public-mcp` is explicitly set.

Zen canvas behavior:
- Browser opens to tabula rasa (blank white screen) or last artifact.
- Tap anywhere to start/stop voice recording. Right-click to type. Keyboard auto-activates.
- Assistant output follows one path only:
  - chat-only (spoken), or
  - file-backed canvas (`:::file`) with canvas content rendered only on canvas.
- Multi-paragraph assistant output is auto-promoted to a temp canvas file and not shown/spoken in chat.
- Responses stream as ephemeral overlays. Click outside to dismiss.
- Edge panels: hover near top edge for projects, right edge for chat log.
- Slash commands: `/plan`, `/plan on`, `/plan off`, `/clear`, `/compact`.
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
npm run test:reports
```

Test report artifacts are written under `.tabura/artifacts/test-reports/`.

## Citation and Archival Metadata

- Citation metadata: `CITATION.cff`
- Zenodo metadata: `.zenodo.json`
