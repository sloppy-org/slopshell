#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

mode="all"
for arg in "$@"; do
  case "$arg" in
    --ios-only)
      mode="ios"
      ;;
    --android-only)
      mode="android"
      ;;
    *)
      echo "unknown argument: $arg" >&2
      exit 1
      ;;
  esac
done

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

log_step() {
  printf '\n[%s] %s\n' "native-flows" "$1" >&2
}

android_sdk_root="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
if [[ -z "$android_sdk_root" ]]; then
  for candidate in "$HOME/android-sdk" "$HOME/Android/Sdk"; do
    if [[ -d "$candidate" ]]; then
      android_sdk_root="$candidate"
      break
    fi
  done
fi
if [[ -n "$android_sdk_root" ]]; then
  export ANDROID_HOME="$android_sdk_root"
  export ANDROID_SDK_ROOT="$android_sdk_root"
fi

if [[ "${TABURA_IOS_REMOTE_ROOT:-}" = /* ]]; then
  ios_remote_root="${TABURA_IOS_REMOTE_ROOT}"
else
  ios_remote_root="~/${TABURA_IOS_REMOTE_ROOT:-tabura-ci}"
fi
ios_ssh_host="${TABURA_IOS_SSH_HOST:-faepmac1}"
ios_destination="${TABURA_IOS_DESTINATION:-platform=iOS Simulator,name=iPhone 17 Pro,OS=26.2}"
ios_repo_synced=0

need_cmd node
need_cmd npm
need_cmd ssh
need_cmd rsync

sync_native_fixtures() {
  log_step "Sync native flow fixtures"
  node ./scripts/sync-native-flow-fixtures.mjs
}

run_web_flows() {
  log_step "Run shared web flow suite"
  npm run test:flows
}

run_ios_contracts() {
  log_step "Run iOS contract suite"
  node ./scripts/sync-native-flow-fixtures.mjs --check
  if command -v swift >/dev/null 2>&1; then
    (cd platforms/ios && swift test)
    return
  fi
  ensure_ios_repo_synced
  ssh "$ios_ssh_host" "cd $ios_remote_root && swift test --package-path platforms/ios"
}

run_android_contracts() {
  need_cmd gradle
  if [[ -z "$android_sdk_root" ]]; then
    echo "ANDROID_HOME or ANDROID_SDK_ROOT must be set for Android validation" >&2
    exit 1
  fi
  log_step "Run Android contract suites"
  node ./scripts/sync-native-flow-fixtures.mjs --check
  PATH="$android_sdk_root/platform-tools:$android_sdk_root/emulator:$PATH" \
    ANDROID_HOME="$android_sdk_root" \
    ANDROID_SDK_ROOT="$android_sdk_root" \
    gradle -p platforms/android app:testDebugUnitTest
  PATH="$android_sdk_root/platform-tools:$android_sdk_root/emulator:$PATH" \
    ANDROID_HOME="$android_sdk_root" \
    ANDROID_SDK_ROOT="$android_sdk_root" \
    gradle -p platforms/android/flow-contracts test
}

android_adb() {
  local adb_bin
  if [[ -n "$android_sdk_root" && -x "$android_sdk_root/platform-tools/adb" ]]; then
    adb_bin="$android_sdk_root/platform-tools/adb"
  else
    adb_bin="$(command -v adb)"
  fi
  printf '%s\n' "$adb_bin"
}

android_emulator() {
  local emulator_bin
  if [[ -n "$android_sdk_root" && -x "$android_sdk_root/emulator/emulator" ]]; then
    emulator_bin="$android_sdk_root/emulator/emulator"
  else
    emulator_bin="$(command -v emulator)"
  fi
  printf '%s\n' "$emulator_bin"
}

boot_android_emulator() {
  local adb_bin emulator_bin serial avd log_file
  adb_bin="$(android_adb)"
  emulator_bin="$(android_emulator)"
  serial="$("$adb_bin" devices | awk 'NR > 1 && $2 == "device" { print $1; exit }')"
  if [[ -n "$serial" ]]; then
    printf '%s\n' "$serial"
    return
  fi

  avd="${TABURA_ANDROID_AVD:-}"
  if [[ -z "$avd" ]]; then
    avd="$("$emulator_bin" -list-avds | head -n1)"
  fi
  if [[ -z "$avd" ]]; then
    echo "no Android AVD available; set TABURA_ANDROID_AVD" >&2
    exit 1
  fi

  log_file="${TABURA_ANDROID_EMULATOR_LOG:-/tmp/tabura-android-emulator.log}"
  log_step "Boot Android emulator $avd"
  nohup "$emulator_bin" -avd "$avd" ${TABURA_ANDROID_EMULATOR_ARGS:--no-window -gpu swiftshader_indirect -no-audio -no-snapshot-save} >"$log_file" 2>&1 &

  for _ in $(seq 1 120); do
    serial="$("$adb_bin" devices | awk 'NR > 1 && $2 == "device" { print $1; exit }')"
    if [[ -n "$serial" ]]; then
      if [[ "$("$adb_bin" -s "$serial" shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" == "1" ]]; then
        printf '%s\n' "$serial"
        return
      fi
    fi
    sleep 2
  done

  echo "Android emulator did not finish booting; see $log_file" >&2
  exit 1
}

run_android_ui() {
  need_cmd gradle
  local serial
  if [[ -z "$android_sdk_root" ]]; then
    echo "ANDROID_HOME or ANDROID_SDK_ROOT must be set for Android UI validation" >&2
    exit 1
  fi
  serial="$(boot_android_emulator)"
  log_step "Run Android UI harness on $serial"
  PATH="$android_sdk_root/platform-tools:$android_sdk_root/emulator:$PATH" gradle -p platforms/android app:connectedDebugAndroidTest
}

sync_repo_to_ios_host() {
  log_step "Sync platforms/ios to $ios_ssh_host:$ios_remote_root"
  ssh "$ios_ssh_host" "mkdir -p $ios_remote_root/platforms"
  rsync -az --delete \
    --exclude '.build/' \
    platforms/ios/ "$ios_ssh_host:$ios_remote_root/platforms/ios/"
  ios_repo_synced=1
}

ensure_ios_repo_synced() {
  if [[ "$ios_repo_synced" -eq 0 ]]; then
    sync_repo_to_ios_host
  fi
}

run_ios_ui() {
  ensure_ios_repo_synced
  log_step "Run iOS UI harness on $ios_ssh_host ($ios_destination)"
  ssh "$ios_ssh_host" "cd $ios_remote_root && xcodebuild test -project platforms/ios/TaburaIOS.xcodeproj -scheme TaburaIOS -destination '$ios_destination' -only-testing:TaburaIOSUITests"
}

sync_native_fixtures

case "$mode" in
  all)
    run_web_flows
    run_ios_contracts
    run_android_contracts
    run_android_ui
    run_ios_ui
    ;;
  ios)
    run_ios_contracts
    run_ios_ui
    ;;
  android)
    run_android_contracts
    run_android_ui
    ;;
esac

log_step "Native flow validation completed"
