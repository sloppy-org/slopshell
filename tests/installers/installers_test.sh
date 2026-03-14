#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

assert_contains() {
    local file="$1"
    local pattern="$2"
    if ! grep -Fq "$pattern" "$file"; then
        echo "assertion failed: expected '$pattern' in $file" >&2
        exit 1
    fi
}

make_fake_cmd() {
    local dir="$1"
    local name="$2"
    cat >"${dir}/${name}" <<SH
#!/usr/bin/env bash
echo "fake-${name} \$*" >&2
SH
    chmod +x "${dir}/${name}"
}

run_llama_helper_checks() {
    local tmpdir fakebin explicit_bin
    tmpdir="$(mktemp -d -t tabura-llama-helper-test-XXXXXX)"
    trap "rm -rf '$tmpdir'" RETURN

    fakebin="${tmpdir}/fakebin"
    explicit_bin="${tmpdir}/explicit-llama-server"
    mkdir -p "$fakebin"

    cat >"${fakebin}/llama-server" <<'SH'
#!/usr/bin/env bash
echo "error while loading shared libraries: libmtmd.so.0: cannot open shared object file" >&2
exit 127
SH
    chmod +x "${fakebin}/llama-server"

    cat >"${explicit_bin}" <<'SH'
#!/usr/bin/env bash
if [ "$1" = "--version" ]; then
  echo "llama-server test version"
  exit 0
fi
exit 0
SH
    chmod +x "${explicit_bin}"

    local resolved
    resolved="$(
        PATH="${fakebin}:/usr/bin:/bin" \
        HOME="${tmpdir}/home" \
        LLAMA_SERVER_BIN="${explicit_bin}" \
        bash -c '
            source "'"${ROOT_DIR}"'/scripts/lib/llama.sh"
            tabura_find_llama_server
        '
    )"
    if [ "${resolved}" != "${explicit_bin}" ]; then
        echo "assertion failed: expected explicit LLAMA_SERVER_BIN to win, got ${resolved}" >&2
        exit 1
    fi
}

run_install_sh_dry_run() {
    local tmpdir out_file fakebin home_dir
    tmpdir="$(mktemp -d -t tabura-installer-test-XXXXXX)"
    trap "rm -rf '$tmpdir'" RETURN

    out_file="${tmpdir}/install.log"
    fakebin="${tmpdir}/fakebin"
    home_dir="${tmpdir}/home"
    mkdir -p "$fakebin" "$home_dir"

    make_fake_cmd "$fakebin" codex
    make_fake_cmd "$fakebin" ffmpeg
    make_fake_cmd "$fakebin" systemctl
    make_fake_cmd "$fakebin" launchctl

    # Stub curl that always fails so detect_llama_server cannot find
    # services running on the host and the test stays deterministic.
    cat >"${fakebin}/curl" <<'SH'
#!/usr/bin/env bash
exit 1
SH
    chmod +x "${fakebin}/curl"

    # Need a real python3 >= 3.10 for the version check.
    # Prefer the system-wide python3 if adequate, otherwise try common paths.
    local real_python3=""
    for candidate in /usr/bin/python3 /usr/local/bin/python3 /opt/homebrew/bin/python3; do
        if [ -x "$candidate" ] && "$candidate" -c 'import sys; sys.exit(0 if sys.version_info >= (3,10) else 1)' 2>/dev/null; then
            real_python3="$candidate"
            break
        fi
    done
    if [ -n "$real_python3" ]; then
        ln -sf "$real_python3" "${fakebin}/python3"
    fi

    PATH="${fakebin}:/usr/bin:/bin" \
    HOME="$home_dir" \
    TABURA_ASSUME_YES=1 \
    TABURA_INSTALL_SKIP_BROWSER=1 \
    TABURA_INSTALL_SKIP_STT=1 \
    "${ROOT_DIR}/scripts/install.sh" --dry-run --version v0.0.0-test >"$out_file" 2>&1

    assert_contains "$out_file" "Install complete"
    local expected_os
    case "$(uname -s | tr '[:upper:]' '[:lower:]')" in
        darwin) expected_os="darwin" ;;
        *)      expected_os="linux" ;;
    esac
    assert_contains "$out_file" "Service mode:  ${expected_os}"
    assert_contains "$out_file" "Piper TTS"
    assert_contains "$out_file" "Local LLM"
    assert_contains "$out_file" "skipping voxtype STT setup"

    PATH="${fakebin}:/usr/bin:/bin" \
    HOME="$home_dir" \
    TABURA_ASSUME_YES=1 \
    "${ROOT_DIR}/scripts/install.sh" --dry-run --uninstall >>"$out_file" 2>&1

    assert_contains "$out_file" "uninstall complete"
}

run_install_ps1_static_checks() {
    local ps1
    ps1="${ROOT_DIR}/scripts/install.ps1"

    assert_contains "$ps1" "Get-FileHash -Algorithm SHA256"
    assert_contains "$ps1" "Speech-to-text requires voxtype (Linux/macOS only)"
    assert_contains "$ps1" "schtasks /Create"
    assert_contains "$ps1" "piper-tts"
    assert_contains "$ps1" "tabura-llm"
    assert_contains "$ps1" "tabura-codex-llm"
    assert_contains "$ps1" "Setup-LocalLlm"
    assert_contains "$ps1" "gpt-oss-120b-default"
    assert_contains "$ps1" "Print-WindowsSTTNotice"
}

run_setup_codex_mcp_checks() {
    local tmpdir config_path
    tmpdir="$(mktemp -d -t tabura-codex-config-test-XXXXXX)"
    trap "rm -rf '$tmpdir'" RETURN

    config_path="${tmpdir}/config.toml"
    printf 'model = "gpt-5.4"\n' >"$config_path"

    CODEX_CONFIG_PATH="$config_path" \
    "${ROOT_DIR}/scripts/setup-codex-mcp.sh" "http://127.0.0.1:9420/mcp" >/dev/null

    assert_contains "$config_path" "[mcp_servers.tabura]"
    assert_contains "$config_path" "url = \"http://127.0.0.1:9420/mcp\""
    assert_contains "$config_path" "[model_providers.tabura_local_agentic]"
    assert_contains "$config_path" "base_url = \"http://127.0.0.1:8430/v1\""
    assert_contains "$config_path" "[model_providers.tabura_local_fast]"
    assert_contains "$config_path" "base_url = \"http://127.0.0.1:8426/v1\""
    assert_contains "$config_path" "[profiles.tabura_local_agentic]"
    assert_contains "$config_path" "model = \"gpt-oss-120b\""
    assert_contains "$config_path" "[profiles.tabura_local_fast]"
    assert_contains "$config_path" "model = \"qwen3.5-9b\""
    assert_contains "$config_path" "wire_api = \"responses\""
}

main() {
    run_install_sh_dry_run
    run_llama_helper_checks
    run_install_ps1_static_checks
    run_setup_codex_mcp_checks
    "${ROOT_DIR}/tests/installers/distribution_artifacts_test.sh"
    echo "installer tests passed"
}

main "$@"
