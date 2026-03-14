#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_CONFIG_PATH="${CODEX_CONFIG_PATH:-$HOME/.codex/config.toml}"
FAST_URL="${TABURA_CODEX_FAST_URL:-http://127.0.0.1:8081/v1}"
FAST_MODEL="${TABURA_CODEX_FAST_MODEL:-qwen3.5-9b}"
LOCAL_URL="${TABURA_CODEX_LOCAL_URL:-http://127.0.0.1:8080/v1}"
LOCAL_MODEL="${TABURA_CODEX_LOCAL_MODEL:-gpt-oss-120b}"
MCP_URL="${TABURA_CODEX_MCP_URL:-http://127.0.0.1:9420/mcp}"

usage() {
  cat <<'EOF'
Usage: scripts/codex-local.sh <fast|local> [codex args...]

Examples:
  scripts/codex-local.sh fast exec "Reply with exactly: hello"
  scripts/codex-local.sh local --search exec "What is the current OpenAI Codex page?"
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
    PROVIDER="fast"
    MODEL="$FAST_MODEL"
    REASONING_EFFORT="minimal"
    ;;
  local)
    PROVIDER="local"
    MODEL="$LOCAL_MODEL"
    REASONING_EFFORT="high"
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
TABURA_CODEX_LOCAL_URL="$LOCAL_URL" \
TABURA_CODEX_LOCAL_MODEL="$LOCAL_MODEL" \
CODEX_CONFIG_PATH="$CONFIG_PATH" \
"$ROOT_DIR/scripts/setup-codex-mcp.sh" "$MCP_URL" >/dev/null

exec env CODEX_CONFIG_PATH="$CONFIG_PATH" codex \
  -c "model=\"$MODEL\"" \
  -c "model_provider=\"$PROVIDER\"" \
  -c "model_reasoning_effort=\"$REASONING_EFFORT\"" \
  "$@"
