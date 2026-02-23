#!/usr/bin/env bash
set -euo pipefail

# Auto-provision F5-TTS server with English + German models.
# Everything goes into ~/.local/share/tabura-f5-tts/
#
# Prerequisites: python3 (3.10+), CUDA toolkit (for GPU acceleration)
# GPU strongly recommended (RTX 5060 Ti or better).
# German model (aihpi/F5-TTS-German) auto-downloads on first run via HuggingFace Hub.

F5_DIR="${HOME}/.local/share/tabura-f5-tts"
VENV_DIR="${F5_DIR}/venv"
REF_WAV="${HOME}/.local/share/tabura-tts/reference.wav"
SERVER_SCRIPT="$(cd "$(dirname "$0")" && pwd)/f5_tts_server.py"

mkdir -p "$F5_DIR"

# --- Step 1: Check reference WAV ---

if [ -f "$REF_WAV" ]; then
    echo "Reference WAV found: $REF_WAV"
else
    echo "WARNING: Reference WAV not found at $REF_WAV"
    echo "  Place a mono 24kHz WAV clip (~5-15s) there for voice cloning."
    echo "  The same file is shared with Chatterbox TTS."
fi

# --- Step 2: Python venv + dependencies ---

if [ -d "$VENV_DIR" ] && "$VENV_DIR/bin/pip" show f5-tts >/dev/null 2>&1; then
    echo "Python venv already provisioned: $VENV_DIR"
else
    echo "Creating Python venv and installing dependencies..."
    python3 -m venv "$VENV_DIR"
    # shellcheck disable=SC1091
    source "${VENV_DIR}/bin/activate"
    pip install --upgrade pip

    # Detect CUDA availability for torch install
    if command -v nvidia-smi >/dev/null 2>&1; then
        echo "CUDA detected, installing torch with cu126 support..."
        pip install torch torchaudio --index-url https://download.pytorch.org/whl/cu126
    else
        echo "No CUDA detected, installing CPU-only torch..."
        pip install torch torchaudio --index-url https://download.pytorch.org/whl/cpu
    fi

    pip install f5-tts fastapi 'uvicorn[standard]'

    deactivate
    echo "Dependencies installed."
fi

# --- Done ---

echo ""
echo "=== F5-TTS Setup Complete ==="
echo "  Install dir:    $F5_DIR"
echo "  Venv:           $VENV_DIR"
echo "  Reference WAV:  $REF_WAV"
echo "  Server script:  $SERVER_SCRIPT"
echo ""
echo "Next steps:"
echo "  1. Run: scripts/install-tabura-user-units.sh"
echo "  2. systemctl --user start tabura-f5-tts.service"
echo "  3. Test:"
echo "     curl -X POST http://127.0.0.1:8424/v1/audio/speech \\"
echo "       -H 'Content-Type: application/json' \\"
echo "       -d '{\"input\":\"Hello world\",\"voice\":\"en\"}' > /tmp/test.wav"
echo "     aplay /tmp/test.wav"
