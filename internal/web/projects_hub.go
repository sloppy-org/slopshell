package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/krystophny/tabura/internal/store"
)

func boolPtr(v bool) *bool {
	return &v
}

func isWelcomeDocName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case lower == "agents.md":
		return true
	case lower == "readme.md":
		return true
	case lower == "readme.markdown":
		return true
	case lower == "readme.txt":
		return true
	case strings.HasPrefix(lower, "readme."):
		return true
	case lower == "docs":
		return true
	}
	return false
}

func shouldSkipWelcomeWalkDir(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "", ".", "..", ".git", ".tabura", "node_modules":
		return true
	}
	return strings.HasPrefix(lower, ".")
}

func describeRecentFileTime(info os.FileInfo) string {
	if info == nil {
		return ""
	}
	modTime := info.ModTime()
	if modTime.IsZero() {
		return ""
	}
	age := time.Since(modTime)
	switch {
	case age < time.Minute:
		return "edited just now"
	case age < time.Hour:
		return fmt.Sprintf("edited %dm ago", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("edited %dh ago", int(age.Hours()))
	default:
		return fmt.Sprintf("edited %dd ago", int(age.Hours()/24))
	}
}

func discoverProjectWelcomeCards(rootPath string) ([]projectWelcomeCard, []projectWelcomeCard) {
	type fileInfo struct {
		rel  string
		info os.FileInfo
	}
	docCandidates := make([]projectWelcomeCard, 0, 6)
	recentFiles := make([]fileInfo, 0, 24)
	seenDocs := map[string]bool{}

	_ = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if path != rootPath && d.IsDir() && shouldSkipWelcomeWalkDir(name) {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(rootPath, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "" || strings.HasPrefix(rel, ".") {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		recentFiles = append(recentFiles, fileInfo{rel: rel, info: info})
		base := filepath.Base(rel)
		if isWelcomeDocName(base) && !seenDocs[rel] {
			seenDocs[rel] = true
			docCandidates = append(docCandidates, projectWelcomeCard{
				ID:          "doc-" + strings.ReplaceAll(rel, "/", "-"),
				Title:       base,
				Subtitle:    rel,
				Description: "Open documentation",
				Action: projectWelcomeAction{
					Type: "open_file",
					Path: rel,
				},
			})
		}
		return nil
	})

	sort.Slice(docCandidates, func(i, j int) bool {
		left := strings.ToLower(docCandidates[i].Subtitle)
		right := strings.ToLower(docCandidates[j].Subtitle)
		if left != right {
			return left < right
		}
		return docCandidates[i].Subtitle < docCandidates[j].Subtitle
	})
	if len(docCandidates) > 6 {
		docCandidates = docCandidates[:6]
	}

	sort.Slice(recentFiles, func(i, j int) bool {
		leftTime := recentFiles[i].info.ModTime()
		rightTime := recentFiles[j].info.ModTime()
		if !leftTime.Equal(rightTime) {
			return leftTime.After(rightTime)
		}
		return recentFiles[i].rel < recentFiles[j].rel
	})
	recentCards := make([]projectWelcomeCard, 0, 6)
	for _, candidate := range recentFiles {
		if len(recentCards) >= 6 {
			break
		}
		recentCards = append(recentCards, projectWelcomeCard{
			ID:          "recent-" + strings.ReplaceAll(candidate.rel, "/", "-"),
			Title:       filepath.Base(candidate.rel),
			Subtitle:    candidate.rel,
			Description: describeRecentFileTime(candidate.info),
			Action: projectWelcomeAction{
				Type: "open_file",
				Path: candidate.rel,
			},
		})
	}
	return docCandidates, recentCards
}

func (a *App) buildProjectWelcomeSections(project store.Project) []projectWelcomeSection {
	docCards, recentCards := discoverProjectWelcomeCards(project.RootPath)
	sections := make([]projectWelcomeSection, 0, 3)
	if len(recentCards) > 0 {
		sections = append(sections, projectWelcomeSection{
			ID:    "recent",
			Title: "Recent Files",
			Cards: recentCards,
		})
	}
	if len(docCards) > 0 {
		sections = append(sections, projectWelcomeSection{
			ID:    "docs",
			Title: "Documentation",
			Cards: docCards,
		})
	}
	sections = append(sections, projectWelcomeSection{
		ID:    "runtime",
		Title: "Modes",
		Cards: []projectWelcomeCard{
			{
				ID:          "silent",
				Title:       "Silent mode",
				Subtitle:    map[bool]string{true: "on", false: "off"}[a.silentModeEnabled()],
				Description: "Global runtime preference",
				Action: projectWelcomeAction{
					Type:       "set_silent_mode",
					SilentMode: boolPtr(!a.silentModeEnabled()),
				},
			},
		},
	})
	return sections
}

func (a *App) handleProjectWelcome(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspace_id"))
	project, err := a.resolveProjectByIDOrActive(workspaceID)
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	item, err := a.buildProjectAPIModel(project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sections := a.buildProjectWelcomeSections(project)
	writeJSON(w, projectWelcomeResponse{
		OK:          true,
		WorkspaceID: projectIDString(project.ID),
		Project:     item,
		Scope:       "project",
		Title:       strings.TrimSpace(project.Name),
		Sections:    sections,
	})
}
