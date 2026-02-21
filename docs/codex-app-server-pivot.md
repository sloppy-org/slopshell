# Codex App Server Pivot (v0.0.5-dev)

This document captures the 2026 integration direction for Tabula’s AI path.

## Why App Server-Centered

Tabula now treats Codex app-server as the primary AI backend for commit-time review flows:

1. Canvas `commit` stays the explicit human control point.
2. Backend aggregates all persistent comments for the active artifact.
3. Backend sends a single structured prompt to Codex app-server.
4. Backend renders the returned rewrite/review output as a new canvas text artifact.

## Transport and Runtime Choices

1. A persistent user service runs `codex app-server --listen ws://127.0.0.1:8787`.
2. Tabula Web connects to app-server via WebSocket JSON-RPC on-demand per commit.
3. For each commit trigger, Tabula opens a short-lived app-server session:
   - `initialize`
   - `thread/start`
   - `turn/start`
   - stream notifications until `turn/completed`
4. Agent output is taken from `item/completed` (`agentMessage`) and task-complete notifications as fallback.

## Artifact Coverage

1. `text_artifact`:
   - Full-document rewrite response expected.
   - Output replaces/refreshes the canvas with revised text.
2. `pdf_artifact`:
   - Binary PDF is not rewritten directly.
   - AI returns structured markdown review notes covering all comments.
   - Notes are rendered as a text artifact for fast iteration.

## Operational Integration

Systemd units now include:

1. `tabula-codex-app-server.service`
2. `tabula-web.service` with dependency on app-server

Install/restart scripts were updated so app-server is started and restarted with the local stack.

## Sources

- OpenAI Codex app-server docs: <https://developers.openai.com/codex/app-server>
- OpenAI Codex MCP docs: <https://developers.openai.com/codex/mcp>
- OpenAI Codex harness article: <https://openai.com/index/unlocking-the-codex-harness/>
