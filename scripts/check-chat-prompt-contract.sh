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

require_literal "Use exactly one response shape:"
require_literal "Spoken chat must be one paragraph max."
require_literal "If your response needs more than one paragraph, write that long content to a temp file"
require_literal "Canvas content must appear only inside :::file blocks"
require_literal "For temporary canvas files, create/remove paths via temp_file_create and temp_file_remove tools."
require_literal "Do not use :::canvas blocks."
require_literal "If output needs more than one paragraph, put it in a temp file with temp_file_create"

echo "prompt-contract check passed"
