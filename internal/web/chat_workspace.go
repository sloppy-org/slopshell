package web

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

var (
	workspaceCreatePattern        = regexp.MustCompile(`(?i)^(?:create|add|register)\s+workspace\s+(.+?)$`)
	workspaceScratchCreatePattern = regexp.MustCompile(`(?i)^(?:create|add)\s+scratch\s+workspace(?:\s+(.+?))?$`)
	workspaceRenamePattern        = regexp.MustCompile(`(?i)^rename\s+workspace\s+(.+?)\s+to\s+(.+?)$`)
	workspaceDeletePattern        = regexp.MustCompile(`(?i)^(?:delete|remove)\s+workspace\s+(.+?)$`)
	workspaceDetailsPattern       = regexp.MustCompile(`(?i)^(?:show\s+)?workspace\s+details(?:\s+for\s+(.+?))?$`)
)

func parseInlineWorkspaceIntent(text string) *SystemAction {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if action := parseCreateWorkspaceFromGitIntent(trimmed); action != nil {
		return action
	}
	lower := normalizeItemCommandText(trimmed)
	switch lower {
	case "show items here", "show open items here", "show items for this workspace", "what's open", "what is open", "what's open here", "what is open here":
		return &SystemAction{Action: "list_workspace_items", Params: map[string]interface{}{}}
	case "list workspaces", "show my workspaces", "show workspaces":
		return &SystemAction{Action: "list_workspaces", Params: map[string]interface{}{}}
	case "list all workspaces", "show all workspaces":
		return &SystemAction{Action: "list_workspaces", Params: map[string]interface{}{"all_spheres": true}}
	case "show workspace details":
		return &SystemAction{Action: "show_workspace_details", Params: map[string]interface{}{}}
	}
	if match := workspaceScratchCreatePattern.FindStringSubmatch(trimmed); len(match) == 2 {
		params := map[string]interface{}{"scratch": true}
		if name := cleanWorkspaceReference(match[1]); name != "" {
			params["name"] = name
		}
		return &SystemAction{Action: "create_workspace", Params: params}
	}
	if match := workspaceRenamePattern.FindStringSubmatch(trimmed); len(match) == 3 {
		workspaceRef := cleanWorkspaceReference(match[1])
		newName := cleanWorkspaceReference(match[2])
		if workspaceRef != "" && newName != "" {
			return &SystemAction{
				Action: "rename_workspace",
				Params: map[string]interface{}{"workspace": workspaceRef, "new_name": newName},
			}
		}
	}
	if match := workspaceDeletePattern.FindStringSubmatch(trimmed); len(match) == 2 {
		if workspaceRef := cleanWorkspaceReference(match[1]); workspaceRef != "" {
			return &SystemAction{Action: "delete_workspace", Params: map[string]interface{}{"workspace": workspaceRef}}
		}
	}
	if match := workspaceDetailsPattern.FindStringSubmatch(trimmed); len(match) == 2 {
		params := map[string]interface{}{}
		if workspaceRef := cleanWorkspaceReference(match[1]); workspaceRef != "" {
			params["workspace"] = workspaceRef
		}
		return &SystemAction{Action: "show_workspace_details", Params: params}
	}
	if match := workspaceCreatePattern.FindStringSubmatch(trimmed); len(match) == 2 {
		pathRef := cleanWorkspaceReference(match[1])
		if pathRef != "" && !strings.HasPrefix(strings.ToLower(pathRef), "from ") {
			return &SystemAction{Action: "create_workspace", Params: map[string]interface{}{"path": pathRef}}
		}
	}

	if name, ok := cutPrefixedWorkspaceReference(trimmed, "open workspace "); ok {
		return &SystemAction{Action: "switch_workspace", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "switch to workspace "); ok {
		return &SystemAction{Action: "switch_workspace", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "switch workspace to "); ok {
		return &SystemAction{Action: "switch_workspace", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "show items for workspace "); ok {
		return &SystemAction{Action: "list_workspace_items", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "show items for "); ok && looksLikeWorkspaceReference(name) {
		return &SystemAction{Action: "list_workspace_items", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "switch to "); ok && looksLikeWorkspaceReference(name) {
		return &SystemAction{Action: "switch_workspace", Params: map[string]interface{}{"workspace": name}}
	}
	return nil
}

func cutPrefixedWorkspaceReference(text, prefix string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(strings.ToLower(trimmed), prefix) {
		return "", false
	}
	value := strings.TrimSpace(trimmed[len(prefix):])
	value = strings.TrimRight(value, " \t\r\n!?,:;.")
	if value == "" {
		return "", false
	}
	return value, true
}

func looksLikeWorkspaceReference(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, `/\~.`) {
		return true
	}
	if strings.EqualFold(value, "here") || strings.EqualFold(value, "this workspace") {
		return true
	}
	return false
}

func cleanWorkspaceReference(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.Trim(text, " \t\r\n!?,:;")
	for _, suffix := range []string{" please", " thanks", " thank you"} {
		if strings.HasSuffix(strings.ToLower(text), suffix) {
			text = strings.TrimSpace(text[:len(text)-len(suffix)])
		}
	}
	return strings.TrimSpace(text)
}

func systemActionWorkspaceRef(params map[string]interface{}) string {
	for _, key := range []string{"workspace", "name", "path", "target"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func systemActionWorkspaceNewName(params map[string]interface{}) string {
	for _, key := range []string{"new_name", "rename_to", "label"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func systemActionBoolParam(params map[string]interface{}, key string) bool {
	switch value := params[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

func expandWorkspacePathReference(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return value
		}
		if value == "~" {
			return home
		}
		return filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	return value
}

func (a *App) fallbackWorkspaceForProjectKey(projectKey string) (*store.Workspace, error) {
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		if workspaces[i].IsActive {
			return &workspaces[i], nil
		}
	}
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return nil, nil
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return nil, nil
	}
	workspaceID, err := a.store.FindWorkspaceContainingPath(project.RootPath)
	if err != nil || workspaceID == nil {
		return nil, err
	}
	workspace, err := a.store.GetWorkspace(*workspaceID)
	if err != nil {
		return nil, err
	}
	return &workspace, nil
}

func (a *App) resolveWorkspaceReference(projectKey string, raw string) (store.Workspace, error) {
	ref := strings.TrimSpace(raw)
	if ref == "" || strings.EqualFold(ref, "here") || strings.EqualFold(ref, "this workspace") {
		workspace, err := a.fallbackWorkspaceForProjectKey(projectKey)
		if err != nil {
			return store.Workspace{}, err
		}
		if workspace == nil {
			return store.Workspace{}, errors.New("no active workspace")
		}
		return *workspace, nil
	}

	expanded := expandWorkspacePathReference(ref)
	if strings.ContainsAny(expanded, `/\~.`) {
		if workspace, err := a.store.GetWorkspaceByPath(expanded); err == nil {
			return workspace, nil
		}
		if workspaceID, err := a.store.FindWorkspaceContainingPath(expanded); err == nil && workspaceID != nil {
			return a.store.GetWorkspace(*workspaceID)
		}
	}

	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return store.Workspace{}, err
	}
	var matches []store.Workspace
	for _, workspace := range workspaces {
		switch {
		case strings.EqualFold(workspace.Name, ref):
			matches = append(matches, workspace)
		case strings.EqualFold(filepath.Base(workspace.DirPath), ref):
			matches = append(matches, workspace)
		case strings.EqualFold(workspace.DirPath, expanded):
			matches = append(matches, workspace)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].IsActive != matches[j].IsActive {
				return matches[i].IsActive
			}
			return strings.ToLower(matches[i].Name) < strings.ToLower(matches[j].Name)
		})
		return store.Workspace{}, fmt.Errorf("workspace %q is ambiguous", ref)
	}
	return store.Workspace{}, fmt.Errorf("workspace %q not found", ref)
}

func (a *App) listOpenWorkspaceItems(workspaceID int64) ([]store.Item, error) {
	items, err := a.store.ListItems()
	if err != nil {
		return nil, err
	}
	out := make([]store.Item, 0, len(items))
	for _, item := range items {
		if item.WorkspaceID == nil || *item.WorkspaceID != workspaceID {
			continue
		}
		if item.State == store.ItemStateDone {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func summarizeWorkspaceItems(workspace store.Workspace, items []store.Item) string {
	if len(items) == 0 {
		return fmt.Sprintf("No open items for workspace %s.", workspace.Name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Open items for workspace %s:\n", workspace.Name)
	for _, item := range items {
		fmt.Fprintf(&b, "- %s [%s]\n", item.Title, item.State)
	}
	return strings.TrimSpace(b.String())
}

type workspacePromptContext struct {
	ActiveSphere  string
	Workspace     store.Workspace
	OpenItemCount int
}

func (a *App) loadWorkspacePromptContext(projectKey string) *workspacePromptContext {
	workspace, err := a.fallbackWorkspaceForProjectKey(projectKey)
	if err != nil || workspace == nil {
		return nil
	}
	activeSphere, err := a.store.ActiveSphere()
	if err != nil {
		return nil
	}
	items, err := a.listOpenWorkspaceItems(workspace.ID)
	if err != nil {
		return nil
	}
	return &workspacePromptContext{
		ActiveSphere:  activeSphere,
		Workspace:     *workspace,
		OpenItemCount: len(items),
	}
}

func prependWorkspacePromptContext(prompt string, ctx *workspacePromptContext) string {
	if ctx == nil || strings.TrimSpace(prompt) == "" {
		return prompt
	}
	var b strings.Builder
	b.WriteString("## Workspace Context\n")
	fmt.Fprintf(&b, "Active sphere: %s\n", ctx.ActiveSphere)
	fmt.Fprintf(&b, "Active workspace: %s (%s)\n", ctx.Workspace.Name, ctx.Workspace.DirPath)
	fmt.Fprintf(&b, "Open items in this workspace: %d\n\n", ctx.OpenItemCount)
	b.WriteString(prompt)
	return b.String()
}

func (a *App) applyWorkspacePromptContext(projectKey, prompt string) string {
	return prependWorkspacePromptContext(prompt, a.loadWorkspacePromptContext(projectKey))
}

func (a *App) workspaceActionBaseDir(projectKey string) string {
	if strings.TrimSpace(projectKey) != "" {
		if project, err := a.store.GetProjectByProjectKey(projectKey); err == nil {
			if root := strings.TrimSpace(project.RootPath); root != "" {
				return root
			}
		}
	}
	if root := strings.TrimSpace(a.localProjectDir); root != "" {
		return root
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func (a *App) resolveWorkspaceCreationPath(projectKey, rawPath string) string {
	pathRef := expandWorkspacePathReference(rawPath)
	if pathRef == "" {
		return ""
	}
	if filepath.IsAbs(pathRef) {
		return filepath.Clean(pathRef)
	}
	return filepath.Clean(filepath.Join(a.workspaceActionBaseDir(projectKey), pathRef))
}

func slugifyWorkspaceName(raw string) string {
	clean := strings.ToLower(strings.TrimSpace(raw))
	if clean == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range clean {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func scratchWorkspaceName(now time.Time) string {
	return "scratch-" + now.UTC().Format("20060102-150405")
}

func workspaceOpenItemCounts(items []store.Item) map[int64]int {
	counts := make(map[int64]int)
	for _, item := range items {
		if item.WorkspaceID == nil || item.State == store.ItemStateDone {
			continue
		}
		counts[*item.WorkspaceID]++
	}
	return counts
}

func summarizeWorkspaces(workspaces []store.Workspace, openCounts map[int64]int, sphere string) string {
	if len(workspaces) == 0 {
		if strings.TrimSpace(sphere) != "" {
			return fmt.Sprintf("No workspaces registered in %s sphere.", sphere)
		}
		return "No workspaces registered."
	}
	var b strings.Builder
	if strings.TrimSpace(sphere) != "" {
		fmt.Fprintf(&b, "Workspaces in %s sphere:\n", sphere)
	} else {
		b.WriteString("Workspaces:\n")
	}
	for _, workspace := range workspaces {
		active := " "
		if workspace.IsActive {
			active = "*"
		}
		fmt.Fprintf(&b, "%s %s — %s (%d open items)\n", active, workspace.Name, workspace.DirPath, openCounts[workspace.ID])
	}
	return strings.TrimSpace(b.String())
}

func (a *App) createWorkspaceFromDialogIntent(projectKey string, action *SystemAction) (store.Workspace, bool, error) {
	scratch := systemActionBoolParam(action.Params, "scratch")
	var (
		dirPath string
		name    string
	)
	if scratch {
		name = cleanWorkspaceReference(systemActionStringParam(action.Params, "name"))
		if name == "" {
			name = scratchWorkspaceName(time.Now())
		}
		baseDir := filepath.Join(a.workspaceActionBaseDir(projectKey), ".tabura", "artifacts", "tmp")
		dirName := slugifyWorkspaceName(name)
		if dirName == "" {
			dirName = scratchWorkspaceName(time.Now())
		}
		dirPath = filepath.Join(baseDir, dirName)
		if _, err := os.Stat(dirPath); err == nil {
			dirPath = filepath.Join(baseDir, dirName+"-"+time.Now().UTC().Format("150405"))
		}
	} else {
		dirPath = a.resolveWorkspaceCreationPath(projectKey, systemActionWorkspaceRef(action.Params))
		if dirPath == "" {
			return store.Workspace{}, false, errors.New("workspace path is required")
		}
		name = filepath.Base(dirPath)
	}
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return store.Workspace{}, false, err
	}
	workspace, err := a.store.CreateWorkspace(name, dirPath)
	if err != nil {
		return store.Workspace{}, false, err
	}
	if err := a.store.SetActiveWorkspace(workspace.ID); err != nil {
		return store.Workspace{}, false, err
	}
	workspace, err = a.store.GetWorkspace(workspace.ID)
	if err != nil {
		return store.Workspace{}, false, err
	}
	return workspace, scratch, nil
}

func (a *App) executeListWorkspacesAction(_ store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return "", nil, err
	}
	items, err := a.store.ListItems()
	if err != nil {
		return "", nil, err
	}
	openCounts := workspaceOpenItemCounts(items)
	return a.executeListWorkspacesResponse(workspaces, openCounts, systemActionAllSpheresParam(action.Params))
}

func (a *App) executeListWorkspacesResponse(workspaces []store.Workspace, openCounts map[int64]int, allSpheres bool) (string, map[string]interface{}, error) {
	activeSphere := ""
	if !allSpheres {
		var err error
		activeSphere, err = a.store.ActiveSphere()
		if err != nil {
			return "", nil, err
		}
		filtered := make([]store.Workspace, 0, len(workspaces))
		for _, workspace := range workspaces {
			if strings.EqualFold(workspace.Sphere, activeSphere) {
				filtered = append(filtered, workspace)
			}
		}
		workspaces = filtered
	}
	payloadList := make([]map[string]interface{}, 0, len(workspaces))
	for _, workspace := range workspaces {
		payloadList = append(payloadList, map[string]interface{}{
			"workspace_id": workspace.ID,
			"name":         workspace.Name,
			"dir_path":     workspace.DirPath,
			"sphere":       workspace.Sphere,
			"is_active":    workspace.IsActive,
			"open_items":   openCounts[workspace.ID],
		})
	}
	payload := map[string]interface{}{
		"type":            "list_workspaces",
		"workspace_count": len(workspaces),
		"workspaces":      payloadList,
	}
	if allSpheres {
		payload["all_spheres"] = true
	} else if activeSphere != "" {
		payload["sphere"] = activeSphere
	}
	return summarizeWorkspaces(workspaces, openCounts, activeSphere), payload, nil
}

func (a *App) executeCreateWorkspaceAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	workspace, scratch, err := a.createWorkspaceFromDialogIntent(session.ProjectKey, action)
	if err != nil {
		return "", nil, err
	}
	payloadType := "create_workspace"
	message := fmt.Sprintf("Created workspace %s at %s.", workspace.Name, workspace.DirPath)
	if scratch {
		payloadType = "create_scratch_workspace"
		message = fmt.Sprintf("Created scratch workspace %s at %s.", workspace.Name, workspace.DirPath)
	}
	return message, map[string]interface{}{
		"type":         payloadType,
		"workspace_id": workspace.ID,
		"name":         workspace.Name,
		"dir_path":     workspace.DirPath,
		"is_active":    workspace.IsActive,
	}, nil
}

func (a *App) executeRenameWorkspaceAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	workspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionWorkspaceRef(action.Params))
	if err != nil {
		return "", nil, err
	}
	updated, err := a.store.UpdateWorkspaceName(workspace.ID, systemActionWorkspaceNewName(action.Params))
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("Renamed workspace %s to %s.", workspace.Name, updated.Name), map[string]interface{}{
		"type":         "rename_workspace",
		"workspace_id": updated.ID,
		"name":         updated.Name,
		"dir_path":     updated.DirPath,
		"is_active":    updated.IsActive,
	}, nil
}

func (a *App) executeDeleteWorkspaceAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	workspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionWorkspaceRef(action.Params))
	if err != nil {
		return "", nil, err
	}
	if err := a.store.DeleteWorkspace(workspace.ID); err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("Deleted workspace %s.", workspace.Name), map[string]interface{}{
		"type":         "delete_workspace",
		"workspace_id": workspace.ID,
		"name":         workspace.Name,
		"dir_path":     workspace.DirPath,
	}, nil
}

func (a *App) executeShowWorkspaceDetailsAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	workspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionWorkspaceRef(action.Params))
	if err != nil {
		return "", nil, err
	}
	items, err := a.listOpenWorkspaceItems(workspace.ID)
	if err != nil {
		return "", nil, err
	}
	repo, err := a.store.GitHubRepoForWorkspace(workspace.ID)
	if err != nil {
		return "", nil, err
	}
	status := "inactive"
	if workspace.IsActive {
		status = "active"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Workspace %s\n", workspace.Name)
	fmt.Fprintf(&b, "- Path: %s\n", workspace.DirPath)
	fmt.Fprintf(&b, "- Status: %s\n", status)
	fmt.Fprintf(&b, "- Open items: %d\n", len(items))
	if repo != "" {
		fmt.Fprintf(&b, "- GitHub remote: %s\n", repo)
	}
	return strings.TrimSpace(b.String()), map[string]interface{}{
		"type":         "show_workspace_details",
		"workspace_id": workspace.ID,
		"name":         workspace.Name,
		"dir_path":     workspace.DirPath,
		"is_active":    workspace.IsActive,
		"open_items":   len(items),
		"github_repo":  repo,
	}, nil
}
