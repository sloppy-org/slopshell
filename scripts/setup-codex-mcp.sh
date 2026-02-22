#!/usr/bin/env bash
set -euo pipefail

MCP_URL="${1:-http://127.0.0.1:9420/mcp}"
CONFIG_PATH="${CODEX_CONFIG_PATH:-$HOME/.codex/config.toml}"
MARKER_BEGIN="# BEGIN TABURA MCP"
MARKER_END="# END TABURA MCP"

mkdir -p "$(dirname "$CONFIG_PATH")"
if [[ -f "$CONFIG_PATH" ]]; then
  cp "$CONFIG_PATH" "$CONFIG_PATH.bak.$(date +%Y%m%d%H%M%S)"
fi

TMP_BASE="$(mktemp)"
TMP_OUT="$(mktemp)"
cleanup() {
  rm -f "$TMP_BASE" "$TMP_OUT"
}
trap cleanup EXIT

if [[ -f "$CONFIG_PATH" ]]; then
  awk -v begin="$MARKER_BEGIN" -v end="$MARKER_END" '
    $0 == begin { in_block = 1; next }
    $0 == end { in_block = 0; next }
    !in_block { print }
  ' "$CONFIG_PATH" >"$TMP_BASE"
else
  : >"$TMP_BASE"
fi

{
  cat "$TMP_BASE"
  if [[ -s "$TMP_BASE" ]]; then
    echo
  fi
  echo "$MARKER_BEGIN"
  echo "[mcp_servers.tabura]"
  printf 'url = "%s"\n' "$MCP_URL"
  echo "$MARKER_END"
  echo
} >"$TMP_OUT"

mv "$TMP_OUT" "$CONFIG_PATH"
echo "updated $CONFIG_PATH"
echo "server key: mcp_servers.tabura"
