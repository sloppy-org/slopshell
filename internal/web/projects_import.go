package web

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/krystophny/tabura/internal/protocol"
	"github.com/krystophny/tabura/internal/store"
)

func (a *App) nextManagedProjectPath(name string) (string, error) {
	baseDir := filepath.Join(a.dataDir, "projects")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", err
	}
	slug := slugifyProjectName(name)
	for i := 0; i < 500; i++ {
		candidate := slug
		if i > 0 {
			candidate = candidate + "-" + strconv.Itoa(i+1)
		}
		path := filepath.Join(baseDir, candidate)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
	}
	return "", errors.New("unable to allocate managed project path")
}

func (a *App) nextTemporaryProjectPath(kind, name string) (string, error) {
	baseDir := filepath.Join(a.dataDir, "projects", "temporary", strings.ToLower(strings.TrimSpace(kind)))
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", err
	}
	slug := slugifyProjectName(name)
	for i := 0; i < 500; i++ {
		candidate := slug
		if i > 0 {
			candidate = candidate + "-" + strconv.Itoa(i+1)
		}
		path := filepath.Join(baseDir, candidate)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
	}
	return "", errors.New("unable to allocate temporary project path")
}

func (a *App) projectSourceByID(projectID string) (store.Project, bool, error) {
	id := strings.TrimSpace(projectID)
	if id == "" {
		return store.Project{}, false, nil
	}
	project, err := a.store.GetProject(id)
	if err != nil {
		return store.Project{}, false, err
	}
	if isHubProject(project) {
		return store.Project{}, false, nil
	}
	return project, true, nil
}

func (a *App) inheritProjectSettings(targetID string, source store.Project) error {
	if strings.TrimSpace(source.ChatModel) != "" {
		if err := a.store.UpdateProjectChatModel(targetID, source.ChatModel); err != nil {
			return err
		}
	}
	if strings.TrimSpace(source.ChatModelReasoningEffort) != "" {
		if err := a.store.UpdateProjectChatModelReasoningEffort(targetID, source.ChatModelReasoningEffort); err != nil {
			return err
		}
	}
	if strings.TrimSpace(source.CompanionConfigJSON) != "" {
		if err := a.store.UpdateProjectCompanionConfig(targetID, source.CompanionConfigJSON); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) createProject(req projectCreateRequest) (store.Project, bool, error) {
	kind := normalizeProjectKindInput(req.Kind, req.Path)
	name := strings.TrimSpace(req.Name)
	mcpURL := strings.TrimSpace(req.MCPURL)
	sourceProject, hasSource, err := a.projectSourceByID(req.SourceProjectID)
	if err != nil {
		return store.Project{}, false, err
	}

	var absRoot string
	switch kind {
	case "linked":
		rootPath := strings.TrimSpace(req.Path)
		if rootPath == "" {
			return store.Project{}, false, errors.New("path is required for linked projects")
		}
		info, err := os.Stat(rootPath)
		if err != nil {
			return store.Project{}, false, err
		}
		if !info.IsDir() {
			return store.Project{}, false, errors.New("path must be a directory")
		}
		boot, err := protocol.BootstrapProject(rootPath)
		if err != nil {
			return store.Project{}, false, err
		}
		absRoot = filepath.Clean(boot.Paths.ProjectDir)
	case "meeting", "task":
		if strings.TrimSpace(req.Path) != "" {
			return store.Project{}, false, errors.New("path is not supported for temporary projects")
		}
		if name == "" {
			name = defaultTemporaryProjectName(kind, time.Now())
		}
		rootPath, err := a.nextTemporaryProjectPath(kind, name)
		if err != nil {
			return store.Project{}, false, err
		}
		if err := os.MkdirAll(rootPath, 0o755); err != nil {
			return store.Project{}, false, err
		}
		boot, err := protocol.BootstrapProject(rootPath)
		if err != nil {
			return store.Project{}, false, err
		}
		absRoot = filepath.Clean(boot.Paths.ProjectDir)
	default:
		rootPath := strings.TrimSpace(req.Path)
		if rootPath == "" {
			nextPath, err := a.nextManagedProjectPath(name)
			if err != nil {
				return store.Project{}, false, err
			}
			rootPath = nextPath
		}
		if err := os.MkdirAll(rootPath, 0o755); err != nil {
			return store.Project{}, false, err
		}
		boot, err := protocol.BootstrapProject(rootPath)
		if err != nil {
			return store.Project{}, false, err
		}
		absRoot = filepath.Clean(boot.Paths.ProjectDir)
	}

	if name == "" {
		name = defaultProjectNameFromPath(absRoot)
	}
	projectKey := absRoot

	if existing, err := a.store.GetProjectByProjectKey(projectKey); err == nil {
		return existing, false, nil
	} else if !isNoRows(err) {
		return store.Project{}, false, err
	}
	if existing, err := a.store.GetProjectByRootPath(absRoot); err == nil {
		return existing, false, nil
	} else if !isNoRows(err) {
		return store.Project{}, false, err
	}

	created, err := a.store.CreateProject(name, projectKey, absRoot, kind, mcpURL, "", false)
	if err != nil {
		if isUniqueConstraint(err) {
			if existing, lookupErr := a.store.GetProjectByProjectKey(projectKey); lookupErr == nil {
				return existing, false, nil
			}
		}
		return store.Project{}, false, err
	}
	if hasSource {
		if err := a.inheritProjectSettings(created.ID, sourceProject); err != nil {
			return store.Project{}, false, err
		}
		refreshed, refreshErr := a.store.GetProject(created.ID)
		if refreshErr != nil {
			return store.Project{}, false, refreshErr
		}
		created = refreshed
	}
	return created, true, nil
}

func (a *App) persistTemporaryProject(projectID string) (store.Project, error) {
	project, err := a.store.GetProject(strings.TrimSpace(projectID))
	if err != nil {
		return store.Project{}, err
	}
	if !isTemporaryProject(project) {
		return store.Project{}, errors.New("project is not temporary")
	}
	if err := a.store.UpdateProjectKind(project.ID, "managed"); err != nil {
		return store.Project{}, err
	}
	return a.store.GetProject(project.ID)
}

func (a *App) temporaryProjectDiscardRoot(project store.Project) string {
	root := filepath.Clean(project.RootPath)
	base := filepath.Clean(filepath.Join(a.dataDir, "projects", "temporary"))
	if !pathWithinRoot(root, base) {
		return ""
	}
	return root
}

func (a *App) fallbackProjectAfterDiscard(discardedProjectID string) (store.Project, error) {
	if hub, err := a.ensureHubProject(); err == nil && hub.ID != strings.TrimSpace(discardedProjectID) {
		return hub, nil
	}
	defaultProject, err := a.ensureDefaultProjectRecord()
	if err == nil && defaultProject.ID != strings.TrimSpace(discardedProjectID) {
		return defaultProject, nil
	}
	projects, err := a.store.ListProjects()
	if err != nil {
		return store.Project{}, err
	}
	for _, project := range projects {
		if project.ID != strings.TrimSpace(discardedProjectID) {
			return project, nil
		}
	}
	return store.Project{}, sql.ErrNoRows
}

func (a *App) discardTemporaryProject(projectID string) (store.Project, error) {
	project, err := a.store.GetProject(strings.TrimSpace(projectID))
	if err != nil {
		return store.Project{}, err
	}
	if !isTemporaryProject(project) {
		return store.Project{}, errors.New("project is not temporary")
	}
	discardRoot := a.temporaryProjectDiscardRoot(project)
	fallback, fallbackErr := a.fallbackProjectAfterDiscard(project.ID)
	if err := a.store.DeleteProject(project.ID); err != nil {
		return store.Project{}, err
	}
	if discardRoot != "" {
		if removeErr := os.RemoveAll(discardRoot); removeErr != nil {
			return store.Project{}, removeErr
		}
	}
	if fallbackErr != nil {
		return store.Project{}, fallbackErr
	}
	return a.activateProject(fallback.ID)
}

func (a *App) handleProjectCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req projectCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	project, created, err := a.createProject(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	activate := true
	if req.Activate != nil {
		activate = *req.Activate
	}
	if activate {
		if project, err = a.activateProject(project.ID); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	item, err := a.buildProjectAPIModel(project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":        true,
		"created":   created,
		"activated": activate,
		"project":   item,
	})
}

func (a *App) handleTemporaryProjectPersist(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	projectID := strings.TrimSpace(chi.URLParam(r, "project_id"))
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	project, err := a.persistTemporaryProject(projectID)
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	item, err := a.buildProjectAPIModel(project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"project": item,
	})
}

func (a *App) handleTemporaryProjectDiscard(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	projectID := strings.TrimSpace(chi.URLParam(r, "project_id"))
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	activeProject, err := a.discardTemporaryProject(projectID)
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	item, err := a.buildProjectAPIModel(activeProject)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":                true,
		"discarded_project": projectID,
		"active_project_id": activeProject.ID,
		"active_project":    item,
	})
}
