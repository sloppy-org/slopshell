package tabura_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTrainHotwordScriptPublishesConfiguredModel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script test")
	}

	repoRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	tempDir := t.TempDir()
	trainerDir := filepath.Join(tempDir, "trainer")
	if err := os.MkdirAll(filepath.Join(trainerDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir trainer git dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(trainerDir, ".venv", "bin"), 0o755); err != nil {
		t.Fatalf("mkdir trainer venv dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(trainerDir, "requirements.txt"), []byte(""), 0o644); err != nil {
		t.Fatalf("write requirements: %v", err)
	}
	if err := os.Symlink(pythonPath, filepath.Join(trainerDir, ".venv", "bin", "python")); err != nil {
		t.Fatalf("symlink python: %v", err)
	}

	configPath := filepath.Join(tempDir, "hotword-config.yaml")
	if err := os.WriteFile(configPath, []byte("model_name: \"sloppy\"\noutput_dir: \"output\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	trainerScript := `from pathlib import Path
import argparse
import re

parser = argparse.ArgumentParser()
parser.add_argument("--config", required=True)
args, _ = parser.parse_known_args()

config = Path(args.config).read_text()
model = re.search(r'^\s*model_name\s*:\s*"?(.*?)"?\s*$', config, re.M).group(1)
output_dir = re.search(r'^\s*output_dir\s*:\s*"?(.*?)"?\s*$', config, re.M).group(1)
output_path = Path(__file__).resolve().parent / output_dir / f"{model}.onnx"
output_path.parent.mkdir(parents=True, exist_ok=True)
output_path.write_bytes(b"sloppy-model")
`
	if err := os.WriteFile(filepath.Join(trainerDir, "train_wakeword.py"), []byte(trainerScript), 0o755); err != nil {
		t.Fatalf("write trainer script: %v", err)
	}

	outputDir := filepath.Join(tempDir, "models", "hotword")
	cmd := exec.Command(filepath.Join(repoRoot, "scripts", "train-hotword.sh"))
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"TABURA_HOTWORD_TRAINER_DIR="+trainerDir,
		"TABURA_HOTWORD_CONFIG="+configPath,
		"TABURA_HOTWORD_OUTPUT_DIR="+outputDir,
		"TABURA_HOTWORD_SKIP_PIP_INSTALL=1",
		"PYTHON=python3",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("train-hotword.sh failed: %v\n%s", err, out)
	}

	matches, err := filepath.Glob(filepath.Join(outputDir, "sloppy-*.onnx"))
	if err != nil {
		t.Fatalf("glob published model: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("published models = %v, want exactly one\n%s", matches, out)
	}
	modelPath := matches[0]
	data, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("read published model: %v", err)
	}
	if string(data) != "sloppy-model" {
		t.Fatalf("published model contents = %q, want %q", string(data), "sloppy-model")
	}
	if !strings.Contains(string(out), "trained model: "+modelPath) {
		t.Fatalf("stdout = %q, want trained model path %q", string(out), modelPath)
	}
}
