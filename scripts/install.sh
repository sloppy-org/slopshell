#!/usr/bin/env bash
set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
REPO_OWNER="${TABURA_REPO_OWNER:-krystophny}"
REPO_NAME="${TABURA_REPO_NAME:-tabura}"
RELEASE_API_BASE="${TABURA_RELEASE_API_BASE:-https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases}"
ASSUME_YES="${TABURA_ASSUME_YES:-0}"
DRY_RUN="${TABURA_INSTALL_DRY_RUN:-0}"
SKIP_BROWSER="${TABURA_INSTALL_SKIP_BROWSER:-0}"
SKIP_VOXTYPE="${TABURA_INSTALL_SKIP_VOXTYPE:-0}"
REQUESTED_VERSION=""
DO_UNINSTALL=0
TABURA_OS=""
TABURA_ARCH=""
DATA_ROOT=""
BIN_DIR=""
BIN_PATH=""
PROJECT_DIR=""
WEB_DATA_DIR=""
PIPER_DIR=""
MODEL_DIR=""
VENV_DIR=""
SCRIPT_DIR=""
PIPER_SERVER_SCRIPT=""
CODEX_PATH=""

log() {
    printf '[tabura-install] %s\n' "$*"
}

fail() {
    printf '[tabura-install] ERROR: %s\n' "$*" >&2
    exit 1
}

run_cmd() {
    if [ "$DRY_RUN" = "1" ]; then
        printf '[tabura-install] [dry-run]'
        printf ' %q' "$@"
        printf '\n'
        return 0
    fi
    "$@"
}

confirm_default_yes() {
    local prompt="$1"
    if [ "$ASSUME_YES" = "1" ]; then
        log "TABURA_ASSUME_YES=1 accepted: ${prompt}"
        return 0
    fi
    if [ ! -t 0 ]; then
        log "non-interactive session defaults to yes: ${prompt}"
        return 0
    fi
    local response
    read -r -p "${prompt} [Y/n] " response
    case "$response" in
        "" | [Yy] | [Yy][Ee][Ss]) return 0 ;;
        *) return 1 ;;
    esac
}

have_cmd() {
    command -v "$1" >/dev/null 2>&1
}

print_help() {
    cat <<USAGE
Usage: ${SCRIPT_NAME} [options]

Options:
  --version <vX.Y.Z>   Install a specific release tag (default: latest)
  --yes                Non-interactive mode (answer yes to prompts)
  --dry-run            Print actions without modifying the system
  --uninstall          Uninstall services and binary
  -h, --help           Show this help

Environment overrides:
  TABURA_INSTALL_DRY_RUN=1
  TABURA_INSTALL_SKIP_BROWSER=1
  TABURA_INSTALL_SKIP_VOXTYPE=1
  TABURA_REPO_OWNER / TABURA_REPO_NAME / TABURA_RELEASE_API_BASE
USAGE
}

parse_args() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --version)
                [ "$#" -ge 2 ] || fail "--version requires a value"
                REQUESTED_VERSION="$2"
                shift 2
                ;;
            --yes)
                ASSUME_YES=1
                shift
                ;;
            --dry-run)
                DRY_RUN=1
                shift
                ;;
            --uninstall)
                DO_UNINSTALL=1
                shift
                ;;
            -h | --help)
                print_help
                exit 0
                ;;
            *)
                fail "unknown argument: $1"
                ;;
        esac
    done
}

normalize_version() {
    local raw="$1"
    raw="${raw#v}"
    raw="${raw#V}"
    printf 'v%s' "$raw"
}

resolve_platform() {
    local uname_s uname_m
    uname_s="$(uname -s | tr '[:upper:]' '[:lower:]')"
    uname_m="$(uname -m | tr '[:upper:]' '[:lower:]')"
    case "$uname_s" in
        linux) TABURA_OS="linux" ;;
        darwin) TABURA_OS="darwin" ;;
        *) fail "unsupported operating system: ${uname_s}" ;;
    esac
    case "$uname_m" in
        x86_64 | amd64) TABURA_ARCH="amd64" ;;
        arm64 | aarch64) TABURA_ARCH="arm64" ;;
        *) fail "unsupported architecture: ${uname_m}" ;;
    esac
}

resolve_paths() {
    local xdg_data
    BIN_DIR="${TABURA_BIN_DIR:-${HOME}/.local/bin}"
    BIN_PATH="${BIN_DIR}/tabura"
    if [ "$TABURA_OS" = "darwin" ]; then
        DATA_ROOT="${TABURA_DATA_ROOT:-${HOME}/Library/Application Support/tabura}"
    else
        xdg_data="${XDG_DATA_HOME:-${HOME}/.local/share}"
        DATA_ROOT="${TABURA_DATA_ROOT:-${xdg_data}/tabura}"
    fi
    PROJECT_DIR="${TABURA_PROJECT_DIR:-${DATA_ROOT}/project}"
    WEB_DATA_DIR="${TABURA_WEB_DATA_DIR:-${DATA_ROOT}/web-data}"
    PIPER_DIR="${DATA_ROOT}/piper-tts"
    MODEL_DIR="${PIPER_DIR}/models"
    VENV_DIR="${PIPER_DIR}/venv"
    SCRIPT_DIR="${DATA_ROOT}/scripts"
    PIPER_SERVER_SCRIPT="${SCRIPT_DIR}/piper_tts_server.py"
}

require_codex_app_server() {
    CODEX_PATH="$(command -v codex || true)"
    [ -n "$CODEX_PATH" ] || fail "codex app-server is required but codex is not in PATH"
}

require_python_310() {
    have_cmd python3 || fail "python3 is required"
    python3 - <<'PY' >/dev/null || fail "python3 3.10+ is required"
import sys
if sys.version_info < (3, 10):
    raise SystemExit(1)
PY
}

require_base_tools() {
    have_cmd curl || fail "curl is required"
    have_cmd tar || fail "tar is required"
    have_cmd awk || fail "awk is required"
}

install_ffmpeg_linux() {
    local -a sudo_prefix
    sudo_prefix=()
    if [ "$(id -u)" -ne 0 ]; then
        have_cmd sudo || fail "ffmpeg install needs sudo privileges"
        sudo_prefix=(sudo)
    fi
    if have_cmd apt-get; then
        run_cmd "${sudo_prefix[@]}" apt-get update
        run_cmd "${sudo_prefix[@]}" apt-get install -y ffmpeg
        return
    fi
    if have_cmd dnf; then
        run_cmd "${sudo_prefix[@]}" dnf install -y ffmpeg
        return
    fi
    if have_cmd pacman; then
        run_cmd "${sudo_prefix[@]}" pacman -Sy --noconfirm ffmpeg
        return
    fi
    if have_cmd zypper; then
        run_cmd "${sudo_prefix[@]}" zypper --non-interactive install ffmpeg
        return
    fi
    fail "no supported package manager found to install ffmpeg"
}

ensure_ffmpeg() {
    if have_cmd ffmpeg; then
        return
    fi
    if ! confirm_default_yes "ffmpeg is missing. Attempt automatic install?"; then
        fail "ffmpeg is required"
    fi
    if [ "$TABURA_OS" = "darwin" ]; then
        have_cmd brew || fail "Homebrew is required to install ffmpeg on macOS"
        run_cmd brew install ffmpeg
    else
        install_ffmpeg_linux
    fi
    if [ "$DRY_RUN" = "0" ] && ! have_cmd ffmpeg; then
        fail "ffmpeg installation did not produce ffmpeg in PATH"
    fi
}

release_api_url() {
    if [ -n "$REQUESTED_VERSION" ]; then
        printf '%s/tags/%s' "$RELEASE_API_BASE" "$(normalize_version "$REQUESTED_VERSION")"
        return
    fi
    printf '%s/latest' "$RELEASE_API_BASE"
}

default_dry_run_release_json() {
    local version tag_nov
    version="$(normalize_version "${REQUESTED_VERSION:-0.0.0-test}")"
    tag_nov="${version#v}"
    cat <<JSON
{"tag_name":"${version}","assets":[{"name":"tabura_${tag_nov}_${TABURA_OS}_${TABURA_ARCH}.tar.gz","browser_download_url":"https://example.invalid/tabura_${tag_nov}_${TABURA_OS}_${TABURA_ARCH}.tar.gz"},{"name":"checksums.txt","browser_download_url":"https://example.invalid/checksums.txt"}]}
JSON
}

fetch_release_json() {
    if [ -n "${TABURA_RELEASE_JSON:-}" ]; then
        printf '%s\n' "$TABURA_RELEASE_JSON"
        return
    fi
    if [ "$DRY_RUN" = "1" ]; then
        default_dry_run_release_json
        return
    fi
    curl -fsSL "$(release_api_url)"
}

release_field() {
    local field="$1"
    local payload="$2"
    TABURA_RELEASE_JSON_PAYLOAD="$payload" python3 - "$field" <<'PY'
import json
import os
import sys
field = sys.argv[1]
data = json.loads(os.environ["TABURA_RELEASE_JSON_PAYLOAD"])
if field == "tag_name":
    value = data.get("tag_name", "")
    if not value:
        raise SystemExit(1)
    print(value)
    raise SystemExit(0)
if field.startswith("asset:"):
    target = field.split(":", 1)[1]
    for asset in data.get("assets", []):
        if asset.get("name") == target:
            print(asset.get("browser_download_url", ""))
            raise SystemExit(0)
    raise SystemExit(1)
raise SystemExit(1)
PY
}

checksum_tool() {
    if have_cmd sha256sum; then
        echo "sha256sum"
        return
    fi
    if have_cmd shasum; then
        echo "shasum"
        return
    fi
    fail "sha256 tool missing (need sha256sum or shasum)"
}

file_sha256() {
    local tool="$1"
    local file="$2"
    if [ "$tool" = "sha256sum" ]; then
        sha256sum "$file" | awk '{print $1}'
        return
    fi
    shasum -a 256 "$file" | awk '{print $1}'
}

download_release_payload() {
    local release_json="$1"
    local tmpdir="$2"
    local tag requested asset_name asset_url checksums_url checksums_file archive_file expected actual tool

    tag="$(release_field tag_name "$release_json")"
    requested="${tag#v}"
    asset_name="tabura_${requested}_${TABURA_OS}_${TABURA_ARCH}.tar.gz"
    asset_url="$(release_field "asset:${asset_name}" "$release_json")" || fail "release missing asset ${asset_name}"
    checksums_url="$(release_field 'asset:checksums.txt' "$release_json")" || fail "release missing checksums.txt"

    archive_file="${tmpdir}/${asset_name}"
    checksums_file="${tmpdir}/checksums.txt"

    if [ "$DRY_RUN" = "1" ]; then
        cat >"${tmpdir}/tabura" <<'BIN'
#!/usr/bin/env bash
echo "tabura dry-run binary"
BIN
        chmod +x "${tmpdir}/tabura"
        if [ -f "scripts/piper_tts_server.py" ]; then
            cp "scripts/piper_tts_server.py" "${tmpdir}/piper_tts_server.py"
        else
            echo "# dry-run piper server" >"${tmpdir}/piper_tts_server.py"
        fi
        printf '%s\n' "$tag"
        return
    fi

    curl -fsSL -o "$archive_file" "$asset_url"
    curl -fsSL -o "$checksums_file" "$checksums_url"

    expected="$(awk -v n="$asset_name" '$2 == n {print $1}' "$checksums_file")"
    [ -n "$expected" ] || fail "checksum entry not found for ${asset_name}"

    tool="$(checksum_tool)"
    actual="$(file_sha256 "$tool" "$archive_file")"
    if [ "${actual}" != "${expected}" ]; then
        fail "checksum mismatch for ${asset_name}: got ${actual}, want ${expected}"
    fi

    tar -xzf "$archive_file" -C "$tmpdir"
    [ -x "${tmpdir}/tabura" ] || fail "tabura binary missing in archive"
    [ -f "${tmpdir}/scripts/piper_tts_server.py" ] || fail "scripts/piper_tts_server.py missing in archive"
    cp "${tmpdir}/scripts/piper_tts_server.py" "${tmpdir}/piper_tts_server.py"
    printf '%s\n' "$tag"
}

install_binary_payload() {
    local staging_dir="$1"
    run_cmd mkdir -p "$BIN_DIR" "$SCRIPT_DIR"
    run_cmd cp "${staging_dir}/tabura" "$BIN_PATH"
    run_cmd chmod +x "$BIN_PATH"
    run_cmd cp "${staging_dir}/piper_tts_server.py" "$PIPER_SERVER_SCRIPT"
    if ! printf ':%s:' "$PATH" | grep -Fq ":${BIN_DIR}:"; then
        log "${BIN_DIR} is not in PATH; add it in your shell profile"
    fi
}

bootstrap_project() {
    run_cmd mkdir -p "$PROJECT_DIR" "$WEB_DATA_DIR"
    if [ "$DRY_RUN" = "1" ]; then
        return
    fi
    "$BIN_PATH" bootstrap --project-dir "$PROJECT_DIR" >/dev/null
}

piper_notice() {
    cat <<NOTICE
=== Piper TTS (GPL, runs as HTTP sidecar) ===
Piper TTS will be installed as a local HTTP service.
License: GPL (isolated via HTTP boundary, does not affect Tabura MIT license)
Voice models: en_GB-alan-medium (MIT-compatible)
NOTICE
}

download_model() {
    local model="$1"
    local subpath="$2"
    local note="$3"
    local hf_base onnx_file json_file
    hf_base="https://huggingface.co/rhasspy/piper-voices/resolve/main"
    onnx_file="${MODEL_DIR}/${model}.onnx"
    json_file="${MODEL_DIR}/${model}.onnx.json"

    if [ -f "$onnx_file" ] && [ -f "$json_file" ]; then
        log "voice model already present: ${model}"
        return
    fi

    log "model notice: ${model}"
    log "${note}"
    log "model card: ${hf_base}/${subpath}/MODEL_CARD"
    if ! confirm_default_yes "Download ${model}?"; then
        log "skipping model ${model}"
        return
    fi

    if [ "$DRY_RUN" = "1" ]; then
        run_cmd mkdir -p "$MODEL_DIR"
        run_cmd touch "$onnx_file" "$json_file"
        return
    fi

    curl -fsSL -o "$onnx_file" "${hf_base}/${subpath}/${model}.onnx"
    curl -fsSL -o "$json_file" "${hf_base}/${subpath}/${model}.onnx.json"
}

setup_piper_tts() {
    piper_notice
    if ! confirm_default_yes "Install Piper TTS?"; then
        log "skipping Piper TTS setup"
        return
    fi

    run_cmd mkdir -p "$MODEL_DIR"
    if [ "$DRY_RUN" = "0" ] && [ ! -x "${VENV_DIR}/bin/python" ]; then
        python3 -m venv "$VENV_DIR"
    fi
    if [ "$DRY_RUN" = "0" ]; then
        "${VENV_DIR}/bin/python" -m pip install --upgrade pip
        "${VENV_DIR}/bin/python" -m pip install piper-tts fastapi 'uvicorn[standard]'
    fi

    download_model "en_GB-alan-medium" "en/en_GB/alan/medium" "Model card indicates MIT-compatible terms."
    download_model "de_DE-karlsson-low" "de/de_DE/karlsson/low" "Per-model terms are documented in the model card."
}

install_voxtype() {
    if [ "$SKIP_VOXTYPE" = "1" ]; then
        log "skipping voxtype setup due to TABURA_INSTALL_SKIP_VOXTYPE=1"
        return
    fi
    if have_cmd voxtype; then
        log "voxtype already installed"
    elif have_cmd cargo; then
        if confirm_default_yes "Install voxtype via cargo install voxtype?"; then
            run_cmd cargo install voxtype
        fi
    elif [ "$TABURA_OS" = "darwin" ] && have_cmd brew; then
        if confirm_default_yes "Install voxtype via Homebrew?"; then
            run_cmd brew install voxtype
        fi
    fi

    if have_cmd voxtype; then
        if confirm_default_yes "Download voxtype Whisper assets now?"; then
            run_cmd voxtype setup --download
        fi
        return
    fi
    log "voxtype was not installed; speech-to-text remains unavailable"
}

write_systemd_units() {
    local systemd_dir codex_service piper_service web_service
    systemd_dir="${HOME}/.config/systemd/user"
    codex_service="${systemd_dir}/tabura-codex-app-server.service"
    piper_service="${systemd_dir}/tabura-piper-tts.service"
    web_service="${systemd_dir}/tabura-web.service"

    if [ "$DRY_RUN" = "1" ]; then
        log "[dry-run] write systemd units under ${systemd_dir}"
        return
    fi

    run_cmd mkdir -p "$systemd_dir"

    cat >"$codex_service" <<UNIT
[Unit]
Description=Codex App Server (Tabura)
After=network.target

[Service]
Type=simple
ExecStart=${CODEX_PATH} app-server --listen ws://127.0.0.1:8787
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
UNIT

    cat >"$piper_service" <<UNIT
[Unit]
Description=Tabura Piper TTS
After=network.target

[Service]
Type=simple
Environment=PIPER_MODEL_DIR=${MODEL_DIR}
ExecStart=${VENV_DIR}/bin/uvicorn piper_tts_server:app --app-dir ${SCRIPT_DIR} --host 127.0.0.1 --port 8424
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
UNIT

    cat >"$web_service" <<UNIT
[Unit]
Description=Tabura Web UI
After=network.target tabura-codex-app-server.service tabura-piper-tts.service
Wants=tabura-codex-app-server.service tabura-piper-tts.service

[Service]
Type=simple
ExecStart=${BIN_PATH} server --project-dir ${PROJECT_DIR} --data-dir ${WEB_DATA_DIR} --web-host 127.0.0.1 --web-port 8420 --mcp-host 127.0.0.1 --mcp-port 9420 --app-server-url ws://127.0.0.1:8787 --tts-url http://127.0.0.1:8424
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
UNIT
}

install_services_linux() {
    have_cmd systemctl || fail "systemctl is required for Linux service setup"
    write_systemd_units
    run_cmd systemctl --user daemon-reload
    run_cmd systemctl --user enable --now tabura-codex-app-server.service tabura-piper-tts.service tabura-web.service
}

write_launchd_plists() {
    local agent_dir codex_plist piper_plist web_plist
    agent_dir="${HOME}/Library/LaunchAgents"
    codex_plist="${agent_dir}/io.tabura.codex-app-server.plist"
    piper_plist="${agent_dir}/io.tabura.piper-tts.plist"
    web_plist="${agent_dir}/io.tabura.web.plist"

    if [ "$DRY_RUN" = "1" ]; then
        log "[dry-run] write launchd plists under ${agent_dir}"
        return
    fi

    run_cmd mkdir -p "$agent_dir"

    cat >"$codex_plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.tabura.codex-app-server</string>
  <key>ProgramArguments</key><array>
    <string>${CODEX_PATH}</string><string>app-server</string><string>--listen</string><string>ws://127.0.0.1:8787</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/tmp/tabura-codex-app-server.log</string>
  <key>StandardErrorPath</key><string>/tmp/tabura-codex-app-server.log</string>
</dict></plist>
PLIST

    cat >"$piper_plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.tabura.piper-tts</string>
  <key>ProgramArguments</key><array>
    <string>${VENV_DIR}/bin/uvicorn</string><string>piper_tts_server:app</string><string>--app-dir</string><string>${SCRIPT_DIR}</string><string>--host</string><string>127.0.0.1</string><string>--port</string><string>8424</string>
  </array>
  <key>EnvironmentVariables</key><dict>
    <key>PIPER_MODEL_DIR</key><string>${MODEL_DIR}</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/tmp/tabura-piper-tts.log</string>
  <key>StandardErrorPath</key><string>/tmp/tabura-piper-tts.log</string>
</dict></plist>
PLIST

    cat >"$web_plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>io.tabura.web</string>
  <key>ProgramArguments</key><array>
    <string>${BIN_PATH}</string><string>server</string><string>--project-dir</string><string>${PROJECT_DIR}</string><string>--data-dir</string><string>${WEB_DATA_DIR}</string><string>--web-host</string><string>127.0.0.1</string><string>--web-port</string><string>8420</string><string>--mcp-host</string><string>127.0.0.1</string><string>--mcp-port</string><string>9420</string><string>--app-server-url</string><string>ws://127.0.0.1:8787</string><string>--tts-url</string><string>http://127.0.0.1:8424</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/tmp/tabura-web.log</string>
  <key>StandardErrorPath</key><string>/tmp/tabura-web.log</string>
</dict></plist>
PLIST
}

load_launchd_service() {
    local plist="$1"
    run_cmd launchctl unload "$plist" >/dev/null 2>&1 || true
    run_cmd launchctl load -w "$plist"
}

install_services_macos() {
    write_launchd_plists
    load_launchd_service "${HOME}/Library/LaunchAgents/io.tabura.codex-app-server.plist"
    load_launchd_service "${HOME}/Library/LaunchAgents/io.tabura.piper-tts.plist"
    load_launchd_service "${HOME}/Library/LaunchAgents/io.tabura.web.plist"
}

open_browser() {
    local url
    url="http://127.0.0.1:8420"
    if [ "$SKIP_BROWSER" = "1" ]; then
        log "skipping browser open due to TABURA_INSTALL_SKIP_BROWSER=1"
        return
    fi
    if [ "$DRY_RUN" = "1" ]; then
        log "[dry-run] open ${url}"
        return
    fi
    if [ "$TABURA_OS" = "darwin" ] && have_cmd open; then
        open "$url" >/dev/null 2>&1 || true
        return
    fi
    if [ "$TABURA_OS" = "linux" ] && have_cmd xdg-open; then
        xdg-open "$url" >/dev/null 2>&1 || true
        return
    fi
    log "open your browser at ${url}"
}

print_summary() {
    local version="$1"
    cat <<SUMMARY

Install complete
  Version:       ${version}
  Binary:        ${BIN_PATH}
  Data root:     ${DATA_ROOT}
  Project dir:   ${PROJECT_DIR}
  Piper models:  ${MODEL_DIR}
  Piper venv:    ${VENV_DIR}
  Service mode:  ${TABURA_OS}
  Web URL:       http://127.0.0.1:8420
SUMMARY
}

remove_linux_services() {
    local systemd_dir
    systemd_dir="${HOME}/.config/systemd/user"
    if have_cmd systemctl; then
        run_cmd systemctl --user disable --now tabura-web.service tabura-piper-tts.service tabura-codex-app-server.service >/dev/null 2>&1 || true
        run_cmd systemctl --user daemon-reload >/dev/null 2>&1 || true
    fi
    run_cmd rm -f "${systemd_dir}/tabura-web.service" "${systemd_dir}/tabura-piper-tts.service" "${systemd_dir}/tabura-codex-app-server.service"
}

remove_macos_services() {
    local agent_dir
    agent_dir="${HOME}/Library/LaunchAgents"
    run_cmd launchctl unload "${agent_dir}/io.tabura.web.plist" >/dev/null 2>&1 || true
    run_cmd launchctl unload "${agent_dir}/io.tabura.piper-tts.plist" >/dev/null 2>&1 || true
    run_cmd launchctl unload "${agent_dir}/io.tabura.codex-app-server.plist" >/dev/null 2>&1 || true
    run_cmd rm -f "${agent_dir}/io.tabura.web.plist" "${agent_dir}/io.tabura.piper-tts.plist" "${agent_dir}/io.tabura.codex-app-server.plist"
}

uninstall_flow() {
    resolve_platform
    resolve_paths
    log "starting uninstall"
    if [ "$TABURA_OS" = "darwin" ]; then
        remove_macos_services
    else
        remove_linux_services
    fi
    run_cmd rm -f "$BIN_PATH"
    if confirm_default_yes "Remove ${DATA_ROOT} data directory?"; then
        run_cmd rm -rf "$DATA_ROOT"
    fi
    log "uninstall complete"
}

install_flow() {
    local release_json tmpdir installed_tag
    resolve_platform
    resolve_paths
    require_base_tools
    require_codex_app_server
    require_python_310
    ensure_ffmpeg

    tmpdir="$(mktemp -d -t tabura-install-XXXXXX)"
    trap "rm -rf '$tmpdir'" EXIT

    release_json="$(fetch_release_json)"
    installed_tag="$(download_release_payload "$release_json" "$tmpdir")"
    install_binary_payload "$tmpdir"
    bootstrap_project
    setup_piper_tts
    install_voxtype
    if [ "$TABURA_OS" = "darwin" ]; then
        install_services_macos
    else
        install_services_linux
    fi
    open_browser
    print_summary "$installed_tag"
}

main() {
    parse_args "$@"
    if [ "$DO_UNINSTALL" = "1" ]; then
        uninstall_flow
        exit 0
    fi
    install_flow
}

main "$@"
