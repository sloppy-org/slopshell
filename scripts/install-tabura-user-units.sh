#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLATFORM="$(uname -s)"
# shellcheck source=scripts/lib/llama.sh
source "${REPO_ROOT}/scripts/lib/llama.sh"

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
LLAMA_SERVER_BIN_RESOLVED=""

configure_codex_cli() {
  local fast_url agentic_url
  if [ -n "$REUSE_LLM_URL" ]; then
    fast_url="${REUSE_LLM_URL}/v1"
    agentic_url="${REUSE_LLM_URL}/v1"
  else
    fast_url="http://127.0.0.1:8426/v1"
    agentic_url="http://127.0.0.1:8430/v1"
  fi

  TABURA_CODEX_FAST_URL="$fast_url" \
  TABURA_CODEX_AGENTIC_URL="$agentic_url" \
  "$REPO_ROOT/scripts/setup-codex-mcp.sh" "http://127.0.0.1:9420/mcp"
}

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
elif LLAMA_SERVER_BIN_RESOLVED="$(tabura_find_llama_server)"; then
  HAVE_LLAMA=1
else
  HAVE_LLAMA=0
  if [ "$PLATFORM" = "Darwin" ]; then
    if [ -n "${TABURA_LLAMA_LAST_ERROR:-}" ]; then
      log "WARNING: llama-server not usable (${TABURA_LLAMA_LAST_ERROR}). Install: brew install llama.cpp"
    else
      log "WARNING: llama-server not found. Install: brew install llama.cpp"
    fi
  else
    if [ -n "${TABURA_LLAMA_LAST_ERROR:-}" ]; then
      fail "llama-server not usable: ${TABURA_LLAMA_LAST_ERROR}"
    fi
    fail "llama-server not found. Build llama.cpp and install to ~/.local/bin"
  fi
fi

if ! command -v voxtype >/dev/null 2>&1; then
  HAVE_VOXTYPE=0
  if [ "$PLATFORM" = "Darwin" ]; then
    log "WARNING: voxtype not in PATH. Build from source: scripts/build-voxtype-macos.sh"
  else
    fail "voxtype not in PATH. Install voxtype"
  fi
fi

if [ "$PLATFORM" = "Darwin" ]; then
  command -v go >/dev/null 2>&1 || fail "go not in PATH. Install: brew install go"
fi

# --- Linux: systemd install ---

install_linux() {
  local unit_src="$REPO_ROOT/deploy/systemd/user"
  local unit_dst="$HOME/.config/systemd/user"
  local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8426}"
  local web_host="${TABURA_WEB_HOST:-127.0.0.1}"
  local -a core_units=(
    tabura-codex-app-server.service
    tabura-piper-tts.service
    tabura-stt.service
    tabura-web.service
  )
  local -a optional_units=(
    tabura-ptt.service
  )

  mkdir -p "$unit_dst"
  for f in "$unit_src"/*.service; do
    local base
    base="$(basename "$f")"
    if { [ "$base" = "tabura-llm.service" ] || [ "$base" = "tabura-codex-llm.service" ]; } && [ -n "$REUSE_LLM_URL" ]; then
      continue
    fi
    sed -e "s|@@REPO_ROOT@@|${REPO_ROOT}|g" \
        -e "s|@@LLAMA_SERVER_BIN@@|${LLAMA_SERVER_BIN_RESOLVED}|g" \
        -e "s|@@TABURA_WEB_HOST@@|${web_host}|g" \
        -e "s|@@TABURA_INTENT_LLM_URL@@|${effective_llm_url}|g" \
        "$f" > "$unit_dst/$base"
  done
  if [ -n "$REUSE_LLM_URL" ]; then
    rm -f "$unit_dst/tabura-llm.service" "$unit_dst/tabura-codex-llm.service"
  fi
  systemctl --user daemon-reload

  # Disable legacy units
  systemctl --user disable --now \
    tabura-dev-watch.path \
    tabura-mcp.service \
    tabura-voxtype-mcp.service \
    helpy-mcp.service \
    voxtype.service \
    >/dev/null 2>&1 || true
  if [ -n "$REUSE_LLM_URL" ]; then
    systemctl --user disable --now tabura-llm.service tabura-codex-llm.service >/dev/null 2>&1 || true
  fi

  # Enable and start all services
  local units=("${core_units[@]}" "${optional_units[@]}")
  if [ -z "$REUSE_LLM_URL" ]; then
    units+=(tabura-llm.service)
    core_units+=(tabura-llm.service)
    units+=(tabura-codex-llm.service)
    optional_units+=(tabura-codex-llm.service)
  fi

  systemctl --user enable --now "${units[@]}"
  log "Enabled: ${units[*]}"

  # Verify all core services are running. Optional helpers are best-effort.
  sleep 3
  local failed=()
  local optional_failed=()
  local unit
  for unit in "${core_units[@]}"; do
    if ! systemctl --user is-active "$unit" >/dev/null 2>&1; then
      failed+=("$unit")
    fi
  done

  for unit in "${optional_units[@]}"; do
    if ! systemctl --user is-active "$unit" >/dev/null 2>&1; then
      optional_failed+=("$unit")
    fi
  done

  if ((${#optional_failed[@]} > 0)); then
    log "Optional services inactive: ${optional_failed[*]}"
    for unit in "${optional_failed[@]}"; do
      systemctl --user status "$unit" --no-pager -n 10 2>&1 || true
    done
  fi

  if ((${#failed[@]} > 0)); then
    log "FAILED services: ${failed[*]}"
    for unit in "${failed[@]}"; do
      systemctl --user status "$unit" --no-pager -n 10 2>&1 || true
    done
    fail "Not all services started"
  fi

  log "All services running"
}

# --- macOS: launchd helpers ---

launchctl_available() {
  local probe="/tmp/tabura-launchctl-probe.plist"
  cat > "$probe" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>io.tabura.probe</string>
  <key>ProgramArguments</key>
  <array><string>/usr/bin/true</string></array>
</dict>
</plist>
PLIST
  if launchctl load "$probe" >/dev/null 2>&1; then
    launchctl unload "$probe" >/dev/null 2>&1 || true
    rm -f "$probe"
    return 0
  fi
  rm -f "$probe"
  return 1
}

# --- macOS: launchd install ---

install_macos() {
  local plist_src="$REPO_ROOT/deploy/launchd"
  local plist_dst="$HOME/Library/LaunchAgents"
  local data_root="$HOME/Library/Application Support/tabura"
  local bin_path codex_path web_data_dir piper_model_dir piper_venv_dir
  local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8426}"
  local web_host="${TABURA_WEB_HOST:-127.0.0.1}"

  [ -d "$plist_src" ] || fail "launchd templates not found: $plist_src"

  # Build Go binary for dev use
  log "Building tabura binary"
  if ! (cd "$REPO_ROOT" && go build -o "$REPO_ROOT/tabura" ./cmd/tabura); then
    fail "go build failed"
  fi

  bin_path="$REPO_ROOT/tabura"
  codex_path="$(command -v codex)"
  web_data_dir="${data_root}/web-data"
  piper_model_dir="${HOME}/.local/share/tabura-piper-tts/models"
  piper_venv_dir="${HOME}/.local/share/tabura-piper-tts/venv"

  mkdir -p "$plist_dst" "$web_data_dir"
  if [ -n "$REUSE_LLM_URL" ]; then
    launchctl unload "$plist_dst/io.tabura.llm.plist" >/dev/null 2>&1 || true
    launchctl unload "$plist_dst/io.tabura.codex-llm.plist" >/dev/null 2>&1 || true
    rm -f "$plist_dst/io.tabura.llm.plist" "$plist_dst/io.tabura.codex-llm.plist"
  fi

  # Determine which agents to install
  local agents=(codex-app-server piper-tts web)
  if [ "$HAVE_LLAMA" = "1" ] && [ -z "$REUSE_LLM_URL" ]; then
    agents+=(llm)
    agents+=(codex-llm)
  fi
  if [ "$HAVE_VOXTYPE" = "1" ]; then
    agents+=(stt)
  fi

  # PTT requires Linux evdev — skip on macOS
  log "Skipping tabura-ptt: push-to-talk requires Linux (evdev)"

  # Install plist files (always, even if launchctl is unavailable)
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
      -e "s|@@TABURA_WEB_HOST@@|${web_host}|g" \
      -e "s|@@VENV_DIR@@|${piper_venv_dir}|g" \
      -e "s|@@SCRIPT_DIR@@|${REPO_ROOT}/scripts|g" \
      -e "s|@@PIPER_MODEL_DIR@@|${piper_model_dir}|g" \
      -e "s|@@LLM_SETUP_SCRIPT@@|${REPO_ROOT}/scripts/setup-local-llm.sh|g" \
      -e "s|@@LLM_MODEL_DIR@@|${LLM_MODEL_DIR}|g" \
      -e "s|@@LLAMA_SERVER_BIN@@|${LLAMA_SERVER_BIN_RESOLVED}|g" \
      -e "s|@@STT_SETUP_SCRIPT@@|${REPO_ROOT}/scripts/setup-voxtype-stt.sh|g" \
      -e "s|@@TABURA_INTENT_LLM_URL@@|${effective_llm_url}|g" \
      "$src" > "$dst"
    log "Installed plist: $dst"
  done

  # Activate services
  if launchctl_available; then
    activate_launchd "${agents[@]}"
  else
    log "launchctl unavailable (SSH/tmux session); starting services directly"
    activate_direct "${agents[@]}"
  fi
}

activate_launchd() {
  local plist_dst="$HOME/Library/LaunchAgents"
  local dst
  for name in "$@"; do
    dst="$plist_dst/io.tabura.${name}.plist"
    launchctl unload "$dst" >/dev/null 2>&1 || true
    launchctl load -w "$dst"
    log "Loaded: io.tabura.${name}"
  done

  sleep 3
  local failed=()
  for name in "$@"; do
    if ! launchctl list "io.tabura.${name}" >/dev/null 2>&1; then
      failed+=("io.tabura.${name}")
    fi
  done

  if ((${#failed[@]} > 0)); then
    log "FAILED agents: ${failed[*]}"
    fail "Not all agents started"
  fi

  log "All agents running (launchd)"
}

activate_direct() {
  local pidfile="/tmp/tabura-pids.txt"
  local web_host="${TABURA_WEB_HOST:-127.0.0.1}"
  : > "$pidfile"

  for name in "$@"; do
    local logfile="/tmp/tabura-${name}.log"
    case "$name" in
      codex-app-server)
        nohup "$codex_path" app-server --listen ws://127.0.0.1:8787 \
          >"$logfile" 2>&1 &
        ;;
      piper-tts)
        PIPER_MODEL_DIR="$piper_model_dir" \
        nohup "$piper_venv_dir/bin/uvicorn" piper_tts_server:app \
          --app-dir "$REPO_ROOT/scripts" --host 127.0.0.1 --port 8424 \
          >"$logfile" 2>&1 &
        ;;
      web)
        TABURA_INTENT_LLM_URL="$effective_llm_url" \
        TABURA_INTENT_LLM_MODEL=local \
        TABURA_INTENT_LLM_PROFILE=qwen3.5-9b \
        TABURA_INTENT_LLM_PROFILE_OPTIONS=qwen3.5-9b,qwen3.5-4b \
        nohup "$bin_path" server \
          --project-dir "$REPO_ROOT" --data-dir "$web_data_dir" \
          --web-host "$web_host" --web-port 8420 \
          --mcp-host 127.0.0.1 --mcp-port 9420 \
          --app-server-url ws://127.0.0.1:8787 \
          --tts-url http://127.0.0.1:8424 \
          >"$logfile" 2>&1 &
        ;;
      llm)
        TABURA_LLM_MODEL_DIR="$LLM_MODEL_DIR" \
        LLAMA_SERVER_BIN="$LLAMA_SERVER_BIN_RESOLVED" \
        nohup "$REPO_ROOT/scripts/setup-local-llm.sh" \
          >"$logfile" 2>&1 &
        ;;
      codex-llm)
        TABURA_LLM_PRESET=codex-gpt-oss-120b \
        LLAMA_SERVER_BIN="$LLAMA_SERVER_BIN_RESOLVED" \
        nohup "$REPO_ROOT/scripts/setup-local-llm.sh" \
          >"$logfile" 2>&1 &
        ;;
      stt)
        TABURA_STT_LANGUAGE=de,en TABURA_STT_MODEL=large-v3-turbo \
        nohup "$REPO_ROOT/scripts/setup-voxtype-stt.sh" \
          >"$logfile" 2>&1 &
        ;;
    esac
    echo "$! io.tabura.${name}" >> "$pidfile"
    log "Started: io.tabura.${name} (pid $!)"
  done

  sleep 3
  local failed=()
  local pid label
  while read -r pid label; do
    if ! kill -0 "$pid" 2>/dev/null; then
      failed+=("$label")
      log "FAILED: $label (pid $pid) — see /tmp/tabura-${label#io.tabura.}.log"
    fi
  done < "$pidfile"

  if ((${#failed[@]} > 0)); then
    fail "Not all services started"
  fi

  log "All services running (direct); PIDs in $pidfile"
  log "Stop all: awk '{print \$1}' $pidfile | xargs kill"
}

# --- Main ---

if [ "$PLATFORM" = "Darwin" ]; then
  install_macos
else
  install_linux
fi

configure_codex_cli
