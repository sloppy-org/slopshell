#!/usr/bin/env bash
set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_OWNER="${TABURA_REPO_OWNER:-krystophny}"
REPO_NAME="${TABURA_REPO_NAME:-tabura}"
RELEASE_API_BASE="${TABURA_RELEASE_API_BASE:-https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases}"
ASSUME_YES="${TABURA_ASSUME_YES:-0}"
DRY_RUN="${TABURA_INSTALL_DRY_RUN:-0}"
SKIP_BROWSER="${TABURA_INSTALL_SKIP_BROWSER:-0}"
SKIP_STT="${TABURA_INSTALL_SKIP_STT:-0}"
SKIP_LLM="${TABURA_INSTALL_SKIP_LLM:-0}"
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
LMSTUDIO_SETUP_SCRIPT=""
LMSTUDIO_CLI_SCRIPT=""
LMSTUDIO_SESSION_SCRIPT=""
STT_SETUP_SCRIPT=""
CODEX_PATH=""
REUSE_LLM_URL=""
AUTOSTART_DIR=""
AUTOSTART_FILE=""

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
  TABURA_INSTALL_SKIP_STT=1
  TABURA_INSTALL_SKIP_LLM=1
  TABURA_INTENT_LLM_URL=<url>   Reuse an existing local LLM (skip LM Studio setup)
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
    LMSTUDIO_SETUP_SCRIPT="${SCRIPT_DIR}/setup-tabura-lmstudio.sh"
    LMSTUDIO_CLI_SCRIPT="${SCRIPT_DIR}/lmstudio-cli.sh"
    LMSTUDIO_SESSION_SCRIPT="${SCRIPT_DIR}/lmstudio-session.sh"
    STT_SETUP_SCRIPT="${SCRIPT_DIR}/setup-voxtype-stt.sh"
    AUTOSTART_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/autostart"
    AUTOSTART_FILE="${AUTOSTART_DIR}/tabura-lmstudio.desktop"
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
        for name in setup-tabura-lmstudio.sh setup-codex-qwen-profile.sh lmstudio-cli.sh lmstudio-session.sh; do
            if [ -f "scripts/${name}" ]; then
                cp "scripts/${name}" "${tmpdir}/${name}"
                chmod +x "${tmpdir}/${name}"
            fi
        done
        if [ -f "scripts/setup-voxtype-stt.sh" ]; then
            cp "scripts/setup-voxtype-stt.sh" "${tmpdir}/setup-voxtype-stt.sh"
        else
            echo "#!/usr/bin/env bash" >"${tmpdir}/setup-voxtype-stt.sh"
        fi
        chmod +x "${tmpdir}/setup-voxtype-stt.sh"
        if [ -f "scripts/build-voxtype-macos.sh" ]; then
            cp "scripts/build-voxtype-macos.sh" "${tmpdir}/build-voxtype-macos.sh"
            chmod +x "${tmpdir}/build-voxtype-macos.sh"
        fi
        if [ -d "deploy/launchd" ]; then
            mkdir -p "${tmpdir}/deploy/launchd"
            cp deploy/launchd/*.plist "${tmpdir}/deploy/launchd/"
        fi
        if [ -d "deploy/autostart" ]; then
            mkdir -p "${tmpdir}/deploy/autostart"
            cp deploy/autostart/*.desktop "${tmpdir}/deploy/autostart/"
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
    for name in setup-tabura-lmstudio.sh setup-codex-qwen-profile.sh lmstudio-cli.sh lmstudio-session.sh; do
        if [ -f "${tmpdir}/scripts/${name}" ]; then
            cp "${tmpdir}/scripts/${name}" "${tmpdir}/${name}"
        fi
    done
    if [ -f "${tmpdir}/scripts/setup-voxtype-stt.sh" ]; then
        cp "${tmpdir}/scripts/setup-voxtype-stt.sh" "${tmpdir}/setup-voxtype-stt.sh"
    fi
    if [ -f "${tmpdir}/scripts/build-voxtype-macos.sh" ]; then
        cp "${tmpdir}/scripts/build-voxtype-macos.sh" "${tmpdir}/build-voxtype-macos.sh"
    fi
    printf '%s\n' "$tag"
}

install_binary_payload() {
    local staging_dir="$1"
    run_cmd mkdir -p "$BIN_DIR" "$SCRIPT_DIR"
    run_cmd cp "${staging_dir}/tabura" "$BIN_PATH"
    run_cmd chmod +x "$BIN_PATH"
    run_cmd cp "${staging_dir}/piper_tts_server.py" "$PIPER_SERVER_SCRIPT"
    for name in setup-tabura-lmstudio.sh setup-codex-qwen-profile.sh lmstudio-cli.sh lmstudio-session.sh; do
        if [ -f "${staging_dir}/${name}" ]; then
            run_cmd cp "${staging_dir}/${name}" "${SCRIPT_DIR}/${name}"
            run_cmd chmod +x "${SCRIPT_DIR}/${name}"
        fi
    done
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

setup_local_lmstudio() {
    if [ "$SKIP_LLM" = "1" ]; then
        log "skipping local LM Studio due to TABURA_INSTALL_SKIP_LLM=1"
        return
    fi

    if [ -n "${TABURA_INTENT_LLM_URL:-}" ]; then
        REUSE_LLM_URL="$TABURA_INTENT_LLM_URL"
        log "TABURA_INTENT_LLM_URL set to ${REUSE_LLM_URL}; skipping local LM Studio setup"
        return
    fi

    local existing_url
    if existing_url="$(detect_local_llm)"; then
        log "existing local LLM detected at ${existing_url}"
        if confirm_default_yes "Reuse existing local LLM at ${existing_url}?"; then
            REUSE_LLM_URL="$existing_url"
            log "TABURA_INTENT_LLM_URL will point to ${REUSE_LLM_URL}"
            return
        fi
    fi

    cat <<NOTICE
=== LM Studio local runtime (Qwen3.5 9B GGUF, optional) ===
Tabura can reuse LM Studio's desktop app and local server on port 1234.
Default model: qwen/qwen3.5-9b GGUF Q4_K_M
Requirement: LM Studio desktop app plus the Enable Thinking toggle turned off in the model config.
NOTICE
    if ! confirm_default_yes "Install or configure LM Studio local runtime?"; then
        log "skipping local LM Studio setup"
        return
    fi

    local staging_llm="${1:-}"
    run_cmd mkdir -p "$SCRIPT_DIR"
    for name in setup-tabura-lmstudio.sh lmstudio-cli.sh lmstudio-session.sh; do
        if [ -n "$staging_llm" ] && [ -f "${staging_llm}/${name}" ]; then
            run_cmd cp "${staging_llm}/${name}" "${SCRIPT_DIR}/${name}"
            run_cmd chmod +x "${SCRIPT_DIR}/${name}"
        fi
    done
    if [ "$DRY_RUN" = "0" ]; then
        "${LMSTUDIO_SETUP_SCRIPT}"
    fi
}

install_voxtype_stt() {
    if [ "$SKIP_STT" = "1" ]; then
        log "skipping voxtype STT setup due to TABURA_INSTALL_SKIP_STT=1"
        return
    fi
    cat <<NOTICE
=== Voxtype STT (MIT, runs as HTTP sidecar) ===
voxtype provides local OpenAI-compatible speech-to-text on port 8427.
License: MIT (isolated sidecar process, does not affect Tabura MIT license)
Model: large-v3-turbo (~1.5 GB download from Hugging Face via voxtype)
NOTICE
    if ! confirm_default_yes "Install voxtype STT sidecar?"; then
        log "skipping voxtype STT setup"
        return
    fi

    if have_cmd voxtype; then
        log "voxtype already installed"
    elif [ "$TABURA_OS" = "linux" ] && have_cmd pacman; then
        if confirm_default_yes "Install voxtype via AUR (voxtype-bin, fallback voxtype)?"; then
            if have_cmd paru; then
                run_cmd paru -S --noconfirm voxtype-bin || run_cmd paru -S --noconfirm voxtype
            elif have_cmd yay; then
                run_cmd yay -S --noconfirm voxtype-bin || run_cmd yay -S --noconfirm voxtype
            else
                log "no AUR helper found (paru/yay); install voxtype manually"
            fi
        fi
    elif [ "$TABURA_OS" = "darwin" ]; then
        if have_cmd brew && brew info voxtype >/dev/null 2>&1; then
            if confirm_default_yes "Install voxtype via Homebrew?"; then
                run_cmd brew install voxtype
            fi
        elif have_cmd cargo && have_cmd cmake; then
            log "No Homebrew formula for voxtype; building from source"
            if confirm_default_yes "Build voxtype from source (Rust + cmake)?"; then
                local staging_build="${1:-}"
                local build_script=""
                if [ -n "$staging_build" ] && [ -f "${staging_build}/build-voxtype-macos.sh" ]; then
                    build_script="${staging_build}/build-voxtype-macos.sh"
                elif [ -f "scripts/build-voxtype-macos.sh" ]; then
                    build_script="scripts/build-voxtype-macos.sh"
                fi
                if [ -n "$build_script" ]; then
                    run_cmd bash "$build_script" --yes
                else
                    log "build script not available; build manually:"
                    log "  git clone --branch feature/single-daemon-openai-stt-api https://github.com/peteonrails/voxtype.git"
                    log "  see: https://github.com/krystophny/tabura#voxtype-stt"
                fi
            fi
        else
            log "voxtype not found; to build from source install Rust and cmake:"
            log "  brew install cmake && curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh"
            log "  then run: scripts/build-voxtype-macos.sh"
        fi
    else
        log "voxtype not found; install voxtype and ensure it is on PATH"
    fi

    if have_cmd voxtype; then
        if confirm_default_yes "Download voxtype model large-v3-turbo (~1.5 GB)?"; then
            run_cmd voxtype setup --download --model large-v3-turbo --no-post-install
        fi
    else
        log "voxtype was not installed; speech-to-text remains unavailable"
    fi

    local staging_stt="${1:-}"
    if [ -n "$staging_stt" ] && [ -f "${staging_stt}/setup-voxtype-stt.sh" ]; then
        run_cmd mkdir -p "$SCRIPT_DIR"
        run_cmd cp "${staging_stt}/setup-voxtype-stt.sh" "$STT_SETUP_SCRIPT"
        run_cmd chmod +x "$STT_SETUP_SCRIPT"
    fi
}

write_systemd_units() {
    local systemd_dir
    systemd_dir="${HOME}/.config/systemd/user"

    if [ "$DRY_RUN" = "1" ]; then
        log "[dry-run] write systemd units under ${systemd_dir}"
        return
    fi

    run_cmd mkdir -p "$systemd_dir"

    cat >"${systemd_dir}/tabura-codex-app-server.service" <<UNIT
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

    cat >"${systemd_dir}/tabura-piper-tts.service" <<UNIT
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

    if [ -x "$STT_SETUP_SCRIPT" ]; then
        cat >"${systemd_dir}/tabura-stt.service" <<UNIT
[Unit]
Description=Tabura STT (voxtype)
After=network.target

[Service]
Type=simple
Environment=TABURA_STT_LANGUAGE=de,en
Environment=TABURA_STT_MODEL=large-v3-turbo
ExecStart=${STT_SETUP_SCRIPT}
Restart=on-failure
RestartSec=5
TimeoutStopSec=15

[Install]
WantedBy=default.target
UNIT
    fi

    local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:1234}"
    local web_host="${TABURA_WEB_HOST:-127.0.0.1}"

    cat >"${systemd_dir}/tabura-web.service" <<UNIT
[Unit]
Description=Tabura Web UI
After=network.target tabura-codex-app-server.service tabura-piper-tts.service
Wants=tabura-codex-app-server.service tabura-piper-tts.service

[Service]
Type=simple
Environment=TABURA_INTENT_LLM_URL=${effective_llm_url}
Environment=TABURA_INTENT_LLM_MODEL=qwen/qwen3.5-9b
Environment=TABURA_INTENT_LLM_PROFILE=qwen3.5-9b
Environment=TABURA_INTENT_LLM_PROFILE_OPTIONS=qwen3.5-9b,qwen3.5-4b
ExecStart=${BIN_PATH} server --project-dir ${PROJECT_DIR} --data-dir ${WEB_DATA_DIR} --web-host ${web_host} --web-port 8420 --mcp-host 127.0.0.1 --mcp-port 9420 --app-server-url ws://127.0.0.1:8787 --tts-url http://127.0.0.1:8424
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
UNIT
}

install_services_linux() {
    local -a units
    have_cmd systemctl || fail "systemctl is required for Linux service setup"
    write_systemd_units
    run_cmd systemctl --user daemon-reload
    units=(tabura-codex-app-server.service tabura-piper-tts.service tabura-web.service)
    if [ -f "${HOME}/.config/systemd/user/tabura-stt.service" ]; then
        units+=(tabura-stt.service)
    fi
    run_cmd systemctl --user enable --now "${units[@]}"
    if should_manage_local_lmstudio; then
        if [ "$DRY_RUN" = "1" ]; then
            log "[dry-run] install LM Studio autostart at ${AUTOSTART_FILE}"
            return
        fi
        run_cmd mkdir -p "${AUTOSTART_DIR}"
        cat >"${AUTOSTART_FILE}" <<DESKTOP
[Desktop Entry]
Type=Application
Version=1.0
Name=Tabura LM Studio
Comment=Start LM Studio, its local API server, and preload the Tabura model
Exec=${LMSTUDIO_SESSION_SCRIPT}
Terminal=false
X-GNOME-Autostart-enabled=true
DESKTOP
    fi
}

substitute_launchd_template() {
    local src="$1" dst="$2"
    local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:1234}"
    local web_host="${TABURA_WEB_HOST:-127.0.0.1}"
    sed \
        -e "s|@@BIN_PATH@@|${BIN_PATH}|g" \
        -e "s|@@CODEX_PATH@@|${CODEX_PATH}|g" \
        -e "s|@@PROJECT_DIR@@|${PROJECT_DIR}|g" \
        -e "s|@@WEB_DATA_DIR@@|${WEB_DATA_DIR}|g" \
        -e "s|@@TABURA_WEB_HOST@@|${web_host}|g" \
        -e "s|@@VENV_DIR@@|${VENV_DIR}|g" \
        -e "s|@@SCRIPT_DIR@@|${SCRIPT_DIR}|g" \
        -e "s|@@PIPER_MODEL_DIR@@|${MODEL_DIR}|g" \
        -e "s|@@LMSTUDIO_SESSION_SCRIPT@@|${LMSTUDIO_SESSION_SCRIPT}|g" \
        -e "s|@@STT_SETUP_SCRIPT@@|${STT_SETUP_SCRIPT}|g" \
        -e "s|@@TABURA_INTENT_LLM_URL@@|${effective_llm_url}|g" \
        "$src" >"$dst"
}

write_launchd_plists() {
    local staging_dir="$1"
    local agent_dir template_dir
    agent_dir="${HOME}/Library/LaunchAgents"
    template_dir="${staging_dir}/deploy/launchd"

    if [ "$DRY_RUN" = "1" ]; then
        log "[dry-run] write launchd plists under ${agent_dir}"
        return
    fi

    run_cmd mkdir -p "$agent_dir"

    [ -d "$template_dir" ] || fail "launchd templates not found in ${template_dir}"

    substitute_launchd_template "${template_dir}/io.tabura.codex-app-server.plist" "${agent_dir}/io.tabura.codex-app-server.plist"
    substitute_launchd_template "${template_dir}/io.tabura.piper-tts.plist" "${agent_dir}/io.tabura.piper-tts.plist"

    if [ -x "$LMSTUDIO_SESSION_SCRIPT" ] && [ -z "$REUSE_LLM_URL" ] && [ "${TABURA_INTENT_LLM_URL:-}" != "off" ]; then
        substitute_launchd_template "${template_dir}/io.tabura.lmstudio.plist" "${agent_dir}/io.tabura.lmstudio.plist"
    fi

    if [ -x "$STT_SETUP_SCRIPT" ]; then
        substitute_launchd_template "${template_dir}/io.tabura.stt.plist" "${agent_dir}/io.tabura.stt.plist"
    fi

    substitute_launchd_template "${template_dir}/io.tabura.web.plist" "${agent_dir}/io.tabura.web.plist"
}

load_launchd_service() {
    local plist="$1"
    run_cmd launchctl unload "$plist" >/dev/null 2>&1 || true
    run_cmd launchctl load -w "$plist"
}

install_services_macos() {
    local staging_dir="$1"
    local agent_dir
    agent_dir="${HOME}/Library/LaunchAgents"
    write_launchd_plists "$staging_dir"
    load_launchd_service "${agent_dir}/io.tabura.codex-app-server.plist"
    load_launchd_service "${agent_dir}/io.tabura.piper-tts.plist"
    if [ -f "${agent_dir}/io.tabura.lmstudio.plist" ]; then
        load_launchd_service "${agent_dir}/io.tabura.lmstudio.plist"
    fi
    if [ -f "${agent_dir}/io.tabura.stt.plist" ]; then
        load_launchd_service "${agent_dir}/io.tabura.stt.plist"
    fi
    load_launchd_service "${agent_dir}/io.tabura.web.plist"
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
    local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:1234}"
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
  Intent LLM:    ${effective_llm_url}
SUMMARY
    if [ -n "$REUSE_LLM_URL" ]; then
        log "using existing local LLM at ${REUSE_LLM_URL} (no LM Studio setup created)"
    fi
}

remove_linux_services() {
    local systemd_dir
    systemd_dir="${HOME}/.config/systemd/user"
    if have_cmd systemctl; then
        run_cmd systemctl --user disable --now \
            tabura-web.service tabura-piper-tts.service tabura-codex-app-server.service \
            tabura-stt.service \
            >/dev/null 2>&1 || true
        run_cmd systemctl --user daemon-reload >/dev/null 2>&1 || true
    fi
    run_cmd rm -f "${AUTOSTART_FILE}"
    run_cmd rm -f \
        "${systemd_dir}/tabura-web.service" \
        "${systemd_dir}/tabura-piper-tts.service" \
        "${systemd_dir}/tabura-codex-app-server.service" \
        "${systemd_dir}/tabura-stt.service"
}

remove_macos_services() {
    local agent_dir plist
    agent_dir="${HOME}/Library/LaunchAgents"
    for plist in io.tabura.web io.tabura.stt io.tabura.lmstudio io.tabura.piper-tts io.tabura.codex-app-server; do
        run_cmd launchctl unload "${agent_dir}/${plist}.plist" >/dev/null 2>&1 || true
    done
    run_cmd rm -f \
        "${agent_dir}/io.tabura.web.plist" \
        "${agent_dir}/io.tabura.stt.plist" \
        "${agent_dir}/io.tabura.piper-tts.plist" \
        "${agent_dir}/io.tabura.codex-app-server.plist" \
        "${agent_dir}/io.tabura.lmstudio.plist"
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
    setup_local_lmstudio "$tmpdir"
    install_voxtype_stt "$tmpdir"
    if [ "$TABURA_OS" = "darwin" ]; then
        install_services_macos "$tmpdir"
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
