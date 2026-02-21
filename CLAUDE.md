# CLAUDE

## Fast Path Rule

For direct runtime requests, run the obvious command first, then verify.
Do not scan source/docs unless the command fails.

## Desktop Canvas Mode

Use one of:

- Browser URL when `tabula web` is already running: `http://localhost:8420/canvas`
- Equivalent URL: `http://localhost:8420/?desktop=1`
- CLI launcher: `tabula canvas`

`tabula canvas` starts web UI and opens desktop canvas route.

## Post-Adjustment Artifact Rule

After making a UI/interaction adjustment, always render a new test artifact in the local desktop canvas session (`session_id: local`) so the user can immediately try the latest behavior.

## Local Services (systemd --user)

Main units:

- `tabula-web.service`
- `tabula-mcp.service`
- `tabula-codex-app-server.service`
- `tabula-voxtype-mcp.service`
- `helpy-mcp.service`

Status:

```bash
systemctl --user status tabula-web.service tabula-mcp.service tabula-codex-app-server.service tabula-voxtype-mcp.service helpy-mcp.service --no-pager -n 40
```

Restart all integration services:

```bash
systemctl --user restart helpy-mcp.service tabula-codex-app-server.service tabula-mcp.service tabula-voxtype-mcp.service tabula-web.service
```

## Handoff-First UI Testing Rule

When testing mail/file workflows intended for canvas UI:

- Do not dump artifact payloads into terminal/chat.
- Create a producer handoff in Helpy (`handoff.create`).
- Import handoff into Tabula (`canvas_import_handoff`).
- Test archive/delete/defer actions in the browser UI.

Default local session and URLs:

- Tabula MCP: `http://127.0.0.1:9420/mcp`
- Helpy MCP: `http://127.0.0.1:8090/mcp`
- Codex App Server: `ws://127.0.0.1:8787`
- Tabula session id: `local`

## Handoff Example: Archive Folder (20 Headers)

```bash
HELPY=http://127.0.0.1:8090/mcp
TAB=http://127.0.0.1:9420/mcp

handoff_id=$(
  curl -sS -X POST "$HELPY" -H 'content-type: application/json' \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"handoff.create","arguments":{"kind":"mail_headers","selector":{"provider":"tugraz","folder":"Archive","limit":20}}}}' \
  | jq -r '.result.structuredContent.handoff_id'
)

curl -sS -X POST "$TAB" -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"canvas_session_open","arguments":{"session_id":"local"}}}'

curl -sS -X POST "$TAB" -H 'content-type: application/json' \
  -d "{\"jsonrpc\":\"2.0\",\"id\":3,\"method\":\"tools/call\",\"params\":{\"name\":\"canvas_import_handoff\",\"arguments\":{\"session_id\":\"local\",\"handoff_id\":\"$handoff_id\",\"producer_mcp_url\":\"http://127.0.0.1:8090/mcp\",\"title\":\"Archive (20)\"}}}"
```

## Troubleshooting

- `unknown tool` from MCP after recent changes usually means stale daemon build; restart relevant services.
- If helper wrappers do not expose newly-added generic tools, call MCP JSON-RPC directly on `/mcp`.

## Start Local Web UI In Temporary Directory

Use this exact sequence:

```bash
TMP_ROOT="$(mktemp -d -t tabula-web-XXXXXX)"
PROJECT_DIR="$TMP_ROOT/project"
DATA_DIR="$TMP_ROOT/data"
LOG_FILE="$TMP_ROOT/web.log"
nohup go run ./cmd/tabula web \
  --project-dir "$PROJECT_DIR" \
  --data-dir "$DATA_DIR" \
  --host 127.0.0.1 \
  --port 8420 >"$LOG_FILE" 2>&1 &
PID=$!
curl -fsS http://127.0.0.1:8420/api/setup
```

Report back:
- URL: `http://127.0.0.1:8420`
- PID: `$PID`
- temp root/project/data/log paths

Stop command:

```bash
kill "$PID"
```

## Cross-Repo Protocol

The generic handoff protocol is maintained in:

- `../handoff-protocol`
- GitHub: `github.com/krystophny/handoff-protocol`

Use that repo as the source of truth for handoff envelope/schema/lifecycle (`handoff.create|peek|consume|revoke|status`).
