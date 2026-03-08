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

func nextWelcomeInputMode(current string) string {
	switch normalizeRuntimeInputMode(current) {
	case "voice":
		return "pen"
	case "pen":
		return "keyboard"
	default:
		return "voice"
	}
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

func (a *App) buildHubWelcomeSections(projects []store.Project, activeProjectID string) []projectWelcomeSection {
	projectCards := make([]projectWelcomeCard, 0, len(projects))
	for _, project := range projects {
		if isHubProject(project) {
			continue
		}
		item, err := a.buildProjectAPIModel(project)
		if err != nil {
			continue
		}
		subtitle := strings.TrimSpace(project.RootPath)
		if project.ID == activeProjectID {
			subtitle = "current active project"
		}
		description := "Open project canvas"
		switch item.RunState.Status {
		case "running":
			description = fmt.Sprintf("%d active run, %d queued", item.RunState.ActiveTurns, item.RunState.QueuedTurns)
		case "queued":
			description = fmt.Sprintf("%d queued run", item.RunState.QueuedTurns)
		}
		projectCards = append(projectCards, projectWelcomeCard{
			ID:          "project-" + project.ID,
			Title:       strings.TrimSpace(project.Name),
			Subtitle:    subtitle,
			Description: description,
			Action: projectWelcomeAction{
				Type:      "switch_project",
				ProjectID: project.ID,
			},
		})
	}
	sort.Slice(projectCards, func(i, j int) bool {
		left := strings.ToLower(projectCards[i].Title)
		right := strings.ToLower(projectCards[j].Title)
		if left != right {
			return left < right
		}
		return projectCards[i].Title < projectCards[j].Title
	})
	quickCards := []projectWelcomeCard{
		{
			ID:          "pref-silent",
			Title:       "Silent mode",
			Subtitle:    map[bool]string{true: "on", false: "off"}[a.silentModeEnabled()],
			Description: "Global runtime preference across projects",
			Action: projectWelcomeAction{
				Type:       "set_silent_mode",
				SilentMode: boolPtr(!a.silentModeEnabled()),
			},
		},
		{
			ID:          "pref-input",
			Title:       "Input mode",
			Subtitle:    a.runtimeInputMode(),
			Description: "Switch between voice, pen, and keyboard input",
			Action: projectWelcomeAction{
				Type:      "set_input_mode",
				InputMode: nextWelcomeInputMode(a.runtimeInputMode()),
			},
		},
		{
			ID:          "pref-startup",
			Title:       "Startup",
			Subtitle:    a.runtimeStartupBehavior(),
			Description: "Fresh app loads start in Hub",
			Action: projectWelcomeAction{
				Type:          "set_startup_behavior",
				StartupTarget: "hub_first",
			},
		},
	}
	sections := []projectWelcomeSection{}
	if len(projectCards) > 0 {
		sections = append(sections, projectWelcomeSection{
			ID:    "projects",
			Title: "Active Projects",
			Cards: projectCards,
		})
	}
	sections = append(sections, projectWelcomeSection{
		ID:    "runtime",
		Title: "Runtime",
		Cards: quickCards,
	})
	return sections
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
				ID:          "go-hub",
				Title:       "Hub",
				Subtitle:    "Return to project switchboard",
				Description: "Open the Hub canvas",
				Action: projectWelcomeAction{
					Type:      "switch_project",
					ProjectID: "hub",
				},
			},
			{
				ID:          "typing",
				Title:       "Input mode",
				Subtitle:    a.runtimeInputMode(),
				Description: "Global runtime preference",
				Action: projectWelcomeAction{
					Type:      "set_input_mode",
					InputMode: nextWelcomeInputMode(a.runtimeInputMode()),
				},
			},
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
	projectID := strings.TrimSpace(chi.URLParam(r, "project_id"))
	project, err := a.resolveProjectByIDOrActive(projectID)
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
	scope := "project"
	title := strings.TrimSpace(project.Name)
	var sections []projectWelcomeSection
	if isHubProject(project) {
		scope = "hub"
		title = "Hub"
		projects, _, listErr := a.listProjectsWithDefault()
		if listErr != nil {
			http.Error(w, listErr.Error(), http.StatusInternalServerError)
			return
		}
		sections = a.buildHubWelcomeSections(projects, project.ID)
	} else {
		sections = a.buildProjectWelcomeSections(project)
	}
	writeJSON(w, projectWelcomeResponse{
		OK:        true,
		ProjectID: project.ID,
		Project:   item,
		Scope:     scope,
		Title:     title,
		Sections:  sections,
	})
}
