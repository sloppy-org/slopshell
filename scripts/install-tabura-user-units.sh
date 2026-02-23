#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT_SRC="$REPO_ROOT/deploy/systemd/user"
UNIT_DST="$HOME/.config/systemd/user"

mkdir -p "$UNIT_DST"
cp "$UNIT_SRC"/*.service "$UNIT_DST"/

systemctl --user daemon-reload
systemctl --user disable --now tabura-dev-watch.path >/dev/null 2>&1 || true
systemctl --user enable --now tabura-ptyd.service tabura-codex-app-server.service tabura-piper-tts.service tabura-web.service tabura-dev-watch.service

echo "Installed and enabled: tabura-ptyd, tabura-codex-app-server, tabura-piper-tts, tabura-web, tabura-dev-watch.service"
