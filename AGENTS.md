# AGENTS

<!-- TABURA_PROTOCOL:BEGIN -->
## Tabura Codex Protocol

Use this protocol for Tabura interactive sessions in this project.

1. Apply this default instruction in all Tabura Codex prompts for this project: Prefer using git and the GitHub CLI (`gh`) for repository and GitHub-related workflow tasks.
2. Keep generated render/output artifacts under `.tabura/artifacts`; keep editable source files in the project workspace (not under `.tabura/artifacts`).
3. Use MCP server `tabura` for all canvas operations; do not rely on filesystem event logs.
4. MCP tools: `canvas_session_open`, `canvas_artifact_show`, `canvas_status`, `canvas_import_handoff`, `temp_file_create`, `temp_file_remove`.
5. Keep interaction chat-canvas-first in the web UI; do not depend on a terminal REPL.
6. Keep `.tabura/artifacts/` gitignored; do not commit files from it unless explicitly requested.

<!-- TABURA_PROTOCOL:END -->

## Local Web UI Service (systemd user unit)

For this user/machine, the Web UI is installed as a user service:

- Unit: `tabura-web.service`
- Start: `systemctl --user start tabura-web.service`
- Stop: `systemctl --user stop tabura-web.service`
- Restart: `systemctl --user restart tabura-web.service`
- Status: `systemctl --user status tabura-web.service --no-pager -n 40`
- Logs: `journalctl --user -u tabura-web.service -f`

Expected URL: `http://localhost:8420` (service is configured with `--host 0.0.0.0 --port 8420`).
