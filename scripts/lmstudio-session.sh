#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
CLI="${SCRIPT_DIR}/lmstudio-cli.sh"
HOST="${TABURA_LMSTUDIO_HOST:-127.0.0.1}"
PORT="${TABURA_LMSTUDIO_PORT:-1234}"
MODEL_KEY="${TABURA_LMSTUDIO_MODEL_KEY:-qwen/qwen3.5-9b}"
GPU="${TABURA_LMSTUDIO_GPU:-max}"
CONTEXT_LENGTH="${TABURA_LMSTUDIO_CONTEXT_LENGTH:-4096}"
PARALLEL="${TABURA_LMSTUDIO_PARALLEL:-1}"
MODEL_IDENTIFIER="${TABURA_LMSTUDIO_MODEL_IDENTIFIER:-}"

"${CLI}" server start --bind "${HOST}" --port "${PORT}" >/tmp/tabura-lmstudio-server.log 2>&1 || true

if "${CLI}" ls "${MODEL_KEY}" --json >/tmp/tabura-lmstudio-models.json 2>&1; then
    load_args=(
        "${CLI}" load "${MODEL_KEY}"
        --gpu "${GPU}"
        -c "${CONTEXT_LENGTH}"
        --parallel "${PARALLEL}"
        -y
    )
    if [ -n "${MODEL_IDENTIFIER}" ]; then
        load_args+=(--identifier "${MODEL_IDENTIFIER}")
    fi
    "${load_args[@]}" >/tmp/tabura-lmstudio-load.log 2>&1 || true
fi
