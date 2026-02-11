# Tabula Spec Index (Code-First)

This codebase is a minimal Codex-centric canvas adapter.
Canonical behavior is defined by code + tests.

## Core contracts

1. Strict canvas event schema and parsing:
- `src/tabula/events.py`
- `tests/unit/test_events.py`
- `tests/bdd/test_mode_and_event_scenarios.py`

2. Mode transitions:
- `prompt -> discussion` on artifact events
- `discussion -> prompt` on `clear_canvas`
- `src/tabula/state.py`
- `tests/unit/test_state.py`

3. Canvas adapter behavior:
- `src/tabula/canvas_adapter.py`
- `tests/bdd/test_canvas_adapter.py`

4. MCP server contract (`tabula-canvas`):
- `src/tabula/mcp_server.py`
- `tests/bdd/test_mcp_server.py`
- `tests/integration/test_mcp_server_stdio.py`

5. Project bootstrap contract:
- `src/tabula/protocol.py`
- `tests/bdd/test_protocol_bootstrap.py`

6. CLI command surface:
- `canvas`, `schema`, `bootstrap`, `mcp-server`
- `src/tabula/cli.py`
- `tests/bdd/test_cli_usage_modes.py`

7. Optional local canvas window runtime:
- `src/tabula/window.py`
- `tests/gui/test_window_mode_switch.py`
