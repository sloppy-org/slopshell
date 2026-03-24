#!/usr/bin/env bash
set -euo pipefail

VOXTYPE_REPO="${VOXTYPE_REPO:-https://github.com/peteonrails/voxtype.git}"
VOXTYPE_BRANCH="${VOXTYPE_BRANCH:-feature/single-daemon-openai-stt-api}"
VOXTYPE_COMMIT="${VOXTYPE_COMMIT:-fe517afb30f43f9abef4fbfa87bca2deb042d6ab}"
INSTALL_DIR="${VOXTYPE_INSTALL_DIR:-${HOME}/.local/bin}"
ASSUME_YES=0

log()  { printf '[build-voxtype] %s\n' "$*"; }
fail() { printf '[build-voxtype] ERROR: %s\n' "$*" >&2; exit 1; }

supports_stt_service() {
    local help_text
    help_text="$("$1" --help 2>&1 || true)"
    case "$help_text" in
        *"--service"*) return 0 ;;
    esac
    return 1
}

print_help() {
    cat <<USAGE
Usage: scripts/build-voxtype-macos.sh [options]

Builds voxtype from source on macOS.
Voxtype provides local OpenAI-compatible speech-to-text.
The pinned branch includes macOS support with Metal GPU acceleration.

Options:
  -h, --help  Show this help

Environment:
  VOXTYPE_REPO         Git clone URL (default: peteonrails/voxtype)
  VOXTYPE_BRANCH       Branch to build (default: feature/single-daemon-openai-stt-api)
  VOXTYPE_COMMIT       Commit to pin after clone (default: ${VOXTYPE_COMMIT})
  VOXTYPE_INSTALL_DIR  Binary install directory (default: ~/.local/bin)
USAGE
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        -h|--help) print_help; exit 0 ;;
        --yes) ASSUME_YES=1; shift ;;
        *) fail "unknown argument: $1" ;;
    esac
done

# --- Prerequisites ---

[ "$(uname -s)" = "Darwin" ] || fail "this script is for macOS only"

command -v cargo >/dev/null 2>&1 || fail "cargo not found; install Rust with Homebrew: brew install rust"
command -v cmake >/dev/null 2>&1 || fail "cmake not found; install: brew install cmake"
command -v git >/dev/null 2>&1 || fail "git not found"

# --- Clone ---

BUILD_DIR="$(mktemp -d -t voxtype-build-XXXXXX)"
trap 'rm -rf "$BUILD_DIR"' EXIT

log "Cloning $VOXTYPE_REPO (branch: $VOXTYPE_BRANCH)"
git clone --depth 1 --branch "$VOXTYPE_BRANCH" "$VOXTYPE_REPO" "$BUILD_DIR/voxtype"

cd "$BUILD_DIR/voxtype"
git fetch --depth 1 origin "$VOXTYPE_COMMIT"
git checkout --detach "$VOXTYPE_COMMIT"
log "Pinned voxtype commit: $VOXTYPE_COMMIT"

# --- Build ---

# macOS native crates (mac-notification-sys, core-graphics, etc.) require
# Apple Clang. Override CC/CXX if they point to non-Apple compilers (e.g.
# Homebrew gcc) which lack -mmacos-version-min and ObjC framework support.
if [ -n "${CC:-}" ]; then
    case "$CC" in
        *gcc*|*g++*)
            log "Overriding CC=$CC with /usr/bin/cc (Apple Clang required)"
            export CC=/usr/bin/cc
            export CXX=/usr/bin/c++
            ;;
    esac
fi

ARCH="$(uname -m)"
FEATURES=""
if [ "$ARCH" = "arm64" ]; then
    FEATURES="gpu-metal"
    log "Building with Metal GPU support (Apple Silicon)"
else
    log "Building without GPU acceleration (Intel Mac)"
fi

log "Building voxtype (this may take several minutes on first build)"
if [ -n "$FEATURES" ]; then
    cargo build --release --features "$FEATURES"
else
    cargo build --release
fi

# --- Install ---

mkdir -p "$INSTALL_DIR"
cp target/release/voxtype "$INSTALL_DIR/voxtype"
chmod +x "$INSTALL_DIR/voxtype"

log "Installed: $INSTALL_DIR/voxtype"
if ! supports_stt_service "$INSTALL_DIR/voxtype"; then
    fail "built voxtype does not expose --service; wrong branch or commit"
fi

if ! printf ':%s:' "$PATH" | grep -Fq ":${INSTALL_DIR}:"; then
    log "WARNING: $INSTALL_DIR is not in PATH; add it to your shell profile"
fi

# --- Download model ---

log "Downloading voxtype model large-v3-turbo (~1.5 GB)"
"$INSTALL_DIR/voxtype" setup --download --model large-v3-turbo --no-post-install

log "Build complete. Verify with: voxtype --version"
