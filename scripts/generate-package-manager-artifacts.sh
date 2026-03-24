#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  scripts/generate-package-manager-artifacts.sh \
    --version <vX.Y.Z|X.Y.Z> \
    --checksums <path/to/checksums.txt> \
    [--output-dir <dir>] \
    [--owner <github-owner>] \
    [--repo <github-repo>] \
    [--publisher <publisher-name>]
EOF
}

fail() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || fail "missing required command: ${cmd}"
}

lookup_checksum() {
  local file="$1"
  local value
  value="$(awk -v target="${file}" '$2 == target || $2 == ("*" target) { print $1; exit }' "${CHECKSUMS_FILE}")"
  [ -n "${value}" ] || fail "checksum not found for ${file} in ${CHECKSUMS_FILE}"
  printf '%s' "${value}"
}

to_upper() {
  local value="$1"
  printf '%s' "${value}" | tr '[:lower:]' '[:upper:]'
}

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

VERSION=""
CHECKSUMS_FILE=""
OUTPUT_DIR=".tabura/artifacts/package-managers"
OWNER="krystophny"
REPO="tabura"
PUBLISHER="Christopher Albert"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      VERSION="${2:-}"
      shift 2
      ;;
    --checksums)
      CHECKSUMS_FILE="${2:-}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    --owner)
      OWNER="${2:-}"
      shift 2
      ;;
    --repo)
      REPO="${2:-}"
      shift 2
      ;;
    --publisher)
      PUBLISHER="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[ -n "${VERSION}" ] || fail "--version is required"
[ -n "${CHECKSUMS_FILE}" ] || fail "--checksums is required"
[ -f "${CHECKSUMS_FILE}" ] || fail "checksums file not found: ${CHECKSUMS_FILE}"

require_cmd awk
require_cmd tr
require_cmd date

VERSION_BARE="${VERSION#v}"
if ! [[ "${VERSION_BARE}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$ ]]; then
  fail "invalid semver: ${VERSION}"
fi
VERSION_TAG="v${VERSION_BARE}"
RELEASE_DATE="$(date -u +%Y-%m-%d)"

LINUX_AMD64="tabura_${VERSION_BARE}_linux_amd64.tar.gz"
LINUX_ARM64="tabura_${VERSION_BARE}_linux_arm64.tar.gz"
DARWIN_AMD64="tabura_${VERSION_BARE}_darwin_amd64.tar.gz"
DARWIN_ARM64="tabura_${VERSION_BARE}_darwin_arm64.tar.gz"
WINDOWS_AMD64="tabura_${VERSION_BARE}_windows_amd64.zip"
WINDOWS_ARM64="tabura_${VERSION_BARE}_windows_arm64.zip"

SHA_LINUX_AMD64="$(lookup_checksum "${LINUX_AMD64}")"
SHA_LINUX_ARM64="$(lookup_checksum "${LINUX_ARM64}")"
SHA_DARWIN_AMD64="$(lookup_checksum "${DARWIN_AMD64}")"
SHA_DARWIN_ARM64="$(lookup_checksum "${DARWIN_ARM64}")"
SHA_WINDOWS_AMD64="$(to_upper "$(lookup_checksum "${WINDOWS_AMD64}")")"
SHA_WINDOWS_ARM64="$(to_upper "$(lookup_checksum "${WINDOWS_ARM64}")")"

RELEASE_URL="https://github.com/${OWNER}/${REPO}/releases/download/${VERSION_TAG}"

HOMEBREW_DIR="${OUTPUT_DIR}/homebrew/Formula"
AUR_DIR="${OUTPUT_DIR}/aur"
WINGET_DIR="${OUTPUT_DIR}/winget/manifests/k/krystophny/tabura/${VERSION_BARE}"

mkdir -p "${HOMEBREW_DIR}" "${AUR_DIR}" "${WINGET_DIR}"

cat > "${HOMEBREW_DIR}/tabura.rb" <<EOF
class Tabura < Formula
  desc "Local-first voice assistant with canvas UI"
  homepage "https://github.com/${OWNER}/${REPO}"
  version "${VERSION_BARE}"
  license "MIT"

  on_macos do
    on_arm do
      url "${RELEASE_URL}/${DARWIN_ARM64}"
      sha256 "${SHA_DARWIN_ARM64}"
    end
    on_intel do
      url "${RELEASE_URL}/${DARWIN_AMD64}"
      sha256 "${SHA_DARWIN_AMD64}"
    end
  end

  on_linux do
    on_arm do
      url "${RELEASE_URL}/${LINUX_ARM64}"
      sha256 "${SHA_LINUX_ARM64}"
    end
    on_intel do
      url "${RELEASE_URL}/${LINUX_AMD64}"
      sha256 "${SHA_LINUX_AMD64}"
    end
  end

  def install
    bin.install "tabura"
  end

  def caveats
    <<~EOS
      Run 'tabura server' or use the full installer:
        curl -fsSL https://github.com/${OWNER}/${REPO}/releases/latest/download/install.sh | bash
      Requires codex app-server and Python 3.10+ for Piper TTS.
    EOS
  end
end
EOF

cat > "${AUR_DIR}/PKGBUILD" <<EOF
pkgname=tabura-bin
pkgver=${VERSION_BARE}
pkgrel=1
pkgdesc="Local-first voice assistant with canvas UI"
arch=('x86_64' 'aarch64')
url="https://github.com/${OWNER}/${REPO}"
license=('MIT')
depends=('glibc')
optdepends=('voxtype: speech-to-text sidecar' 'python: Piper TTS server' 'ffmpeg: audio conversion')
source_x86_64=("${RELEASE_URL}/${LINUX_AMD64}")
source_aarch64=("${RELEASE_URL}/${LINUX_ARM64}")
sha256sums_x86_64=('${SHA_LINUX_AMD64}')
sha256sums_aarch64=('${SHA_LINUX_ARM64}')

package() {
  install -Dm755 "\${srcdir}/tabura" "\${pkgdir}/usr/bin/tabura"
  install -Dm644 "\${srcdir}/LICENSE" "\${pkgdir}/usr/share/licenses/\${pkgname}/LICENSE"
}
EOF

cat > "${WINGET_DIR}/krystophny.tabura.yaml" <<EOF
PackageIdentifier: krystophny.tabura
PackageVersion: ${VERSION_BARE}
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.10.0
EOF

cat > "${WINGET_DIR}/krystophny.tabura.locale.en-US.yaml" <<EOF
PackageIdentifier: krystophny.tabura
PackageVersion: ${VERSION_BARE}
PackageLocale: en-US
Publisher: ${PUBLISHER}
PublisherUrl: https://github.com/${OWNER}
PublisherSupportUrl: https://github.com/${OWNER}/${REPO}/issues
Author: ${PUBLISHER}
PackageName: Tabura
PackageUrl: https://github.com/${OWNER}/${REPO}
License: MIT
LicenseUrl: https://github.com/${OWNER}/${REPO}/blob/main/LICENSE
ShortDescription: Local-first voice assistant with canvas UI
Description: Local-first voice assistant with canvas UI, voice capture, and MCP canvas integration.
Moniker: tabura
Tags:
  - assistant
  - voice
  - mcp
ManifestType: defaultLocale
ManifestVersion: 1.10.0
EOF

cat > "${WINGET_DIR}/krystophny.tabura.installer.yaml" <<EOF
PackageIdentifier: krystophny.tabura
PackageVersion: ${VERSION_BARE}
InstallerType: zip
NestedInstallerType: portable
NestedInstallerFiles:
  - RelativeFilePath: tabura.exe
    PortableCommandAlias: tabura
ReleaseDate: ${RELEASE_DATE}
Installers:
  - Architecture: x64
    InstallerUrl: ${RELEASE_URL}/${WINDOWS_AMD64}
    InstallerSha256: ${SHA_WINDOWS_AMD64}
  - Architecture: arm64
    InstallerUrl: ${RELEASE_URL}/${WINDOWS_ARM64}
    InstallerSha256: ${SHA_WINDOWS_ARM64}
ManifestType: installer
ManifestVersion: 1.10.0
EOF

echo "Generated package-manager artifacts under ${OUTPUT_DIR}"
echo "  - Homebrew formula: ${HOMEBREW_DIR}/tabura.rb"
echo "  - AUR PKGBUILD: ${AUR_DIR}/PKGBUILD"
echo "  - winget manifests: ${WINGET_DIR}"
