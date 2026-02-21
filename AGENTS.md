# AGENTS

<!-- TABULA_PROTOCOL:BEGIN -->
## Tabula Codex Protocol

Use this protocol for Tabula interactive sessions in this project.

1. Read extra instructions from `.tabula/prompt-injection.txt` and apply them.
2. Keep generated render/output artifacts under `.tabula/artifacts`; keep editable source files in the project workspace (not under `.tabula/artifacts`).
3. Use MCP server `tabula` for all canvas operations; do not rely on filesystem event logs.
4. MCP tools: `canvas_session_open`, `canvas_artifact_show`, `canvas_mark_set`, `canvas_mark_delete`, `canvas_marks_list`, `canvas_mark_focus`, `canvas_commit`, `canvas_status`.
5. Keep interaction chat-canvas-first in web UI; do not depend on a terminal REPL.
6. Keep `.tabula/artifacts/` gitignored; do not commit files from it unless explicitly requested.

<!-- TABULA_PROTOCOL:END -->

## Local Web UI Service (systemd user unit)

For this user/machine, the Web UI is installed as a user service:

- Unit: `tabula-web.service`
- Start: `systemctl --user start tabula-web.service`
- Stop: `systemctl --user stop tabula-web.service`
- Restart: `systemctl --user restart tabula-web.service`
- Status: `systemctl --user status tabula-web.service --no-pager -n 40`
- Logs: `journalctl --user -u tabula-web.service -f`

Expected URL: `http://localhost:8420` (service is configured with `--host 0.0.0.0 --port 8420`).
