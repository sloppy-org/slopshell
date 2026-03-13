#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLATFORM="$(uname -s)"

log() { printf '[tabura-units] %s\n' "$*"; }
fail() { printf '[tabura-units] ERROR: %s\n' "$*" >&2; exit 1; }

confirm_default_yes() {
    local prompt="$1"
    if [ ! -t 0 ]; then
        return 0
    fi
    local response
    read -r -p "$prompt [Y/n] " response
    case "$response" in
        "" | [Yy] | [Yy][Ee][Ss]) return 0 ;;
        *) return 1 ;;
    esac
}

detect_local_llm() {
    local port url
    for port in 1234 8080 8081 8426; do
        url="http://127.0.0.1:${port}"
        if curl -fsS --max-time 2 "${url}/health" >/dev/null 2>&1; then
            printf '%s' "$url"
            return 0
        fi
    done
    return 1
}

should_manage_local_lmstudio() {
    if [ -n "${TABURA_INTENT_LLM_URL:-}" ] && [ "${TABURA_INTENT_LLM_URL}" != "off" ]; then
        return 1
    fi
    case "${REUSE_LLM_URL}" in
        "" | "http://127.0.0.1:1234" | "http://localhost:1234") ;;
        *) return 1 ;;
    esac
    [ -x "${HOME}/.lmstudio/bin/lms" ] || command -v lm-studio >/dev/null 2>&1
}

REUSE_LLM_URL=""
MAC_BIN_PATH=""
MAC_CODEX_PATH=""
MAC_WEB_DATA_DIR=""
MAC_PIPER_MODEL_DIR=""
MAC_PIPER_VENV_DIR=""
MAC_EFFECTIVE_LLM_URL=""
MAC_WEB_HOST=""
LMSTUDIO_SESSION_SCRIPT="${REPO_ROOT}/scripts/lmstudio-session.sh"
LINUX_AUTOSTART_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/autostart"
LINUX_AUTOSTART_FILE="${LINUX_AUTOSTART_DIR}/tabura-lmstudio.desktop"

case "$PLATFORM" in
    Linux|Darwin) ;;
    *) fail "unsupported platform: $PLATFORM" ;;
esac

if [ -n "${TABURA_INTENT_LLM_URL:-}" ]; then
    REUSE_LLM_URL="$TABURA_INTENT_LLM_URL"
    log "TABURA_INTENT_LLM_URL set to ${REUSE_LLM_URL}; skipping local LM Studio setup"
elif existing_url="$(detect_local_llm)"; then
    log "Existing local LLM detected at ${existing_url}"
    if confirm_default_yes "Reuse existing local LLM at ${existing_url}?"; then
        REUSE_LLM_URL="$existing_url"
    fi
fi

command -v codex >/dev/null 2>&1 || fail "codex not in PATH"

if ! command -v voxtype >/dev/null 2>&1; then
    if [ "$PLATFORM" = "Darwin" ]; then
        log "WARNING: voxtype not in PATH. Build from source: scripts/build-voxtype-macos.sh"
    else
        fail "voxtype not in PATH. Install voxtype"
    fi
fi

if [ "$PLATFORM" = "Darwin" ]; then
    command -v go >/dev/null 2>&1 || fail "go not in PATH. Install: brew install go"
fi

install_linux() {
    local unit_src="$REPO_ROOT/deploy/systemd/user"
    local unit_dst="$HOME/.config/systemd/user"
    local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:1234}"
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
        sed -e "s|@@REPO_ROOT@@|${REPO_ROOT}|g" \
            -e "s|@@TABURA_WEB_HOST@@|${web_host}|g" \
            -e "s|@@TABURA_INTENT_LLM_URL@@|${effective_llm_url}|g" \
            "$f" >"$unit_dst/$base"
    done
    systemctl --user daemon-reload

    systemctl --user disable --now \
        tabura-dev-watch.path \
        tabura-dev-watch.service \
        tabura-mcp.service \
        tabura-voxtype-mcp.service \
        helpy-mcp.service \
        voxtype.service \
        >/dev/null 2>&1 || true

    local units=("${core_units[@]}" "${optional_units[@]}")
    systemctl --user enable --now "${units[@]}"
    log "Enabled: ${units[*]}"

    if should_manage_local_lmstudio; then
        "${REPO_ROOT}/scripts/setup-tabura-lmstudio.sh"
        mkdir -p "${LINUX_AUTOSTART_DIR}"
        sed -e "s|@@LMSTUDIO_SESSION_SCRIPT@@|${LMSTUDIO_SESSION_SCRIPT}|g" \
            "${REPO_ROOT}/deploy/autostart/tabura-lmstudio.desktop" >"${LINUX_AUTOSTART_FILE}"
        chmod +x "${LMSTUDIO_SESSION_SCRIPT}"
        log "Installed LM Studio autostart: ${LINUX_AUTOSTART_FILE}"
    else
        rm -f "${LINUX_AUTOSTART_FILE}"
    fi

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
    fi
    if ((${#failed[@]} > 0)); then
        for unit in "${failed[@]}"; do
            systemctl --user status "$unit" --no-pager -n 20 2>&1 || true
        done
        fail "Not all services started"
    fi

    log "All services running"
}

launchctl_available() {
    local probe="/tmp/tabura-launchctl-probe.plist"
    cat >"$probe" <<'PLIST'
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

install_macos() {
    local plist_src="$REPO_ROOT/deploy/launchd"
    local plist_dst="$HOME/Library/LaunchAgents"
    local data_root="$HOME/Library/Application Support/tabura"
    local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:1234}"
    local web_host="${TABURA_WEB_HOST:-127.0.0.1}"
    local piper_model_dir="$HOME/.local/share/tabura-piper-tts/models"
    local piper_venv_dir="$HOME/.local/share/tabura-piper-tts/venv"
    local web_data_dir="${data_root}/web-data"
    local bin_path codex_path

    [ -d "$plist_src" ] || fail "launchd templates not found: $plist_src"

    log "Building tabura binary"
    (cd "$REPO_ROOT" && go build -o "$REPO_ROOT/tabura" ./cmd/tabura) || fail "go build failed"

    bin_path="$REPO_ROOT/tabura"
    codex_path="$(command -v codex)"
    mkdir -p "$plist_dst" "$web_data_dir"

    MAC_BIN_PATH="$bin_path"
    MAC_CODEX_PATH="$codex_path"
    MAC_WEB_DATA_DIR="$web_data_dir"
    MAC_PIPER_MODEL_DIR="$piper_model_dir"
    MAC_PIPER_VENV_DIR="$piper_venv_dir"
    MAC_EFFECTIVE_LLM_URL="$effective_llm_url"
    MAC_WEB_HOST="$web_host"

    local agents=(codex-app-server piper-tts web)
    if should_manage_local_lmstudio; then
        agents+=(lmstudio)
        "${REPO_ROOT}/scripts/setup-tabura-lmstudio.sh"
    fi
    if command -v voxtype >/dev/null 2>&1; then
        agents+=(stt)
    fi

    local src dst
    for name in "${agents[@]}"; do
        src="$plist_src/io.tabura.${name}.plist"
        dst="$plist_dst/io.tabura.${name}.plist"
        [ -f "$src" ] || fail "missing template: $src"
        sed \
            -e "s|@@BIN_PATH@@|${bin_path}|g" \
            -e "s|@@CODEX_PATH@@|${codex_path}|g" \
            -e "s|@@PROJECT_DIR@@|${REPO_ROOT}|g" \
            -e "s|@@WEB_DATA_DIR@@|${web_data_dir}|g" \
            -e "s|@@TABURA_WEB_HOST@@|${web_host}|g" \
            -e "s|@@VENV_DIR@@|${piper_venv_dir}|g" \
            -e "s|@@SCRIPT_DIR@@|${REPO_ROOT}/scripts|g" \
            -e "s|@@PIPER_MODEL_DIR@@|${piper_model_dir}|g" \
            -e "s|@@LMSTUDIO_SESSION_SCRIPT@@|${LMSTUDIO_SESSION_SCRIPT}|g" \
            -e "s|@@STT_SETUP_SCRIPT@@|${REPO_ROOT}/scripts/setup-voxtype-stt.sh|g" \
            -e "s|@@TABURA_INTENT_LLM_URL@@|${effective_llm_url}|g" \
            "$src" >"$dst"
        log "Installed plist: $dst"
    done

    if launchctl_available; then
        activate_launchd "${agents[@]}"
    else
        log "launchctl unavailable; starting services directly"
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
    log "All agents running (launchd)"
}

activate_direct() {
    local pidfile="/tmp/tabura-pids.txt"
    : >"$pidfile"

    for name in "$@"; do
        local logfile="/tmp/tabura-${name}.log"
        case "$name" in
            codex-app-server)
                nohup "$MAC_CODEX_PATH" app-server --listen ws://127.0.0.1:8787 >"$logfile" 2>&1 &
                ;;
            piper-tts)
                PIPER_MODEL_DIR="$MAC_PIPER_MODEL_DIR" \
                    nohup "$MAC_PIPER_VENV_DIR/bin/uvicorn" piper_tts_server:app \
                    --app-dir "$REPO_ROOT/scripts" --host 127.0.0.1 --port 8424 >"$logfile" 2>&1 &
                ;;
            web)
                TABURA_INTENT_LLM_URL="$MAC_EFFECTIVE_LLM_URL" \
                    TABURA_INTENT_LLM_MODEL=qwen/qwen3.5-9b \
                    TABURA_INTENT_LLM_PROFILE=qwen3.5-9b \
                    TABURA_INTENT_LLM_PROFILE_OPTIONS=qwen3.5-9b,qwen3.5-4b \
                    nohup "$MAC_BIN_PATH" server \
                    --project-dir "$REPO_ROOT" \
                    --data-dir "$MAC_WEB_DATA_DIR" \
                    --web-host "$MAC_WEB_HOST" \
                    --web-port 8420 \
                    --mcp-host 127.0.0.1 \
                    --mcp-port 9420 \
                    --app-server-url ws://127.0.0.1:8787 \
                    --tts-url http://127.0.0.1:8424 >"$logfile" 2>&1 &
                ;;
            lmstudio)
                nohup "$LMSTUDIO_SESSION_SCRIPT" >"$logfile" 2>&1 &
                ;;
            stt)
                TABURA_STT_LANGUAGE=de,en TABURA_STT_MODEL=large-v3-turbo \
                    nohup "$REPO_ROOT/scripts/setup-voxtype-stt.sh" >"$logfile" 2>&1 &
                ;;
        esac
        echo "$! io.tabura.${name}" >>"$pidfile"
    done

    sleep 3
    local failed=()
    local pid label
    while read -r pid label; do
        if ! kill -0 "$pid" 2>/dev/null; then
            failed+=("$label")
        fi
    done <"$pidfile"
    if ((${#failed[@]} > 0)); then
        fail "Not all services started: ${failed[*]}"
    fi
    log "All services running (direct)"
}

if [ "$PLATFORM" = "Darwin" ]; then
    install_macos
else
    install_linux
fi
