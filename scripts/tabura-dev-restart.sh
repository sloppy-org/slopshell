#!/usr/bin/env bash
set -euo pipefail

LOCK_FILE="${XDG_RUNTIME_DIR:-/tmp}/tabura-dev-reload.lock"
INCLUDE_PTYD="${1:-}"
mkdir -p "$(dirname "$LOCK_FILE")"
exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  exit 0
fi

# Coalesce rapid save bursts.
sleep 0.35

if [[ "$INCLUDE_PTYD" == "--include-ptyd" ]]; then
  systemctl --user restart tabura-ptyd.service
fi
systemctl --user restart tabura-voxtype-mcp.service
systemctl --user restart tabura-codex-app-server.service
systemctl --user restart tabura-mcp.service
systemctl --user restart tabura-web.service
