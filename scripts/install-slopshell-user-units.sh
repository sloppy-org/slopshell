#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLATFORM="$(uname -s)"
# shellcheck source=scripts/lib/llama.sh
source "${REPO_ROOT}/scripts/lib/llama.sh"
# shellcheck source=scripts/lib/python.sh
source "${REPO_ROOT}/scripts/lib/python.sh"

log() { printf '[slopshell-units] %s\n' "$*"; }
fail() { printf '[slopshell-units] ERROR: %s\n' "$*" >&2; exit 1; }

detect_llama_server() {
    local port url
    for port in 8081 8080; do
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
LLM_VENV_DIR=""
CODEX_PATH=""
VOXTYPE_PATH=""
BIN_PATH=""
SLOPTOOLS_BIN_PATH=""
SLOPTOOLS_REPO_ROOT="${SLOPTOOLS_REPO_ROOT:-}"
SLOPTOOLS_DATA_DIR=""
WEB_DATA_DIR=""

resolve_sloptools_repo() {
  if [ -n "$SLOPTOOLS_REPO_ROOT" ] && [ -d "$SLOPTOOLS_REPO_ROOT" ]; then
    SLOPTOOLS_REPO_ROOT="$(cd "$SLOPTOOLS_REPO_ROOT" && pwd)"
    return 0
  fi
  if [ -d "${REPO_ROOT}/../sloptools" ]; then
    SLOPTOOLS_REPO_ROOT="$(cd "${REPO_ROOT}/../sloptools" && pwd)"
    return 0
  fi
  fail "sloptools repo not found. Set SLOPTOOLS_REPO_ROOT or place it next to slopshell."
}

build_sloptools_binary() {
  resolve_sloptools_repo
  log "Building sloptools binary"
  if ! (cd "$SLOPTOOLS_REPO_ROOT" && go build -o "$SLOPTOOLS_REPO_ROOT/sloptools" ./cmd/sloptools); then
    fail "go build failed for sloptools"
  fi
  SLOPTOOLS_BIN_PATH="$SLOPTOOLS_REPO_ROOT/sloptools"
}

install_slsh_binary() {
  local slsh_bin_dir slsh_bin_path
  slsh_bin_dir="${SLOPSHELL_BIN_DIR:-${HOME}/.local/bin}"
  slsh_bin_path="${slsh_bin_dir}/slsh"
  log "Building slsh terminal client -> ${slsh_bin_path}"
  mkdir -p "$slsh_bin_dir"
  if ! (cd "$REPO_ROOT" && go build -o "$slsh_bin_path" ./cmd/slsh); then
    fail "go build failed for slsh"
  fi
  if ! printf ':%s:' "$PATH" | grep -Fq ":${slsh_bin_dir}:"; then
    log "${slsh_bin_dir} is not in PATH; add it to your shell profile to use slsh"
  fi
}

configure_codex_cli() {
  local fast_url agentic_url
  if [ -n "$REUSE_LLM_URL" ]; then
    fast_url="${REUSE_LLM_URL}/v1"
    agentic_url="${REUSE_LLM_URL}/v1"
  elif [ "$PLATFORM" = "Darwin" ]; then
    fast_url="http://127.0.0.1:8081/v1"
    agentic_url="http://127.0.0.1:8081/v1"
  else
    fast_url="http://127.0.0.1:8081/v1"
    agentic_url="http://127.0.0.1:8080/v1"
  fi

  SLOPSHELL_CODEX_FAST_URL="$fast_url" \
  SLOPSHELL_CODEX_LOCAL_URL="$agentic_url" \
  SLOPSHELL_CODEX_AGENTIC_URL="$agentic_url" \
  "$REPO_ROOT/scripts/setup-codex-mcp.sh" "http://127.0.0.1:9420/mcp"
}

install_hotword_assets() {
  local script_path="${REPO_ROOT}/scripts/fetch-hotword-assets.sh"
  [ -x "$script_path" ] || fail "hotword asset bootstrap missing: $script_path"
  SLOPSHELL_WEB_DATA_DIR="$WEB_DATA_DIR" "$script_path"
}

ensure_macos_vllm_prereqs() {
  command -v brew >/dev/null 2>&1 || fail "brew not in PATH. Install Homebrew first."
  if ! slopshell_find_python3 3 10 >/dev/null 2>&1; then
    log "Installing python via Homebrew"
    brew install python
  fi
  if ! command -v uv >/dev/null 2>&1; then
    log "Installing uv via Homebrew"
    brew install uv
  fi
}

sync_macos_vllm_source_checkout() {
  local source_dir="$1"
  local remote_url="git@github.com:computor-org/vllm-mlx.git"

  mkdir -p "$(dirname "$source_dir")"
  if [ -d "${source_dir}/.git" ]; then
    git -C "$source_dir" remote set-url origin "$remote_url"
    git -C "$source_dir" fetch origin main --prune
  else
    rm -rf "$source_dir"
    git clone --branch main "$remote_url" "$source_dir"
  fi
  git -C "$source_dir" checkout main
  git -C "$source_dir" reset --hard origin/main
}

# --- Platform detection ---

case "$PLATFORM" in
  Linux)  ;;
  Darwin) ;;
  *)      fail "unsupported platform: $PLATFORM" ;;
esac

# --- Resolve data paths ---

if [ "$PLATFORM" = "Darwin" ]; then
  LLM_MODEL_DIR="${HOME}/Library/Application Support/slopshell/llm/models"
  LLM_VENV_DIR="${HOME}/Library/Application Support/slopshell/llm/venv"
  SLOPTOOLS_DATA_DIR="${HOME}/Library/Application Support/sloptools"
else
  LLM_MODEL_DIR="${HOME}/.local/share/slopshell-llm/models"
  LLM_VENV_DIR="${HOME}/.local/share/slopshell-llm/venv"
  SLOPTOOLS_DATA_DIR="${HOME}/.local/share/sloptools"
fi

# --- Detect existing local LLM ---

if [ -n "${SLOPSHELL_INTENT_LLM_URL:-}" ]; then
  REUSE_LLM_URL="$SLOPSHELL_INTENT_LLM_URL"
  log "SLOPSHELL_INTENT_LLM_URL set to ${REUSE_LLM_URL}; skipping LLM setup"
elif existing_url="$(detect_llama_server)"; then
  log "Existing local LLM detected at ${existing_url}"
  if [ "$PLATFORM" = "Darwin" ] && [ "$existing_url" = "http://127.0.0.1:8081" ]; then
    log "Keeping managed macOS local LLM under launchd control"
  elif confirm_default_yes "Reuse existing local LLM at ${existing_url}?"; then
    REUSE_LLM_URL="$existing_url"
    log "SLOPSHELL_INTENT_LLM_URL will point to ${REUSE_LLM_URL}"
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
elif [ "$PLATFORM" = "Darwin" ]; then
  ensure_macos_vllm_prereqs
  HAVE_LLAMA=1
elif LLAMA_SERVER_BIN_RESOLVED="$(slopshell_find_llama_server)"; then
  HAVE_LLAMA=1
else
  HAVE_LLAMA=0
  if [ -n "${SLOPSHELL_LLAMA_LAST_ERROR:-}" ]; then
    fail "llama-server not usable: ${SLOPSHELL_LLAMA_LAST_ERROR}"
  fi
  fail "llama-server not found. Build llama.cpp and install to ~/.local/bin"
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
  local sloptools_unit_src
  local unit_dst="$HOME/.config/systemd/user"
  local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8081}"
  local web_host="${SLOPSHELL_WEB_HOST:-127.0.0.1}"
  local -a core_units=(
    sloptools.service
    slopshell-codex-app-server.service
    slopshell-piper-tts.service
    slopshell-stt.service
    slopshell-web.service
  )
  local -a optional_units=()

  install_slsh_binary
  build_sloptools_binary
  sloptools_unit_src="$SLOPTOOLS_REPO_ROOT/deploy/systemd/user/sloptools.service"
  mkdir -p "$unit_dst"
  sed -e "s|@@SLOPTOOLS_BIN@@|${SLOPTOOLS_BIN_PATH}|g" \
      -e "s|@@PROJECT_DIR@@|${REPO_ROOT}|g" \
      -e "s|@@DATA_DIR@@|${SLOPTOOLS_DATA_DIR}|g" \
      "$sloptools_unit_src" > "$unit_dst/sloptools.service"
  for f in "$unit_src"/*.service; do
    local base
    base="$(basename "$f")"
    if { [ "$base" = "slopshell-llm.service" ] || [ "$base" = "slopshell-codex-llm.service" ]; } && [ -n "$REUSE_LLM_URL" ]; then
      continue
    fi
    sed -e "s|@@REPO_ROOT@@|${REPO_ROOT}|g" \
        -e "s|@@LLAMA_SERVER_BIN@@|${LLAMA_SERVER_BIN_RESOLVED}|g" \
        -e "s|@@SLOPSHELL_WEB_HOST@@|${web_host}|g" \
        -e "s|@@SLOPSHELL_INTENT_LLM_URL@@|${effective_llm_url}|g" \
        "$f" > "$unit_dst/$base"
  done
  if [ -n "$REUSE_LLM_URL" ]; then
    rm -f "$unit_dst/slopshell-llm.service" "$unit_dst/slopshell-codex-llm.service"
  fi
  systemctl --user daemon-reload

  # Disable legacy units
  systemctl --user disable --now \
    slopshell-dev-watch.path \
    slopshell-mcp.service \
    slopshell-voxtype-mcp.service \
    slopshell-ptt.service \
    tabura.service \
    tabura-web.service \
    tabura-mcp.service \
    tabura-llm.service \
    tabura-stt.service \
    tabura-piper-tts.service \
    tabura-codex-app-server.service \
    helpy-mcp.service \
    voxtype.service \
    >/dev/null 2>&1 || true
  if [ -n "$REUSE_LLM_URL" ]; then
    systemctl --user disable --now slopshell-llm.service slopshell-codex-llm.service >/dev/null 2>&1 || true
  fi

  # Enable and start all services
  local units=("${core_units[@]}" "${optional_units[@]}")
  if [ -z "$REUSE_LLM_URL" ]; then
    units+=(slopshell-llm.service)
    core_units+=(slopshell-llm.service)
    units+=(slopshell-codex-llm.service)
    optional_units+=(slopshell-codex-llm.service)
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
  local probe="/tmp/slopshell-launchctl-probe.plist"
  cat > "$probe" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>io.slopshell.probe</string>
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
  local sloptools_plist_src
  local plist_dst="$HOME/Library/LaunchAgents"
  local data_root="$HOME/Library/Application Support/slopshell"
  local piper_model_dir piper_venv_dir llm_source_dir
  local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8081}"
  local web_host="${SLOPSHELL_WEB_HOST:-127.0.0.1}"

  [ -d "$plist_src" ] || fail "launchd templates not found: $plist_src"

  # Build Go binary for dev use
  log "Building slopshell binary"
  if ! (cd "$REPO_ROOT" && go build -o "$REPO_ROOT/slopshell" ./cmd/slopshell); then
    fail "go build failed"
  fi
  install_slsh_binary
  build_sloptools_binary

  BIN_PATH="$REPO_ROOT/slopshell"
  CODEX_PATH="$(command -v codex 2>/dev/null || true)"
  VOXTYPE_PATH="$(command -v voxtype 2>/dev/null || echo voxtype)"
  WEB_DATA_DIR="${data_root}/web-data"
  sloptools_plist_src="${SLOPTOOLS_REPO_ROOT}/deploy/launchd/io.sloptools.mcp.plist"
  piper_model_dir="${HOME}/.local/share/slopshell-piper-tts/models"
  piper_venv_dir="${HOME}/.local/share/slopshell-piper-tts/venv"
  llm_source_dir="${data_root}/llm/vllm-mlx"

  mkdir -p "$plist_dst" "$WEB_DATA_DIR"
  install_hotword_assets
  if [ "$HAVE_LLAMA" = "1" ] && [ -z "$REUSE_LLM_URL" ]; then
    sync_macos_vllm_source_checkout "$llm_source_dir"
  fi
  if [ -n "$REUSE_LLM_URL" ]; then
    launchctl unload "$plist_dst/io.slopshell.llm.plist" >/dev/null 2>&1 || true
    launchctl unload "$plist_dst/io.slopshell.codex-llm.plist" >/dev/null 2>&1 || true
    rm -f "$plist_dst/io.slopshell.llm.plist" "$plist_dst/io.slopshell.codex-llm.plist"
  fi
  launchctl unload "$plist_dst/io.sloptools.mcp.plist" >/dev/null 2>&1 || true
  launchctl unload "$plist_dst/io.slopshell.piper-tts.plist" >/dev/null 2>&1 || true
  launchctl unload "$plist_dst/io.slopshell.macos-tts.plist" >/dev/null 2>&1 || true
  launchctl unload "$plist_dst/io.slopshell.codex-app-server.plist" >/dev/null 2>&1 || true
  launchctl unload "$plist_dst/io.tabura.web.plist" >/dev/null 2>&1 || true
  launchctl unload "$plist_dst/io.tabura.stt.plist" >/dev/null 2>&1 || true
  launchctl unload "$plist_dst/io.tabura.llm.plist" >/dev/null 2>&1 || true
  launchctl unload "$plist_dst/io.tabura.piper-tts.plist" >/dev/null 2>&1 || true
  launchctl unload "$plist_dst/io.tabura.codex-app-server.plist" >/dev/null 2>&1 || true
  launchctl unload "$plist_dst/io.tabura.mcp.plist" >/dev/null 2>&1 || true
  rm -f "$plist_dst/io.sloptools.mcp.plist" "$plist_dst/io.slopshell.piper-tts.plist" "$plist_dst/io.slopshell.macos-tts.plist" "$plist_dst/io.slopshell.codex-app-server.plist" "$plist_dst"/io.tabura.*.plist

  # Determine which agents to install
  local agents=(sloptools-mcp codex-app-server piper-tts web)
  if [ "$HAVE_LLAMA" = "1" ] && [ -z "$REUSE_LLM_URL" ]; then
    agents+=(llm)
  fi
  if [ "$HAVE_VOXTYPE" = "1" ]; then
    agents+=(stt)
  fi

  # Install plist files (always, even if launchctl is unavailable)
  local src dst
  for name in "${agents[@]}"; do
    if [ "$name" = "sloptools-mcp" ]; then
      src="$sloptools_plist_src"
      dst="$plist_dst/io.sloptools.mcp.plist"
      if [ ! -f "$src" ]; then
        fail "sloptools launchd template missing: $src"
      fi
      sed \
        -e "s|@@SLOPTOOLS_BIN@@|${SLOPTOOLS_BIN_PATH}|g" \
        -e "s|@@PROJECT_DIR@@|${REPO_ROOT}|g" \
        -e "s|@@DATA_DIR@@|${SLOPTOOLS_DATA_DIR}|g" \
        "$src" > "$dst"
      log "Installed plist: $dst"
      continue
    fi
    src="$plist_src/io.slopshell.${name}.plist"
    dst="$plist_dst/io.slopshell.${name}.plist"
    if [ ! -f "$src" ]; then
      log "WARNING: template missing: $src"
      continue
    fi
    sed \
      -e "s|@@BIN_PATH@@|${BIN_PATH}|g" \
      -e "s|@@CODEX_PATH@@|${CODEX_PATH}|g" \
      -e "s|@@PROJECT_DIR@@|${REPO_ROOT}|g" \
      -e "s|@@WEB_DATA_DIR@@|${WEB_DATA_DIR}|g" \
      -e "s|@@SLOPSHELL_WEB_HOST@@|${web_host}|g" \
      -e "s|@@VENV_DIR@@|${piper_venv_dir}|g" \
      -e "s|@@SCRIPT_DIR@@|${REPO_ROOT}/scripts|g" \
      -e "s|@@PIPER_MODEL_DIR@@|${piper_model_dir}|g" \
      -e "s|@@LLM_SETUP_SCRIPT@@|${REPO_ROOT}/scripts/setup-local-llm.sh|g" \
      -e "s|@@LLM_MODEL_DIR@@|${LLM_MODEL_DIR}|g" \
      -e "s|@@LLM_VENV_DIR@@|${LLM_VENV_DIR}|g" \
      -e "s|@@LLM_SOURCE_DIR@@|${llm_source_dir}|g" \
      -e "s|@@LLAMA_SERVER_BIN@@|${LLAMA_SERVER_BIN_RESOLVED}|g" \
      -e "s|@@STT_SETUP_SCRIPT@@|${REPO_ROOT}/scripts/setup-voxtype-stt.sh|g" \
      -e "s|@@VOXTYPE_BIN@@|${VOXTYPE_PATH}|g" \
      -e "s|@@SLOPSHELL_INTENT_LLM_URL@@|${effective_llm_url}|g" \
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
    if [ "$name" = "sloptools-mcp" ]; then
      dst="$plist_dst/io.sloptools.mcp.plist"
      launchctl unload "$dst" >/dev/null 2>&1 || true
      launchctl load -w "$dst"
      log "Loaded: io.sloptools.mcp"
      continue
    fi
    dst="$plist_dst/io.slopshell.${name}.plist"
    launchctl unload "$dst" >/dev/null 2>&1 || true
    launchctl load -w "$dst"
    log "Loaded: io.slopshell.${name}"
  done

  sleep 3
  local failed=()
  for name in "$@"; do
    if [ "$name" = "sloptools-mcp" ]; then
      if ! launchctl list "io.sloptools.mcp" >/dev/null 2>&1; then
        failed+=("io.sloptools.mcp")
      fi
      continue
    fi
    if ! launchctl list "io.slopshell.${name}" >/dev/null 2>&1; then
      failed+=("io.slopshell.${name}")
    fi
  done

  if ((${#failed[@]} > 0)); then
    log "FAILED agents: ${failed[*]}"
    fail "Not all agents started"
  fi

  log "All agents running (launchd)"
}

activate_direct() {
  local pidfile="/tmp/slopshell-pids.txt"
  local web_host="${SLOPSHELL_WEB_HOST:-127.0.0.1}"
  local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8081}"
  : > "$pidfile"

  for name in "$@"; do
    local logfile="/tmp/slopshell-${name}.log"
    case "$name" in
      sloptools-mcp)
        logfile="/tmp/sloptools-mcp.log"
        nohup "$SLOPTOOLS_BIN_PATH" server \
          --project-dir "$REPO_ROOT" --data-dir "$SLOPTOOLS_DATA_DIR" \
          --mcp-host 127.0.0.1 --mcp-port 9420 \
          >"$logfile" 2>&1 &
        ;;
      codex-app-server)
        nohup "$CODEX_PATH" app-server --listen ws://127.0.0.1:8787 \
          >"$logfile" 2>&1 &
        ;;
      piper-tts)
        PIPER_MODEL_DIR="${HOME}/.local/share/slopshell-piper-tts/models" \
        nohup "${HOME}/.local/share/slopshell-piper-tts/venv/bin/uvicorn" piper_tts_server:app \
          --app-dir "$REPO_ROOT/scripts" --host 127.0.0.1 --port 8424 \
          >"$logfile" 2>&1 &
        ;;
      web)
        SLOPSHELL_INTENT_LLM_URL="$effective_llm_url" \
        SLOPSHELL_ASSISTANT_MODE=local \
        SLOPSHELL_INTENT_LLM_MODEL=qwen3.5-9b \
        SLOPSHELL_ASSISTANT_LLM_MODEL=qwen3.5-9b \
        SLOPSHELL_INTENT_LLM_PROFILE=qwen3.5-9b \
        SLOPSHELL_INTENT_LLM_PROFILE_OPTIONS=qwen3.5-9b \
        nohup "$BIN_PATH" server \
          --project-dir "$REPO_ROOT" --data-dir "$WEB_DATA_DIR" \
          --local-mcp-url http://127.0.0.1:9420/mcp \
          --web-host "$web_host" --web-port 8420 \
          --tts-url http://127.0.0.1:8424 \
          >"$logfile" 2>&1 &
        ;;
      llm)
        SLOPSHELL_LLM_MODEL_DIR="$LLM_MODEL_DIR" \
        SLOPSHELL_LLM_VENV_DIR="$LLM_VENV_DIR" \
        nohup "$REPO_ROOT/scripts/setup-local-llm.sh" \
          >"$logfile" 2>&1 &
        ;;
      stt)
        SLOPSHELL_STT_LANGUAGE=de,en SLOPSHELL_STT_MODEL=large-v3-turbo \
        nohup "$REPO_ROOT/scripts/setup-voxtype-stt.sh" \
          >"$logfile" 2>&1 &
        ;;
    esac
    if [ "$name" = "sloptools-mcp" ]; then
      echo "$! io.sloptools.mcp" >> "$pidfile"
      log "Started: io.sloptools.mcp (pid $!)"
    else
      echo "$! io.slopshell.${name}" >> "$pidfile"
      log "Started: io.slopshell.${name} (pid $!)"
    fi
  done

  sleep 3
  local failed=()
  local pid label
  while read -r pid label; do
    if ! kill -0 "$pid" 2>/dev/null; then
      failed+=("$label")
      if [ "$label" = "io.sloptools.mcp" ]; then
        log "FAILED: $label (pid $pid) — see /tmp/sloptools-mcp.log"
      else
        log "FAILED: $label (pid $pid) — see /tmp/slopshell-${label#io.slopshell.}.log"
      fi
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
