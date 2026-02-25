# Tabura Architecture

Tabura is a Go monolithic web runtime with a split listener model:
- public web/UI listener
- local-only MCP listener

Runtime stack:
- `tabura-web.service` runs the Go monolith (`tabura server`)
- `tabura-codex-app-server.service` runs Codex app-server
- `tabura-piper-tts.service` runs Piper TTS API on loopback
- `tabura-intent.service` runs local intent classification on loopback (`/classify`)
- `tabura-llm.service` runs Qwen3 0.6B intent fallback on loopback (`/v1/chat/completions`)

## Components

- `cmd/tabura/main.go`
  - CLI entrypoint and subcommand dispatch.
- `internal/mcp/server.go`
  - MCP JSON-RPC methods and tool dispatch.
- `internal/canvas/adapter.go`
  - Canvas sessions, artifact state, and event log.
- `internal/serve/app.go`
  - MCP HTTP daemon (`/mcp`) and canvas websocket (`/ws/canvas`) mounted on the MCP listener.
- `internal/web/server.go`
  - Browser APIs for chat sessions, canvas APIs, and chat/canvas websocket routes on the web listener.
- `internal/store/store.go`
  - SQLite persistence for auth and chat session/message history.
- `internal/protocol/bootstrap.go`
  - Bootstrap behavior for project-local integration files.

## Runtime Modes

- `tabura mcp-server`: stdio MCP runtime
- `tabura server`: monolithic runtime (web + local MCP listeners)

## Local Sidecars

- Codex app-server remains a separate local service and is consumed over `ws://127.0.0.1:8787`.
- Piper TTS remains a separate local HTTP service on `http://127.0.0.1:8424`.
- Intent classifier remains a separate local HTTP service on `http://127.0.0.1:8425/classify`.
- Intent LLM fallback remains a separate local HTTP service on `http://127.0.0.1:8426/v1/chat/completions`.
- Piper is intentionally not linked into the Go binary (`libpiper`) to avoid GPL-linked distribution coupling.

## UI Layout (Zen Canvas)

The browser UI is a full-viewport canvas with no visible chrome:

- **Tabula rasa**: blank white screen when no artifact is loaded.
- **Artifact mode**: document (text, image, PDF) fills the viewport.
- No toolbar, no prompt bar, no chat column. All interaction is invisible.
- **Edge panels** (hidden): top edge = project switcher, right edge = chat log / diagnostics. Revealed by hovering near screen edge (desktop) or swiping inward (mobile).

## Primary Data Flows

1. MCP client calls tool on `tabura mcp-server` or the local MCP listener from `tabura server`.
2. Tool dispatch in `internal/mcp/server.go` resolves into adapter operations.
3. Adapter updates session/artifact state in memory and emits events.
4. Browser consumes websocket events: responses stream into ephemeral overlay, artifacts update the canvas in place.

## Interaction Model

- **Tap/left-click** toggles voice recording. A red dot appears at the tap position.
- Pure VAD detects end-of-utterance and commits speech input.
- **Right-click** opens a floating text input at the cursor position.
- **Keyboard typing** (when nothing is focused) auto-activates text input.
- **Enter** sends the message; input is cleared.
- **Ctrl long-press** (300ms) starts push-to-talk; release stops and sends.
- **Escape** dismisses overlay/input. If nothing is open and an artifact is showing, clears to tabula rasa.
- On artifact: tap/right-click captures line context (`[Line N of "title"]`) prepended to the message.
- Responses stream as ephemeral overlays; click outside to dismiss. Document edits update the canvas in place with diff highlighting.

## Handoff Import Flow

1. Producer creates handoff payload (outside Tabura).
2. Tabura receives `canvas_import_handoff` with `handoff_id`.
3. Tabura peeks/consumes producer handoff payload and renders artifact.

## Trust and Access Boundaries

- Tabura does not require direct credentials to producer systems.
- Producer endpoint authority remains outside Tabura.
- Tabura stores local auth/session state in SQLite under web data dir.
- MCP routes are not mounted on the web listener and default to loopback-only bind.
