#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_CONFIG_PATH="${CODEX_CONFIG_PATH:-$HOME/.codex/config.toml}"
FAST_URL="${TABURA_CODEX_FAST_URL:-http://127.0.0.1:8426/v1}"
FAST_MODEL="${TABURA_CODEX_FAST_MODEL:-qwen3.5-9b}"
AGENTIC_URL="${TABURA_CODEX_AGENTIC_URL:-http://127.0.0.1:8430/v1}"
AGENTIC_MODEL="${TABURA_CODEX_AGENTIC_MODEL:-gpt-oss-120b}"
MCP_URL="${TABURA_CODEX_MCP_URL:-http://127.0.0.1:9420/mcp}"

usage() {
  cat <<'EOF'
Usage: scripts/codex-local.sh <fast|agentic> [codex args...]

Examples:
  scripts/codex-local.sh fast exec "Reply with exactly: hello"
  scripts/codex-local.sh agentic --search exec "What is the current OpenAI Codex page?"
EOF
}

fail() {
  printf '[codex-local] ERROR: %s\n' "$*" >&2
  exit 1
}

[ "$#" -ge 1 ] || {
  usage >&2
  exit 1
}

PROFILE="$1"
shift

case "$PROFILE" in
  fast)
    PROVIDER="tabura_local_fast"
    MODEL="$FAST_MODEL"
    ;;
  agentic)
    PROVIDER="tabura_local_agentic"
    MODEL="$AGENTIC_MODEL"
    ;;
  -h | --help | help)
    usage
    exit 0
    ;;
  *)
    fail "unknown profile: $PROFILE"
    ;;
esac

command -v codex >/dev/null 2>&1 || fail "codex is not in PATH"

TMPDIR="$(mktemp -d -t tabura-codex-local-XXXXXX)"
CONFIG_PATH="${TMPDIR}/config.toml"

cleanup() {
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

if [ -f "$BASE_CONFIG_PATH" ]; then
  cp "$BASE_CONFIG_PATH" "$CONFIG_PATH"
else
  : >"$CONFIG_PATH"
fi

TABURA_CODEX_FAST_URL="$FAST_URL" \
TABURA_CODEX_FAST_MODEL="$FAST_MODEL" \
TABURA_CODEX_AGENTIC_URL="$AGENTIC_URL" \
TABURA_CODEX_AGENTIC_MODEL="$AGENTIC_MODEL" \
CODEX_CONFIG_PATH="$CONFIG_PATH" \
"$ROOT_DIR/scripts/setup-codex-mcp.sh" "$MCP_URL" >/dev/null

{
  printf 'model = "%s"\n' "$MODEL"
  printf 'model_provider = "%s"\n' "$PROVIDER"
  cat "$CONFIG_PATH"
} >"${CONFIG_PATH}.next"
mv "${CONFIG_PATH}.next" "$CONFIG_PATH"

exec env CODEX_CONFIG_PATH="$CONFIG_PATH" codex "$@"
