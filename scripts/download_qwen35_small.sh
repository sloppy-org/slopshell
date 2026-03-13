#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MODEL_DIR="${TABURA_LLM_MODEL_DIR:-$HOME/.local/share/tabura-llm/models}"
MODELS="${QWEN35_SMALL_MODELS:-0.8b,2b,4b,9b}"

LOG_DIR="${REPO_ROOT}/.tabura/artifacts/downloads"
LOG_FILE="${LOG_DIR}/qwen35-small-download.log"
UNIT_NAME="${QWEN35_DOWNLOAD_UNIT:-tabura-qwen35-download}"

declare -A FILE_BY_KEY=(
  ["0.8b"]="Qwen3.5-0.8B-Q4_K_M.gguf"
  ["2b"]="Qwen3.5-2B-Q4_K_M.gguf"
  ["4b"]="Qwen3.5-4B-Q4_K_M.gguf"
  ["9b"]="Qwen3.5-9B-Q4_K_M.gguf"
)

declare -A URL_BY_KEY=(
  ["0.8b"]="https://huggingface.co/lmstudio-community/Qwen3.5-0.8B-GGUF/resolve/main/Qwen3.5-0.8B-Q4_K_M.gguf?download=true"
  ["2b"]="https://huggingface.co/lmstudio-community/Qwen3.5-2B-GGUF/resolve/main/Qwen3.5-2B-Q4_K_M.gguf?download=true"
  ["4b"]="https://huggingface.co/lmstudio-community/Qwen3.5-4B-GGUF/resolve/main/Qwen3.5-4B-Q4_K_M.gguf?download=true"
  ["9b"]="https://huggingface.co/lmstudio-community/Qwen3.5-9B-GGUF/resolve/main/Qwen3.5-9B-Q4_K_M.gguf?download=true"
)

run_downloads() {
  mkdir -p "$MODEL_DIR"
  IFS=',' read -r -a keys <<< "$MODELS"
  for raw in "${keys[@]}"; do
    key="$(echo "$raw" | tr '[:upper:]' '[:lower:]' | xargs)"
    file="${FILE_BY_KEY[$key]:-}"
    url="${URL_BY_KEY[$key]:-}"
    if [[ -z "$file" || -z "$url" ]]; then
      echo "[skip] unknown model key: $raw (valid: 0.8b,2b,4b,9b)"
      continue
    fi
    out="$MODEL_DIR/$file"
    tmp="$out.tmp"
    if [[ -s "$out" ]]; then
      echo "[ok] already present: $out"
      continue
    fi
    echo "[download] $key -> $out"
    # Resume partial downloads if tmp exists.
    curl -fL --retry 5 --retry-delay 2 -C - -o "$tmp" "$url"
    mv "$tmp" "$out"
    echo "[done] $out"
  done
}

if [[ "${1:-}" == "--background" ]]; then
  mkdir -p "$LOG_DIR"
  if command -v systemd-run >/dev/null 2>&1; then
    systemctl --user stop "$UNIT_NAME" >/dev/null 2>&1 || true
    systemd-run --user \
      --unit "$UNIT_NAME" \
      --working-directory "$REPO_ROOT" \
      --collect \
      /usr/bin/env \
        TABURA_LLM_MODEL_DIR="$MODEL_DIR" \
        QWEN35_SMALL_MODELS="$MODELS" \
        bash "$0" >"$LOG_FILE" 2>&1
    echo "Started background download via systemd unit=$UNIT_NAME log=$LOG_FILE"
    echo "Follow progress: journalctl --user -u $UNIT_NAME -f"
  else
    nohup bash "$0" >"$LOG_FILE" 2>&1 &
    echo "Started background download. PID=$! log=$LOG_FILE"
  fi
  exit 0
fi

run_downloads
