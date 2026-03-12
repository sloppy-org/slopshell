#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLATFORM="$(uname -s)"
ARCH="$(uname -m)"
ASSUME_YES="${TABURA_ASSUME_YES:-0}"

log()  { printf '[dev-setup] %s\n' "$*"; }
warn() { printf '[dev-setup] WARNING: %s\n' "$*"; }
fail() { printf '[dev-setup] ERROR: %s\n' "$*" >&2; exit 1; }

print_help() {
    cat <<USAGE
Usage: scripts/dev-setup.sh [options]

Sets up a complete Tabura development environment from a repo checkout.

Steps performed:
  1. Detect platform and architecture
  2. Build tabura binary from source
  3. Set up Piper TTS (venv + voice models)
  4. Detect existing llama-server or prepare LLM service
  5. Check for voxtype (STT)
  6. Install and start service definitions (systemd/launchd)
  7. Bootstrap a default project directory
  8. Print summary with endpoints and log locations

Options:
  --yes       Non-interactive mode (answer yes to all prompts)
  -h, --help  Show this help

Environment:
  TABURA_ASSUME_YES=1         Same as --yes
  TABURA_INTENT_LLM_URL=<url> Reuse an existing llama-server (skip LLM setup)
  TABURA_INTENT_LLM_URL=off   Disable intent LLM entirely
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

# --- Step 1: Platform detection ---

case "$PLATFORM" in
    Linux|Darwin) ;;
    *) fail "unsupported platform: $PLATFORM" ;;
esac
log "Platform: $PLATFORM ($ARCH)"

# --- Step 2: Required prerequisites ---

command -v go >/dev/null 2>&1 || fail "go not found (https://go.dev/dl/)"
command -v python3 >/dev/null 2>&1 || fail "python3 not found"
command -v curl >/dev/null 2>&1 || fail "curl not found"
command -v codex >/dev/null 2>&1 || fail "codex not found (npm install -g @openai/codex)"

# --- Step 3: Build tabura binary ---

BIN_DIR="${HOME}/.local/bin"
BIN_PATH="${BIN_DIR}/tabura"
log "Building tabura binary"
mkdir -p "$BIN_DIR"
(cd "$REPO_ROOT" && go build -o "$BIN_PATH" ./cmd/tabura)
log "Built: $BIN_PATH"

if ! printf ':%s:' "$PATH" | grep -Fq ":${BIN_DIR}:"; then
    warn "${BIN_DIR} is not in PATH; add it to your shell profile"
fi

# --- Step 4: Piper TTS setup ---

log "Setting up Piper TTS"
if "$REPO_ROOT/scripts/setup-tabura-piper-tts.sh"; then
    log "Piper TTS setup complete"
else
    warn "Piper TTS setup failed; TTS will be unavailable"
fi

# --- Step 5: LLM detection ---

if [ -z "${TABURA_INTENT_LLM_URL:-}" ]; then
    for port in 8080 8081 8426; do
        if curl -fsS --max-time 2 "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
            export TABURA_INTENT_LLM_URL="http://127.0.0.1:${port}"
            log "Detected existing llama-server at $TABURA_INTENT_LLM_URL"
            break
        fi
    done
fi

if [ -z "${TABURA_INTENT_LLM_URL:-}" ] && ! command -v llama-server >/dev/null 2>&1; then
    warn "llama-server not found; intent LLM will be disabled"
    if [ "$PLATFORM" = "Darwin" ]; then
        warn "  Install: brew install llama.cpp"
    else
        warn "  Build llama.cpp and place llama-server in ~/.local/bin"
    fi
    export TABURA_INTENT_LLM_URL="off"
fi

# --- Step 6: STT (voxtype) check ---

if ! command -v voxtype >/dev/null 2>&1; then
    warn "voxtype not found; STT will be unavailable"
    if [ "$PLATFORM" = "Darwin" ]; then
        warn "  Install: brew install voxtype"
    else
        warn "  Install: paru -S voxtype-bin"
    fi
fi

# --- Step 7: Install service definitions and start services ---

log "Installing and starting services"
"$REPO_ROOT/scripts/install-tabura-user-units.sh"

# --- Step 8: Bootstrap default project ---

PROJECT_DIR="$REPO_ROOT"
log "Bootstrapping project at $PROJECT_DIR"
"$BIN_PATH" bootstrap --project-dir "$PROJECT_DIR"

# --- Step 9: Summary ---

EFFECTIVE_LLM_URL="${TABURA_INTENT_LLM_URL:-http://127.0.0.1:8426}"
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
  /tmp/tabura-llm.log
  /tmp/tabura-stt.log
LOGS
else
    cat <<LOGS
Logs:
  journalctl --user -u tabura-web.service
  journalctl --user -u tabura-codex-app-server.service
  journalctl --user -u tabura-piper-tts.service
  journalctl --user -u tabura-llm.service
  journalctl --user -u tabura-stt.service
LOGS
fi
