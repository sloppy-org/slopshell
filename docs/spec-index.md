# Tabura Spec Index

> **Legal notice:** Tabura is provided "as is" and "as available" without warranties, and to the maximum extent permitted by applicable law the authors/contributors accept no liability for damages, data loss, or misuse. You are solely responsible for backups, verification, and safe operation. See [`DISCLAIMER.md`](/DISCLAIMER.md).

Canonical documentation.

Current runtime baseline:
- `tabura-web.service`
- `tabura-codex-app-server.service`
- `tabura-piper-tts.service`
- `tabura-stt.service`
- `tabura-intent.service`
- `tabura-llm.service`

## Product and Behavior Specs

Read in this order:

1. `object-scoped-intent-ui.md`
2. `interfaces.md`
3. `architecture.md`
4. `companion-mode-whitepaper.md`
5. `meeting-notes-privacy.md`
6. `codex-app-server-pivot.md`

Integrated protocol reference:

- `handoff-protocol/README.md`

Release notes:

- Published release: `release-v0.1.8.md`
- Previous release: `release-v0.1.7.md`
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
- `internal/web/static/conversation.js`
- `internal/web/static/hotword.js`
- `internal/web/static/zen.js`
- `internal/web/static/canvas.js`
- `internal/web/static/style.css`

## Scope Boundaries

- Tabura defines the interaction/runtime layer for object-scoped intent workflows.
- Producer-side source access (files/calendar/etc.) is external and pluggable.
- Handoff transport contracts are documented in this repo under `docs/handoff-protocol/`.
