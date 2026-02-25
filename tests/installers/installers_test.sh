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

    PATH="${fakebin}:/usr/bin:/bin" \
    HOME="$home_dir" \
    TABURA_ASSUME_YES=1 \
    TABURA_INSTALL_SKIP_BROWSER=1 \
    TABURA_INSTALL_SKIP_VOXTYPE=1 \
    "${ROOT_DIR}/scripts/install.sh" --dry-run --version v0.0.0-test >"$out_file" 2>&1

    assert_contains "$out_file" "Install complete"
    assert_contains "$out_file" "Service mode:  linux"
    assert_contains "$out_file" "Piper TTS"

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
}

main() {
    run_install_sh_dry_run
    run_install_ps1_static_checks
    echo "installer tests passed"
}

main "$@"
