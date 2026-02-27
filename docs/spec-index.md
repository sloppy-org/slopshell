# Tabura Spec Index

Canonical documentation.

Current runtime baseline:
- `tabura-web.service`
- `tabura-codex-app-server.service`
- `tabura-piper-tts.service`
- `tabura-intent.service`
- `tabura-llm.service`

## Product and Behavior Specs

Read in this order:

1. `object-scoped-intent-ui.md`
2. `interfaces.md`
3. `architecture.md`

Integrated protocol reference:

- `handoff-protocol/README.md`

Release notes:

- Published release: `release-v0.1.4.md`
- Previous release: `release-v0.1.3.md`
- Published baseline: `release-v0.0.1.md`
- Older release notes are historical and may mention retired runtime paths.

Migration/support docs:

- `helpy-recovery-issue-pack.md`

## Source Code Anchors

### CLI and Runtime Entrypoints

- `cmd/tabura/main.go`
- `internal/serve/app.go`
- `internal/web/server.go`

### MCP Surface

- `internal/mcp/server.go`
- `internal/canvas/adapter.go`
- `internal/canvas/events.go`

### Browser UI

- `internal/web/static/index.html`
- `internal/web/static/app.js`
- `internal/web/static/conversation.js`
- `internal/web/static/hotword.js`
- `internal/web/static/zen.js`
- `internal/web/static/canvas.js`
- `internal/web/static/style.css`

## Scope Boundaries

- Tabura defines the interaction/runtime layer for object-scoped intent workflows.
- Producer-side source access (files/calendar/etc.) is external and pluggable.
- Handoff transport contracts are documented in this repo under `docs/handoff-protocol/`.
