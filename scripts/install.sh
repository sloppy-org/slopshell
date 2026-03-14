#!/usr/bin/env bash
set -euo pipefail

SCRIPT_NAME="$(basename "$0")"
SCRIPT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ -f "${SCRIPT_ROOT}/lib/llama.sh" ]; then
    # shellcheck source=scripts/lib/llama.sh
    source "${SCRIPT_ROOT}/lib/llama.sh"
else
    TABURA_LLAMA_LAST_ERROR=""
    tabura_llama_prepend_library_dirs() { :; }
    tabura_find_llama_server() {
        local candidate
        TABURA_LLAMA_LAST_ERROR=""
        if [ -n "${LLAMA_SERVER_BIN:-}" ]; then
            if [ -x "$LLAMA_SERVER_BIN" ]; then
                printf '%s' "$LLAMA_SERVER_BIN"
                return 0
            fi
            if candidate="$(command -v "$LLAMA_SERVER_BIN" 2>/dev/null)"; then
                printf '%s' "$candidate"
                return 0
            fi
        fi
        if candidate="$(command -v llama-server 2>/dev/null)"; then
            printf '%s' "$candidate"
            return 0
        fi
        candidate="${HOME}/.local/llama.cpp/llama-server"
        if [ -x "$candidate" ]; then
            printf '%s' "$candidate"
            return 0
        fi
        TABURA_LLAMA_LAST_ERROR="llama-server not found"
        return 1
    }
fi
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
LLM_DIR=""
LLM_MODEL_DIR=""
LLM_SETUP_SCRIPT=""
STT_SETUP_SCRIPT=""
CODEX_PATH=""
REUSE_LLM_URL=""
LLAMA_SERVER_BIN_RESOLVED=""

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

detect_llama_server() {
    local port url
    for port in 8080 8081 8081; do
        url="http://127.0.0.1:${port}"
        if curl -fsS --max-time 2 "${url}/health" >/dev/null 2>&1; then
            printf '%s' "$url"
            return 0
        fi
    done
    return 1
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
  TABURA_INTENT_LLM_URL=<url>   Reuse an existing llama-server (skip download/service)
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
    LLM_DIR="${DATA_ROOT}/llm"
    LLM_MODEL_DIR="${LLM_DIR}/models"
    LLM_SETUP_SCRIPT="${SCRIPT_DIR}/setup-local-llm.sh"
    STT_SETUP_SCRIPT="${SCRIPT_DIR}/setup-voxtype-stt.sh"
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
        if [ -f "scripts/setup-local-llm.sh" ]; then
            cp "scripts/setup-local-llm.sh" "${tmpdir}/setup-local-llm.sh"
        else
            echo "#!/usr/bin/env bash" >"${tmpdir}/setup-local-llm.sh"
        fi
        chmod +x "${tmpdir}/setup-local-llm.sh"
        if [ -f "scripts/lib/llama.sh" ]; then
            mkdir -p "${tmpdir}/scripts/lib"
            cp "scripts/lib/llama.sh" "${tmpdir}/scripts/lib/llama.sh"
        fi
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
    if [ -f "${tmpdir}/scripts/setup-local-llm.sh" ]; then
        cp "${tmpdir}/scripts/setup-local-llm.sh" "${tmpdir}/setup-local-llm.sh"
    fi
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
    if [ -f "${staging_dir}/scripts/lib/llama.sh" ]; then
        run_cmd mkdir -p "${SCRIPT_DIR}/lib"
        run_cmd cp "${staging_dir}/scripts/lib/llama.sh" "${SCRIPT_DIR}/lib/llama.sh"
    fi
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

configure_codex_cli() {
    local staging_dir="${1:-}"
    local script_path=""
    local fast_url agentic_url

    if [ -n "$staging_dir" ] && [ -f "${staging_dir}/scripts/setup-codex-mcp.sh" ]; then
        script_path="${staging_dir}/scripts/setup-codex-mcp.sh"
    elif [ -f "scripts/setup-codex-mcp.sh" ]; then
        script_path="scripts/setup-codex-mcp.sh"
    fi

    if [ -z "$script_path" ]; then
        log "setup-codex-mcp.sh not available; skipping Codex local provider config"
        return
    fi

    if [ -n "$REUSE_LLM_URL" ]; then
        fast_url="${REUSE_LLM_URL}/v1"
        agentic_url="${REUSE_LLM_URL}/v1"
    else
        fast_url="http://127.0.0.1:8081/v1"
        agentic_url="http://127.0.0.1:8080/v1"
    fi

    if [ "$DRY_RUN" = "1" ]; then
        log "[dry-run] configure Codex MCP and local model profiles via ${script_path}"
        return
    fi

    TABURA_CODEX_FAST_URL="$fast_url" \
    TABURA_CODEX_AGENTIC_URL="$agentic_url" \
    bash "$script_path" "http://127.0.0.1:9420/mcp" >/dev/null
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

ensure_llama_server() {
    if LLAMA_SERVER_BIN_RESOLVED="$(tabura_find_llama_server)"; then
        return 0
    fi
    if [ "$TABURA_OS" = "darwin" ]; then
        if ! have_cmd brew; then
            if [ -n "${TABURA_LLAMA_LAST_ERROR:-}" ] && [ "${TABURA_LLAMA_LAST_ERROR}" != "llama-server not found" ]; then
                log "llama-server not usable: ${TABURA_LLAMA_LAST_ERROR}"
            else
                log "llama-server not found; install llama.cpp via Homebrew: brew install llama.cpp"
            fi
            return 1
        fi
        if confirm_default_yes "Install llama.cpp via Homebrew?"; then
            run_cmd brew install llama.cpp
            if LLAMA_SERVER_BIN_RESOLVED="$(tabura_find_llama_server)"; then
                return 0
            fi
        fi
    else
        if [ -n "${TABURA_LLAMA_LAST_ERROR:-}" ]; then
            log "llama-server not usable: ${TABURA_LLAMA_LAST_ERROR}"
        else
            log "llama-server not found; install llama.cpp and ensure llama-server is on PATH"
        fi
    fi
    return 1
}

setup_local_llm() {
    if [ "$SKIP_LLM" = "1" ]; then
        log "skipping local LLM due to TABURA_INSTALL_SKIP_LLM=1"
        return
    fi

    if [ -n "${TABURA_INTENT_LLM_URL:-}" ]; then
        REUSE_LLM_URL="$TABURA_INTENT_LLM_URL"
        log "TABURA_INTENT_LLM_URL set to ${REUSE_LLM_URL}; skipping LLM setup"
        return
    fi

    local existing_url
    if existing_url="$(detect_llama_server)"; then
        log "existing llama-server detected at ${existing_url}"
        if confirm_default_yes "Reuse existing llama-server at ${existing_url}?"; then
            REUSE_LLM_URL="$existing_url"
            log "TABURA_INTENT_LLM_URL will point to ${REUSE_LLM_URL}"
            return
        fi
    fi

    cat <<NOTICE
=== Local LLMs (llama.cpp, optional) ===
A fast Qwen3.5 9B coordinator runs on port 8081 for Tabura routing and replies.
A Codex-focused gpt-oss-120b runtime runs on port 8080 for local Codex agent profiles.
Requires llama.cpp (llama-server binary).
NOTICE
    if ! confirm_default_yes "Install local LLM service?"; then
        log "skipping local LLM setup"
        return
    fi

    if ! ensure_llama_server; then
        log "skipping local LLM setup"
        return
    fi
    run_cmd mkdir -p "$LLM_MODEL_DIR" "$SCRIPT_DIR"

    local staging_llm="${1:-}"
    if [ -n "$staging_llm" ] && [ -f "${staging_llm}/setup-local-llm.sh" ]; then
        run_cmd cp "${staging_llm}/setup-local-llm.sh" "$LLM_SETUP_SCRIPT"
        run_cmd chmod +x "$LLM_SETUP_SCRIPT"
    fi

    local model_file="Qwen3.5-9B-Q4_K_M.gguf"
    local model_url="https://huggingface.co/lmstudio-community/Qwen3.5-9B-GGUF/resolve/main/Qwen3.5-9B-Q4_K_M.gguf?download=true"
    local model_path="${LLM_MODEL_DIR}/${model_file}"
    if [ -f "$model_path" ]; then
        log "LLM model already present: ${model_file}"
    elif confirm_default_yes "Download Qwen3.5 9B GGUF model (~5.3 GB)?"; then
        if [ "$DRY_RUN" = "1" ]; then
            run_cmd curl -fL -o "$model_path" "$model_url"
        else
            curl -fL --retry 3 --retry-delay 2 -o "${model_path}.tmp" "$model_url"
            mv "${model_path}.tmp" "$model_path"
        fi
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

    if [ -x "$LLM_SETUP_SCRIPT" ] && [ -z "$REUSE_LLM_URL" ]; then
        cat >"${systemd_dir}/tabura-llm.service" <<UNIT
[Unit]
Description=Tabura Local Coordinator LLM (Qwen3.5 9B GGUF)
After=network.target

[Service]
Type=simple
Environment=TABURA_LLM_MODEL_DIR=${LLM_MODEL_DIR}
Environment=TABURA_LLM_MODEL_FILE=Qwen3.5-9B-Q4_K_M.gguf
Environment=TABURA_LLM_MODEL_URL=https://huggingface.co/lmstudio-community/Qwen3.5-9B-GGUF/resolve/main/Qwen3.5-9B-Q4_K_M.gguf?download=true
Environment=LLAMA_SERVER_BIN=${LLAMA_SERVER_BIN_RESOLVED}
ExecStart=${LLM_SETUP_SCRIPT}
Restart=on-failure
RestartSec=5
TimeoutStopSec=15

[Install]
WantedBy=default.target
UNIT

        cat >"${systemd_dir}/tabura-codex-llm.service" <<UNIT
[Unit]
Description=Tabura Local Codex LLM (gpt-oss-120b via llama.cpp)
After=network.target

[Service]
Type=simple
Environment=TABURA_LLM_PRESET=codex-gpt-oss-120b
Environment=LLAMA_SERVER_BIN=${LLAMA_SERVER_BIN_RESOLVED}
ExecStart=${LLM_SETUP_SCRIPT}
Restart=on-failure
RestartSec=5
TimeoutStopSec=15

[Install]
WantedBy=default.target
UNIT
    fi
    if [ -n "$REUSE_LLM_URL" ]; then
        run_cmd rm -f "${systemd_dir}/tabura-llm.service" "${systemd_dir}/tabura-codex-llm.service"
    fi

    local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8081}"
    local web_host="${TABURA_WEB_HOST:-127.0.0.1}"

    cat >"${systemd_dir}/tabura-web.service" <<UNIT
[Unit]
Description=Tabura Web UI
After=network.target tabura-codex-app-server.service tabura-piper-tts.service
Wants=tabura-codex-app-server.service tabura-piper-tts.service

[Service]
Type=simple
Environment=TABURA_INTENT_LLM_URL=${effective_llm_url}
Environment=TABURA_INTENT_LLM_MODEL=local
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
    if [ -n "$REUSE_LLM_URL" ]; then
        run_cmd systemctl --user disable --now tabura-llm.service tabura-codex-llm.service >/dev/null 2>&1 || true
    fi
    units=(tabura-codex-app-server.service tabura-piper-tts.service tabura-web.service)
    if [ -f "${HOME}/.config/systemd/user/tabura-llm.service" ]; then
        units+=(tabura-llm.service)
    fi
    if [ -f "${HOME}/.config/systemd/user/tabura-codex-llm.service" ]; then
        units+=(tabura-codex-llm.service)
    fi
    run_cmd systemctl --user enable --now "${units[@]}"
}

substitute_launchd_template() {
    local src="$1" dst="$2"
    local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8081}"
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
        -e "s|@@LLM_SETUP_SCRIPT@@|${LLM_SETUP_SCRIPT}|g" \
        -e "s|@@LLM_MODEL_DIR@@|${LLM_MODEL_DIR}|g" \
        -e "s|@@LLAMA_SERVER_BIN@@|${LLAMA_SERVER_BIN_RESOLVED}|g" \
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

    if [ -x "$LLM_SETUP_SCRIPT" ] && [ -z "$REUSE_LLM_URL" ]; then
        substitute_launchd_template "${template_dir}/io.tabura.llm.plist" "${agent_dir}/io.tabura.llm.plist"
        substitute_launchd_template "${template_dir}/io.tabura.codex-llm.plist" "${agent_dir}/io.tabura.codex-llm.plist"
    else
        run_cmd launchctl unload "${agent_dir}/io.tabura.llm.plist" >/dev/null 2>&1 || true
        run_cmd launchctl unload "${agent_dir}/io.tabura.codex-llm.plist" >/dev/null 2>&1 || true
        run_cmd rm -f "${agent_dir}/io.tabura.llm.plist" "${agent_dir}/io.tabura.codex-llm.plist"
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
    if [ -f "${agent_dir}/io.tabura.intent.plist" ]; then
        load_launchd_service "${agent_dir}/io.tabura.intent.plist"
    fi
    if [ -f "${agent_dir}/io.tabura.llm.plist" ]; then
        load_launchd_service "${agent_dir}/io.tabura.llm.plist"
    fi
    if [ -f "${agent_dir}/io.tabura.codex-llm.plist" ]; then
        load_launchd_service "${agent_dir}/io.tabura.codex-llm.plist"
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
    local effective_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8081}"
    local effective_codex_llm_url="${REUSE_LLM_URL:-http://127.0.0.1:8080}"
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
  Intent LLM:   ${effective_llm_url}
  Codex LLM:    ${effective_codex_llm_url}
SUMMARY
    if [ -n "$REUSE_LLM_URL" ]; then
        log "using existing llama-server at ${REUSE_LLM_URL} (no tabura-llm service created)"
    fi
}

remove_linux_services() {
    local systemd_dir
    systemd_dir="${HOME}/.config/systemd/user"
    if have_cmd systemctl; then
        run_cmd systemctl --user disable --now \
            tabura-web.service tabura-piper-tts.service tabura-codex-app-server.service \
            tabura-llm.service tabura-codex-llm.service >/dev/null 2>&1 || true
        run_cmd systemctl --user daemon-reload >/dev/null 2>&1 || true
    fi
    run_cmd rm -f \
        "${systemd_dir}/tabura-web.service" \
        "${systemd_dir}/tabura-piper-tts.service" \
        "${systemd_dir}/tabura-codex-app-server.service" \
        "${systemd_dir}/tabura-llm.service" \
        "${systemd_dir}/tabura-codex-llm.service"
}

remove_macos_services() {
    local agent_dir plist
    agent_dir="${HOME}/Library/LaunchAgents"
    for plist in io.tabura.web io.tabura.stt io.tabura.llm io.tabura.codex-llm io.tabura.piper-tts io.tabura.codex-app-server; do
        run_cmd launchctl unload "${agent_dir}/${plist}.plist" >/dev/null 2>&1 || true
    done
    run_cmd rm -f \
        "${agent_dir}/io.tabura.web.plist" \
        "${agent_dir}/io.tabura.stt.plist" \
        "${agent_dir}/io.tabura.piper-tts.plist" \
        "${agent_dir}/io.tabura.codex-app-server.plist" \
        "${agent_dir}/io.tabura.llm.plist" \
        "${agent_dir}/io.tabura.codex-llm.plist"
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
    setup_local_llm "$tmpdir"
    install_voxtype_stt "$tmpdir"
    if [ "$TABURA_OS" = "darwin" ]; then
        install_services_macos "$tmpdir"
    else
        install_services_linux
    fi
    configure_codex_cli "$tmpdir"
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
