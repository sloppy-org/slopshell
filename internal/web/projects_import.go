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

type temporaryProjectPersistRequest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

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

func (a *App) projectSourceByID(workspaceID string) (store.Project, bool, error) {
	id := strings.TrimSpace(workspaceID)
	if id == "" {
		return store.Project{}, false, nil
	}
	project, err := a.store.GetProject(id)
	if err != nil {
		return store.Project{}, false, err
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
	sourceProject, hasSource, err := a.projectSourceByID(req.SourceWorkspaceID)
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
	workspacePath := absRoot

	if existing, err := a.store.GetProjectByWorkspacePath(workspacePath); err == nil {
		return existing, false, nil
	} else if !isNoRows(err) {
		return store.Project{}, false, err
	}
	if existing, err := a.store.GetProjectByRootPath(absRoot); err == nil {
		return existing, false, nil
	} else if !isNoRows(err) {
		return store.Project{}, false, err
	}

	created, err := a.store.CreateProject(name, workspacePath, absRoot, kind, mcpURL, "", false)
	if err != nil {
		if isUniqueConstraint(err) {
			if existing, lookupErr := a.store.GetProjectByWorkspacePath(workspacePath); lookupErr == nil {
				return existing, false, nil
			}
		}
		return store.Project{}, false, err
	}
	if hasSource {
		if err := a.inheritProjectSettings(projectIDString(created.ID), sourceProject); err != nil {
			return store.Project{}, false, err
		}
		refreshed, refreshErr := a.store.GetProject(projectIDString(created.ID))
		if refreshErr != nil {
			return store.Project{}, false, refreshErr
		}
		created = refreshed
	}
	return created, true, nil
}

func (a *App) persistTemporaryProjectTarget(project store.Project, req temporaryProjectPersistRequest) (string, string, error) {
	targetName := strings.TrimSpace(req.Name)
	targetPath := strings.TrimSpace(req.Path)
	if targetPath == "" {
		targetPath = project.RootPath
	}
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return "", "", err
	}
	absTarget = filepath.Clean(absTarget)
	if targetName == "" {
		targetName = defaultProjectNameFromPath(absTarget)
	}
	if targetName == "" {
		targetName = project.Name
	}
	if absTarget != filepath.Clean(project.RootPath) {
		if _, err := os.Stat(absTarget); err == nil {
			return "", "", errors.New("target path already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		if existing, err := a.store.GetProjectByRootPath(absTarget); err == nil && existing.ID != project.ID {
			return "", "", errors.New("path is already used by another project")
		} else if err != nil && !isNoRows(err) {
			return "", "", err
		}
		if _, err := a.store.GetWorkspaceByPath(absTarget); err == nil {
			return "", "", errors.New("path is already used by another workspace")
		} else if err != nil && !isNoRows(err) {
			return "", "", err
		}
	}
	return targetName, absTarget, nil
}

func (a *App) updateWorkspaceArtifactPaths(workspaceID int64, oldRoot, newRoot string) error {
	if filepath.Clean(oldRoot) == filepath.Clean(newRoot) {
		return nil
	}
	artifacts, err := a.store.ListArtifactsForWorkspace(workspaceID)
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if artifact.RefPath == nil || !pathWithinRoot(*artifact.RefPath, oldRoot) {
			continue
		}
		rel, err := filepath.Rel(oldRoot, *artifact.RefPath)
		if err != nil {
			return err
		}
		nextPath := filepath.Join(newRoot, rel)
		if err := a.store.UpdateArtifact(artifact.ID, store.ArtifactUpdate{RefPath: &nextPath}); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) persistTemporaryProject(workspaceID string, req temporaryProjectPersistRequest) (store.Project, error) {
	project, err := a.store.GetProject(strings.TrimSpace(workspaceID))
	if err != nil {
		return store.Project{}, err
	}
	if !isTemporaryProject(project) {
		return store.Project{}, errors.New("project is not temporary")
	}
	workspace, err := a.ensureWorkspaceForProject(project, false)
	if err != nil {
		return store.Project{}, err
	}
	targetName, targetPath, err := a.persistTemporaryProjectTarget(project, req)
	if err != nil {
		return store.Project{}, err
	}
	oldRoot := filepath.Clean(project.RootPath)
	if targetPath != oldRoot {
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return store.Project{}, err
		}
		if err := os.Rename(oldRoot, targetPath); err != nil {
			return store.Project{}, err
		}
		if _, err := protocol.BootstrapProject(targetPath); err != nil {
			return store.Project{}, err
		}
		if err := a.updateWorkspaceArtifactPaths(workspace.ID, oldRoot, targetPath); err != nil {
			return store.Project{}, err
		}
	}
	if _, err := a.store.UpdateWorkspaceLocation(workspace.ID, targetName, targetPath); err != nil {
		return store.Project{}, err
	}
	if err := a.store.UpdateProjectLocation(projectIDString(project.ID), targetName, targetPath, targetPath, "managed"); err != nil {
		return store.Project{}, err
	}
	return a.activateProject(projectIDString(project.ID))
}

func (a *App) temporaryProjectDiscardRoot(project store.Project) string {
	root := filepath.Clean(project.RootPath)
	base := filepath.Clean(filepath.Join(a.dataDir, "projects", "temporary"))
	if !pathWithinRoot(root, base) {
		return ""
	}
	return root
}

func (a *App) fallbackProjectAfterDiscard(discardedWorkspaceID string) (store.Project, error) {
	defaultProject, err := a.ensureDefaultProjectRecord()
	if err == nil && projectIDString(defaultProject.ID) != strings.TrimSpace(discardedWorkspaceID) {
		return defaultProject, nil
	}
	projects, err := a.store.ListProjects()
	if err != nil {
		return store.Project{}, err
	}
	for _, project := range projects {
		if projectIDString(project.ID) != strings.TrimSpace(discardedWorkspaceID) {
			return project, nil
		}
	}
	return store.Project{}, sql.ErrNoRows
}

func (a *App) discardTemporaryProject(workspaceID string) (store.Project, error) {
	project, err := a.store.GetProject(strings.TrimSpace(workspaceID))
	if err != nil {
		return store.Project{}, err
	}
	if !isTemporaryProject(project) {
		return store.Project{}, errors.New("project is not temporary")
	}
	workspaces, err := a.store.ListWorkspacesForProject(projectIDString(project.ID))
	if err != nil {
		return store.Project{}, err
	}
	if workspace, workspaceErr := a.store.GetWorkspaceByPath(project.RootPath); workspaceErr == nil {
		found := false
		for _, existing := range workspaces {
			if existing.ID == workspace.ID {
				found = true
				break
			}
		}
		if !found {
			workspaces = append(workspaces, workspace)
		}
	} else if workspaceErr != nil && !isNoRows(workspaceErr) {
		return store.Project{}, workspaceErr
	}
	discardRoot := a.temporaryProjectDiscardRoot(project)
	fallback, fallbackErr := a.fallbackProjectAfterDiscard(projectIDString(project.ID))
	if err := a.store.DeleteProject(projectIDString(project.ID)); err != nil {
		return store.Project{}, err
	}
	for _, workspace := range workspaces {
		if err := a.store.DeleteWorkspace(workspace.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return store.Project{}, err
		}
	}
	if discardRoot != "" {
		if removeErr := os.RemoveAll(discardRoot); removeErr != nil {
			return store.Project{}, removeErr
		}
	}
	if fallbackErr != nil {
		return store.Project{}, fallbackErr
	}
	return a.activateProject(projectIDString(fallback.ID))
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
		if project, err = a.activateProject(projectIDString(project.ID)); err != nil {
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
		"workspace": item,
		"project":   item,
	})
}

func (a *App) handleTemporaryProjectPersist(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspace_id"))
	if workspaceID == "" {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}
	var req temporaryProjectPersistRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
	}
	project, err := a.persistTemporaryProject(workspaceID, req)
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
		"ok":        true,
		"workspace": item,
		"project":   item,
	})
}

func (a *App) handleTemporaryProjectDiscard(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspace_id"))
	if workspaceID == "" {
		http.Error(w, "workspace_id is required", http.StatusBadRequest)
		return
	}
	activeProject, err := a.discardTemporaryProject(workspaceID)
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
		"ok":                  true,
		"discarded_workspace": workspaceID,
		"active_workspace_id": projectIDString(activeProject.ID),
		"active_workspace":    item,
		"active_project":      item,
	})
}
