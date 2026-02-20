# tabula

Minimal MCP canvas adapter for Codex/Claude workflows.

Tabula keeps the assistant as the driver process and provides:
- `tabula` MCP server (stdio framed + JSONL compatibility)
- desktop browser canvas mode (`/canvas`) with shared web UI code
- HTTP daemon (`tabula serve`) with MCP, canvas WebSocket, and file proxy
- Web UI (`tabula web`) with auth, host management, terminal, and canvas relay
- bootstrap tooling for protocol files and MCP config snippets

## Install

```bash
go build ./cmd/tabula
# optional: install to $GOBIN
go install ./cmd/tabula
```

Requirements:
- Go 1.24+

## CLI Commands

```bash
tabula bootstrap --project-dir .
tabula mcp-server --project-dir . --headless --no-canvas
tabula run --project-dir . "your prompt"
tabula run --assistant claude --project-dir . "your prompt"
tabula run --project-dir . --mcp-url http://127.0.0.1:9420/mcp "prompt via HTTP MCP"
tabula serve --project-dir . --host 127.0.0.1 --port 9420
tabula web --data-dir ~/.tabula-web --project-dir . --host 127.0.0.1 --port 8420
tabula web --project-dir . --local-mcp-url http://127.0.0.1:9420/mcp --ptyd-url http://127.0.0.1:9333 --dev-runtime
tabula ptyd --data-dir ~/.local/share/tabula-ptyd --host 127.0.0.1 --port 9333
tabula canvas
tabula schema
```

Notes:
- `tabula run` defaults to `codex` and uses MCP URL mode.
- `tabula run` defaults to `http://127.0.0.1:9420/mcp`; override with `--mcp-url`.
- when no `DISPLAY`/`WAYLAND_DISPLAY` is available, canvas runtime falls back to headless mode.
- `tabula web --dev-runtime` enables `/api/runtime` metadata used by browser auto-reload.
- `tabula canvas` opens the desktop canvas view in your default browser (`/canvas` -> `/?desktop=1`).

## Dev Hot Reload (Systemd User Units)

Unit templates and install helper live in:

- `deploy/systemd/user/tabula-web.service`
- `deploy/systemd/user/tabula-mcp.service`
- `deploy/systemd/user/tabula-ptyd.service`
- `deploy/systemd/user/tabula-dev-watch.service`
- `scripts/install-tabula-user-units.sh`

Install/enable:

```bash
./scripts/install-tabula-user-units.sh
```

This setup keeps local shell sessions in `tabula-ptyd` so `tabula-web` restarts do not kill your browser terminal session, while MCP code reload is picked up via `tabula-mcp` restart.
The watcher restarts `tabula-ptyd` only when PTY daemon files change.

## Web UI Auth + Session Persistence

`tabula web` stores data in `--data-dir` (default `~/.tabula-web`) using SQLite.

- Admin password hash is persistent.
- Login sessions are DB-backed and survive server restarts.
- Auth cookie is persistent (long-lived `Max-Age`) and survives browser restarts.
- Active remote SSH session mappings are persisted and auto-restored when the web server restarts.
- Browser client stores the last active remote session in `localStorage` and auto-reattaches after reload if the session is available.

## Codex MCP Integration

`tabula bootstrap` writes `.tabula/codex-mcp.toml` with:

```toml
[mcp_servers.tabula]
command = "tabula"
args = ["mcp-server", "--project-dir", "/abs/path/to/project"]
```

Merge that snippet into `~/.codex/config.toml`.

For global local setup (Codex + Claude):

```bash
./scripts/setup-tabula-mcp.sh http://127.0.0.1:9420/mcp
```

Note: `scripts/setup-claude-mcp.sh` requires `jq`.

Individual scripts:
- `scripts/setup-codex-mcp.sh`
- `scripts/setup-claude-mcp.sh`
- `scripts/setup-tabula-mcp.sh`

Bootstrap behavior:
- If `AGENTS.md` does not exist, Tabula creates it with the protocol block.
- If `AGENTS.md` already exists, Tabula does not modify it.
- Tabula always writes `.tabula/AGENTS.tabula.md` as protocol sidecar.
- `.tabula/artifacts/` is reserved for render/output artifacts and is kept gitignored.

## MCP Tools

- `canvas_session_open`
- `canvas_artifact_show`
- `canvas_mark_set`
- `canvas_mark_delete`
- `canvas_marks_list`
- `canvas_mark_focus`
- `canvas_commit`
- `canvas_status`

Canvas state is MCP-first and in-memory; no filesystem event log is required.

## Architecture

Tabula is a standalone UI/canvas MCP server. It does not route to external backends.

In dual-server controller mode:
- configure `tabula` and `helpy` MCP servers separately in Codex/Claude
- let the assistant orchestrate calls between them

## Tests

```bash
go test ./...
```
