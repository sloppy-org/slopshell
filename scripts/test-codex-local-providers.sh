#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="${TABURA_CODEX_TEST_WORKDIR:-$ROOT_DIR}"
FAST_URL="${TABURA_CODEX_FAST_URL:-http://127.0.0.1:8426/v1}"
FAST_MODEL="${TABURA_CODEX_FAST_MODEL:-qwen3.5-9b}"
AGENTIC_URL="${TABURA_CODEX_AGENTIC_URL:-http://127.0.0.1:8430/v1}"
AGENTIC_MODEL="${TABURA_CODEX_AGENTIC_MODEL:-gpt-oss-120b}"
MCP_URL="${TABURA_CODEX_MCP_URL:-http://127.0.0.1:9420/mcp}"

fail() {
  printf '[codex-local-test] ERROR: %s\n' "$*" >&2
  exit 1
}

log() {
  printf '[codex-local-test] %s\n' "$*"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

endpoint_live() {
  local base_url="$1"
  local health_url="${base_url%/v1}/health"
  curl -fsS --max-time 3 "$health_url" >/dev/null 2>&1
}

run_codex() {
  local config_path="$1"
  shift
  CODEX_CONFIG_PATH="$config_path" codex "$@"
}

build_active_config() {
  local provider="$1"
  local model="$2"
  local output_path="$3"
  {
    printf 'model = "%s"\n' "$model"
    printf 'model_provider = "%s"\n' "$provider"
    cat "$CONFIG_PATH"
  } >"$output_path"
}

require_cmd codex
require_cmd curl

TMPDIR="$(mktemp -d -t tabura-codex-local-test-XXXXXX)"
CONFIG_PATH="${TMPDIR}/config.toml"
FAST_OUT="${TMPDIR}/fast.jsonl"
AGENTIC_OUT="${TMPDIR}/agentic.jsonl"
SEARCH_OUT="${TMPDIR}/search.jsonl"
WORKDIR_ESCAPED="${WORKDIR//\\/\\\\}"
WORKDIR_ESCAPED="${WORKDIR_ESCAPED//\"/\\\"}"

cleanup() {
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

printf 'model = "gpt-5.4"\n' >"$CONFIG_PATH"
printf '\n[projects."%s"]\ntrust_level = "trusted"\n' "$WORKDIR_ESCAPED" >>"$CONFIG_PATH"

TABURA_CODEX_FAST_URL="$FAST_URL" \
TABURA_CODEX_FAST_MODEL="$FAST_MODEL" \
TABURA_CODEX_AGENTIC_URL="$AGENTIC_URL" \
TABURA_CODEX_AGENTIC_MODEL="$AGENTIC_MODEL" \
CODEX_CONFIG_PATH="$CONFIG_PATH" \
"$ROOT_DIR/scripts/setup-codex-mcp.sh" "$MCP_URL" >/dev/null

if ! endpoint_live "$FAST_URL"; then
  fail "fast local provider is not reachable at ${FAST_URL%/v1}"
fi

log "Testing fast profile via $FAST_URL"
FAST_CONFIG="${TMPDIR}/fast-config.toml"
build_active_config "tabura_local_fast" "$FAST_MODEL" "$FAST_CONFIG"
run_codex "$FAST_CONFIG" exec \
  -C "$WORKDIR" \
  --skip-git-repo-check \
  --color never \
  --json \
  "Reply with exactly: fast-ok" >"$FAST_OUT"

grep -Fq '"text":"fast-ok"' "$FAST_OUT" || fail "fast profile did not return fast-ok"

SEARCH_PROVIDER="tabura_local_fast"
SEARCH_MODEL="$FAST_MODEL"
if endpoint_live "$AGENTIC_URL"; then
  log "Testing agentic profile via $AGENTIC_URL"
  AGENTIC_CONFIG="${TMPDIR}/agentic-config.toml"
  build_active_config "tabura_local_agentic" "$AGENTIC_MODEL" "$AGENTIC_CONFIG"
  run_codex "$AGENTIC_CONFIG" exec \
    -C "$WORKDIR" \
    --skip-git-repo-check \
    --color never \
    --json \
    "Reply with exactly: agentic-ok" >"$AGENTIC_OUT"

  grep -Fq '"text":"agentic-ok"' "$AGENTIC_OUT" || fail "agentic profile did not return agentic-ok"
  SEARCH_PROVIDER="tabura_local_agentic"
  SEARCH_MODEL="$AGENTIC_MODEL"
else
  log "Agentic endpoint ${AGENTIC_URL%/v1} is not reachable; skipping direct agentic turn"
fi

log "Testing Codex web search with provider $SEARCH_PROVIDER"
SEARCH_CONFIG="${TMPDIR}/search-config.toml"
build_active_config "$SEARCH_PROVIDER" "$SEARCH_MODEL" "$SEARCH_CONFIG"
run_codex "$SEARCH_CONFIG" --search exec \
  -C "$WORKDIR" \
  --skip-git-repo-check \
  --color never \
  --json \
  "What is the current OpenAI Codex page? Answer with the URL only." >"$SEARCH_OUT"

grep -Fq '"type":"web_search"' "$SEARCH_OUT" || fail "web search tool was not invoked"
if ! grep -Fq 'openai.com/codex' "$SEARCH_OUT" && ! grep -Fq 'developers.openai.com/codex' "$SEARCH_OUT"; then
  fail "web search output did not include an OpenAI Codex URL"
fi

log "All Codex local-provider checks passed"
