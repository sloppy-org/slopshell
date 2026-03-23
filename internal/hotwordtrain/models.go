package hotwordtrain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (m *Manager) ListModels() ([]Model, error) {
	models := make([]Model, 0)
	modelDirEntries, err := os.ReadDir(m.modelsDir())
	if err != nil && !isNotFound(err) {
		return nil, err
	}
	for _, entry := range modelDirEntries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".onnx") {
			continue
		}
		path := filepath.Join(m.modelsDir(), entry.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		models = append(models, Model{
			Name:       strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			FileName:   entry.Name(),
			Path:       path,
			CreatedAt:  info.ModTime().UTC().Format(time.RFC3339),
			SizeBytes:  modelTotalSize(path, info),
			Production: false,
		})
	}
	vendorPath := m.vendorModelPath()
	if info, err := os.Stat(vendorPath); err == nil && !info.IsDir() {
		models = append(models, Model{
			Name:       strings.TrimSuffix(filepath.Base(vendorPath), filepath.Ext(vendorPath)),
			FileName:   filepath.Base(vendorPath),
			Path:       vendorPath,
			CreatedAt:  info.ModTime().UTC().Format(time.RFC3339),
			SizeBytes:  modelTotalSize(vendorPath, info),
			Production: true,
		})
	}
	sortModels(models)
	return models, nil
}

func (m *Manager) DeployModel(fileName string) (Model, error) {
	clean := filepath.Base(strings.TrimSpace(fileName))
	if clean == "" {
		return Model{}, fmt.Errorf("missing model name")
	}
	sourcePath := filepath.Join(m.modelsDir(), clean)
	info, err := os.Stat(sourcePath)
	if err != nil || info.IsDir() {
		return Model{}, os.ErrNotExist
	}
	vendorPath := m.vendorModelPath()
	if err := m.ensureDir(filepath.Dir(vendorPath)); err != nil {
		return Model{}, err
	}
	if archived, err := m.archiveActiveModel(vendorPath); err != nil {
		return Model{}, err
	} else if archived != "" {
		_ = archived
	}
	if err := copyFile(sourcePath, vendorPath); err != nil {
		return Model{}, err
	}
	sourceDataPath := modelDataPath(sourcePath)
	vendorDataPath := modelDataPath(vendorPath)
	if modelFileExists(sourceDataPath) {
		if err := copyFile(sourceDataPath, vendorDataPath); err != nil {
			return Model{}, err
		}
	} else if err := os.Remove(vendorDataPath); err != nil && !os.IsNotExist(err) {
		return Model{}, err
	}
	sizeBytes := modelTotalSize(sourcePath, info)
	model := Model{
		Name:       strings.TrimSuffix(clean, filepath.Ext(clean)),
		FileName:   filepath.Base(vendorPath),
		Path:       vendorPath,
		CreatedAt:  nowRFC3339(),
		SizeBytes:  sizeBytes,
		Production: true,
	}
	return model, nil
}

func modelDataPath(path string) string {
	return path + ".data"
}

func modelTotalSize(path string, info os.FileInfo) int64 {
	size := info.Size()
	if dataInfo, err := os.Stat(modelDataPath(path)); err == nil && !dataInfo.IsDir() {
		size += dataInfo.Size()
	}
	return size
}

func modelArchiveFileName(fileName string, stamp time.Time, label string) string {
	base := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	ext := filepath.Ext(fileName)
	ts := stamp.UTC().Format("2006-01-02_15-04-05Z")
	if strings.TrimSpace(label) != "" {
		return fmt.Sprintf("%s-%s-%s%s", base, label, ts, ext)
	}
	return fmt.Sprintf("%s-%s%s", base, ts, ext)
}

func copyFile(sourcePath, targetPath string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, data, 0o644)
}

func (m *Manager) archiveActiveModel(vendorPath string) (string, error) {
	info, err := os.Stat(vendorPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("active model path is a directory: %s", vendorPath)
	}
	archiveName := modelArchiveFileName(filepath.Base(vendorPath), info.ModTime(), "production")
	archivePath := filepath.Join(m.modelsDir(), archiveName)
	if !modelFileExists(archivePath) {
		if err := copyFile(vendorPath, archivePath); err != nil {
			return "", err
		}
	}
	vendorDataPath := modelDataPath(vendorPath)
	archiveDataPath := modelDataPath(archivePath)
	if modelFileExists(vendorDataPath) && !modelFileExists(archiveDataPath) {
		if err := copyFile(vendorDataPath, archiveDataPath); err != nil {
			return "", err
		}
	}
	return archivePath, nil
}

func modelFileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
