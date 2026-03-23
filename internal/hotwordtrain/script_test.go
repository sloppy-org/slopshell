package hotwordtrain

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTrainHotwordScriptAllowsResolveConfigWithoutModel(t *testing.T) {
	root := repoRootFromTestFile(t)
	trainerDir := filepath.Join(t.TempDir(), "trainer")
	if err := os.MkdirAll(filepath.Join(trainerDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir trainer git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(trainerDir, ".venv", "bin"), 0o755); err != nil {
		t.Fatalf("mkdir trainer venv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trainerDir, "requirements.txt"), []byte(""), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trainerDir, "train_wakeword.py"), []byte("# stub\n"), 0o644); err != nil {
		t.Fatalf("write train_wakeword: %v", err)
	}
	pythonStub := filepath.Join(trainerDir, ".venv", "bin", "python")
	pythonScript := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == *"train_wakeword.py" ]]; then
  shift
fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --step)
      STEP="${2:-}"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
if [[ "${STEP:-}" == "resolve-config" ]]; then
  echo "resolved config"
  exit 0
fi
echo "unexpected invocation" >&2
exit 1
`
	if err := os.WriteFile(pythonStub, []byte(pythonScript), 0o755); err != nil {
		t.Fatalf("write python stub: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "hotword.yaml")
	config := strings.Join([]string{
		`model_name: "sloppy"`,
		`output_dir: "output"`,
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	outputDir := filepath.Join(t.TempDir(), "models")
	cmd := exec.Command(filepath.Join(root, "scripts", "train-hotword.sh"), "--step", "resolve-config")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"TABURA_HOTWORD_TRAINER_DIR="+trainerDir,
		"TABURA_HOTWORD_CONFIG="+configPath,
		"TABURA_HOTWORD_OUTPUT_DIR="+outputDir,
		"TABURA_HOTWORD_SKIP_PIP_INSTALL=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("train-hotword resolve-config failed: %v\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "trainer step complete: resolve-config") {
		t.Fatalf("resolve-config output = %q, want completion marker", string(out))
	}
	if _, err := os.Stat(filepath.Join(outputDir, "sloppy.onnx")); err == nil {
		t.Fatalf("resolve-config unexpectedly created model")
	}
}

func TestTrainHotwordScriptCopiesDatedModelAndData(t *testing.T) {
	root := repoRootFromTestFile(t)
	trainerDir := filepath.Join(t.TempDir(), "trainer")
	if err := os.MkdirAll(filepath.Join(trainerDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir trainer git: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(trainerDir, ".venv", "bin"), 0o755); err != nil {
		t.Fatalf("mkdir trainer venv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trainerDir, "requirements.txt"), []byte(""), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trainerDir, "train_wakeword.py"), []byte("# stub\n"), 0o644); err != nil {
		t.Fatalf("write train_wakeword: %v", err)
	}

	trainerOutput := filepath.Join(t.TempDir(), "trainer-output")
	pythonStub := filepath.Join(trainerDir, ".venv", "bin", "python")
	pythonScript := `#!/usr/bin/env bash
set -euo pipefail
CONFIG_PATH=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --config)
      CONFIG_PATH="${2:-}"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
mkdir -p "` + trainerOutput + `"
printf 'trained-model' >"` + trainerOutput + `/sloppy.onnx"
printf 'trained-data' >"` + trainerOutput + `/sloppy.onnx.data"
touch -t 202603232103.09 "` + trainerOutput + `/sloppy.onnx"
touch -t 202603232103.09 "` + trainerOutput + `/sloppy.onnx.data"
echo "trained model: ` + trainerOutput + `/sloppy.onnx"
`
	if err := os.WriteFile(pythonStub, []byte(pythonScript), 0o755); err != nil {
		t.Fatalf("write python stub: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "hotword.yaml")
	config := strings.Join([]string{
		`model_name: "sloppy"`,
		`output_dir: "` + trainerOutput + `"`,
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	modelsDir := filepath.Join(t.TempDir(), "models")
	cmd := exec.Command(filepath.Join(root, "scripts", "train-hotword.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"TABURA_HOTWORD_TRAINER_DIR="+trainerDir,
		"TABURA_HOTWORD_CONFIG="+configPath,
		"TABURA_HOTWORD_OUTPUT_DIR="+modelsDir,
		"TABURA_HOTWORD_SKIP_PIP_INSTALL=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("train-hotword failed: %v\n%s", err, string(out))
	}
	matches, err := filepath.Glob(filepath.Join(modelsDir, "sloppy-*.onnx"))
	if err != nil {
		t.Fatalf("glob dated models: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("dated model files = %v, want exactly one\n%s", matches, string(out))
	}
	modelPath := matches[0]
	if _, err := os.Stat(modelPath + ".data"); err != nil {
		t.Fatalf("dated model data missing: %v\n%s", err, string(out))
	}
	if !strings.Contains(string(out), "trained model: "+modelPath) {
		t.Fatalf("train-hotword output = %q, want trained model path", string(out))
	}
}

func repoRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs repo root: %v", err)
	}
	return absRoot
}
