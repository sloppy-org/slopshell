#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/llama.sh
source "${SCRIPT_DIR}/lib/llama.sh"

default_if_empty() {
  local value="$1"
  local fallback="$2"
  if [ -n "$value" ]; then
    printf '%s' "$value"
    return
  fi
  printf '%s' "$fallback"
}

PROFILE_PRESET="${TABURA_LLM_PRESET:-}"
MODEL_DIR="${TABURA_LLM_MODEL_DIR:-$HOME/.local/share/tabura-llm/models}"
MODEL_FILE="${TABURA_LLM_MODEL_FILE:-}"
MODEL_URL="${TABURA_LLM_MODEL_URL:-}"
MODEL_PRESET="${TABURA_LLM_MODEL_PRESET:-}"
HOST="${TABURA_LLM_HOST:-}"
PORT="${TABURA_LLM_PORT:-}"
THREADS="${TABURA_LLM_THREADS:-}"
CTX_SIZE="${TABURA_LLM_CTX:-}"
NGL="${TABURA_LLM_NGL:-}"
PARALLEL="${TABURA_LLM_PARALLEL:-}"
ALIAS="${TABURA_LLM_ALIAS:-}"
REASONING_BUDGET="${TABURA_LLM_REASONING_BUDGET:-}"

case "$PROFILE_PRESET" in
  "" | "fast-qwen9b")
    MODEL_FILE="$(default_if_empty "$MODEL_FILE" "Qwen3.5-9B-Q4_K_M.gguf")"
    MODEL_URL="$(default_if_empty "$MODEL_URL" "https://huggingface.co/lmstudio-community/Qwen3.5-9B-GGUF/resolve/main/Qwen3.5-9B-Q4_K_M.gguf?download=true")"
    HOST="$(default_if_empty "$HOST" "127.0.0.1")"
    PORT="$(default_if_empty "$PORT" "8081")"
    THREADS="$(default_if_empty "$THREADS" "4")"
    CTX_SIZE="$(default_if_empty "$CTX_SIZE" "32768")"
    NGL="$(default_if_empty "$NGL" "99")"
    PARALLEL="$(default_if_empty "$PARALLEL" "1")"
    ALIAS="$(default_if_empty "$ALIAS" "qwen3.5-9b")"
    REASONING_BUDGET="$(default_if_empty "$REASONING_BUDGET" "0")"
    ;;
  "codex-gpt-oss-120b")
    MODEL_PRESET="$(default_if_empty "$MODEL_PRESET" "gpt-oss-120b-default")"
    HOST="$(default_if_empty "$HOST" "127.0.0.1")"
    PORT="$(default_if_empty "$PORT" "8080")"
    THREADS="$(default_if_empty "$THREADS" "8")"
    CTX_SIZE="$(default_if_empty "$CTX_SIZE" "32768")"
    NGL="$(default_if_empty "$NGL" "auto")"
    PARALLEL="$(default_if_empty "$PARALLEL" "1")"
    ALIAS="$(default_if_empty "$ALIAS" "gpt-oss-120b")"
    REASONING_BUDGET="$(default_if_empty "$REASONING_BUDGET" "-1")"
    ;;
  *)
    echo "Unknown TABURA_LLM_PRESET: ${PROFILE_PRESET}" >&2
    exit 1
    ;;
esac

SERVER_BIN="$(tabura_find_llama_server)" || {
  echo "llama.cpp server binary not found or unusable." >&2
  if [ -n "${TABURA_LLAMA_LAST_ERROR:-}" ]; then
    echo "Last error: ${TABURA_LLAMA_LAST_ERROR}" >&2
  fi
  echo "Install: brew install llama.cpp, or run devstral-infra/scripts/setup_llamacpp.sh" >&2
  exit 1
}
tabura_llama_prepend_library_dirs "$SERVER_BIN"

if curl -fsS --max-time 2 "http://${HOST}:${PORT}/health" >/dev/null 2>&1; then
  echo "llama-server already running at http://${HOST}:${PORT}; exiting"
  exit 0
fi

echo "Starting local llama.cpp runtime at http://$HOST:$PORT"
args=(
  --host "$HOST"
  --port "$PORT"
  -c "$CTX_SIZE"
  --threads "$THREADS"
  -ngl "$NGL"
  --parallel "$PARALLEL"
  --no-webui
)

if [ -n "$ALIAS" ]; then
  args+=(--alias "$ALIAS")
fi
if [ -n "$REASONING_BUDGET" ]; then
  args+=(--reasoning-budget "$REASONING_BUDGET")
fi

if [ -n "$MODEL_PRESET" ]; then
  if ! "$SERVER_BIN" --help 2>&1 | grep -Fq -- "--${MODEL_PRESET}"; then
    echo "llama-server does not support built-in preset --${MODEL_PRESET}" >&2
    exit 1
  fi
  args=("--${MODEL_PRESET}" "${args[@]}")
else
  [ -n "$MODEL_FILE" ] || {
    echo "TABURA_LLM_MODEL_FILE is required when TABURA_LLM_MODEL_PRESET is unset" >&2
    exit 1
  }
  [ -n "$MODEL_URL" ] || {
    echo "TABURA_LLM_MODEL_URL is required when TABURA_LLM_MODEL_PRESET is unset" >&2
    exit 1
  }
  mkdir -p "$MODEL_DIR"
  MODEL_PATH="$MODEL_DIR/$MODEL_FILE"
  if [ ! -s "$MODEL_PATH" ]; then
    echo "Downloading $MODEL_FILE to $MODEL_PATH"
    curl -fL --retry 3 --retry-delay 2 -o "$MODEL_PATH.tmp" "$MODEL_URL"
    mv "$MODEL_PATH.tmp" "$MODEL_PATH"
  fi
  args=(-m "$MODEL_PATH" "${args[@]}")
fi

exec "$SERVER_BIN" "${args[@]}"
