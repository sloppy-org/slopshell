#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
CLI="${SCRIPT_DIR}/lmstudio-cli.sh"
DOWNLOAD_QUERY="${TABURA_LMSTUDIO_DOWNLOAD_QUERY:-qwen3.5-9b@q4_k_m}"
MODEL_KEY="${TABURA_LMSTUDIO_MODEL_KEY:-qwen/qwen3.5-9b}"
HOST="${TABURA_LMSTUDIO_HOST:-127.0.0.1}"
PORT="${TABURA_LMSTUDIO_PORT:-1234}"
ASSUME_YES="${TABURA_ASSUME_YES:-0}"

log() { printf '[tabura-lmstudio] %s\n' "$*"; }
fail() { printf '[tabura-lmstudio] ERROR: %s\n' "$*" >&2; exit 1; }

confirm_default_yes() {
    local prompt="$1"
    if [ "${ASSUME_YES}" = "1" ] || [ ! -t 0 ]; then
        return 0
    fi
    local response
    read -r -p "${prompt} [Y/n] " response
    case "$response" in
        "" | [Yy] | [Yy][Ee][Ss]) return 0 ;;
        *) return 1 ;;
    esac
}

install_lmstudio_if_needed() {
    if [ -x "${HOME}/.lmstudio/bin/lms" ] || command -v lm-studio >/dev/null 2>&1; then
        return
    fi
    case "$(uname -s)" in
        Linux)
            if command -v yay >/dev/null 2>&1; then
                confirm_default_yes "Install LM Studio via yay (lmstudio-bin)?" || fail "LM Studio is required"
                yay -S --noconfirm lmstudio-bin
                return
            fi
            if command -v paru >/dev/null 2>&1; then
                confirm_default_yes "Install LM Studio via paru (lmstudio-bin)?" || fail "LM Studio is required"
                paru -S --noconfirm lmstudio-bin
                return
            fi
            fail "LM Studio is required. Install lmstudio-bin with yay/paru."
            ;;
        Darwin)
            command -v brew >/dev/null 2>&1 || fail "Homebrew is required to install LM Studio on macOS"
            confirm_default_yes "Install LM Studio via Homebrew cask?" || fail "LM Studio is required"
            brew install --cask lm-studio
            ;;
        *)
            fail "unsupported platform: $(uname -s)"
            ;;
    esac
}

verify_no_thinking() {
    local response
    response="$(
        curl -sS "http://${HOST}:${PORT}/v1/chat/completions" \
            -H 'Content-Type: application/json' \
            -d "{\"model\":\"${MODEL_KEY}\",\"messages\":[{\"role\":\"user\",\"content\":\"Reply with exactly: hello\"}],\"max_tokens\":64,\"temperature\":0,\"chat_template_kwargs\":{\"enable_thinking\":false}}"
    )" || return 1
    if printf '%s' "${response}" | grep -Eq '"content"\s*:\s*"hello"'; then
        return 0
    fi
    return 1
}

install_lmstudio_if_needed
"${CLI}" get "${DOWNLOAD_QUERY}" --gguf -y
"${SCRIPT_DIR}/lmstudio-session.sh"
"${SCRIPT_DIR}/setup-codex-qwen-profile.sh"

log "Verifying no-thinking mode through the local server"
if verify_no_thinking; then
    log "LM Studio is serving ${MODEL_KEY} on http://${HOST}:${PORT} with clean no-thinking responses."
    exit 0
fi

cat >&2 <<EOF
[tabura-lmstudio] ERROR: The local LM Studio server is up, but Qwen 3.5 is still emitting reasoning text.
[tabura-lmstudio] In the LM Studio UI open Qwen 3.5 9B -> settings/config -> set "Enable Thinking" to off, then rerun:
[tabura-lmstudio]   ${SCRIPT_DIR}/lmstudio-session.sh
EOF
exit 1
