#!/usr/bin/env bash
set -euo pipefail

LOCK_FILE="${XDG_RUNTIME_DIR:-/tmp}/tabula-dev-reload.lock"
INCLUDE_PTYD="${1:-}"
mkdir -p "$(dirname "$LOCK_FILE")"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  exit 0
fi

# Coalesce rapid save bursts.
sleep 0.35

if [[ "$INCLUDE_PTYD" == "--include-ptyd" ]]; then
  systemctl --user restart tabula-ptyd.service
fi
systemctl --user restart tabula-codex-app-server.service
systemctl --user restart tabula-mcp.service
systemctl --user restart tabula-web.service
