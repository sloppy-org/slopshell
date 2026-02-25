#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT_SRC="$REPO_ROOT/deploy/systemd/user"
UNIT_DST="$HOME/.config/systemd/user"

mkdir -p "$UNIT_DST"
cp "$UNIT_SRC"/*.service "$UNIT_DST"/

systemctl --user daemon-reload
systemctl --user disable --now tabura-dev-watch.path >/dev/null 2>&1 || true
systemctl --user disable --now tabura-mcp.service tabura-voxtype-mcp.service helpy-mcp.service >/dev/null 2>&1 || true
has_unit() {
  systemctl --user list-unit-files "$1" --no-legend >/dev/null 2>&1
}

required_units=(
  tabura-codex-app-server.service
  tabura-piper-tts.service
  tabura-web.service
  tabura-dev-watch.service
)
optional_units=(
  tabura-intent.service
  tabura-llm.service
  tabura-ptyd.service
)

enable_units=()
missing_required=()
skipped_optional=()

for unit in "${required_units[@]}"; do
  if has_unit "$unit"; then
    enable_units+=("$unit")
  else
    missing_required+=("$unit")
  fi
done

if ((${#missing_required[@]} > 0)); then
  echo "Missing required units: ${missing_required[*]}" >&2
  exit 1
fi

for unit in "${optional_units[@]}"; do
  if has_unit "$unit"; then
    enable_units+=("$unit")
  else
    skipped_optional+=("$unit")
  fi
done

systemctl --user enable --now "${enable_units[@]}"

echo "Installed and enabled: ${enable_units[*]}"
if ((${#skipped_optional[@]} > 0)); then
  echo "Skipped optional units (not installed): ${skipped_optional[*]}"
fi
echo "Disabled legacy sidecars: tabura-mcp.service, tabura-voxtype-mcp.service, helpy-mcp.service"
