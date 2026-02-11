# tabula

Minimal Codex-first canvas adapter.

Tabula is not a workflow orchestrator. Codex remains the master process.
Tabula provides:

- stdio MCP server (`tabula-canvas`) for canvas tools (framed + JSONL compatible)
- optional local canvas window runtime
- project bootstrap (non-destructive `AGENTS.md` handling + MCP snippet + artifact folders)

## Install

```bash
python -m pip install -e .[test]
python -m pip install -e .[gui]   # optional for local canvas window
# Arch/PEP668 user install example:
python -m pip install --user --break-system-packages PySide6
```

## Core commands

```bash
tabula bootstrap --project-dir .
tabula mcp-server --project-dir . --headless --no-canvas --fresh-canvas
tabula run --project-dir . "your prompt"
tabula run --assistant claude --project-dir . "your prompt"
tabula canvas
tabula schema
```

`tabula run` launches an interactive assistant with inline Tabula MCP configuration.
Default assistant is `codex` (with `--yolo --search`); use `--assistant claude` for Claude Code.
It also requests a fresh canvas process (`--fresh-canvas`) per launch.
If no `DISPLAY`/`WAYLAND_DISPLAY` is available, it warns and runs headless.
When available, it forwards display-related env vars (`DISPLAY`, `WAYLAND_DISPLAY`, `XAUTHORITY`, etc.) into MCP startup.

## Codex MCP integration

`tabula bootstrap` writes `.tabula/codex-mcp.toml` with a snippet like:

```toml
[mcp_servers.tabula-canvas]
command = "tabula"
args = ["mcp-server", "--project-dir", "/abs/path/to/project"]
```

Merge that snippet into `~/.codex/config.toml`.

Bootstrap AGENTS behavior:
- If `AGENTS.md` does not exist, Tabula creates it with the protocol block.
- If `AGENTS.md` already exists, Tabula does **not** modify it.
- Tabula always writes `.tabula/AGENTS.tabula.md` as the protocol sidecar.
- Protocol default: keep editable source files in project workspace; use `.tabula/artifacts/` only for render/output artifacts.
- Bootstrap always ensures `.tabula/artifacts/` is gitignored.

## MCP tools exposed

- `canvas_activate`
- `canvas_render_text`
- `canvas_render_image`
- `canvas_render_pdf`
- `canvas_clear`
- `canvas_status`
- `canvas_selection`
- `canvas_history`

Canvas state is MCP-first and in-memory; no filesystem event log is required.
UX scope for this MVP is only `prompt` and `review` canvas modes.
`canvas_activate`/`canvas_status` also report `canvas_process_alive` and `canvas_launch_error` for startup diagnostics.
When users select text in the canvas text view, selection metadata (line range + text) is exposed via `canvas_status.selection` and `canvas_selection`.

## Tests

```bash
PYTHONPATH=src python -m pytest
```

Optional real interactive Codex E2E (tmux terminal session, no `codex exec`):

```bash
TABULA_RUN_REAL_CODEX_INTERACTIVE=1 PYTHONPATH=src python -m pytest tests/integration/test_codex_interactive_loop.py -q
```
