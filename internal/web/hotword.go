package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/hotwordtrain"
)

const (
	hotwordModelFileName = "sloppy.onnx"
)

var hotwordRuntimeAssetFiles = []string{
	"melspectrogram.onnx",
	"embedding_model.onnx",
	hotwordModelFileName,
}

func (a *App) hotwordProjectRoot() string {
	root := strings.TrimSpace(a.localProjectDir)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	return abs
}

func hotwordVendorDir(root string) string {
	return filepath.Join(root, "internal", "web", "static", "vendor", "openwakeword")
}

func hotwordVendorModelPath(root string) string {
	return filepath.Join(hotwordVendorDir(root), hotwordModelFileName)
}

func hotwordModelDataPath(path string) string {
	return path + ".data"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func checkHotwordStatus(root string) map[string]interface{} {
	vendorDir := hotwordVendorDir(root)
	missing := make([]string, 0, len(hotwordRuntimeAssetFiles))
	for _, file := range hotwordRuntimeAssetFiles {
		if !fileExists(filepath.Join(vendorDir, file)) {
			missing = append(missing, file)
		}
	}
	ready := len(missing) == 0
	modelPath := hotwordVendorModelPath(root)
	model := map[string]interface{}{
		"exists":   false,
		"file":     hotwordModelFileName,
		"revision": "",
	}
	if info, err := os.Stat(modelPath); err == nil && !info.IsDir() {
		modifiedAt := info.ModTime().UTC().Format(time.RFC3339)
		sizeBytes := info.Size()
		if dataInfo, err := os.Stat(hotwordModelDataPath(modelPath)); err == nil && !dataInfo.IsDir() {
			sizeBytes += dataInfo.Size()
		}
		model["exists"] = true
		model["modified_at"] = modifiedAt
		model["size_bytes"] = sizeBytes
		model["revision"] = fmt.Sprintf("%s:%d", modifiedAt, sizeBytes)
	}
	return map[string]interface{}{
		"ok":                   true,
		"model":                model,
		"ready":                ready,
		"missing":              missing,
		"training_in_progress": false,
	}
}

func (a *App) handleHotwordStatus(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	root := a.hotwordProjectRoot()
	status := checkHotwordStatus(root)
	if a.hotwordTrainer != nil {
		training := a.hotwordTrainer.TrainingStatus()
		status["training_in_progress"] = training.State == "running"
		status["training_status"] = training
		feedback, err := a.hotwordTrainer.ListFeedback()
		if err == nil {
			status["feedback_summary"] = hotwordtrain.SummarizeFeedback(feedback)
		}
	}
	writeJSON(w, status)
}
