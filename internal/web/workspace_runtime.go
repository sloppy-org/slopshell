package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/protocol"
	"github.com/krystophny/tabura/internal/store"
)

type projectCreateRequest struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	Path            string `json:"path"`
	MCPURL          string `json:"mcp_url"`
	SourceProjectID string `json:"source_project_id"`
	Activate        *bool  `json:"activate"`
}

type projectAPIModel struct {
	ID                       string          `json:"id"`
	Name                     string          `json:"name"`
	Kind                     string          `json:"kind"`
	RootPath                 string          `json:"root_path"`
	Sphere                   string          `json:"sphere,omitempty"`
	ProjectKey               string          `json:"project_key"`
	MCPURL                   string          `json:"mcp_url,omitempty"`
	IsDefault                bool            `json:"is_default"`
	ChatSessionID            string          `json:"chat_session_id"`
	ChatMode                 string          `json:"chat_mode"`
	ChatModel                string          `json:"chat_model"`
	ChatModelReasoningEffort string          `json:"chat_model_reasoning_effort"`
	CanvasSessionID          string          `json:"canvas_session_id"`
	RunState                 projectRunState `json:"run_state"`
	Unread                   bool            `json:"unread"`
	ReviewPending            bool            `json:"review_pending"`
}

type projectChatModelRequest struct {
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
}

type projectFileEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
}

type projectWelcomeAction struct {
	Type          string `json:"type"`
	ProjectID     string `json:"project_id,omitempty"`
	Path          string `json:"path,omitempty"`
	SilentMode    *bool  `json:"silent_mode,omitempty"`
	StartupTarget string `json:"startup_behavior,omitempty"`
}

type projectWelcomeCard struct {
	ID          string               `json:"id"`
	Title       string               `json:"title"`
	Subtitle    string               `json:"subtitle,omitempty"`
	Description string               `json:"description,omitempty"`
	Action      projectWelcomeAction `json:"action"`
}

type projectWelcomeSection struct {
	ID    string               `json:"id"`
	Title string               `json:"title"`
	Cards []projectWelcomeCard `json:"cards"`
}

type projectWelcomeResponse struct {
	OK        bool                    `json:"ok"`
	ProjectID string                  `json:"project_id"`
	Project   projectAPIModel         `json:"project"`
	Scope     string                  `json:"scope"`
	Title     string                  `json:"title"`
	Sections  []projectWelcomeSection `json:"sections"`
}

type projectActivityItem struct {
	ProjectID     string          `json:"project_id"`
	ProjectKey    string          `json:"project_key"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	ChatSessionID string          `json:"chat_session_id"`
	ChatMode      string          `json:"chat_mode"`
	RunState      projectRunState `json:"run_state"`
	Unread        bool            `json:"unread"`
	ReviewPending bool            `json:"review_pending"`
}

func normalizeProjectKindInput(kind, path string) string {
	cleanKind := strings.ToLower(strings.TrimSpace(kind))
	switch cleanKind {
	case "managed", "linked", "meeting", "task":
		return cleanKind
	}
	if strings.TrimSpace(path) != "" {
		return "linked"
	}
	return "managed"
}

func isTemporaryProjectKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "meeting", "task":
		return true
	default:
		return false
	}
}

func isTemporaryProject(project store.Project) bool {
	return isTemporaryProjectKind(project.Kind)
}

func defaultProjectNameFromPath(path string) string {
	base := strings.TrimSpace(filepath.Base(path))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "Project"
	}
	return base
}

func slugifyProjectName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return "project"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteRune('-')
				lastDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "project"
	}
	return slug
}

func defaultTemporaryProjectName(kind string, now time.Time) string {
	label := "Task"
	if strings.EqualFold(strings.TrimSpace(kind), "meeting") {
		label = "Meeting"
	}
	return fmt.Sprintf("%s %s", label, now.Format("2006-01-02 15:04"))
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

func (a *App) ensureDefaultProjectRecord() (store.Project, error) {
	localProjectKey := strings.TrimSpace(a.localProjectDir)
	if localProjectKey != "" {
		existing, err := a.store.GetProjectByProjectKey(localProjectKey)
		if err == nil {
			canvasID := a.canvasSessionIDForProject(existing)
			mcpURL := strings.TrimSpace(existing.MCPURL)
			if mcpURL == "" {
				mcpURL = strings.TrimSpace(a.localMCPURL)
			}
			if canvasID != strings.TrimSpace(existing.CanvasSessionID) || mcpURL != strings.TrimSpace(existing.MCPURL) {
				_ = a.store.UpdateProjectRuntime(existing.ID, mcpURL, canvasID)
				if refreshed, refreshErr := a.store.GetProject(existing.ID); refreshErr == nil {
					existing = refreshed
				}
			}
			return existing, nil
		}
		if !isNoRows(err) {
			return store.Project{}, err
		}
	}

	projects, err := a.store.ListProjects()
	if err != nil {
		return store.Project{}, err
	}
	for _, project := range projects {
		if !project.IsDefault {
			continue
		}
		canvasID := a.canvasSessionIDForProject(project)
		if canvasID != strings.TrimSpace(project.CanvasSessionID) {
			if err := a.store.UpdateProjectRuntime(project.ID, strings.TrimSpace(project.MCPURL), canvasID); err == nil {
				if refreshed, refreshErr := a.store.GetProject(project.ID); refreshErr == nil {
					project = refreshed
				}
			}
		}
		return project, nil
	}

	kind := "managed"
	rootPath := filepath.Join(a.dataDir, "projects", "default")
	name := "Default Project"
	if localProjectKey != "" {
		kind = "linked"
		rootPath = localProjectKey
		name = defaultProjectNameFromPath(rootPath)
	}
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return store.Project{}, err
	}
	absRoot = filepath.Clean(absRoot)
	if kind == "managed" {
		if err := os.MkdirAll(absRoot, 0o755); err != nil {
			return store.Project{}, err
		}
		boot, err := protocol.BootstrapProject(absRoot)
		if err != nil {
			return store.Project{}, err
		}
		absRoot = filepath.Clean(boot.Paths.ProjectDir)
		name = defaultProjectNameFromPath(absRoot)
	}
	projectKey := absRoot
	if existing, err := a.store.GetProjectByProjectKey(projectKey); err == nil {
		return existing, nil
	} else if !isNoRows(err) {
		return store.Project{}, err
	}
	if existing, err := a.store.GetProjectByRootPath(absRoot); err == nil {
		return existing, nil
	} else if !isNoRows(err) {
		return store.Project{}, err
	}

	created, err := a.store.CreateProject(
		name,
		projectKey,
		absRoot,
		kind,
		strings.TrimSpace(a.localMCPURL),
		LocalSessionID,
		true,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			if existing, lookupErr := a.store.GetProjectByProjectKey(projectKey); lookupErr == nil {
				return existing, nil
			}
		}
		return store.Project{}, err
	}
	return created, nil
}

func (a *App) listProjectsWithDefault() ([]store.Project, store.Project, error) {
	defaultProject, err := a.ensureDefaultProjectRecord()
	if err != nil {
		return nil, store.Project{}, err
	}
	projects, err := a.store.ListProjects()
	if err != nil {
		return nil, store.Project{}, err
	}
	if len(projects) == 0 {
		return []store.Project{defaultProject}, defaultProject, nil
	}
	return projects, defaultProject, nil
}

func (a *App) chooseActiveProject(projects []store.Project, defaultProject store.Project) (store.Project, error) {
	if len(projects) == 0 {
		return store.Project{}, errors.New("no projects available")
	}
	activeSphere := a.runtimeActiveSphere()
	if workspace, err := a.store.ActiveWorkspace(); err == nil {
		if cleanSphere := normalizeRuntimeActiveSphere(workspace.Sphere); cleanSphere != "" && cleanSphere != activeSphere {
			if err := a.store.SetActiveSphere(cleanSphere); err != nil {
				return store.Project{}, err
			}
			activeSphere = cleanSphere
		}
		for _, project := range projects {
			if workspace.ProjectID != nil && project.ID == strings.TrimSpace(*workspace.ProjectID) {
				if err := a.store.SetActiveProjectID(project.ID); err != nil {
					return store.Project{}, err
				}
				return project, nil
			}
		}
	} else if !isNoRows(err) {
		return store.Project{}, err
	}
	activeID, err := a.store.ActiveProjectID()
	if err != nil {
		return store.Project{}, err
	}
	if activeID != "" {
		for _, project := range projects {
			if project.ID != activeID {
				continue
			}
			rank, err := a.projectSelectionRank(project, activeSphere)
			if err != nil {
				return store.Project{}, err
			}
			if rank < 4 {
				return project, nil
			}
		}
	}

	bestIndex := -1
	bestRank := 5
	for i, project := range projects {
		rank, err := a.projectSelectionRank(project, activeSphere)
		if err != nil {
			return store.Project{}, err
		}
		if rank >= 4 {
			continue
		}
		if bestIndex == -1 || rank < bestRank {
			bestIndex = i
			bestRank = rank
		}
	}

	fallback := defaultProject
	if bestIndex >= 0 {
		fallback = projects[bestIndex]
	} else if strings.TrimSpace(fallback.ID) == "" {
		fallback = projects[0]
	}
	if err := a.store.SetActiveProjectID(fallback.ID); err != nil {
		return store.Project{}, err
	}
	return fallback, nil
}

func (a *App) handleProjectsList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	if err := a.ensureStartupProjectWithWorkspace(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	projects, defaultProject, err := a.listProjectsWithDefault()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	activeProject, err := a.chooseActiveProject(projects, defaultProject)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]projectAPIModel, 0, len(projects))
	for _, project := range projects {
		item, err := a.buildProjectAPIModel(project)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, map[string]interface{}{
		"ok":                 true,
		"default_project_id": defaultProject.ID,
		"active_project_id":  activeProject.ID,
		"projects":           items,
	})
}

func (a *App) handleProjectsActivity(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	projects, _, err := a.listProjectsWithDefault()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]projectActivityItem, 0, len(projects))
	for _, project := range projects {
		item, err := a.buildProjectActivityItem(project)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"projects": items,
	})
}

func (a *App) findProjectByCanvasSession(sessionID string) (store.Project, error) {
	cleanSessionID := strings.TrimSpace(sessionID)
	if cleanSessionID == "" {
		return store.Project{}, sql.ErrNoRows
	}
	project, err := a.store.GetProjectByCanvasSession(cleanSessionID)
	if err == nil {
		return project, nil
	}
	if !isNoRows(err) {
		return store.Project{}, err
	}
	if cleanSessionID == LocalSessionID {
		return a.ensureDefaultProjectRecord()
	}
	return store.Project{}, sql.ErrNoRows
}
