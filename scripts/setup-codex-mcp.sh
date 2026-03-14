#!/usr/bin/env bash
set -euo pipefail

MCP_URL="${1:-http://127.0.0.1:9420/mcp}"
CONFIG_PATH="${CODEX_CONFIG_PATH:-$HOME/.codex/config.toml}"
FAST_URL="${TABURA_CODEX_FAST_URL:-http://127.0.0.1:8426/v1}"
FAST_MODEL="${TABURA_CODEX_FAST_MODEL:-qwen3.5-9b}"
AGENTIC_URL="${TABURA_CODEX_AGENTIC_URL:-http://127.0.0.1:8430/v1}"
AGENTIC_MODEL="${TABURA_CODEX_AGENTIC_MODEL:-gpt-oss-120b}"
MCP_MARKER_BEGIN="# BEGIN TABURA MCP"
MCP_MARKER_END="# END TABURA MCP"
MODELS_MARKER_BEGIN="# BEGIN TABURA LOCAL MODELS"
MODELS_MARKER_END="# END TABURA LOCAL MODELS"

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

strip_block() {
  local input="$1"
  local output="$2"
  local begin="$3"
  local end="$4"
  awk -v begin="$begin" -v end="$end" '
    $0 == begin { in_block = 1; next }
    $0 == end { in_block = 0; next }
    !in_block { print }
  ' "$input" >"$output"
}

if [[ -f "$CONFIG_PATH" ]]; then
  strip_block "$CONFIG_PATH" "$TMP_BASE.mcp" "$MCP_MARKER_BEGIN" "$MCP_MARKER_END"
  strip_block "$TMP_BASE.mcp" "$TMP_BASE" "$MODELS_MARKER_BEGIN" "$MODELS_MARKER_END"
  rm -f "$TMP_BASE.mcp"
else
  : >"$TMP_BASE"
fi

{
  cat "$TMP_BASE"
  if [[ -s "$TMP_BASE" ]]; then
    echo
  fi
  echo "$MCP_MARKER_BEGIN"
  echo "[mcp_servers.tabura]"
  printf 'url = "%s"\n' "$MCP_URL"
  echo "$MCP_MARKER_END"
  echo
  echo "$MODELS_MARKER_BEGIN"
  echo "[model_providers.tabura_local_agentic]"
  echo 'name = "Tabura llama.cpp Agentic"'
  printf 'base_url = "%s"\n' "$AGENTIC_URL"
  echo 'wire_api = "responses"'
  echo
  echo "[model_providers.tabura_local_fast]"
  echo 'name = "Tabura llama.cpp Fast"'
  printf 'base_url = "%s"\n' "$FAST_URL"
  echo 'wire_api = "responses"'
  echo
  echo "[profiles.tabura_local_agentic]"
  echo 'model_provider = "tabura_local_agentic"'
  printf 'model = "%s"\n' "$AGENTIC_MODEL"
  echo 'model_reasoning_effort = "high"'
  echo
  echo "[profiles.tabura_local_fast]"
  echo 'model_provider = "tabura_local_fast"'
  printf 'model = "%s"\n' "$FAST_MODEL"
  echo 'model_reasoning_effort = "minimal"'
  echo "$MODELS_MARKER_END"
  echo
} >"$TMP_OUT"

mv "$TMP_OUT" "$CONFIG_PATH"
echo "updated $CONFIG_PATH"
echo "server key: mcp_servers.tabura"
echo "profile keys: tabura_local_agentic, tabura_local_fast"
