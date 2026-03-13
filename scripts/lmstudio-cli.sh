#!/usr/bin/env bash
set -euo pipefail

APP_LOG="${LM_STUDIO_LOG:-/tmp/lm-studio.log}"
USER_LMS="${HOME}/.lmstudio/bin/lms"

find_ephemeral_lms() {
    ps -eo args= | awk '
        match($0, /\/tmp\/\.mount_lm-stu[^ ]*\/lm-studio/) {
            print substr($0, RSTART, RLENGTH)
            exit
        }
    ' | sed 's#/lm-studio$#/resources/app/.webpack/lms#'
}

app_running() {
    if pgrep -f 'LM Studio|/opt/lm-studio/lm-studio.AppImage|/tmp/.mount_lm-stu.*/lm-studio|/Applications/LM Studio.app' >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

start_app() {
    if app_running; then
        return
    fi
    if command -v lm-studio >/dev/null 2>&1; then
        nohup lm-studio >"${APP_LOG}" 2>&1 &
        return
    fi
    if [ "$(uname -s)" = "Darwin" ] && command -v open >/dev/null 2>&1; then
        open -a "LM Studio" >/dev/null 2>&1 || true
        return
    fi
    echo "LM Studio app is not installed or not found in PATH." >&2
    exit 1
}

resolve_lms() {
    if [ -x "${USER_LMS}" ]; then
        printf '%s\n' "${USER_LMS}"
        return 0
    fi
    if command -v lms >/dev/null 2>&1; then
        command -v lms
        return 0
    fi
    local ephemeral
    ephemeral="$(find_ephemeral_lms || true)"
    if [ -n "${ephemeral}" ] && [ -x "${ephemeral}" ]; then
        printf '%s\n' "${ephemeral}"
        return 0
    fi
    return 1
}

for _ in $(seq 1 60); do
    start_app
    LMS_BIN="$(resolve_lms || true)"
    if [ -n "${LMS_BIN}" ] && [ -x "${LMS_BIN}" ]; then
        exec "${LMS_BIN}" "$@"
    fi
    sleep 1
done

echo "LM Studio CLI was not available after waiting for the desktop app to start." >&2
exit 1
