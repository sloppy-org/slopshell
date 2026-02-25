#!/usr/bin/env bash
set -euo pipefail

MODEL_DIR="${TABURA_LLM_MODEL_DIR:-$HOME/.local/share/tabura-llm/models}"
MODEL_FILE="${TABURA_LLM_MODEL_FILE:-Qwen3-0.6B-Q4_K_M.gguf}"
MODEL_URL="${TABURA_LLM_MODEL_URL:-https://huggingface.co/lmstudio-community/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q4_K_M.gguf?download=true}"
SERVER_BIN="${LLAMA_SERVER_BIN:-llama-server}"
HOST="${TABURA_LLM_HOST:-127.0.0.1}"
PORT="${TABURA_LLM_PORT:-8426}"
THREADS="${TABURA_LLM_THREADS:-4}"
CTX_SIZE="${TABURA_LLM_CTX:-2048}"
NGL="${TABURA_LLM_NGL:-99}"

if ! command -v "$SERVER_BIN" >/dev/null 2>&1; then
  echo "llama.cpp server binary not found: $SERVER_BIN" >&2
  echo "Install llama.cpp and ensure llama-server is on PATH (or set LLAMA_SERVER_BIN)." >&2
  exit 1
fi

mkdir -p "$MODEL_DIR"
MODEL_PATH="$MODEL_DIR/$MODEL_FILE"
if [ ! -s "$MODEL_PATH" ]; then
  echo "Downloading $MODEL_FILE to $MODEL_PATH"
  curl -fL --retry 3 --retry-delay 2 -o "$MODEL_PATH.tmp" "$MODEL_URL"
  mv "$MODEL_PATH.tmp" "$MODEL_PATH"
fi

echo "Starting local intent LLM at http://$HOST:$PORT"
exec "$SERVER_BIN" \
  -m "$MODEL_PATH" \
  --host "$HOST" \
  --port "$PORT" \
  -c "$CTX_SIZE" \
  --threads "$THREADS" \
  -ngl "$NGL"
