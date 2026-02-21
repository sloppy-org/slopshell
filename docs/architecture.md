# Tabula Architecture

Tabula is a Go-first MCP canvas/runtime stack with a browser UI.

## Components

- `cmd/tabula/main.go`
  - CLI entrypoint and subcommand dispatch.
- `internal/mcp/server.go`
  - MCP JSON-RPC methods and tool dispatch.
- `internal/canvas/adapter.go`
  - Canvas sessions, artifact state, marks, commit, and event log.
- `internal/serve/app.go`
  - MCP HTTP daemon (`/mcp`) and canvas websocket (`/ws/canvas`).
- `internal/web/server.go`
  - Browser APIs for chat sessions, canvas/mail actions, and chat/canvas websocket routes.
- `internal/store/store.go`
  - SQLite persistence for auth and chat session/message history.
- `internal/protocol/bootstrap.go`
  - Bootstrap behavior for project-local integration files.

## Runtime Modes

- `tabula mcp-server`: stdio MCP runtime
- `tabula serve`: HTTP MCP + canvas websocket runtime
- `tabula web`: browser-facing runtime
- `tabula canvas`: convenience browser launcher

## Primary Data Flows

1. MCP client calls tool on `tabula mcp-server` or `tabula serve`.
2. Tool dispatch in `internal/mcp/server.go` resolves into adapter operations.
3. Adapter updates session/artifact/mark state in memory and emits events.
4. Browser chat and canvas consume websocket events and render UI state.
5. `canvas_commit` persists review annotations sidecar files.

## Handoff Import Flow

1. Producer creates handoff payload (outside Tabula).
2. Tabula receives `canvas_import_handoff` with `handoff_id`.
3. Tabula peeks/consumes producer handoff payload and renders artifact.
4. Imported artifact becomes reviewable in the same canvas session.

## Trust and Access Boundaries

- Tabula does not require direct credentials to producer systems.
- Producer endpoint authority remains outside Tabula.
- Tabula stores local auth/session state in SQLite under web data dir.
- Browser actions only become persistent changes through explicit commit operations.
