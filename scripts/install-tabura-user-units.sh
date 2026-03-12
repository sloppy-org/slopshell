#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLATFORM="$(uname -s)"

log() { printf '[tabura-units] %s\n' "$*"; }
fail() { printf '[tabura-units] ERROR: %s\n' "$*" >&2; exit 1; }

detect_llama_server() {
    local port url
    for port in 8080 8081 8426; do
        url="http://127.0.0.1:${port}"
        if curl -fsS --max-time 2 "${url}/health" >/dev/null 2>&1; then
            printf '%s' "$url"
            return 0
        fi
    done
    return 1
}

confirm_default_yes() {
    local prompt="$1"
    if [ ! -t 0 ]; then return 0; fi
    local response
    read -r -p "$prompt [Y/n] " response
    case "$response" in
        "" | [Yy] | [Yy][Ee][Ss]) return 0 ;;
        *) return 1 ;;
    esac
}

REUSE_LLM_URL=""

# --- Platform detection ---

case "$PLATFORM" in
  Linux)  ;;
  Darwin) ;;
  *)      fail "unsupported platform: $PLATFORM" ;;
esac

# --- Resolve data paths ---

if [ "$PLATFORM" = "Darwin" ]; then
  LLM_MODEL_DIR="${HOME}/Library/Application Support/tabura/llm/models"
else
  LLM_MODEL_DIR="${HOME}/.local/share/tabura-llm/models"
fi

# --- Detect existing llama-server ---

if [ -n "${TABURA_INTENT_LLM_URL:-}" ]; then
  REUSE_LLM_URL="$TABURA_INTENT_LLM_URL"
  log "TABURA_INTENT_LLM_URL set to ${REUSE_LLM_URL}; skipping LLM setup"
elif existing_url="$(detect_llama_server)"; then
  log "Existing llama-server detected at ${existing_url}"
  if confirm_default_yes "Reuse existing llama-server at ${existing_url}?"; then
    REUSE_LLM_URL="$existing_url"
    log "TABURA_INTENT_LLM_URL will point to ${REUSE_LLM_URL}"
  fi
fi

# --- Verify prerequisites ---

HAVE_LLAMA=1
HAVE_VOXTYPE=1

if ! command -v codex >/dev/null 2>&1; then
  if [ "$PLATFORM" = "Darwin" ]; then
    fail "codex not in PATH. Install: npm install -g @openai/codex"
  else
    fail "codex not in PATH. Install @openai/codex"
  fi
fi

if [ -n "$REUSE_LLM_URL" ]; then
  HAVE_LLAMA=0
elif ! command -v llama-server >/dev/null 2>&1; then
  HAVE_LLAMA=0
  if [ "$PLATFORM" = "Darwin" ]; then
    log "WARNING: llama-server not in PATH. Install: brew install llama.cpp"
  else
    fail "llama-server not in PATH. Build llama.cpp and install to ~/.local/bin"
  fi
fi

if ! command -v voxtype >/dev/null 2>&1; then
  HAVE_VOXTYPE=0
  if [ "$PLATFORM" = "Darwin" ]; then
    log "WARNING: voxtype not in PATH. Install: brew install voxtype"
  else
    fail "voxtype not in PATH. Install voxtype"
  fi
fi

if [ "$PLATFORM" = "Darwin" ]; then
  command -v go >/dev/null 2>&1 || fail "go not in PATH. Install: brew install go"
fi

# --- Bootstrap service dependencies ---

if [ "$HAVE_LLAMA" = "1" ] && [ -z "$REUSE_LLM_URL" ]; then
  log "Ensuring LLM model is downloaded"
  MODEL_FILE="Qwen3.5-9B-Q4_K_M.gguf"
  MODEL_URL="https://huggingface.co/lmstudio-community/Qwen3.5-9B-GGUF/resolve/main/Qwen3.5-9B-Q4_K_M.gguf?download=true"
  mkdir -p "$LLM_MODEL_DIR"
  if [ ! -s "${LLM_MODEL_DIR}/${MODEL_FILE}" ]; then
    curl -fL --retry 3 --retry-delay 2 -o "${LLM_MODEL_DIR}/${MODEL_FILE}.tmp" "$MODEL_URL"
    mv "${LLM_MODEL_DIR}/${MODEL_FILE}.tmp" "${LLM_MODEL_DIR}/${MODEL_FILE}"
  fi
fi

# --- Linux: systemd install ---

install_linux() {
  local unit_src="$REPO_ROOT/deploy/systemd/user"
  local unit_dst="$HOME/.config/systemd/user"
  local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8426}"

  mkdir -p "$unit_dst"
  for f in "$unit_src"/*.service; do
    local base
    base="$(basename "$f")"
    if [ "$base" = "tabura-llm.service" ] && [ -n "$REUSE_LLM_URL" ]; then
      continue
    fi
    sed -e "s|@@REPO_ROOT@@|${REPO_ROOT}|g" \
        -e "s|@@TABURA_INTENT_LLM_URL@@|${effective_llm_url}|g" \
        "$f" > "$unit_dst/$base"
  done
  systemctl --user daemon-reload

  # Disable legacy units
  systemctl --user disable --now \
    tabura-dev-watch.path \
    tabura-mcp.service \
    tabura-voxtype-mcp.service \
    helpy-mcp.service \
    voxtype.service \
    >/dev/null 2>&1 || true

  # Enable and start all services
  local units=(
    tabura-codex-app-server.service
    tabura-piper-tts.service
    tabura-stt.service
    tabura-ptt.service
    tabura-web.service
  )
  if [ -z "$REUSE_LLM_URL" ]; then
    units+=(tabura-llm.service)
  fi

  systemctl --user enable --now "${units[@]}"
  log "Enabled: ${units[*]}"

  # Verify all services are running
  sleep 3
  local failed=()
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
}

# --- macOS: launchd install ---

install_macos() {
  local plist_src="$REPO_ROOT/deploy/launchd"
  local plist_dst="$HOME/Library/LaunchAgents"
  local data_root="$HOME/Library/Application Support/tabura"
  local bin_path codex_path web_data_dir piper_model_dir piper_venv_dir
  local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8426}"

  [ -d "$plist_src" ] || fail "launchd templates not found: $plist_src"

  # Build Go binary for dev use
  log "Building tabura binary"
  if ! (cd "$REPO_ROOT" && go build -o "$REPO_ROOT/tabura" ./cmd/tabura); then
    fail "go build failed"
  fi

  bin_path="$REPO_ROOT/tabura"
  codex_path="$(command -v codex)"
  web_data_dir="${data_root}/web-data"
  # Use the same paths as setup-tabura-piper-tts.sh so models are found
  piper_model_dir="${HOME}/.local/share/tabura-piper-tts/models"
  piper_venv_dir="${HOME}/.local/share/tabura-piper-tts/venv"

  mkdir -p "$plist_dst" "$web_data_dir"

  # Determine which agents to install
  local agents=(codex-app-server piper-tts web)
  if [ "$HAVE_LLAMA" = "1" ] && [ -z "$REUSE_LLM_URL" ]; then
    agents+=(llm)
  fi
  if [ "$HAVE_VOXTYPE" = "1" ]; then
    agents+=(stt)
  fi

  # PTT requires Linux evdev — skip on macOS
  log "Skipping tabura-ptt: push-to-talk requires Linux (evdev)"

  # Install and load agents
  local src dst
  for name in "${agents[@]}"; do
    src="$plist_src/io.tabura.${name}.plist"
    dst="$plist_dst/io.tabura.${name}.plist"
    if [ ! -f "$src" ]; then
      log "WARNING: template missing: $src"
      continue
    fi
    sed \
      -e "s|@@BIN_PATH@@|${bin_path}|g" \
      -e "s|@@CODEX_PATH@@|${codex_path}|g" \
      -e "s|@@PROJECT_DIR@@|${REPO_ROOT}|g" \
      -e "s|@@WEB_DATA_DIR@@|${web_data_dir}|g" \
      -e "s|@@VENV_DIR@@|${piper_venv_dir}|g" \
      -e "s|@@SCRIPT_DIR@@|${REPO_ROOT}/scripts|g" \
      -e "s|@@PIPER_MODEL_DIR@@|${piper_model_dir}|g" \
      -e "s|@@LLM_SETUP_SCRIPT@@|${REPO_ROOT}/scripts/setup-local-llm.sh|g" \
      -e "s|@@LLM_MODEL_DIR@@|${LLM_MODEL_DIR}|g" \
      -e "s|@@STT_SETUP_SCRIPT@@|${REPO_ROOT}/scripts/setup-voxtype-stt.sh|g" \
      -e "s|@@TABURA_INTENT_LLM_URL@@|${effective_llm_url}|g" \
      "$src" > "$dst"
    launchctl unload "$dst" >/dev/null 2>&1 || true
    launchctl load -w "$dst"
    log "Loaded: io.tabura.${name}"
  done

  # Verify agents are loaded
  sleep 3
  local failed=()
  local label
  for name in "${agents[@]}"; do
    label="io.tabura.${name}"
    if ! launchctl list "$label" >/dev/null 2>&1; then
      failed+=("$label")
    fi
  done

  if ((${#failed[@]} > 0)); then
    log "FAILED agents: ${failed[*]}"
    fail "Not all agents started"
  fi

  log "All agents running"
}

# --- Main ---

if [ "$PLATFORM" = "Darwin" ]; then
  install_macos
else
  install_linux
fi
