#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TARGET_FILE="${ROOT_DIR}/internal/web/chat.go"

require_literal() {
  local pattern="$1"
  if command -v rg >/dev/null 2>&1; then
    if rg -F --quiet -- "$pattern" "$TARGET_FILE"; then
      return 0
    fi
  else
    if grep -F -q -- "$pattern" "$TARGET_FILE"; then
      return 0
    fi
  fi
  {
    echo "prompt-contract check failed: missing literal in ${TARGET_FILE}: ${pattern}" >&2
    exit 1
  }
}

require_literal "Tools available this turn are described in the tools parameter"
require_literal "emit a :::file{path=\"...\"} block"
require_literal "Do not use :::canvas blocks."
require_literal "Chat-first"
require_literal "canvas_artifact_show"
require_literal "[[request_position:"
require_literal "[lang:de]"

echo "prompt-contract check passed"
