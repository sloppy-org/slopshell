#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() { printf 'FATAL: %s\n' "$1" >&2; exit 1; }

wait_for_command() {
  local description="$1"
  local attempts="$2"
  shift 2
  local try
  for ((try = 1; try <= attempts; try++)); do
    if "$@" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  fail "$description"
}

latest_workspace_epoch() {
  local -a paths=(
    "$ROOT_DIR/cmd"
    "$ROOT_DIR/internal"
    "$ROOT_DIR/scripts"
    "$ROOT_DIR/tests"
    "$ROOT_DIR/go.mod"
    "$ROOT_DIR/go.sum"
    "$ROOT_DIR/package.json"
    "$ROOT_DIR/package-lock.json"
    "$ROOT_DIR/playwright.config.ts"
    "$ROOT_DIR/playwright.playtest.config.ts"
  )
  local latest=0
  local path
  for path in "${paths[@]}"; do
    [[ -e "$path" ]] || continue
    while IFS= read -r mtime; do
      [[ -n "$mtime" ]] || continue
      if awk "BEGIN { exit !($mtime > $latest) }"; then
        latest="$mtime"
      fi
    done < <(find "$path" -type f -printf '%T@\n' 2>/dev/null)
  done
  printf '%s\n' "${latest%%.*}"
}

maybe_sync_live_runtime() {
  if ! systemctl --user is-active --quiet tabura-web.service; then
    return
  fi
  local started_at started_epoch latest_epoch
  started_at="$(systemctl --user show tabura-web.service --property=ExecMainStartTimestamp --value)"
  started_epoch="$(date -d "$started_at" +%s 2>/dev/null || printf '0')"
  latest_epoch="$(latest_workspace_epoch)"
  if (( latest_epoch <= started_epoch )); then
    return
  fi
  printf 'Syncing live runtime to current workspace...\n'
  "$ROOT_DIR/scripts/tabura-dev-restart.sh"
  wait_for_command \
    'Tabura web server did not come back on :8420 after restart' \
    30 \
    curl -fsS --max-time 3 http://127.0.0.1:8420/api/setup
}

maybe_sync_live_runtime

printf 'Checking live services...\n'

wait_for_command \
  'Tabura web server not running on :8420' \
  5 \
  curl -fsS --max-time 3 http://127.0.0.1:8420/api/setup

curl -fsS --max-time 3 -o /dev/null -w '' \
  -X POST http://127.0.0.1:8424/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"input":"ok","voice":"en","response_format":"wav"}' \
  || fail 'Piper TTS not running on :8424'

curl -fsS --max-time 3 http://127.0.0.1:8427/healthz >/dev/null \
  || fail 'voxtype STT not running on :8427'

if python3 - <<'PY' >/dev/null 2>&1
import socket
sock = socket.create_connection(("127.0.0.1", 8425), timeout=3)
sock.close()
PY
then
  printf 'Intent classifier detected on :8425.\n'
else
  printf 'Intent classifier not detected on :8425; continuing with live runtime defaults.\n'
fi

if python3 - <<'PY' >/dev/null 2>&1
import socket
sock = socket.create_connection(("127.0.0.1", 8426), timeout=3)
sock.close()
PY
then
  printf 'Intent LLM fallback detected on :8426.\n'
else
  printf 'Intent LLM fallback not detected on :8426; continuing with live runtime defaults.\n'
fi

python3 - <<'PY' >/dev/null 2>&1 || fail 'Codex app-server websocket not reachable on :8787'
import socket
sock = socket.create_connection(("127.0.0.1", 8787), timeout=3)
sock.close()
PY

command -v ffmpeg >/dev/null 2>&1 || fail 'ffmpeg not installed'
command -v gh >/dev/null 2>&1 || fail 'gh not installed'
command -v curl >/dev/null 2>&1 || fail 'curl not installed'

printf 'All services OK.\n'

SPEECH_WAV="/tmp/tabura-playtest-speech-raw.wav"
PADDED_WAV="/tmp/tabura-playtest-speech.wav"

printf 'Generating voice sample via Piper TTS...\n'
curl -sS -X POST http://127.0.0.1:8424/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"input":"Hello, this is a test of voice recording.","voice":"en","response_format":"wav"}' \
  -o "$SPEECH_WAV"

ffmpeg -hide_banner -loglevel error -nostdin -y \
  -f lavfi -t 0.5 -i anullsrc=r=22050:cl=mono \
  -i "$SPEECH_WAV" \
  -f lavfi -t 3 -i anullsrc=r=22050:cl=mono \
  -filter_complex '[0][1][2]concat=n=3:v=0:a=1[out]' \
  -map '[out]' -ar 22050 -ac 1 -c:a pcm_s16le "$PADDED_WAV"

printf 'Audio ready: %s\n' "$PADDED_WAV"

export E2E_AUDIO_FILE="$PADDED_WAV"
export PLAYTEST_FILE_ISSUES="${PLAYTEST_FILE_ISSUES:-1}"

cd "$ROOT_DIR"
exec npx playwright test --config=playwright.playtest.config.ts "$@"
