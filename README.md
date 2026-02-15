# tabula

Minimal MCP canvas adapter for Codex/Claude workflows.

Tabula keeps the assistant as the driver process and provides:
- `tabula-canvas` MCP server (stdio framed + JSONL compatibility)
- optional desktop canvas window runtime
- HTTP daemon (`tabula serve`) with MCP, canvas WebSocket, and file proxy
- Web UI (`tabula web`) with auth, host management, terminal, and canvas relay
- bootstrap tooling for protocol files and MCP config snippets

## Install

```bash
python -m pip install -e .[test]
python -m pip install -e .[web]   # serve/web features (aiohttp + asyncssh)
python -m pip install -e .[gui]   # optional local canvas window (PySide6)
```

## CLI Commands

```bash
tabula bootstrap --project-dir .
tabula mcp-server --project-dir . --headless --no-canvas --fresh-canvas
tabula run --project-dir . "your prompt"
tabula run --assistant claude --project-dir . "your prompt"
tabula run --project-dir . --mcp-url http://127.0.0.1:9420/mcp "prompt via HTTP MCP"
tabula serve --project-dir . --host 127.0.0.1 --port 9420
tabula web --data-dir ~/.tabula-web --project-dir . --host 127.0.0.1 --port 8420
tabula mcp-http-bridge --mcp-url http://127.0.0.1:9420/mcp
tabula canvas
tabula schema
```

Notes:
- `tabula run` defaults to `codex` and configures MCP inline.
- stdio mode always requests a fresh canvas process (`--fresh-canvas`) for each run.
- when no `DISPLAY`/`WAYLAND_DISPLAY` is available, canvas runtime falls back to headless mode.

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
[mcp_servers.tabula-canvas]
command = "tabula"
args = ["mcp-server", "--project-dir", "/abs/path/to/project"]
```

Merge that snippet into `~/.codex/config.toml`.

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

## Tests

```bash
PYTHONPATH=src python -m pytest
```

Optional real interactive Codex E2E (tmux session):

```bash
TABULA_RUN_REAL_CODEX_INTERACTIVE=1 PYTHONPATH=src python -m pytest tests/integration/test_codex_interactive_loop.py -q
```
