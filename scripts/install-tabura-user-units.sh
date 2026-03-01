#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT_SRC="$REPO_ROOT/deploy/systemd/user"
UNIT_DST="$HOME/.config/systemd/user"

log() { printf '[tabura-units] %s\n' "$*"; }
fail() { printf '[tabura-units] ERROR: %s\n' "$*" >&2; exit 1; }

# --- Verify prerequisites ---

command -v llama-server >/dev/null 2>&1 || fail "llama-server not in PATH. Build llama.cpp and install to ~/.local/bin"
command -v voxtype >/dev/null 2>&1 || fail "voxtype not in PATH. Install voxtype"
command -v codex >/dev/null 2>&1 || fail "codex not in PATH. Install @openai/codex"

# --- Bootstrap service dependencies ---

log "Setting up intent classifier"
"$REPO_ROOT/scripts/setup-intent-classifier.sh"

log "Ensuring LLM model is downloaded"
MODEL_DIR="${HOME}/.local/share/tabura-llm/models"
MODEL_FILE="Qwen3-0.6B-Q4_K_M.gguf"
MODEL_URL="https://huggingface.co/lmstudio-community/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q4_K_M.gguf?download=true"
mkdir -p "$MODEL_DIR"
if [ ! -s "${MODEL_DIR}/${MODEL_FILE}" ]; then
  curl -fL --retry 3 --retry-delay 2 -o "${MODEL_DIR}/${MODEL_FILE}.tmp" "$MODEL_URL"
  mv "${MODEL_DIR}/${MODEL_FILE}.tmp" "${MODEL_DIR}/${MODEL_FILE}"
fi

# --- Install unit files ---

mkdir -p "$UNIT_DST"
cp "$UNIT_SRC"/*.service "$UNIT_DST"/
systemctl --user daemon-reload

# --- Disable legacy units ---

systemctl --user disable --now \
  tabura-dev-watch.path \
  tabura-mcp.service \
  tabura-voxtype-mcp.service \
  helpy-mcp.service \
  voxtype.service \
  >/dev/null 2>&1 || true

# --- Enable and start all services ---

units=(
  tabura-codex-app-server.service
  tabura-piper-tts.service
  tabura-stt.service
  tabura-intent.service
  tabura-llm.service
  tabura-ptt.service
  tabura-web.service
)

systemctl --user enable --now "${units[@]}"
log "Enabled: ${units[*]}"

# --- Verify all services are running ---

sleep 3
failed=()
for unit in "${units[@]}"; do
  if ! systemctl --user is-active "$unit" >/dev/null 2>&1; then
    failed+=("$unit")
  fi
done

if ((${#failed[@]} > 0)); then
  log "FAILED services: ${failed[*]}"
  for unit in "${failed[@]}"; do
    systemctl --user status "$unit" --no-pager -n 10 2>&1 || true
  done
  fail "Not all services started"
fi

log "All services running"
