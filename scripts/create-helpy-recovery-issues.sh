#!/usr/bin/env bash
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required" >&2
  exit 1
fi

HELPY_REPO="${HELPY_REPO:-krystophny/helpy-private}"
TABURA_SHA="${TABURA_SHA:-$(git rev-parse --short=12 HEAD)}"

create_issue() {
  local title="$1"
  local body="$2"
  gh issue create --repo "${HELPY_REPO}" --title "${title}" --body "${body}"
}

create_issue \
  "Rebuild email header handoff producer for file-first Tabura" \
  "Context: Tabura monolith simplification removed Helpy/mail coupling.\n\nGoal:\n- Implement Helpy-side producer support for email-header handoff payloads.\n- Keep transport contracts deterministic and versioned.\n\nScope:\n- selector support (provider/folder/limit)\n- handoff policy support (ttl/max_consumes)\n- envelope validation + tests\n\nTabura references:\n- d7c3ef7 (email-header handoff import path)\n- 15e03c2 (deterministic mail action foundation)\n- migration commit: ${TABURA_SHA}\n"

create_issue \
  "Rebuild deterministic message action API profile" \
  "Context: action endpoints were removed from Tabura; functionality should live in Helpy.\n\nGoal:\n- Provide deterministic capabilities + action APIs for open/archive/delete/defer.\n\nScope:\n- provider capability contract\n- action execution contract with explicit provider mode (native/stub)\n- audit trail and error taxonomy\n\nTabura references:\n- 15e03c2\n- migration commit: ${TABURA_SHA}\n"

create_issue \
  "Rebuild draft intent + draft reply flow in Helpy" \
  "Context: draft intent/reply paths were removed from Tabura.\n\nGoal:\n- Own transcript intent classification and draft generation within Helpy.\n\nScope:\n- intent classifier API\n- draft generation API\n- fallback policy behavior + tests\n\nTabura references:\n- 15e03c2\n- 7218a10\n- migration commit: ${TABURA_SHA}\n"

create_issue \
  "Rebuild STT ingestion for voice drafting in Helpy" \
  "Context: Tabura no longer carries Helpy-linked voice draft STT paths.\n\nGoal:\n- Implement STT ingestion and transcript normalization on Helpy side.\n\nScope:\n- MIME/audio-size validation\n- transcript quality/error handling\n- deterministic integration tests\n\nTabura references:\n- 7218a10\n- migration commit: ${TABURA_SHA}\n"

echo "Created Helpy recovery issues in ${HELPY_REPO}"
