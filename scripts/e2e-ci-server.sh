#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_ROOT="$(mktemp -d -t tabura-e2e-ci-XXXXXX)"
DATA_DIR="${TMP_ROOT}/data"
PROJECT_DIR="${E2E_PROJECT_DIR:-$ROOT_DIR}"
WEB_HOST="${E2E_WEB_HOST:-127.0.0.1}"
WEB_PORT="${E2E_WEB_PORT:-8420}"
MCP_HOST="${E2E_MCP_HOST:-127.0.0.1}"
MCP_PORT="${E2E_MCP_PORT:-9420}"
LOG_FILE="${TMP_ROOT}/web.log"
PASSWORD="${TABURA_TEST_PASSWORD:-tabura-test-password}"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${TMP_ROOT}"
}

trap cleanup EXIT INT TERM

cd "${ROOT_DIR}"
go run ./cmd/tabura server \
  --project-dir "${PROJECT_DIR}" \
  --data-dir "${DATA_DIR}" \
  --web-host "${WEB_HOST}" \
  --web-port "${WEB_PORT}" \
  --mcp-host "${MCP_HOST}" \
  --mcp-port "${MCP_PORT}" >"${LOG_FILE}" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 160); do
  if curl -fsS "http://${WEB_HOST}:${WEB_PORT}/api/setup" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "${SERVER_PID}" 2>/dev/null; then
    cat "${LOG_FILE}" >&2
    exit 1
  fi
  sleep 0.25
done

curl -fsS "http://${WEB_HOST}:${WEB_PORT}/api/setup" >/dev/null
printf '%s\n' "${PASSWORD}" | go run ./cmd/tabura set-password --data-dir "${DATA_DIR}" >/dev/null

wait "${SERVER_PID}"
