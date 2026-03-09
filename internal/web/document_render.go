package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/document"
)

const documentRenderTimeout = 90 * time.Second

func isDocumentSourceFilePath(path string) bool {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".tex":
		return true
	default:
		return false
	}
}

func shouldRenderDocumentArtifact(projectRoot, inputPath string) bool {
	if isDocumentSourceFilePath(inputPath) {
		return true
	}
	if !isPandocDocumentSourcePath(inputPath) {
		return false
	}
	cfg, err := document.LoadBuildConfig(projectRoot)
	if err == nil {
		if strings.TrimSpace(cfg.MainFile) != "" {
			mainFile, mainErr := filepath.Abs(filepath.Join(projectRoot, filepath.FromSlash(cfg.MainFile)))
			if mainErr == nil && sameDocumentPath(mainFile, inputPath) {
				return true
			}
		}
		if cfg.Builder == "pandoc" && strings.TrimSpace(cfg.MainFile) == "" {
			return true
		}
	}
	return markdownHasFrontMatter(inputPath)
}

func isPandocDocumentSourcePath(path string) bool {
	switch strings.ToLower(strings.TrimSpace(filepath.Ext(path))) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}

func markdownHasFrontMatter(path string) bool {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(string(bytes), "---\n")
}

func sameDocumentPath(left, right string) bool {
	cleanLeft := filepath.Clean(strings.TrimSpace(left))
	cleanRight := filepath.Clean(strings.TrimSpace(right))
	if cleanLeft == cleanRight {
		return true
	}
	absLeft, errLeft := filepath.Abs(cleanLeft)
	absRight, errRight := filepath.Abs(cleanRight)
	return errLeft == nil && errRight == nil && absLeft == absRight
}

func (a *App) renderDocumentArtifact(projectRoot, inputPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), documentRenderTimeout)
	defer cancel()
	result, err := document.BuildWorkspaceDocument(ctx, projectRoot, inputPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(projectRoot, result.PDFPath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
