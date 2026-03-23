package hotwordtrain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDeployModelCopiesDataAndArchivesActiveProduction(t *testing.T) {
	dataDir := t.TempDir()
	projectRoot := t.TempDir()
	manager := New(dataDir, projectRoot)

	modelsDir := filepath.Join(dataDir, "hotword-train", "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	vendorDir := filepath.Join(projectRoot, "internal", "web", "static", "vendor", "openwakeword")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatalf("mkdir vendor dir: %v", err)
	}

	activePath := filepath.Join(vendorDir, "sloppy.onnx")
	if err := os.WriteFile(activePath, []byte("old-model"), 0o644); err != nil {
		t.Fatalf("write active model: %v", err)
	}
	if err := os.WriteFile(activePath+".data", []byte("old-data"), 0o644); err != nil {
		t.Fatalf("write active model data: %v", err)
	}
	oldTime := time.Date(2026, time.March, 22, 10, 5, 0, 0, time.UTC)
	if err := os.Chtimes(activePath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes active model: %v", err)
	}

	candidateName := "sloppy-2026-03-23_21-03-09Z.onnx"
	candidatePath := filepath.Join(modelsDir, candidateName)
	if err := os.WriteFile(candidatePath, []byte("new-model"), 0o644); err != nil {
		t.Fatalf("write candidate model: %v", err)
	}
	if err := os.WriteFile(candidatePath+".data", []byte("new-data"), 0o644); err != nil {
		t.Fatalf("write candidate model data: %v", err)
	}

	model, err := manager.DeployModel(candidateName)
	if err != nil {
		t.Fatalf("DeployModel: %v", err)
	}
	if !model.Production {
		t.Fatalf("deployed model production = false, want true")
	}
	if model.FileName != "sloppy.onnx" {
		t.Fatalf("deployed model file name = %q, want sloppy.onnx", model.FileName)
	}

	deployed, err := os.ReadFile(activePath)
	if err != nil {
		t.Fatalf("read deployed model: %v", err)
	}
	if string(deployed) != "new-model" {
		t.Fatalf("deployed model = %q, want new-model", string(deployed))
	}
	deployedData, err := os.ReadFile(activePath + ".data")
	if err != nil {
		t.Fatalf("read deployed model data: %v", err)
	}
	if string(deployedData) != "new-data" {
		t.Fatalf("deployed model data = %q, want new-data", string(deployedData))
	}

	models, err := manager.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	archived := ""
	for _, entry := range models {
		if strings.HasPrefix(entry.FileName, "sloppy-production-2026-03-22_10-05-00Z") {
			archived = entry.FileName
			break
		}
	}
	if archived == "" {
		t.Fatalf("archived production model missing from models list: %#v", models)
	}
	archivedData, err := os.ReadFile(filepath.Join(modelsDir, archived+".data"))
	if err != nil {
		t.Fatalf("read archived model data: %v", err)
	}
	if string(archivedData) != "old-data" {
		t.Fatalf("archived model data = %q, want old-data", string(archivedData))
	}
}
