#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/llama.sh
source "${SCRIPT_DIR}/lib/llama.sh"

MODEL_DIR="${TABURA_LLM_MODEL_DIR:-$HOME/.local/share/tabura-llm/models}"
MODEL_FILE="${TABURA_LLM_MODEL_FILE:-Qwen3.5-9B-Q4_K_M.gguf}"
MODEL_URL="${TABURA_LLM_MODEL_URL:-https://huggingface.co/lmstudio-community/Qwen3.5-9B-GGUF/resolve/main/Qwen3.5-9B-Q4_K_M.gguf?download=true}"
HOST="${TABURA_LLM_HOST:-127.0.0.1}"
PORT="${TABURA_LLM_PORT:-8426}"
THREADS="${TABURA_LLM_THREADS:-4}"
CTX_SIZE="${TABURA_LLM_CTX:-16384}"
NGL="${TABURA_LLM_NGL:-99}"

SERVER_BIN="$(tabura_find_llama_server)" || {
  echo "llama.cpp server binary not found or unusable." >&2
  if [ -n "${TABURA_LLAMA_LAST_ERROR:-}" ]; then
    echo "Last error: ${TABURA_LLAMA_LAST_ERROR}" >&2
  fi
  echo "Install: brew install llama.cpp, or run devstral-infra/scripts/setup_llamacpp.sh" >&2
  exit 1
}
tabura_llama_prepend_library_dirs "$SERVER_BIN"

mkdir -p "$MODEL_DIR"
MODEL_PATH="$MODEL_DIR/$MODEL_FILE"
if [ ! -s "$MODEL_PATH" ]; then
  echo "Downloading $MODEL_FILE to $MODEL_PATH"
  curl -fL --retry 3 --retry-delay 2 -o "$MODEL_PATH.tmp" "$MODEL_URL"
  mv "$MODEL_PATH.tmp" "$MODEL_PATH"
fi

if curl -fsS --max-time 2 "http://${HOST}:${PORT}/health" >/dev/null 2>&1; then
  echo "llama-server already running at http://${HOST}:${PORT}; exiting"
  exit 0
fi

echo "Starting local intent LLM at http://$HOST:$PORT"
exec "$SERVER_BIN" \
  -m "$MODEL_PATH" \
  --host "$HOST" \
  --port "$PORT" \
  -c "$CTX_SIZE" \
  --threads "$THREADS" \
  -ngl "$NGL"
