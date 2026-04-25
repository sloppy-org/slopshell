#!/usr/bin/env bash
# Build and install the slsh terminal client into the user's PATH.
# Idempotent; safe to run repeatedly.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${SLOPSHELL_BIN_DIR:-${HOME}/.local/bin}"
BIN_PATH="${BIN_DIR}/slsh"

command -v go >/dev/null 2>&1 || { echo "go toolchain not found in PATH" >&2; exit 1; }

mkdir -p "$BIN_DIR"
(cd "$REPO_ROOT" && go build -o "$BIN_PATH" ./cmd/slsh)
chmod +x "$BIN_PATH"

if ! printf ':%s:' "$PATH" | grep -Fq ":${BIN_DIR}:"; then
  echo "warning: ${BIN_DIR} is not in PATH; add it to your shell profile" >&2
fi

echo "installed slsh -> ${BIN_PATH}"
