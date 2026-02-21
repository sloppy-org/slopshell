# Tabula Spec Index

Canonical documentation.

## Product and Behavior Specs

Read in this order:

1. `object-scoped-intent-ui.md`
2. `review-mode-workflow.md`
3. `interfaces.md`
4. `architecture.md`

Integrated protocol reference:

- `handoff-protocol/README.md`

Release notes:

- Published release: `release-v0.0.5.md`
- Previous release: `release-v0.0.4.md`
- Published baseline: `release-v0.0.1.md`

## Source Code Anchors

### CLI and Runtime Entrypoints

- `cmd/tabula/main.go`
- `internal/serve/app.go`
- `internal/web/server.go`

### MCP Surface

- `internal/mcp/server.go`
- `internal/canvas/adapter.go`
- `internal/canvas/events.go`

### Browser UI

- `internal/web/static/index.html`
- `internal/web/static/app.js`
- `internal/web/static/canvas.js`
- `internal/web/static/style.css`

## Scope Boundaries

- Tabula defines the interaction/runtime layer for object-scoped intent workflows.
- Producer-side source access (mail/files/calendar/etc.) is external and pluggable.
- Handoff transport contracts are documented in this repo under `docs/handoff-protocol/`.
