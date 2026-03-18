package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/protocol"
	"github.com/krystophny/tabura/internal/store"
)

type projectCreateRequest struct {
	Name              string `json:"name"`
	Kind              string `json:"kind"`
	Path              string `json:"path"`
	MCPURL            string `json:"mcp_url"`
	SourceWorkspaceID string `json:"source_workspace_id"`
	Activate          *bool  `json:"activate"`
}

type projectAPIModel struct {
	ID                       string          `json:"id"`
	Name                     string          `json:"name"`
	Kind                     string          `json:"kind"`
	RootPath                 string          `json:"root_path"`
	Sphere                   string          `json:"sphere,omitempty"`
	WorkspacePath            string          `json:"workspace_path"`
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
	WorkspaceID   string `json:"workspace_id,omitempty"`
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
	OK          bool                    `json:"ok"`
	WorkspaceID string                  `json:"workspace_id"`
	Project     projectAPIModel         `json:"workspace"`
	Scope       string                  `json:"scope"`
	Title       string                  `json:"title"`
	Sections    []projectWelcomeSection `json:"sections"`
}

type projectActivityItem struct {
	WorkspaceID   string          `json:"workspace_id"`
	WorkspacePath string          `json:"workspace_path"`
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	ChatSessionID string          `json:"chat_session_id"`
	ChatMode      string          `json:"chat_mode"`
	RunState      projectRunState `json:"run_state"`
	Unread        bool            `json:"unread"`
	ReviewPending bool            `json:"review_pending"`
}

func projectIDString(id int64) string {
	return strconv.FormatInt(id, 10)
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

func isTaburaRepoPath(path string) bool {
	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return false
	}
	goModPath := filepath.Join(cleanPath, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "module github.com/krystophny/tabura")
}

func defaultProjectNameForPath(path string) string {
	if isTaburaRepoPath(path) {
		return "Tabura"
	}
	return defaultProjectNameFromPath(path)
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

func (a *App) runtimeNow() time.Time {
	if a.calendarNow != nil {
		return a.calendarNow()
	}
	return time.Now()
}

func dailyWorkspaceDate(now time.Time) string {
	return now.Format("2006-01-02")
}

func (a *App) dailyWorkspacePath(now time.Time) string {
	return filepath.Join(a.dataDir, "daily", now.Format("2006"), now.Format("01"), now.Format("02"))
}

func workspaceDailyDate(workspace store.Workspace) string {
	if workspace.DailyDate == nil {
		return ""
	}
	return strings.TrimSpace(*workspace.DailyDate)
}

func (a *App) ensureTodayDailyWorkspace() (store.Workspace, error) {
	now := a.runtimeNow()
	dirPath := a.dailyWorkspacePath(now)
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return store.Workspace{}, err
	}
	workspace, err := a.store.EnsureDailyWorkspace(dailyWorkspaceDate(now), dirPath)
	if err != nil {
		return store.Workspace{}, err
	}
	active, activeErr := a.store.ActiveWorkspace()
	switch {
	case activeErr == nil:
		if active.ID != workspace.ID && active.IsDaily && workspaceDailyDate(active) != dailyWorkspaceDate(now) {
			if err := a.setActiveWorkspaceTracked(workspace.ID, "workspace_switch"); err != nil {
				return store.Workspace{}, err
			}
			workspace, err = a.store.GetWorkspace(workspace.ID)
			if err != nil {
				return store.Workspace{}, err
			}
		}
	case isNoRows(activeErr):
		if err := a.store.SetActiveWorkspace(workspace.ID); err != nil {
			return store.Workspace{}, err
		}
		workspace, err = a.store.GetWorkspace(workspace.ID)
		if err != nil {
			return store.Workspace{}, err
		}
	default:
		return store.Workspace{}, activeErr
	}
	if _, err := a.store.GetOrCreateChatSessionForWorkspace(workspace.ID); err != nil {
		return store.Workspace{}, err
	}
	return workspace, nil
}

func (a *App) ensureStartupWorkspace() (store.Workspace, error) {
	workspace, err := a.store.ActiveWorkspace()
	switch {
	case err == nil:
		if workspace.IsDaily && workspaceDailyDate(workspace) != dailyWorkspaceDate(a.runtimeNow()) {
			return a.ensureTodayDailyWorkspace()
		}
		if _, err := a.store.GetOrCreateChatSessionForWorkspace(workspace.ID); err != nil {
			return store.Workspace{}, err
		}
		return workspace, nil
	case !isNoRows(err):
		return store.Workspace{}, err
	default:
		return a.ensureTodayDailyWorkspace()
	}
}

func (a *App) ensureDefaultProjectRecord() (store.Project, error) {
	localWorkspacePath := strings.TrimSpace(a.localProjectDir)
	if localWorkspacePath != "" {
		existing, err := a.store.GetProjectByWorkspacePath(localWorkspacePath)
		if err == nil {
			canvasID := a.canvasSessionIDForProject(existing)
			mcpURL := strings.TrimSpace(existing.MCPURL)
			targetName := defaultProjectNameForPath(localWorkspacePath)
			if mcpURL == "" {
				mcpURL = strings.TrimSpace(a.localMCPURL)
			}
			if strings.TrimSpace(existing.Name) != targetName {
				_ = a.store.UpdateProjectLocation(projectIDString(existing.ID), targetName, existing.WorkspacePath, existing.RootPath, existing.Kind)
			}
			if canvasID != strings.TrimSpace(existing.CanvasSessionID) || mcpURL != strings.TrimSpace(existing.MCPURL) || strings.TrimSpace(existing.Name) != targetName {
				_ = a.store.UpdateProjectRuntime(projectIDString(existing.ID), mcpURL, canvasID)
				if refreshed, refreshErr := a.store.GetProject(projectIDString(existing.ID)); refreshErr == nil {
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
			if err := a.store.UpdateProjectRuntime(projectIDString(project.ID), strings.TrimSpace(project.MCPURL), canvasID); err == nil {
				if refreshed, refreshErr := a.store.GetProject(projectIDString(project.ID)); refreshErr == nil {
					project = refreshed
				}
			}
		}
		return project, nil
	}

	kind := "managed"
	rootPath := filepath.Join(a.dataDir, "projects", "default")
	name := "Default Project"
	if localWorkspacePath != "" {
		kind = "linked"
		rootPath = localWorkspacePath
		name = defaultProjectNameForPath(rootPath)
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
		name = defaultProjectNameForPath(absRoot)
	}
	workspacePath := absRoot
	if existing, err := a.store.GetProjectByWorkspacePath(workspacePath); err == nil {
		targetName := defaultProjectNameForPath(absRoot)
		if strings.TrimSpace(existing.Name) != targetName {
			_ = a.store.UpdateProjectLocation(projectIDString(existing.ID), targetName, existing.WorkspacePath, existing.RootPath, existing.Kind)
			if refreshed, refreshErr := a.store.GetProject(projectIDString(existing.ID)); refreshErr == nil {
				existing = refreshed
			}
		}
		return existing, nil
	} else if !isNoRows(err) {
		return store.Project{}, err
	}
	if existing, err := a.store.GetProjectByRootPath(absRoot); err == nil {
		targetName := defaultProjectNameForPath(absRoot)
		if strings.TrimSpace(existing.Name) != targetName {
			_ = a.store.UpdateProjectLocation(projectIDString(existing.ID), targetName, existing.WorkspacePath, existing.RootPath, existing.Kind)
			if refreshed, refreshErr := a.store.GetProject(projectIDString(existing.ID)); refreshErr == nil {
				existing = refreshed
			}
		}
		return existing, nil
	} else if !isNoRows(err) {
		return store.Project{}, err
	}

	created, err := a.store.CreateProject(
		name,
		workspacePath,
		absRoot,
		kind,
		strings.TrimSpace(a.localMCPURL),
		LocalSessionID,
		false,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			if existing, lookupErr := a.store.GetProjectByWorkspacePath(workspacePath); lookupErr == nil {
				return existing, nil
			}
		}
		return store.Project{}, err
	}
	if _, activeErr := a.store.ActiveWorkspace(); isNoRows(activeErr) {
		if err := a.store.SetActiveWorkspaceID(projectIDString(created.ID)); err != nil {
			return store.Project{}, err
		}
		if created.ID <= 0 {
			return store.Project{}, errors.New("invalid workspace id")
		}
		if err := a.store.SetActiveWorkspace(created.ID); err != nil {
			return store.Project{}, err
		}
		created, err = a.store.GetProject(projectIDString(created.ID))
		if err != nil {
			return store.Project{}, err
		}
	} else if activeErr != nil {
		return store.Project{}, activeErr
	}
	return created, nil
}

func (a *App) listProjectsWithDefault() ([]store.Project, store.Project, error) {
	projects, err := a.store.ListProjects()
	if err != nil {
		return nil, store.Project{}, err
	}
	if len(projects) == 0 {
		return nil, store.Project{}, errors.New("no projects available")
	}
	defaultProject := store.Project{}
	for _, project := range projects {
		if project.IsDefault {
			defaultProject = project
			break
		}
	}
	if defaultProject.ID == 0 {
		defaultProject = projects[0]
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
			if project.ID == workspace.ID {
				if err := a.store.SetActiveWorkspaceID(projectIDString(project.ID)); err != nil {
					return store.Project{}, err
				}
				return project, nil
			}
		}
	} else if !isNoRows(err) {
		return store.Project{}, err
	}
	activeID, err := a.store.ActiveWorkspaceID()
	if err != nil {
		return store.Project{}, err
	}
	if activeID != "" {
		for _, project := range projects {
			if projectIDString(project.ID) != activeID {
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
	} else if fallback.ID == 0 {
		fallback = projects[0]
	}
	if err := a.store.SetActiveWorkspaceID(projectIDString(fallback.ID)); err != nil {
		return store.Project{}, err
	}
	return fallback, nil
}

func (a *App) handleProjectsList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	if _, err := a.ensureStartupWorkspace(); err != nil {
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
		"ok":                   true,
		"default_workspace_id": projectIDString(defaultProject.ID),
		"active_workspace_id":  projectIDString(activeProject.ID),
		"workspaces":           items,
	})
}

func (a *App) handleProjectsActivity(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	if _, err := a.ensureStartupWorkspace(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
		"ok":         true,
		"workspaces": items,
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
