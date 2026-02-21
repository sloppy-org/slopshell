# tabula

Tabula is a local-first MCP canvas and review runtime built around an object-scoped intent interface.

Core paradigm:
- Invoke AI on the object itself, not in a persistent global chat panel.
- Capture intent in context (voice, prompt, or comment mode).
- Keep all generated output as explicit proposals under user control.
- Commit changes through review workflows instead of hidden auto-apply.

License: MIT (`LICENSE`)
Risk notice: see [`DISCLAIMER.md`](DISCLAIMER.md)

## Start Here

- **Spec hub**: [`docs/spec-index.md`](docs/spec-index.md)
- **UI paradigm**: [`docs/object-scoped-intent-ui.md`](docs/object-scoped-intent-ui.md)
- **Review state and commit flow**: [`docs/review-mode-workflow.md`](docs/review-mode-workflow.md)
- **HTTP/MCP interface inventory**: [`docs/interfaces.md`](docs/interfaces.md)
- **Integrated handoff protocol spec**: [`docs/handoff-protocol/README.md`](docs/handoff-protocol/README.md)
- **System architecture**: [`docs/architecture.md`](docs/architecture.md)
- **Codex app-server pivot notes**: [`docs/codex-app-server-pivot.md`](docs/codex-app-server-pivot.md)
- **Published release (v0.0.4)**: [`docs/release-v0.0.4.md`](docs/release-v0.0.4.md)
- **Previous release (v0.0.3)**: [`docs/release-v0.0.3.md`](docs/release-v0.0.3.md)
- **Published baseline (v0.0.1)**: [`docs/release-v0.0.1.md`](docs/release-v0.0.1.md)

## Install

```bash
go build ./cmd/tabula
go install ./cmd/tabula
```

Requirements:
- Go 1.24+

## Core Commands

```bash
tabula bootstrap --project-dir .
tabula mcp-server --project-dir . --headless --no-canvas
tabula serve --project-dir . --host 127.0.0.1 --port 9420
tabula web --data-dir ~/.tabula-web --project-dir . --host 127.0.0.1 --port 8420 --app-server-url ws://127.0.0.1:8787
tabula ptyd --data-dir ~/.local/share/tabula-ptyd --host 127.0.0.1 --port 9333
tabula canvas
```

## Local Integration Defaults

- Web UI: `http://localhost:8420`
- MCP HTTP: `http://127.0.0.1:9420/mcp`
- Canvas websocket: `ws://127.0.0.1:9420/ws/canvas`
- Codex app-server websocket: `ws://127.0.0.1:8787`
- Local browser session id: `local`

Commit-triggered AI rewrite:
- On `Commit`, Tabula now aggregates persistent review comments for the active artifact.
- For text artifacts (`markdown/plain text`), it asks Codex app-server for a full rewritten document.
- For PDF artifacts, it asks Codex app-server for structured review notes and renders them as a text artifact.

## Novel UI Focus (What To Evaluate First)

1. Object-scoped invocation behavior (`long press` and local prompt/capture paths).
2. Explicit proposal lifecycle (`Accept`, `Edit`, `Reject`) with no hidden mutation.
3. Annotation-first review semantics with commit-controlled persistence.
4. Low-refresh and e-ink-friendly interaction constraints.

See:
- [`docs/object-scoped-intent-ui.md`](docs/object-scoped-intent-ui.md)
- [`docs/review-mode-workflow.md`](docs/review-mode-workflow.md)
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
go test ./...
npm run test:reports
```

Test report artifacts are written under `.tabula/artifacts/test-reports/`.

## Citation and Archival Metadata

- Citation metadata: `CITATION.cff`
- Zenodo metadata: `.zenodo.json`
