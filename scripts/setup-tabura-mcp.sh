#!/usr/bin/env bash
set -euo pipefail

MCP_URL="${1:-http://127.0.0.1:9420/mcp}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

"$SCRIPT_DIR/setup-codex-mcp.sh" "$MCP_URL"
"$SCRIPT_DIR/setup-claude-mcp.sh" "$MCP_URL"

echo "Configured Codex and Claude to use tabura at $MCP_URL"
