#!/usr/bin/env bash
# Pre-commit hook: verify all version strings are consistent.
# Install: cp scripts/check-version-consistency.sh .git/hooks/pre-commit
#    or:   git config core.hooksPath githooks
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

extract() {
  local file="$1" pattern="$2"
  grep -m1 "$pattern" "$file" | grep -oP '[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?' | head -1
}

v_zenodo="$(extract .zenodo.json '"version"')"
v_citation="$(extract CITATION.cff '^version:')"
v_cli="$(extract cmd/tabura/main.go 'defaultBinaryVersion')"
v_mcp="$(extract internal/mcp/server.go 'ServerVersion')"
v_web_runtime="$(extract internal/web/server.go 'RuntimeVersion:')"
v_web="$(extract internal/web/server.go '"version":')"
v_appserver="$(extract internal/appserver/client.go '"version":')"
v_appserver_session="$(extract internal/appserver/session.go '"version":')"

files=(
  ".zenodo.json"
  "CITATION.cff"
  "cmd/tabura/main.go"
  "internal/mcp/server.go"
  "internal/web/server.go (RuntimeVersion)"
  "internal/web/server.go"
  "internal/appserver/client.go"
  "internal/appserver/session.go"
)
versions=(
  "$v_zenodo"
  "$v_citation"
  "$v_cli"
  "$v_mcp"
  "$v_web_runtime"
  "$v_web"
  "$v_appserver"
  "$v_appserver_session"
)

canonical="$v_zenodo"
mismatch=0
for i in "${!files[@]}"; do
  if [ "${versions[$i]}" != "$canonical" ]; then
    echo "VERSION MISMATCH: ${files[$i]} has ${versions[$i]} (expected $canonical)" >&2
    mismatch=1
  fi
done

if [ "$mismatch" -eq 1 ]; then
  echo "" >&2
  echo "Run: scripts/bump-version.sh v$canonical" >&2
  exit 1
fi
