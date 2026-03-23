#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TRAINER_DIR="${TABURA_HOTWORD_TRAINER_DIR:-$ROOT_DIR/tools/openwakeword-trainer}"
CONFIG_PATH="${TABURA_HOTWORD_CONFIG:-$ROOT_DIR/scripts/hotword-config.yaml}"
OUTPUT_DIR="${TABURA_HOTWORD_OUTPUT_DIR:-$ROOT_DIR/models/hotword}"
PYTHON_BIN="${PYTHON:-python3.12}"
SKIP_PIP_INSTALL="${TABURA_HOTWORD_SKIP_PIP_INSTALL:-0}"

read_config_scalar() {
  local key="$1"
  awk -F: -v key="$key" '
    $1 ~ "^[[:space:]]*" key "[[:space:]]*$" {
      value = substr($0, index($0, ":") + 1)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      gsub(/^["'"'"']|["'"'"']$/, "", value)
      print value
      exit
    }
  ' "$CONFIG_PATH"
}

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "hotword config not found: $CONFIG_PATH" >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

MODEL_NAME="$(read_config_scalar model_name)"
if [[ -z "$MODEL_NAME" ]]; then
  echo "hotword config missing model_name: $CONFIG_PATH" >&2
  exit 1
fi
CONFIG_OUTPUT_DIR="$(read_config_scalar output_dir)"
TARGET_MODEL_PATH=""

ARGS=("$@")
REQUESTED_STEP=""
REQUESTED_FROM=""
VERIFY_ONLY=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --step)
      REQUESTED_STEP="${2:-}"
      shift 2
      ;;
    --from)
      REQUESTED_FROM="${2:-}"
      shift 2
      ;;
    --verify-only)
      VERIFY_ONLY=1
      shift
      ;;
    *)
      shift
      ;;
  esac
done

EXPECT_MODEL=1
if [[ "$VERIFY_ONLY" == "1" ]]; then
  EXPECT_MODEL=0
elif [[ -n "$REQUESTED_STEP" ]]; then
  case "$REQUESTED_STEP" in
    train|verify-model|export)
      EXPECT_MODEL=1
      ;;
    *)
      EXPECT_MODEL=0
      ;;
  esac
elif [[ -n "$REQUESTED_FROM" ]]; then
  EXPECT_MODEL=1
fi

if [[ ! -d "$TRAINER_DIR/.git" ]]; then
  mkdir -p "$(dirname "$TRAINER_DIR")"
  git clone --depth=1 https://github.com/lgpearson1771/openwakeword-trainer "$TRAINER_DIR"
fi

VENV_DIR="$TRAINER_DIR/.venv"
if [[ ! -x "$VENV_DIR/bin/python" ]]; then
  "$PYTHON_BIN" -m venv "$VENV_DIR"
fi

if [[ "$SKIP_PIP_INSTALL" != "1" ]]; then
  "$VENV_DIR/bin/python" -m pip install --upgrade pip 'setuptools<82' wheel >/dev/null
  # piper-phonemize has no wheels for Python >=3.12; use the community fix package.
  "$VENV_DIR/bin/python" -m pip install piper-phonemize-fix >/dev/null
  ORT_PACKAGE="onnxruntime"
  if [[ "${TABURA_HOTWORD_ORT_PACKAGE:-}" != "" ]]; then
    ORT_PACKAGE="$TABURA_HOTWORD_ORT_PACKAGE"
  elif [[ "$(uname -s)" == "Linux" ]] && command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi -L >/dev/null 2>&1; then
    ORT_PACKAGE="onnxruntime-gpu"
  fi
  "$VENV_DIR/bin/python" -m pip uninstall -y onnxruntime onnxruntime-gpu >/dev/null 2>&1 || true
  grep -v '^piper-phonemize' "$TRAINER_DIR/requirements.txt" \
    | "$VENV_DIR/bin/python" -m pip install -r /dev/stdin >/dev/null
  "$VENV_DIR/bin/python" -m pip install "$ORT_PACKAGE" >/dev/null
fi

TRAIN_CMD=("$VENV_DIR/bin/python" "$TRAINER_DIR/train_wakeword.py")

"${TRAIN_CMD[@]}" --config "$CONFIG_PATH" "${ARGS[@]}"

if [[ "$EXPECT_MODEL" != "1" ]]; then
  echo "trainer step complete: ${REQUESTED_STEP:-verify-only}"
  exit 0
fi

MODEL_SOURCE_PATH=""
if [[ -n "$CONFIG_OUTPUT_DIR" ]]; then
  if [[ "$CONFIG_OUTPUT_DIR" = /* ]]; then
    if [[ -f "$CONFIG_OUTPUT_DIR/$MODEL_NAME.onnx" ]]; then
      MODEL_SOURCE_PATH="$CONFIG_OUTPUT_DIR/$MODEL_NAME.onnx"
    fi
  else
    for candidate in \
      "$TRAINER_DIR/$CONFIG_OUTPUT_DIR/$MODEL_NAME.onnx" \
      "$ROOT_DIR/$CONFIG_OUTPUT_DIR/$MODEL_NAME.onnx" \
      "$(dirname "$CONFIG_PATH")/$CONFIG_OUTPUT_DIR/$MODEL_NAME.onnx"
    do
      if [[ -f "$candidate" ]]; then
        MODEL_SOURCE_PATH="$candidate"
        break
      fi
    done
  fi
fi

if [[ -z "$MODEL_SOURCE_PATH" && -f "$TARGET_MODEL_PATH" ]]; then
  MODEL_SOURCE_PATH="$TARGET_MODEL_PATH"
fi

if [[ -z "$MODEL_SOURCE_PATH" ]]; then
  echo "training finished but expected model missing in configured output dir: ${CONFIG_OUTPUT_DIR:-<unset>}" >&2
  exit 1
fi

model_stamp="$("$PYTHON_BIN" -c 'import datetime, os, sys; print(datetime.datetime.fromtimestamp(os.path.getmtime(sys.argv[1]), datetime.timezone.utc).strftime("%Y-%m-%d_%H-%M-%SZ"))' "$MODEL_SOURCE_PATH")"
TARGET_MODEL_PATH="$OUTPUT_DIR/$MODEL_NAME-$model_stamp.onnx"
TARGET_MODEL_DATA_PATH="$TARGET_MODEL_PATH.data"
MODEL_SOURCE_DATA_PATH="$MODEL_SOURCE_PATH.data"

if [[ "$MODEL_SOURCE_PATH" != "$TARGET_MODEL_PATH" || ! -f "$TARGET_MODEL_PATH" ]]; then
  cp "$MODEL_SOURCE_PATH" "$TARGET_MODEL_PATH"
fi
if [[ -f "$MODEL_SOURCE_DATA_PATH" ]]; then
  if [[ "$MODEL_SOURCE_DATA_PATH" != "$TARGET_MODEL_DATA_PATH" || ! -f "$TARGET_MODEL_DATA_PATH" ]]; then
    cp "$MODEL_SOURCE_DATA_PATH" "$TARGET_MODEL_DATA_PATH"
  fi
fi

echo "trained model: $TARGET_MODEL_PATH"
