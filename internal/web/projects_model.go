package web

import (
	"context"
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
	"github.com/krystophny/tabura/internal/serve"
	"github.com/krystophny/tabura/internal/store"
)

const projectServeStartTimeout = 10 * time.Second

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

func (a *App) buildProjectAPIModel(project store.Project) (projectAPIModel, error) {
	session, err := a.chatSessionForProject(project)
	if err != nil {
		return projectAPIModel{}, err
	}
	sphere, err := a.projectSphere(project)
	if err != nil {
		return projectAPIModel{}, err
	}
	alias := a.effectiveProjectChatModelAlias(project)
	effort := strings.TrimSpace(modelprofile.NormalizeReasoningEffort(alias, project.ChatModelReasoningEffort))
	unread, reviewPending := a.projectUnreadState(project, session)
	return projectAPIModel{
		ID:                       project.ID,
		Name:                     project.Name,
		Kind:                     project.Kind,
		RootPath:                 project.RootPath,
		Sphere:                   sphere,
		ProjectKey:               project.ProjectKey,
		MCPURL:                   strings.TrimSpace(project.MCPURL),
		IsDefault:                project.IsDefault,
		ChatSessionID:            session.ID,
		ChatMode:                 session.Mode,
		ChatModel:                alias,
		ChatModelReasoningEffort: effort,
		CanvasSessionID:          a.canvasSessionIDForProject(project),
		RunState:                 a.projectRunStateForSession(session.ID),
		Unread:                   unread,
		ReviewPending:            reviewPending,
	}, nil
}

func (a *App) projectSphere(project store.Project) (string, error) {
	workspaceID, err := a.store.FindWorkspaceContainingPath(project.RootPath)
	if err != nil || workspaceID == nil {
		return "", err
	}
	workspace, err := a.store.GetWorkspace(*workspaceID)
	if err != nil {
		return "", err
	}
	return workspace.Sphere, nil
}

func (a *App) projectSelectionRank(project store.Project, activeSphere string) (int, error) {
	projectSphere, err := a.projectSphere(project)
	if err != nil {
		return 0, err
	}
	cleanProjectSphere := normalizeRuntimeActiveSphere(projectSphere)
	cleanActiveSphere := normalizeRuntimeActiveSphere(activeSphere)
	switch {
	case cleanActiveSphere != "" && cleanProjectSphere == cleanActiveSphere:
		return 0, nil
	case cleanProjectSphere == "" && project.IsDefault:
		return 1, nil
	case cleanProjectSphere == "":
		return 2, nil
	default:
		return 4, nil
	}
}

func (a *App) buildProjectActivityItem(project store.Project) (projectActivityItem, error) {
	session, err := a.chatSessionForProject(project)
	if err != nil {
		return projectActivityItem{}, err
	}
	unread, reviewPending := a.projectUnreadState(project, session)
	return projectActivityItem{
		ProjectID:     project.ID,
		ProjectKey:    project.ProjectKey,
		Name:          project.Name,
		Kind:          project.Kind,
		ChatSessionID: session.ID,
		ChatMode:      session.Mode,
		RunState:      a.projectRunStateForSession(session.ID),
		Unread:        unread,
		ReviewPending: reviewPending,
	}, nil
}

func (a *App) projectUnreadState(project store.Project, session store.ChatSession) (bool, bool) {
	lastSeenAt, lastCanvasChangeAt, lastReviewSubmitAt := a.projectAttention.snapshot(project.ProjectKey)
	dbSeenAt := project.LastOpenedAt * int64(time.Second)
	if lastSeenAt < dbSeenAt {
		lastSeenAt = dbSeenAt
	}
	reviewPending := strings.EqualFold(session.Mode, "review") && lastCanvasChangeAt > lastReviewSubmitAt
	unread := lastCanvasChangeAt > lastSeenAt || reviewPending
	activeProjectID, err := a.store.ActiveProjectID()
	if err == nil && strings.TrimSpace(activeProjectID) == project.ID && !reviewPending {
		unread = false
	}
	return unread, reviewPending
}

func (a *App) markProjectSeen(project store.Project) error {
	if err := a.store.TouchProject(project.ID); err != nil {
		return err
	}
	a.projectAttention.markSeen(project.ProjectKey, time.Now().UnixNano())
	return nil
}

func (a *App) markProjectReviewSubmitted(project store.Project) error {
	now := time.Now().UnixNano()
	if err := a.store.TouchProject(project.ID); err != nil {
		return err
	}
	a.projectAttention.markSeen(project.ProjectKey, now)
	a.projectAttention.markReviewSubmitted(project.ProjectKey, now)
	return nil
}

func (a *App) markProjectOutput(projectKey string) {
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return
	}
	now := time.Now().UnixNano()
	a.projectAttention.markCanvasChange(key, now)
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return
	}
	activeProjectID, err := a.store.ActiveProjectID()
	if err != nil || strings.TrimSpace(activeProjectID) != project.ID {
		return
	}
	session, err := a.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil || strings.EqualFold(session.Mode, "review") {
		return
	}
	a.projectAttention.markSeen(project.ProjectKey, now)
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
	projectApp := serve.NewApp(projectDir, "")
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
	projectSphere, err := a.projectSphere(project)
	if err != nil {
		return store.Project{}, err
	}
	if cleanSphere := normalizeRuntimeActiveSphere(projectSphere); cleanSphere != "" && cleanSphere != a.runtimeActiveSphere() {
		if err := a.store.SetActiveSphere(cleanSphere); err != nil {
			return store.Project{}, err
		}
	}
	if err := a.ensureProjectCanvasReady(project); err != nil {
		return store.Project{}, err
	}
	if err := a.store.SetActiveProjectID(project.ID); err != nil {
		return store.Project{}, err
	}
	if _, err := a.ensureWorkspaceForProject(project, true); err != nil {
		return store.Project{}, err
	}
	if err := a.markProjectSeen(project); err != nil {
		return store.Project{}, err
	}
	if _, _, err := a.syncTimeTrackingContext("project_switch"); err != nil {
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
		"active_sphere":     a.runtimeActiveSphere(),
		"project":           item,
	})
}

func (a *App) updateProjectChatModel(projectID, rawModel, rawReasoningEffort string) (store.Project, error) {
	project, err := a.store.GetProject(strings.TrimSpace(projectID))
	if err != nil {
		return store.Project{}, err
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

func (a *App) handleWorkspaceFilesList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	workspace, err := a.resolveWorkspaceByIDOrActive(chi.URLParam(r, "workspace_id"))
	if err != nil {
		if isNoRows(err) {
			http.Error(w, "workspace not found", http.StatusNotFound)
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
	rootPath := filepath.Clean(strings.TrimSpace(workspace.DirPath))
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
		"ok":           true,
		"workspace_id": workspace.ID,
		"path":         relPath,
		"is_root":      relPath == "",
		"entries":      items,
	})
}
