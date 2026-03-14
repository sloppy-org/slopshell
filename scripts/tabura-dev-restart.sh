#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LOCK_FILE="${XDG_RUNTIME_DIR:-/tmp}/tabura-dev-reload.lock"
INCLUDE_PTYD="${1:-}"
mkdir -p "$(dirname "$LOCK_FILE")"

if command -v flock >/dev/null 2>&1; then
  exec 9>"$LOCK_FILE"
  if ! flock -n 9; then
    exit 0
  fi
else
  if ! mkdir "$LOCK_FILE.d" 2>/dev/null; then
    exit 0
  fi
  trap 'rmdir "$LOCK_FILE.d" 2>/dev/null || true' EXIT
fi

# Coalesce rapid save bursts.
sleep 0.35

# Build frontend and binary (static files are embedded via go:embed).
cd "$REPO_ROOT"
npm run build:frontend --silent 2>/dev/null || true
go build -o tabura ./cmd/tabura

restart_launchd_service() {
  local label="$1"
  local pid
  pid="$(launchctl list | awk -v l="$label" '$3 == l { print $1 }')"
  if [[ -n "$pid" && "$pid" != "-" ]]; then
    kill "$pid" 2>/dev/null || true
    # KeepAlive=true in the plist means launchd restarts it automatically.
    return
  fi
  # Service not loaded — try load.
  local plist="$HOME/Library/LaunchAgents/${label}.plist"
  if [[ -f "$plist" ]]; then
    launchctl load "$plist" 2>/dev/null || true
  fi
}

restart_service() {
  local unit="$1"
  local label="$2"
  if [[ "$(uname)" == "Darwin" ]]; then
    restart_launchd_service "$label"
  else
    if ! systemctl --user list-unit-files "$unit" --no-legend 2>/dev/null | awk '{print $1}' | grep -Fxq "$unit"; then
      return
    fi
    systemctl --user restart "$unit"
  fi
}

if [[ "$INCLUDE_PTYD" == "--include-ptyd" ]]; then
  restart_service tabura-ptyd.service io.tabura.ptyd
fi
restart_service tabura-codex-app-server.service io.tabura.codex-app-server
restart_service tabura-llm.service io.tabura.llm
restart_service tabura-codex-llm.service io.tabura.codex-llm
restart_service tabura-web.service io.tabura.web
