# Tabura Spec Index

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

Canonical documentation.

Current runtime baseline:
- `tabura-web.service`
- `tabura-codex-app-server.service`
- `tabura-piper-tts.service`
- `tabura-stt.service`
- `tabura-llm.service`

## Product and Behavior Specs

Read in this order:

1. `interaction-grammar.md`
2. `design-lineage.md`
3. `object-scoped-intent-ui.md`
4. `gesture-truth-table.md`
5. `approval-execution-policy.md`
6. `artifact-kind-taxonomy.md`
7. `interfaces.md`
8. `auxiliary-surfaces.md`
9. `architecture.md`
10. `live-runtime-whitepaper.md`
11. `meeting-notes-privacy.md`
12. `codex-app-server-pivot.md`

Integrated protocol reference:

- `handoff-protocol/README.md`

Release notes:

- Published release: `release-v0.1.9.md`
- Previous release: `release-v0.1.8.md`
- Published baseline: `release-v0.0.1.md`
- Older release notes are historical and may mention retired runtime paths.

Privacy and security specs:

- `meeting-notes-privacy.md`

Migration/support docs:

- Historical retired direction notes:
  - `plugins.md`
  - `meeting-partner-whitepaper.md`
  - `extension-platform-whitepaper.md`
  - `helpy-recovery-issue-pack.md`
    - current use: Helpy interop boundary notes, not a private-repo recovery plan

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
- `internal/web/static/live-session.js`
- `internal/web/static/hotword.js`
- `internal/web/static/zen.js`
- `internal/web/static/canvas.js`
- `internal/web/static/style.css`

## Scope Boundaries

- Tabura defines the interaction/runtime layer for object-scoped intent workflows.
- Producer-side source access (files/calendar/etc.) is external and pluggable.
- Handoff transport contracts are documented in this repo under `docs/handoff-protocol/`.
