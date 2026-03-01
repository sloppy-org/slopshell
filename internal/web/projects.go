package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/krystophny/tabura/internal/modelprofile"
	"github.com/krystophny/tabura/internal/protocol"
	"github.com/krystophny/tabura/internal/serve"
	"github.com/krystophny/tabura/internal/store"
)

const projectServeStartTimeout = 10 * time.Second

type projectCreateRequest struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	MCPURL   string `json:"mcp_url"`
	Activate *bool  `json:"activate"`
}

type projectAPIModel struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	Kind                     string `json:"kind"`
	RootPath                 string `json:"root_path"`
	ProjectKey               string `json:"project_key"`
	MCPURL                   string `json:"mcp_url,omitempty"`
	IsDefault                bool   `json:"is_default"`
	ChatSessionID            string `json:"chat_session_id"`
	ChatMode                 string `json:"chat_mode"`
	ChatModel                string `json:"chat_model"`
	ChatModelReasoningEffort string `json:"chat_model_reasoning_effort"`
	CanvasSessionID          string `json:"canvas_session_id"`
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

func normalizeProjectKindInput(kind, path string) string {
	cleanKind := strings.ToLower(strings.TrimSpace(kind))
	switch cleanKind {
	case "managed", "linked":
		return cleanKind
	}
	if strings.TrimSpace(path) != "" {
		return "linked"
	}
	return "managed"
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

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique")
}

func (a *App) canvasSessionIDForProject(project store.Project) string {
	sessionID := strings.TrimSpace(project.CanvasSessionID)
	if sessionID != "" {
		return sessionID
	}
	if project.IsDefault {
		return LocalSessionID
	}
	return project.ID
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
	if _, err := a.ensureHubProject(); err != nil {
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
	activeID, err := a.store.ActiveProjectID()
	if err != nil {
		return store.Project{}, err
	}
	if activeID != "" {
		for _, project := range projects {
			if project.ID == activeID {
				return project, nil
			}
		}
	}
	fallback := defaultProject
	if strings.TrimSpace(fallback.ID) == "" {
		fallback = projects[0]
	}
	if err := a.store.SetActiveProjectID(fallback.ID); err != nil {
		return store.Project{}, err
	}
	return fallback, nil
}

func (a *App) buildProjectAPIModel(project store.Project) (projectAPIModel, error) {
	session, err := a.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		return projectAPIModel{}, err
	}
	return projectAPIModel{
		ID:                       project.ID,
		Name:                     project.Name,
		Kind:                     project.Kind,
		RootPath:                 project.RootPath,
		ProjectKey:               project.ProjectKey,
		MCPURL:                   strings.TrimSpace(project.MCPURL),
		IsDefault:                project.IsDefault,
		ChatSessionID:            session.ID,
		ChatMode:                 session.Mode,
		ChatModel:                a.effectiveProjectChatModelAlias(project),
		ChatModelReasoningEffort: strings.TrimSpace(project.ChatModelReasoningEffort),
		CanvasSessionID:          a.canvasSessionIDForProject(project),
	}, nil
}

func (a *App) handleProjectsList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
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

func (a *App) nextManagedProjectPath(name string) (string, error) {
	baseDir := filepath.Join(a.dataDir, "projects")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", err
	}
	slug := slugifyProjectName(name)
	for i := 0; i < 500; i++ {
		candidate := slug
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", slug, i+1)
		}
		path := filepath.Join(baseDir, candidate)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return path, nil
		}
	}
	return "", errors.New("unable to allocate managed project path")
}

func (a *App) createProject(req projectCreateRequest) (store.Project, bool, error) {
	kind := normalizeProjectKindInput(req.Kind, req.Path)
	name := strings.TrimSpace(req.Name)
	mcpURL := strings.TrimSpace(req.MCPURL)

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
	return created, true, nil
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

func chooseLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || addr.Port <= 0 {
		return 0, errors.New("unable to allocate tcp port")
	}
	return addr.Port, nil
}

func (a *App) startProjectServe(sessionID, projectDir string) error {
	sessionID = strings.TrimSpace(sessionID)
	projectDir = strings.TrimSpace(projectDir)
	if sessionID == "" {
		return errors.New("project session is required")
	}
	if projectDir == "" {
		return errors.New("project path is required")
	}
	if a.tunnels.hasPort(sessionID) {
		return nil
	}

	port, err := chooseLoopbackPort()
	if err != nil {
		return err
	}
	projectApp := serve.NewApp(projectDir)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = projectApp.Start("127.0.0.1", port)
	}()
	deadline := time.Now().Add(projectServeStartTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			cancel()
			return errors.New("project serve canceled")
		default:
		}
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				a.tunnels.setProjectServe(sessionID, projectApp, cancel)
				a.tunnels.setPort(sessionID, port)
				a.startCanvasRelay(sessionID, port)
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = projectApp.Stop(stopCtx)
	return errors.New("project tabura MCP listener did not become healthy in time")
}

func (a *App) ensureProjectCanvasReady(project store.Project) error {
	sessionID := a.canvasSessionIDForProject(project)
	if a.tunnels.hasPort(sessionID) {
		return nil
	}

	if mcpURL := strings.TrimSpace(project.MCPURL); mcpURL != "" {
		port, err := extractPort(mcpURL)
		if err != nil {
			return err
		}
		a.tunnels.setPort(sessionID, port)
		a.startCanvasRelay(sessionID, port)
		return nil
	}

	if sessionID == LocalSessionID && strings.TrimSpace(a.localProjectDir) != "" {
		if err := a.startLocalServe(); err != nil {
			return err
		}
		if a.tunnels.hasPort(sessionID) {
			return nil
		}
	}

	return a.startProjectServe(sessionID, project.RootPath)
}

func (a *App) activateProject(projectID string) (store.Project, error) {
	project, err := a.store.GetProject(strings.TrimSpace(projectID))
	if err != nil {
		return store.Project{}, err
	}
	if err := a.ensureProjectCanvasReady(project); err != nil {
		return store.Project{}, err
	}
	if err := a.store.SetActiveProjectID(project.ID); err != nil {
		return store.Project{}, err
	}
	if err := a.store.TouchProject(project.ID); err != nil {
		return store.Project{}, err
	}
	return a.store.GetProject(project.ID)
}

func (a *App) handleProjectActivate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	projectID := strings.TrimSpace(chi.URLParam(r, "project_id"))
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	project, err := a.activateProject(projectID)
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	item, err := a.buildProjectAPIModel(project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":                true,
		"active_project_id": project.ID,
		"project":           item,
	})
}

func (a *App) updateProjectChatModel(projectID, rawModel, rawReasoningEffort string) (store.Project, error) {
	project, err := a.store.GetProject(strings.TrimSpace(projectID))
	if err != nil {
		return store.Project{}, err
	}
	if isHubProject(project) {
		return store.Project{}, errors.New("hub model is fixed to spark/low")
	}
	modelAlias := modelprofile.ResolveAlias(rawModel, "")
	if modelAlias == "" {
		return store.Project{}, errors.New("model must be one of: codex, gpt, spark")
	}
	reasoningEffort := strings.TrimSpace(modelprofile.NormalizeReasoningEffort(modelAlias, rawReasoningEffort))
	if reasoningEffort == "" {
		reasoningEffort = strings.TrimSpace(modelprofile.MainThreadReasoningEffort(modelAlias))
	}
	if err := a.store.UpdateProjectChatModel(project.ID, modelAlias); err != nil {
		return store.Project{}, err
	}
	if err := a.store.UpdateProjectChatModelReasoningEffort(project.ID, reasoningEffort); err != nil {
		return store.Project{}, err
	}
	_ = a.store.SetAppState(appStateDefaultChatModelKey, modelAlias)
	updated, err := a.store.GetProject(project.ID)
	if err != nil {
		return store.Project{}, err
	}
	a.resetProjectChatAppSession(updated.ProjectKey)
	return updated, nil
}

func (a *App) handleProjectChatModelUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	projectID := strings.TrimSpace(chi.URLParam(r, "project_id"))
	if projectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	var req projectChatModelRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	project, err := a.updateProjectChatModel(projectID, req.Model, req.ReasoningEffort)
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

func (a *App) resolveProjectByIDOrActive(projectID string) (store.Project, error) {
	id := strings.TrimSpace(projectID)
	if id == "" || strings.EqualFold(id, "active") {
		projects, defaultProject, err := a.listProjectsWithDefault()
		if err != nil {
			return store.Project{}, err
		}
		return a.chooseActiveProject(projects, defaultProject)
	}
	return a.store.GetProject(id)
}

func normalizeProjectListPath(raw string) (string, error) {
	cleaned := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" || cleaned == "." {
		return "", nil
	}
	if strings.ContainsRune(cleaned, '\x00') {
		return "", errors.New("invalid path")
	}
	parts := strings.Split(cleaned, "/")
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", errors.New("invalid path")
		default:
			normalized = append(normalized, part)
		}
	}
	return strings.Join(normalized, "/"), nil
}

func pathWithinRoot(path, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if cleanPath == cleanRoot {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator))
}

func (a *App) handleProjectContext(w http.ResponseWriter, r *http.Request) {
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
	project, err = a.activateProject(project.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	item, err := a.buildProjectAPIModel(project)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":                true,
		"active_project_id": project.ID,
		"project":           item,
	})
}

func (a *App) handleProjectFilesList(w http.ResponseWriter, r *http.Request) {
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

	relPath, err := normalizeProjectListPath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rootPath := filepath.Clean(project.RootPath)
	targetPath := rootPath
	if relPath != "" {
		targetPath = filepath.Join(rootPath, filepath.FromSlash(relPath))
	}
	targetPath = filepath.Clean(targetPath)
	if !pathWithinRoot(targetPath, rootPath) {
		http.Error(w, "invalid path", http.StatusForbidden)
		return
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "path not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !info.IsDir() {
		http.Error(w, "path is not a directory", http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]projectFileEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}
		entryPath := name
		if relPath != "" {
			entryPath = relPath + "/" + name
		}
		items = append(items, projectFileEntry{
			Name:  name,
			Path:  entryPath,
			IsDir: entry.IsDir(),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].IsDir != items[j].IsDir {
			return items[i].IsDir
		}
		leftLower := strings.ToLower(items[i].Name)
		rightLower := strings.ToLower(items[j].Name)
		if leftLower != rightLower {
			return leftLower < rightLower
		}
		return items[i].Name < items[j].Name
	})
	writeJSON(w, map[string]interface{}{
		"ok":         true,
		"project_id": project.ID,
		"path":       relPath,
		"is_root":    relPath == "",
		"entries":    items,
	})
}

func (a *App) resolveProjectKey(projectID, projectKey string) (string, error) {
	key := strings.TrimSpace(projectKey)
	if key != "" {
		return key, nil
	}
	id := strings.TrimSpace(projectID)
	if id != "" {
		project, err := a.store.GetProject(id)
		if err != nil {
			return "", err
		}
		return project.ProjectKey, nil
	}
	activeID, err := a.store.ActiveProjectID()
	if err != nil {
		return "", err
	}
	if activeID != "" {
		project, err := a.store.GetProject(activeID)
		if err == nil {
			return project.ProjectKey, nil
		}
		if !isNoRows(err) {
			return "", err
		}
	}
	defaultProject, err := a.ensureDefaultProjectRecord()
	if err != nil {
		return "", err
	}
	return defaultProject.ProjectKey, nil
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
