#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLATFORM="$(uname -s)"
ARCH="$(uname -m)"
ASSUME_YES="${TABURA_ASSUME_YES:-0}"

log() { printf '[dev-setup] %s\n' "$*"; }
warn() { printf '[dev-setup] WARNING: %s\n' "$*"; }
fail() { printf '[dev-setup] ERROR: %s\n' "$*" >&2; exit 1; }

print_help() {
    cat <<USAGE
Usage: scripts/dev-setup.sh [options]

Sets up a complete Tabura development environment from a repo checkout.

Steps performed:
  1. Detect platform and architecture
  2. Build the tabura binary from source
  3. Set up Piper TTS
  4. Reuse an existing local LLM or prepare LM Studio + Qwen3.5 9B GGUF
  5. Check for voxtype STT
  6. Install and start service definitions
  7. Bootstrap the repo as the default project
  8. Print endpoints and log locations

Options:
  --yes       Non-interactive mode (answer yes to all prompts)
  -h, --help  Show this help

Environment:
  TABURA_ASSUME_YES=1
  TABURA_INTENT_LLM_URL=<url> Reuse an existing local LLM
  TABURA_INTENT_LLM_URL=off   Disable the local intent runtime
USAGE
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --yes) ASSUME_YES=1; shift ;;
        -h|--help) print_help; exit 0 ;;
        *) fail "unknown argument: $1" ;;
    esac
done

export TABURA_ASSUME_YES="$ASSUME_YES"

case "$PLATFORM" in
    Linux|Darwin) ;;
    *) fail "unsupported platform: $PLATFORM" ;;
esac
log "Platform: $PLATFORM ($ARCH)"

command -v go >/dev/null 2>&1 || fail "go not found (https://go.dev/dl/)"
command -v python3 >/dev/null 2>&1 || fail "python3 not found"
command -v curl >/dev/null 2>&1 || fail "curl not found"
command -v codex >/dev/null 2>&1 || fail "codex not found (npm install -g @openai/codex)"

BIN_DIR="${HOME}/.local/bin"
BIN_PATH="${BIN_DIR}/tabura"
log "Building tabura binary"
mkdir -p "$BIN_DIR"
(cd "$REPO_ROOT" && go build -o "$BIN_PATH" ./cmd/tabura)
log "Built: $BIN_PATH"

if ! printf ':%s:' "$PATH" | grep -Fq ":${BIN_DIR}:"; then
    warn "${BIN_DIR} is not in PATH; add it to your shell profile"
fi

log "Setting up Piper TTS"
if "$REPO_ROOT/scripts/setup-tabura-piper-tts.sh"; then
    log "Piper TTS setup complete"
else
    warn "Piper TTS setup failed; TTS will be unavailable"
fi

if [ -z "${TABURA_INTENT_LLM_URL:-}" ]; then
    for port in 1234 8080 8081 8426; do
        if curl -fsS --max-time 2 "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
            export TABURA_INTENT_LLM_URL="http://127.0.0.1:${port}"
            log "Detected existing local LLM at $TABURA_INTENT_LLM_URL"
            break
        fi
    done
fi

if [ -z "${TABURA_INTENT_LLM_URL:-}" ] && [ "${TABURA_INTENT_LLM_URL:-}" != "off" ]; then
    log "Preparing LM Studio local runtime"
    "$REPO_ROOT/scripts/setup-tabura-lmstudio.sh"
fi

if ! command -v voxtype >/dev/null 2>&1; then
    if [ "$PLATFORM" = "Darwin" ]; then
        command -v cargo >/dev/null 2>&1 || fail "cargo not found; install Rust first"
        command -v cmake >/dev/null 2>&1 || fail "cmake not found; install: brew install cmake"
        log "Building voxtype from source for macOS"
        "$REPO_ROOT/scripts/build-voxtype-macos.sh" --yes
        log "voxtype built and installed successfully"
    else
        fail "voxtype not found; install: paru -S voxtype-bin"
    fi
fi

log "Installing and starting services"
"$REPO_ROOT/scripts/install-tabura-user-units.sh"

PROJECT_DIR="$REPO_ROOT"
log "Bootstrapping project at $PROJECT_DIR"
"$BIN_PATH" bootstrap --project-dir "$PROJECT_DIR"

EFFECTIVE_LLM_URL="${TABURA_INTENT_LLM_URL:-http://127.0.0.1:1234}"
cat <<SUMMARY

=== Tabura Dev Setup Complete ===
  Platform:    $PLATFORM ($ARCH)
  Binary:      $BIN_PATH
  Repo root:   $REPO_ROOT
  Project dir: $PROJECT_DIR

Endpoints:
  Web UI:  http://127.0.0.1:8420
  MCP:     http://127.0.0.1:9420/mcp
  TTS:     http://127.0.0.1:8424/v1/audio/speech
  LLM:     $EFFECTIVE_LLM_URL
  STT:     http://127.0.0.1:8427/v1/audio/transcriptions

SUMMARY

if [ "$PLATFORM" = "Darwin" ]; then
    cat <<LOGS
Log files:
  /tmp/tabura-web.log
  /tmp/tabura-codex-app-server.log
  /tmp/tabura-piper-tts.log
  /tmp/tabura-lmstudio.log
  /tmp/tabura-stt.log
LOGS
else
    cat <<LOGS
Logs:
  journalctl --user -u tabura-web.service
  journalctl --user -u tabura-codex-app-server.service
  journalctl --user -u tabura-piper-tts.service
  journalctl --user -u tabura-stt.service
  /tmp/tabura-lmstudio-server.log
  /tmp/tabura-lmstudio-load.log
LOGS
fi
