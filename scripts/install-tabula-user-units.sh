#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT_SRC="$REPO_ROOT/deploy/systemd/user"
UNIT_DST="$HOME/.config/systemd/user"

mkdir -p "$UNIT_DST"
cp "$UNIT_SRC"/*.service "$UNIT_DST"/

systemctl --user daemon-reload
systemctl --user disable --now tabula-dev-watch.path >/dev/null 2>&1 || true
systemctl --user enable --now tabula-ptyd.service tabula-mcp.service tabula-codex-app-server.service tabula-web.service tabula-dev-watch.service

echo "Installed and enabled: tabula-ptyd, tabula-mcp, tabula-codex-app-server, tabula-web, tabula-dev-watch.service"
