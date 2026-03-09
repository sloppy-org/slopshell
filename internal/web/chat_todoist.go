package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
	"github.com/krystophny/tabura/internal/todoist"
)

var (
	todoistMapWorkspacePattern = regexp.MustCompile(`(?i)^map\s+todoist\s+project\s+(.+?)\s+to\s+workspace\s+(.+?)$`)
	todoistMapProjectPattern   = regexp.MustCompile(`(?i)^map\s+todoist\s+project\s+(.+?)\s+to\s+project\s+(.+?)$`)
	todoistCreateTaskPattern   = regexp.MustCompile(`(?i)^create\s+todoist\s+task\s*:?\s+(.+?)$`)
	todoistDueStringPattern    = regexp.MustCompile(`(?i)^(.*?)\s+(?:by|due)\s+(.+?)$`)
)

type todoistAccountConfig struct {
	BaseURL     string `json:"base_url"`
	MoveBaseURL string `json:"move_base_url"`
}

func parseInlineTodoistIntent(text string) *SystemAction {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	normalized := normalizeItemCommandText(trimmed)
	if normalized == "sync todoist" {
		return &SystemAction{Action: "sync_todoist", Params: map[string]interface{}{}}
	}
	if match := todoistMapWorkspacePattern.FindStringSubmatch(trimmed); len(match) == 3 {
		projectRef := strings.TrimSpace(match[1])
		workspaceRef := cleanWorkspaceReference(match[2])
		if projectRef != "" && workspaceRef != "" {
			return &SystemAction{
				Action: "map_todoist_project",
				Params: map[string]interface{}{
					"project":   projectRef,
					"workspace": workspaceRef,
				},
			}
		}
	}
	if match := todoistMapProjectPattern.FindStringSubmatch(trimmed); len(match) == 3 {
		projectRef := strings.TrimSpace(match[1])
		targetProject := strings.TrimSpace(match[2])
		if projectRef != "" && targetProject != "" {
			return &SystemAction{
				Action: "map_todoist_project",
				Params: map[string]interface{}{
					"project":        projectRef,
					"target_project": targetProject,
				},
			}
		}
	}
	if match := todoistCreateTaskPattern.FindStringSubmatch(trimmed); len(match) == 2 {
		taskText := strings.TrimSpace(match[1])
		if taskText != "" {
			return &SystemAction{
				Action: "create_todoist_task",
				Params: map[string]interface{}{"text": taskText},
			}
		}
	}
	return nil
}

func todoistActionFailurePrefix(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "map_todoist_project":
		return "I couldn't save the Todoist mapping: "
	case "sync_todoist":
		return "I couldn't sync Todoist: "
	case "create_todoist_task":
		return "I couldn't create the Todoist task: "
	default:
		return "I couldn't resolve the Todoist request: "
	}
}

func todoistTaskSourceRef(taskID string) string {
	return "task:" + strings.TrimSpace(taskID)
}

func decodeTodoistAccountConfig(account store.ExternalAccount) (todoistAccountConfig, error) {
	var cfg todoistAccountConfig
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return todoistAccountConfig{}, fmt.Errorf("decode todoist config: %w", err)
	}
	return cfg, nil
}

func todoistClientForAccount(account store.ExternalAccount) (*todoist.Client, error) {
	cfg, err := decodeTodoistAccountConfig(account)
	if err != nil {
		return nil, err
	}
	opts := []todoist.Option{}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		opts = append(opts, todoist.WithBaseURL(cfg.BaseURL))
	}
	if strings.TrimSpace(cfg.MoveBaseURL) != "" {
		opts = append(opts, todoist.WithMoveBaseURL(cfg.MoveBaseURL))
	}
	return todoist.NewClientFromEnv(account.Label, opts...)
}

func todoistTaskFollowUpAt(task todoist.Task) *string {
	if task.Due == nil {
		return nil
	}
	if task.Due.DateTime != nil {
		value := strings.TrimSpace(*task.Due.DateTime)
		if value != "" {
			return &value
		}
	}
	if value := strings.TrimSpace(task.Due.Date); value != "" {
		followUp := value + "T09:00:00Z"
		return &followUp
	}
	return nil
}

func todoistEnabledAccounts(accounts []store.ExternalAccount, sphere string) []store.ExternalAccount {
	out := make([]store.ExternalAccount, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		if sphere != "" && !strings.EqualFold(account.Sphere, sphere) {
			continue
		}
		out = append(out, account)
	}
	return out
}

func todoistProjectMapping(mappings []store.ExternalContainerMapping, projectName string) *store.ExternalContainerMapping {
	cleanName := strings.ToLower(strings.TrimSpace(projectName))
	if cleanName == "" {
		return nil
	}
	for i := range mappings {
		mapping := &mappings[i]
		if !strings.EqualFold(mapping.Provider, store.ExternalProviderTodoist) {
			continue
		}
		if !strings.EqualFold(mapping.ContainerType, "project") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(mapping.ContainerRef), cleanName) {
			return mapping
		}
	}
	return nil
}

func todoistProjectNameByID(projects []todoist.Project) map[string]string {
	out := make(map[string]string, len(projects))
	for _, project := range projects {
		projectID := strings.TrimSpace(project.ID)
		if projectID == "" {
			continue
		}
		out[projectID] = strings.TrimSpace(project.Name)
	}
	return out
}

func todoistProjectIDByName(projects []todoist.Project) map[string]string {
	out := make(map[string]string, len(projects))
	for _, project := range projects {
		name := strings.ToLower(strings.TrimSpace(project.Name))
		projectID := strings.TrimSpace(project.ID)
		if name == "" || projectID == "" {
			continue
		}
		out[name] = projectID
	}
	return out
}

func activeWorkspaceID(workspaces []store.Workspace) *int64 {
	for i := range workspaces {
		if workspaces[i].IsActive {
			id := workspaces[i].ID
			return &id
		}
	}
	return nil
}

func (a *App) activeTodoistAccounts() ([]store.ExternalAccount, error) {
	accounts, err := a.store.ListExternalAccountsByProvider(store.ExternalProviderTodoist)
	if err != nil {
		return nil, err
	}
	activeSphere, err := a.store.ActiveSphere()
	if err != nil {
		return nil, err
	}
	enabled := todoistEnabledAccounts(accounts, activeSphere)
	if len(enabled) > 0 {
		return enabled, nil
	}
	enabled = todoistEnabledAccounts(accounts, "")
	if len(enabled) == 0 {
		return nil, errors.New("no enabled Todoist account is configured")
	}
	return enabled, nil
}

func (a *App) persistTodoistTask(account store.ExternalAccount, task todoist.Task, mappings []store.ExternalContainerMapping, projectNames map[string]string) (store.Item, error) {
	projectName := ""
	if task.ProjectID != nil {
		projectName = strings.TrimSpace(projectNames[strings.TrimSpace(*task.ProjectID)])
	}
	mapping := todoistProjectMapping(mappings, projectName)
	source := store.ExternalProviderTodoist
	sourceRef := todoistTaskSourceRef(task.ID)
	title := strings.TrimSpace(task.Content)
	if title == "" {
		return store.Item{}, errors.New("todoist task content is required")
	}
	followUpAt := todoistTaskFollowUpAt(task)
	if existing, err := a.store.GetItemBySource(source, sourceRef); err == nil {
		updates := store.ItemUpdate{
			Title:      &title,
			FollowUpAt: followUpAt,
		}
		if mapping != nil {
			if mapping.WorkspaceID != nil {
				updates.WorkspaceID = mapping.WorkspaceID
			}
			updates.ProjectID = mapping.ProjectID
		}
		if err := a.store.UpdateItem(existing.ID, updates); err != nil {
			return store.Item{}, err
		}
		item, err := a.store.GetItem(existing.ID)
		if err != nil {
			return store.Item{}, err
		}
		containerRef := strings.TrimSpace(projectName)
		if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
			AccountID:    account.ID,
			Provider:     store.ExternalProviderTodoist,
			ObjectType:   "task",
			RemoteID:     strings.TrimSpace(task.ID),
			ItemID:       &item.ID,
			ContainerRef: optionalStringPointer(containerRef),
		}); err != nil {
			return store.Item{}, err
		}
		return item, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Item{}, err
	}

	opts := store.ItemOptions{
		ProjectID:  mappingProjectID(mapping),
		Sphere:     &account.Sphere,
		FollowUpAt: followUpAt,
		Source:     &source,
		SourceRef:  &sourceRef,
	}
	if mapping != nil && mapping.WorkspaceID != nil {
		opts.WorkspaceID = mapping.WorkspaceID
	}
	item, err := a.store.CreateItem(title, opts)
	if err != nil {
		return store.Item{}, err
	}
	containerRef := strings.TrimSpace(projectName)
	if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:    account.ID,
		Provider:     store.ExternalProviderTodoist,
		ObjectType:   "task",
		RemoteID:     strings.TrimSpace(task.ID),
		ItemID:       &item.ID,
		ContainerRef: optionalStringPointer(containerRef),
	}); err != nil {
		return store.Item{}, err
	}
	return item, nil
}

func mappingProjectID(mapping *store.ExternalContainerMapping) *string {
	if mapping == nil {
		return nil
	}
	return mapping.ProjectID
}

func optionalStringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func (a *App) executeMapTodoistProjectAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	projectRef := strings.TrimSpace(systemActionStringParam(action.Params, "project"))
	if projectRef == "" {
		return "", nil, errors.New("todoist project name is required")
	}
	var workspaceID *int64
	if workspaceRef := systemActionWorkspaceRef(action.Params); workspaceRef != "" {
		workspace, err := a.resolveWorkspaceReference(session.ProjectKey, workspaceRef)
		if err != nil {
			return "", nil, err
		}
		workspaceID = &workspace.ID
		mapping, err := a.store.SetContainerMapping(store.ExternalProviderTodoist, "project", projectRef, workspaceID, nil, nil)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Mapped Todoist project %s to workspace %s.", projectRef, workspace.Name), map[string]interface{}{
			"type":          "todoist_container_mapping",
			"mapping_id":    mapping.ID,
			"container_ref": mapping.ContainerRef,
			"workspace_id":  workspace.ID,
		}, nil
	}
	if targetProjectName := strings.TrimSpace(systemActionStringParam(action.Params, "target_project")); targetProjectName != "" {
		project, err := a.hubFindProjectByName(targetProjectName)
		if err != nil {
			return "", nil, err
		}
		projectID := project.ID
		mapping, err := a.store.SetContainerMapping(store.ExternalProviderTodoist, "project", projectRef, nil, &projectID, nil)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Mapped Todoist project %s to project %s.", projectRef, project.Name), map[string]interface{}{
			"type":          "todoist_container_mapping",
			"mapping_id":    mapping.ID,
			"container_ref": mapping.ContainerRef,
			"project_id":    project.ID,
		}, nil
	}
	return "", nil, errors.New("todoist mapping target is required")
}

func (a *App) executeSyncTodoistAction() (string, map[string]interface{}, error) {
	accounts, err := a.activeTodoistAccounts()
	if err != nil {
		return "", nil, err
	}
	mappings, err := a.store.ListContainerMappings(store.ExternalProviderTodoist)
	if err != nil {
		return "", nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	syncedCount := 0
	for _, account := range accounts {
		client, err := todoistClientForAccount(account)
		if err != nil {
			return "", nil, err
		}
		projects, err := client.ListProjects(ctx)
		if err != nil {
			return "", nil, err
		}
		projectNames := todoistProjectNameByID(projects)
		tasks, err := client.ListTasks(ctx, todoist.ListTasksOptions{})
		if err != nil {
			return "", nil, err
		}
		for _, task := range tasks {
			if _, err := a.persistTodoistTask(account, task, mappings, projectNames); err != nil {
				return "", nil, err
			}
			syncedCount++
		}
	}

	return fmt.Sprintf("Synced %d Todoist task(s).", syncedCount), map[string]interface{}{
		"type":  "sync_todoist",
		"count": syncedCount,
	}, nil
}

func parseTodoistTaskDraft(raw string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	if match := todoistDueStringPattern.FindStringSubmatch(trimmed); len(match) == 3 {
		content := strings.TrimSpace(match[1])
		dueString := strings.TrimSpace(match[2])
		if content != "" && dueString != "" {
			return content, dueString
		}
	}
	return trimmed, ""
}

func (a *App) createTodoistTaskProjectTarget(client *todoist.Client) (string, error) {
	mappings, err := a.store.ListContainerMappings(store.ExternalProviderTodoist)
	if err != nil {
		return "", err
	}
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return "", err
	}
	activeWorkspaceID := activeWorkspaceID(workspaces)
	activeProjectID, err := a.store.ActiveProjectID()
	if err != nil {
		return "", err
	}
	targetName := ""
	for _, mapping := range mappings {
		if !strings.EqualFold(mapping.Provider, store.ExternalProviderTodoist) || !strings.EqualFold(mapping.ContainerType, "project") {
			continue
		}
		if mapping.WorkspaceID != nil && activeWorkspaceID != nil && *mapping.WorkspaceID == *activeWorkspaceID {
			targetName = mapping.ContainerRef
			break
		}
		if mapping.ProjectID != nil && strings.EqualFold(*mapping.ProjectID, activeProjectID) {
			targetName = mapping.ContainerRef
			break
		}
	}
	if targetName == "" {
		return "", nil
	}
	projects, err := client.ListProjects(context.Background())
	if err != nil {
		return "", err
	}
	projectIDs := todoistProjectIDByName(projects)
	return projectIDs[strings.ToLower(strings.TrimSpace(targetName))], nil
}

func (a *App) executeCreateTodoistTaskAction(action *SystemAction) (string, map[string]interface{}, error) {
	accounts, err := a.activeTodoistAccounts()
	if err != nil {
		return "", nil, err
	}
	account := accounts[0]
	client, err := todoistClientForAccount(account)
	if err != nil {
		return "", nil, err
	}
	return a.executeCreateTodoistTaskActionWith(account, client, action)
}

func (a *App) executeCreateTodoistTaskActionWith(account store.ExternalAccount, client *todoist.Client, action *SystemAction) (string, map[string]interface{}, error) {
	taskText := strings.TrimSpace(systemActionStringParam(action.Params, "text"))
	if taskText == "" {
		return "", nil, errors.New("todoist task text is required")
	}
	content, dueString := parseTodoistTaskDraft(taskText)
	if content == "" {
		return "", nil, errors.New("todoist task content is required")
	}
	targetProjectID, err := a.createTodoistTaskProjectTarget(client)
	if err != nil {
		return "", nil, err
	}
	req := todoist.CreateTaskRequest{
		Content:   content,
		ProjectID: targetProjectID,
	}
	if dueString != "" {
		req.DueString = dueString
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	task, err := client.CreateTask(ctx, req)
	if err != nil {
		return "", nil, err
	}
	mappings, err := a.store.ListContainerMappings(store.ExternalProviderTodoist)
	if err != nil {
		return "", nil, err
	}
	projects, err := client.ListProjects(ctx)
	if err != nil {
		return "", nil, err
	}
	item, err := a.persistTodoistTask(account, task, mappings, todoistProjectNameByID(projects))
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("Created Todoist task %q.", task.Content), map[string]interface{}{
		"type":       "todoist_task_created",
		"task_id":    strings.TrimSpace(task.ID),
		"item_id":    item.ID,
		"title":      item.Title,
		"project_id": targetProjectID,
	}, nil
}

func (a *App) executeTodoistAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "map_todoist_project":
		return a.executeMapTodoistProjectAction(session, action)
	case "sync_todoist":
		return a.executeSyncTodoistAction()
	case "create_todoist_task":
		return a.executeCreateTodoistTaskAction(action)
	default:
		return "", nil, fmt.Errorf("unsupported todoist action: %s", action.Action)
	}
}
