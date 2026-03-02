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
UNIT_GO_TEST_LOG="${COVERAGE_DIR}/go-test.log"

TABURA_COVERAGE_MIN_TOTAL="${TABURA_COVERAGE_MIN_TOTAL:-45.0}"
TABURA_COVERAGE_MIN_PACKAGES="${TABURA_COVERAGE_MIN_PACKAGES:-}"

printf '\n[reports] Generating Tabura Go coverage...\n'
(
  cd "${ROOT_DIR}"
  go test $(go list ./... | grep -v /tests/services) -covermode=atomic -coverprofile="${TABURA_PROFILE}" | tee "${UNIT_GO_TEST_LOG}"
)
TABURA_TOTAL="$(go tool cover -func="${TABURA_PROFILE}" | awk '/^total:/ {print $3}')"
go tool cover -html="${TABURA_PROFILE}" -o "${TABURA_HTML}"

TABURA_TOTAL_NUM="$(printf '%s' "${TABURA_TOTAL}" | tr -d '%')"
COVERAGE_FAILURES=0

if ! awk -v total="${TABURA_TOTAL_NUM}" -v min="${TABURA_COVERAGE_MIN_TOTAL}" 'BEGIN { exit (total+0 >= min+0 ? 0 : 1) }'; then
  echo "[reports] coverage gate failed: total ${TABURA_TOTAL} < ${TABURA_COVERAGE_MIN_TOTAL}%"
  COVERAGE_FAILURES=$((COVERAGE_FAILURES + 1))
fi

if [[ -n "${TABURA_COVERAGE_MIN_PACKAGES}" ]]; then
  IFS=',' read -r -a pkg_rules <<< "${TABURA_COVERAGE_MIN_PACKAGES}"
  for rule in "${pkg_rules[@]}"; do
    rule="$(echo "${rule}" | xargs)"
    [[ -z "${rule}" ]] && continue
    pkg="${rule%%=*}"
    min="${rule#*=}"
    if [[ "${pkg}" == "${min}" ]]; then
      echo "[reports] invalid package coverage rule '${rule}' (expected package=min)"
      COVERAGE_FAILURES=$((COVERAGE_FAILURES + 1))
      continue
    fi
    pkg_cov="$(
      awk -v target="${pkg}" '
        $2 == target && /coverage:/ {
          for (i = 1; i <= NF; i++) {
            if ($i == "coverage:") {
              gsub("%", "", $(i+1))
              print $(i+1)
              exit
            }
          }
        }
      ' "${UNIT_GO_TEST_LOG}"
    )"
    if [[ -z "${pkg_cov}" ]]; then
      echo "[reports] coverage gate failed: package ${pkg} not found in go test output"
      COVERAGE_FAILURES=$((COVERAGE_FAILURES + 1))
      continue
    fi
    if ! awk -v cov="${pkg_cov}" -v min="${min}" 'BEGIN { exit (cov+0 >= min+0 ? 0 : 1) }'; then
      echo "[reports] coverage gate failed: package ${pkg} ${pkg_cov}% < ${min}%"
      COVERAGE_FAILURES=$((COVERAGE_FAILURES + 1))
    fi
  done
fi

PLAY_JSON="${E2E_DIR}/playwright-summary.json"
PLAY_LOG="${E2E_DIR}/playwright.log"
PLAY_REPORT_DIR="${E2E_DIR}/playwright-report"
PLAY_RESULTS_DIR="${E2E_DIR}/test-results"
E2E_SUMMARY="${E2E_DIR}/summary.txt"

rm -rf "${PLAY_REPORT_DIR}" "${PLAY_RESULTS_DIR}"
mkdir -p "${PLAY_REPORT_DIR}" "${PLAY_RESULTS_DIR}"

printf '\n[reports] Running Playwright E2E suite...\n'
PLAY_EXIT=0
(
  cd "${ROOT_DIR}"
  PLAYWRIGHT_HTML_REPORT="${PLAY_REPORT_DIR}" \
    ./scripts/playwright.sh --config=playwright.config.ts --output="${PLAY_RESULTS_DIR}" --reporter=json \
    > "${PLAY_JSON}" 2> "${PLAY_LOG}"
) || PLAY_EXIT=$?

if [[ "${PLAY_EXIT}" -ne 0 ]]; then
  printf '[reports] Playwright exited with code %d\n' "${PLAY_EXIT}"
  if [[ -s "${PLAY_LOG}" ]]; then
    printf '[reports] Playwright stderr (last 40 lines):\n'
    tail -40 "${PLAY_LOG}"
  fi
fi

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
  </ul>
  <p>See <a href="summary.txt">summary.txt</a> for text summary.</p>
</body>
</html>
EOF_HTML

cat > "${UNIT_SUMMARY}" <<EOF_SUMMARY
Unit Coverage Summary
Generated at: $(date -u +"%Y-%m-%dT%H:%M:%SZ")

Tabura total: ${TABURA_TOTAL}
Coverage min total: ${TABURA_COVERAGE_MIN_TOTAL}%
Coverage min packages: ${TABURA_COVERAGE_MIN_PACKAGES}
Coverage gate failures: ${COVERAGE_FAILURES}
Tabura profile: ${TABURA_PROFILE}
Tabura html: ${TABURA_HTML}
Go test log: ${UNIT_GO_TEST_LOG}

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

if [[ "${COVERAGE_FAILURES}" -gt 0 ]]; then
  echo "[reports] failing due to coverage gate violations"
  exit 1
fi

if [[ "${PLAY_EXIT}" -ne 0 ]]; then
  echo "[reports] failing due to Playwright exit code ${PLAY_EXIT}"
  exit 1
fi
