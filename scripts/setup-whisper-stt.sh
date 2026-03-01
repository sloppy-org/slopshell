#!/usr/bin/env bash
set -euo pipefail

VOXTYPE_BIN="${VOXTYPE_BIN:-voxtype}"
HOST="${TABURA_STT_HOST:-127.0.0.1}"
PORT="${TABURA_STT_PORT:-8427}"
LANGUAGE_RAW="${TABURA_STT_LANGUAGE:-en,de}"
THREADS="${TABURA_STT_THREADS:-4}"
PROMPT="${TABURA_STT_PROMPT:-}"
MODEL="${TABURA_STT_MODEL:-large-v3-turbo}"

if ! command -v "$VOXTYPE_BIN" >/dev/null 2>&1; then
  echo "voxtype binary not found: $VOXTYPE_BIN" >&2
  echo "Install voxtype and ensure it is in PATH (or set VOXTYPE_BIN)." >&2
  exit 1
fi

LANGUAGE_CSV="$(printf '%s' "$LANGUAGE_RAW" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
if [ -z "$LANGUAGE_CSV" ]; then
  LANGUAGE_CSV="en,de"
fi
PRIMARY_LANGUAGE="${LANGUAGE_CSV%%,*}"
LANGUAGE_MODE="$PRIMARY_LANGUAGE"
if [[ "$LANGUAGE_CSV" == *,* ]]; then
  LANGUAGE_MODE="auto"
fi

echo "Starting voxtype STT service at http://$HOST:$PORT (languages=$LANGUAGE_CSV model=$MODEL)"

export VOXTYPE_SERVICE_ENABLED=true
export VOXTYPE_SERVICE_HOST="$HOST"
export VOXTYPE_SERVICE_PORT="$PORT"
export VOXTYPE_SERVICE_ALLOWED_LANGUAGES="$LANGUAGE_CSV"
export VOXTYPE_LANGUAGE="$LANGUAGE_MODE"
export VOXTYPE_MODEL="$MODEL"
export VOXTYPE_THREADS="$THREADS"
export VOXTYPE_HOTKEY_ENABLED=false

args=(
  --service
  --service-host "$HOST"
  --service-port "$PORT"
  --no-hotkey
  --model "$MODEL"
  --language "$LANGUAGE_MODE"
  --threads "$THREADS"
)
if [ -n "$PROMPT" ]; then
  args+=(--initial-prompt "$PROMPT")
fi

exec "$VOXTYPE_BIN" "${args[@]}"
