#!/usr/bin/env bash
set -euo pipefail

MCP_URL="${1:-http://127.0.0.1:9420/mcp}"
SETTINGS_PATH="${CLAUDE_SETTINGS_PATH:-$HOME/.claude/settings.json}"

mkdir -p "$(dirname "$SETTINGS_PATH")"
if [[ -f "$SETTINGS_PATH" ]]; then
  cp "$SETTINGS_PATH" "$SETTINGS_PATH.bak.$(date +%Y%m%d%H%M%S)"
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "error: jq is required for $0" >&2
  exit 1
fi

BASE_JSON='{}'
if [[ -f "$SETTINGS_PATH" ]] && [[ -s "$SETTINGS_PATH" ]]; then
  BASE_JSON="$(cat "$SETTINGS_PATH")"
fi

TMP_OUT="$(mktemp)"
cleanup() {
  rm -f "$TMP_OUT"
}
trap cleanup EXIT

printf '%s\n' "$BASE_JSON" | jq -S --arg mcp_url "$MCP_URL" '
  if type != "object" then
    error("settings root must be a JSON object")
  else
    .
  end
  | .mcpServers = (.mcpServers // {})
  | .mcpServers |= (if type == "object" then . else {} end)
  | .mcpServers.tabula = {"url": $mcp_url}
' >"$TMP_OUT"

mv "$TMP_OUT" "$SETTINGS_PATH"
echo "updated $SETTINGS_PATH"
echo "server key: mcpServers.tabula"
