# Codex App Server Pivot (v0.0.5-dev)

This document captures the 2026 integration direction for Tabula’s AI path.

## Why App Server-Centered

Tabula now treats Codex app-server as the primary AI backend for both chat turns and commit-time review flows:

1. Browser starts in a persistent project chat canvas.
2. Backend streams assistant turn events to the chat UI.
3. Canvas `commit` remains the explicit human control point for annotation persistence.
4. Commit-time rewrite/review still routes through Codex app-server.

## Transport and Runtime Choices

1. A persistent user service runs `codex app-server --listen ws://127.0.0.1:8787`.
2. Tabula Web backend connects to app-server via WebSocket JSON-RPC for chat/commit turns.
3. For each turn trigger, Tabula opens an app-server session:
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

## App Server vs CLI (Practical)

1. `codex` CLI is user-interactive (terminal-first UI).
2. `codex app-server` is programmatic (JSON-RPC endpoint over stdio/WebSocket).
3. Both use the same underlying Codex runtime concepts (threads, turns, tools, policies).
4. App-server gives product code explicit control of lifecycle and event handling.

## Sessions, Context, and Persistence

1. A single `threadId` can be reused across many turns to retain context.
2. `thread/start` supports ephemeral and non-ephemeral behavior; choose based on product UX.
3. In Tabula’s current commit-trigger flow we use short-lived commit sessions by default.
4. `cwd` on thread/turn controls filesystem working directory.

## Tool Access and Policy

1. Tool execution remains policy-gated by approval and sandbox settings.
2. MCP tool access is inherited from configured MCP servers (same model as CLI).
3. App-server integration does not imply unrestricted shell or filesystem access.
4. Product code must treat tool availability as dynamic and handle tool/runtime failures cleanly.

## Latency Characteristics

1. End-to-end latency is dominated by model generation + tool execution, not WebSocket transport.
2. App-server supports streaming notifications for better perceived responsiveness.
3. Localhost transport overhead is usually small versus model/tool time.

## Current Tabula Behavior

1. Chat tab is the default shell and persists per-project message history.
2. Assistant responses stream to browser chat and render Markdown + LaTeX.
3. Canvas tab stays manual-switch (no forced auto-switch on new artifacts).
4. Commit triggers backend aggregation of persistent comments and app-server rewrite/review output.
