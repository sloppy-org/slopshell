#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPORT_ROOT="${ROOT_DIR}/.tabura/artifacts/test-reports"
COVERAGE_DIR="${REPORT_ROOT}/coverage/unit"
E2E_DIR="${REPORT_ROOT}/e2e"

mkdir -p "${COVERAGE_DIR}" "${E2E_DIR}"

for cmd in go node npx; do
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    echo "[reports] missing required command: ${cmd}" >&2
    exit 1
  fi
done

TABURA_PROFILE="${COVERAGE_DIR}/tabura.cover.out"
TABURA_HTML="${COVERAGE_DIR}/tabura.html"
UNIT_INDEX="${COVERAGE_DIR}/index.html"
UNIT_SUMMARY="${COVERAGE_DIR}/summary.txt"

printf '\n[reports] Generating Tabura Go coverage...\n'
(
  cd "${ROOT_DIR}"
  go test ./... -covermode=atomic -coverprofile="${TABURA_PROFILE}"
)
TABURA_TOTAL="$(go tool cover -func="${TABURA_PROFILE}" | awk '/^total:/ {print $3}')"
go tool cover -html="${TABURA_PROFILE}" -o "${TABURA_HTML}"

HELPY_DIR_DEFAULT="${ROOT_DIR}/../helpy"
HELPY_DIR="${HELPY_DIR:-${HELPY_DIR_DEFAULT}}"
HELPY_PROFILE="${COVERAGE_DIR}/helpy.cover.out"
HELPY_HTML="${COVERAGE_DIR}/helpy.html"
HELPY_TOTAL="N/A"
HELPY_NOTE="skipped (Helpy repo not found at ${HELPY_DIR})"
if [[ -d "${HELPY_DIR}" && -f "${HELPY_DIR}/go.mod" ]]; then
  printf '\n[reports] Generating Helpy Go coverage from %s...\n' "${HELPY_DIR}"
  (
    cd "${HELPY_DIR}"
    go test ./... -covermode=atomic -coverprofile="${HELPY_PROFILE}"
  )
  HELPY_TOTAL="$(cd "${HELPY_DIR}" && go tool cover -func="${HELPY_PROFILE}" | awk '/^total:/ {print $3}')"
  (
    cd "${HELPY_DIR}"
    go tool cover -html="${HELPY_PROFILE}" -o "${HELPY_HTML}"
  )
  HELPY_NOTE="generated"
fi

PLAY_JSON="${E2E_DIR}/playwright-summary.json"
PLAY_LOG="${E2E_DIR}/playwright.log"
PLAY_REPORT_DIR="${E2E_DIR}/playwright-report"
PLAY_RESULTS_DIR="${E2E_DIR}/test-results"
E2E_SUMMARY="${E2E_DIR}/summary.txt"

rm -rf "${PLAY_REPORT_DIR}" "${PLAY_RESULTS_DIR}"
mkdir -p "${PLAY_REPORT_DIR}" "${PLAY_RESULTS_DIR}"

printf '\n[reports] Running Playwright E2E suite...\n'
(
  cd "${ROOT_DIR}"
  PLAYWRIGHT_HTML_REPORT="${PLAY_REPORT_DIR}" \
    npx playwright test --config=playwright.config.ts --output="${PLAY_RESULTS_DIR}" --reporter=json \
    > "${PLAY_JSON}" 2> "${PLAY_LOG}"
)

read -r E2E_EXPECTED E2E_UNEXPECTED E2E_SKIPPED E2E_FLAKY E2E_DURATION_MS <<EOF_STATS
$(node -e "const fs=require('fs');const p=process.argv[1];const j=JSON.parse(fs.readFileSync(p,'utf8'));const s=j.stats||{};process.stdout.write([s.expected||0,s.unexpected||0,s.skipped||0,s.flaky||0,s.duration||0].join(' '));" "${PLAY_JSON}")
EOF_STATS

cat > "${E2E_SUMMARY}" <<EOF_E2E
Playwright E2E Summary
Generated at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")

Expected (pass): ${E2E_EXPECTED}
Unexpected (fail): ${E2E_UNEXPECTED}
Skipped: ${E2E_SKIPPED}
Flaky: ${E2E_FLAKY}
Duration (ms): ${E2E_DURATION_MS}

Report dir: ${PLAY_REPORT_DIR}
Raw JSON: ${PLAY_JSON}
Log: ${PLAY_LOG}
EOF_E2E

cat > "${UNIT_INDEX}" <<EOF_HTML
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>Unit Coverage Reports</title>
  <style>
    body { font-family: sans-serif; margin: 2rem; }
    code { background: #f2f2f2; padding: 0.1rem 0.3rem; }
  </style>
</head>
<body>
  <h1>Unit Coverage Reports</h1>
  <p>Generated at <code>$(date -u +"%Y-%m-%dT%H:%M:%SZ")</code></p>
  <ul>
    <li>Tabura total coverage: <strong>${TABURA_TOTAL}</strong> - <a href="tabura.html">tabura.html</a></li>
    <li>Helpy total coverage: <strong>${HELPY_TOTAL}</strong> - ${HELPY_NOTE}</li>
  </ul>
  <p>See <a href="summary.txt">summary.txt</a> for text summary.</p>
</body>
</html>
EOF_HTML

cat > "${UNIT_SUMMARY}" <<EOF_SUMMARY
Unit Coverage Summary
Generated at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")

Tabura total: ${TABURA_TOTAL}
Tabura profile: ${TABURA_PROFILE}
Tabura html: ${TABURA_HTML}

Helpy total: ${HELPY_TOTAL}
Helpy note: ${HELPY_NOTE}
Helpy profile: ${HELPY_PROFILE}
Helpy html: ${HELPY_HTML}

E2E expected: ${E2E_EXPECTED}
E2E unexpected: ${E2E_UNEXPECTED}
E2E skipped: ${E2E_SKIPPED}
E2E flaky: ${E2E_FLAKY}
E2E duration_ms: ${E2E_DURATION_MS}
E2E summary: ${E2E_SUMMARY}
EOF_SUMMARY

printf '\n[reports] Done.\n'
printf '[reports] Unit coverage index: %s\n' "${UNIT_INDEX}"
printf '[reports] Unit summary: %s\n' "${UNIT_SUMMARY}"
printf '[reports] E2E report dir: %s\n' "${PLAY_REPORT_DIR}"
printf '[reports] E2E summary: %s\n\n' "${E2E_SUMMARY}"
