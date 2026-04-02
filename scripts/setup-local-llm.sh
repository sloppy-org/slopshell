#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLATFORM="$(uname -s)"

if [ "$PLATFORM" != "Darwin" ]; then
  # shellcheck source=lib/llama.sh
  source "${SCRIPT_DIR}/lib/llama.sh"
fi
# shellcheck source=lib/python.sh
source "${SCRIPT_DIR}/lib/python.sh"

default_if_empty() {
  local value="$1"
  local fallback="$2"
  if [ -n "$value" ]; then
    printf '%s' "$value"
    return
  fi
  printf '%s' "$fallback"
}

build_vllm_mlx_install_spec() {
  if [ -n "${TABURA_VLLM_MLX_SOURCE_DIR:-}" ]; then
    local source_dir="${TABURA_VLLM_MLX_SOURCE_DIR}"
    local source_head=""
    if [ ! -d "$source_dir" ] || ! source_head="$(git -C "$source_dir" rev-parse HEAD 2>/dev/null)"; then
      echo "TABURA_VLLM_MLX_SOURCE_DIR is not a valid git checkout: ${source_dir}" >&2
      exit 1
    fi
    printf '%s@%s' "$source_dir" "$source_head"
    return
  fi
  if [ -n "${TABURA_VLLM_MLX_INSTALL_SPEC:-}" ]; then
    printf '%s' "$TABURA_VLLM_MLX_INSTALL_SPEC"
    return
  fi
  local git_url="${TABURA_VLLM_MLX_GIT_URL:-git+ssh://git@github.com/computor-org/vllm-mlx.git}"
  local git_ref="${TABURA_VLLM_MLX_GIT_REF:-19d41cd093fcb0f5cb474d049147f3e119818214}"
  if [ -n "$git_ref" ]; then
    printf '%s@%s' "$git_url" "$git_ref"
    return
  fi
  printf '%s' "$git_url"
}

build_vllm_mlx_pip_target() {
  if [ -n "${TABURA_VLLM_MLX_SOURCE_DIR:-}" ]; then
    printf '%s' "${TABURA_VLLM_MLX_SOURCE_DIR}"
    return
  fi
  printf '%s' "$1"
}

ensure_vllm_mlx_install() {
  local install_spec="$1"
  local pip_target="$2"
  local python_bin="$3"
  local marker_path="${VENV_DIR}/.tabura-vllm-mlx-install-spec"
  if [ -x "${VENV_DIR}/bin/python" ] && ! tabura_python_meets_min_version "${VENV_DIR}/bin/python" 3 10; then
    rm -rf "$VENV_DIR"
  fi
  if [ ! -x "${VENV_DIR}/bin/python" ]; then
    mkdir -p "$(dirname "$VENV_DIR")"
    "$python_bin" -m venv "$VENV_DIR"
  fi
  if [ ! -x "${VENV_DIR}/bin/vllm-mlx" ] || [ ! -f "$marker_path" ] || [ "$(cat "$marker_path" 2>/dev/null)" != "$install_spec" ]; then
    "${VENV_DIR}/bin/python" -m pip install --upgrade pip setuptools wheel >/dev/null
    "${VENV_DIR}/bin/python" -m pip uninstall -y vllm-mlx >/dev/null 2>&1 || true
    "${VENV_DIR}/bin/python" -m pip install --upgrade --force-reinstall --no-cache-dir --no-deps "$pip_target" >/dev/null
    printf '%s' "$install_spec" >"$marker_path"
  fi
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
VENV_DIR="${TABURA_LLM_VENV_DIR:-}"
VLLM_MLX_MODEL_REPO="${TABURA_MLX_MODEL_REPO:-}"
VLLM_MLX_ENABLE_BATCHING="${TABURA_VLLM_MLX_ENABLE_BATCHING:-}"
VLLM_MLX_USE_PAGED_CACHE="${TABURA_VLLM_MLX_USE_PAGED_CACHE:-}"
VLLM_MLX_CACHE_MEMORY_PERCENT="${TABURA_VLLM_MLX_CACHE_MEMORY_PERCENT:-}"
VLLM_MLX_REASONING_PARSER="${TABURA_VLLM_MLX_REASONING_PARSER:-}"
VLLM_MLX_CHUNKED_PREFILL_TOKENS="${TABURA_VLLM_MLX_CHUNKED_PREFILL_TOKENS:-${TABURA_VLLM_MLX_PREFILL_STEP_SIZE:-}}"
VLLM_MLX_MAX_TOKENS="${TABURA_VLLM_MLX_MAX_TOKENS:-}"

if [ "$PLATFORM" = "Darwin" ]; then
  PYTHON_BIN="$(tabura_find_python3 3 10 || true)"
  VENV_DIR="$(default_if_empty "$VENV_DIR" "$HOME/Library/Application Support/tabura/llm/venv")"
  case "$PROFILE_PRESET" in
    "" | "fast-qwen9b" | "codex-gpt-oss-120b")
      VLLM_MLX_MODEL_REPO="$(default_if_empty "$VLLM_MLX_MODEL_REPO" "mlx-community/Qwen3.5-9B-4bit")"
      ALIAS="$(default_if_empty "$ALIAS" "qwen3.5-9b")"
      HOST="$(default_if_empty "$HOST" "127.0.0.1")"
      PORT="$(default_if_empty "$PORT" "8081")"
      VLLM_MLX_ENABLE_BATCHING="$(default_if_empty "$VLLM_MLX_ENABLE_BATCHING" "1")"
      VLLM_MLX_USE_PAGED_CACHE="$(default_if_empty "$VLLM_MLX_USE_PAGED_CACHE" "1")"
      VLLM_MLX_CACHE_MEMORY_PERCENT="$(default_if_empty "$VLLM_MLX_CACHE_MEMORY_PERCENT" "0.20")"
      VLLM_MLX_REASONING_PARSER="$(default_if_empty "$VLLM_MLX_REASONING_PARSER" "qwen3")"
      VLLM_MLX_CHUNKED_PREFILL_TOKENS="$(default_if_empty "$VLLM_MLX_CHUNKED_PREFILL_TOKENS" "2048")"
      VLLM_MLX_MAX_TOKENS="$(default_if_empty "$VLLM_MLX_MAX_TOKENS" "32768")"
      ;;
    *)
      echo "Unknown TABURA_LLM_PRESET on macOS: ${PROFILE_PRESET}" >&2
      exit 1
      ;;
  esac

  if curl -fsS --max-time 2 "http://${HOST}:${PORT}/health" >/dev/null 2>&1; then
    echo "vllm-mlx already running at http://${HOST}:${PORT}; exiting"
    exit 0
  fi

  if [ -z "$PYTHON_BIN" ]; then
    echo "python3 3.10+ is required to start vllm-mlx." >&2
    exit 1
  fi

  install_spec="$(build_vllm_mlx_install_spec)"
  pip_target="$(build_vllm_mlx_pip_target "$install_spec")"
  ensure_vllm_mlx_install "$install_spec" "$pip_target" "$PYTHON_BIN"

  echo "Starting local vllm-mlx runtime at http://$HOST:$PORT"
  args=(
    serve "$VLLM_MLX_MODEL_REPO"
    --host "$HOST"
    --port "$PORT"
    --served-model-name "$ALIAS"
    --max-tokens "$VLLM_MLX_MAX_TOKENS"
    --chunked-prefill-tokens "$VLLM_MLX_CHUNKED_PREFILL_TOKENS"
    --reasoning-parser "$VLLM_MLX_REASONING_PARSER"
  )
  if [ "$VLLM_MLX_ENABLE_BATCHING" = "1" ]; then
    args+=(--continuous-batching)
  fi
  if [ "$VLLM_MLX_USE_PAGED_CACHE" = "1" ]; then
    args+=(--use-paged-cache)
  fi
  if [ -n "$VLLM_MLX_CACHE_MEMORY_PERCENT" ]; then
    args+=(--cache-memory-percent "$VLLM_MLX_CACHE_MEMORY_PERCENT")
  fi
  exec "${VENV_DIR}/bin/vllm-mlx" "${args[@]}"
fi

case "$PROFILE_PRESET" in
  "" | "fast-qwen9b")
    MODEL_FILE="$(default_if_empty "$MODEL_FILE" "Qwen3.5-9B-Q4_K_M.gguf")"
    MODEL_URL="$(default_if_empty "$MODEL_URL" "https://huggingface.co/lmstudio-community/Qwen3.5-9B-GGUF/resolve/main/Qwen3.5-9B-Q4_K_M.gguf?download=true")"
    THREADS="$(default_if_empty "$THREADS" "4")"
    CTX_SIZE="$(default_if_empty "$CTX_SIZE" "65536")"
    HOST="$(default_if_empty "$HOST" "127.0.0.1")"
    PORT="$(default_if_empty "$PORT" "8081")"
    NGL="$(default_if_empty "$NGL" "99")"
    PARALLEL="$(default_if_empty "$PARALLEL" "1")"
    ALIAS="$(default_if_empty "$ALIAS" "qwen3.5-9b")"
    REASONING_BUDGET="$(default_if_empty "$REASONING_BUDGET" "-1")"
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
  --jinja
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
