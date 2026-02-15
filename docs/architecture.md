# Tabula Architecture

## Overview

Tabula is a minimal MCP canvas adapter that provides interactive canvas
rendering for AI assistants (Codex, Claude) through the Model Context Protocol.
It supports three deployment modes: local desktop, HTTP daemon, and web interface.

## System Diagram

```
                        ┌──────────────────────────┐
                        │     AI Assistant          │
                        │  (Codex / Claude Code)    │
                        └────────────┬─────────────┘
                                     │ MCP JSON-RPC 2.0
                        ┌────────────┴─────────────┐
                        │                          │
              stdio (framed/JSONL)          HTTP POST /mcp
                        │                          │
                        ▼                          ▼
              ┌─────────────────┐      ┌────────────────────┐
              │  TabulaMcpServer│      │  TabulaServeApp    │
              │  (mcp_server.py)│      │  (serve.py)        │
              └────────┬────────┘      │                    │
                       │               │  POST /mcp ──────┐ │
                       │               │  GET  /mcp (SSE)  ││
                       │               │  GET  /ws/canvas  ││
                       │               │  GET  /files/{p}  ││
                       │               │  GET  /health     ││
                       │               └───────┬───────────┘│
                       │                       │             │
                       └───────────┬───────────┘             │
                                   │                         │
                                   ▼                         │
                        ┌─────────────────────┐              │
                        │   CanvasAdapter     │              │
                        │ (canvas_adapter.py) │              │
                        │                     │              │
                        │ Sessions ─────────┐ │              │
                        │ Events            │ │              │
                        │ State             │ │              │
                        │ History           │ │              │
                        └────┬─────────┬────┘ │              │
                             │         │      │              │
                   ┌─────────┘         └──────┼──────────────┘
                   │                          │
                   ▼                          ▼
         ┌─────────────────┐       ┌──────────────────┐
         │  Canvas Window  │       │  WebSocket        │
         │  (window.py)    │       │  Broadcast        │
         │                 │       │                   │
         │  PySide6 GUI    │       │  on_event callback│
         │  stdin ← events │       │  → WS clients     │
         │  stdout → sel.  │       └────────┬──────────┘
         └─────────────────┘                │
                                            ▼
                                    ┌──────────────┐
                                    │  Browser /   │
                                    │  Web Client  │
                                    └──────────────┘
```

## Deployment Modes

### Mode 1: Local Desktop

```
  AI ──stdio──▶ TabulaMcpServer ──▶ CanvasAdapter ──stdin──▶ PySide6 Window
                                         ◀──stdout── selection feedback
```

The AI assistant launches `tabula mcp-server` as a subprocess. MCP messages
flow over stdin/stdout. The adapter spawns a PySide6 window as a child process,
feeding it events via stdin JSON and reading text selection feedback from stdout.

### Mode 2: HTTP Daemon (`tabula serve`)

```
  AI ──HTTP POST /mcp──▶ TabulaServeApp ──▶ CanvasAdapter (headless)
                                                    │
                              WS /ws/canvas ◀───────┘ on_event
                                    │
                                 Clients
```

Headless mode. No GUI subprocess. Canvas events are broadcast to WebSocket
subscribers. File serving is project-scoped. Supports SSE streaming for
long-lived MCP sessions.

### Mode 3: Web Interface (`tabula web`)

```
  Browser
    ├── Terminal WS ───▶ TabulaWebApp ───SSH───▶ Remote Host
    │                        │                       │
    │                   PTY Transport            tabula serve
    │                   (local/SSH)                   │
    │                        │                   HTTP /mcp
    │                   Terminal                  WS /ws/canvas
    │                   Emulator                      │
    │                        │                   ┌────┘
    └── Canvas WS ◀── relay task ◀───────────────┘
```

Full web interface with SSH host management, browser-based terminal,
and canvas relay. Supports both local projects (embedded serve) and
remote hosts (SSH tunnel + port forward).
Auth and remote-session metadata are persisted in SQLite under the web data dir.

## Core Components

### Event System (`events.py`, `state.py`)

```
  CanvasEvent (immutable, frozen dataclass)
  ├── text_artifact   {title, markdown_or_text}
  ├── image_artifact  {title, path}
  ├── pdf_artifact    {title, path, page}
  └── clear_canvas    {reason}

  CanvasState (immutable)
  ├── mode: "prompt" | "review"
  └── active_event: CanvasEvent | None

  reduce_state(state, event) → CanvasState   # pure function
```

Events are immutable with UUID IDs and ISO-8601 timestamps. State is derived
via a pure reducer. History is append-only per session.

### MCP Tool Surface

| Tool | Description |
|------|-------------|
| `canvas_session_open` | Initialize/open session state |
| `canvas_artifact_show` | Display text/image/pdf artifact or clear canvas |
| `canvas_mark_set` | Create/update ephemeral, draft, or persistent marks |
| `canvas_mark_delete` | Delete an existing mark |
| `canvas_marks_list` | List marks for a session/artifact |
| `canvas_mark_focus` | Set/clear focused mark |
| `canvas_commit` | Persist draft marks, write sidecar, write PDF annotations |
| `canvas_status` | Session state, selection, process health |

MCP resources: `tabula://sessions`, `tabula://session/{id}`,
`tabula://session/{id}/marks`.

### Web Server Components

```
  TabulaWebApp (server.py)
  ├── SSHService (ssh.py)          asyncssh connections, tunnels
  ├── Store (store.py)             SQLite: hosts, admin auth, auth sessions, remote sessions
  ├── TerminalEmulator             VT100 cell grid, CSI/OSC parsing
  ├── PtyTransport                 LocalPtyTransport | SshPtyTransport
  └── Static Client
       ├── app.js                  SPA controller, state, routing
       ├── terminal.js             Custom terminal renderer
       ├── canvas.js               Markdown/image/PDF rendering
       ├── auth.js                 Login/setup flows
       ├── hosts.js                Host management UI
       └── mcp-log.js              MCP activity viewer
```

### Auth + Session Persistence

- Admin password is hashed with PBKDF2-SHA256 and stored in SQLite.
- Auth tokens are stored server-side in `auth_sessions`; cookie auth survives server restart.
- Auth cookie is long-lived (`Max-Age`) so login survives browser restart.
- Remote SSH sessions are tracked in `remote_sessions` and reconnected on web app startup.
- Frontend stores last active remote session in `localStorage` and auto-reattaches when available.

### HTTP Bridge (`mcp_http_bridge.py`)

```
  AI ──stdio──▶ mcp_http_bridge ──HTTP POST──▶ tabula serve
```

Translates stdio MCP JSON-RPC to HTTP transport. Used by the web UI to
connect AI assistants to a remote `tabula serve` instance.

## Data Flow: Text Selection Feedback

```
  User selects text in canvas
       │
       ▼
  Canvas sink emits JSON on stdout/WS:
  {"kind":"text_selection","event_id":"...","line_start":5,"line_end":12,"text":"..."}
       │
       ▼
  CanvasAdapter.handle_feedback(line)
       │
       ▼
  SessionRecord.selection updated
       │
       ▼
  AI queries via canvas_marks_list / canvas_status
```

## Dependencies

| Layer | Dependencies |
|-------|-------------|
| Core (adapter, MCP, events, state) | stdlib only |
| GUI (`[gui]`) | PySide6 >= 6.7 |
| Web (`[web]`) | aiohttp >= 3.9, asyncssh >= 2.14 |
| Frontend | marked.js (vendored), custom terminal |

## CLI Entry Points

| Command | Description |
|---------|-------------|
| `tabula canvas` | Launch standalone canvas window |
| `tabula mcp-server` | Run MCP server over stdio |
| `tabula serve` | HTTP daemon (MCP + WS + files) |
| `tabula web` | Web server (SSH + terminal + canvas) |
| `tabula run` | Launch AI assistant with MCP config |
| `tabula bootstrap` | Initialize project structure |
| `tabula mcp-http-bridge` | Proxy stdio MCP to HTTP |
| `tabula schema` | Print event JSON schema |
