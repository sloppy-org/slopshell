package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
	"github.com/krystophny/tabura/internal/todoist"
)

const todoistItemSyncTimeout = 10 * time.Second

func todoistBackedItem(item store.Item) bool {
	return item.Source != nil && strings.EqualFold(strings.TrimSpace(*item.Source), store.ExternalProviderTodoist)
}

func todoistTaskIDFromSourceRef(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(clean), "task:") {
		return strings.TrimSpace(clean[len("task:"):])
	}
	return clean
}

func todoistItemState(task todoist.Task) string {
	if task.IsCompleted {
		return store.ItemStateDone
	}
	return store.ItemStateInbox
}

func todoistCommentMeta(comments []todoist.Comment) []map[string]any {
	if len(comments) == 0 {
		return nil
	}
	payload := make([]map[string]any, 0, len(comments))
	for _, comment := range comments {
		entry := map[string]any{
			"id":        strings.TrimSpace(comment.ID),
			"posted_at": strings.TrimSpace(comment.PostedAt),
			"content":   strings.TrimSpace(comment.Content),
		}
		if taskID := strings.TrimSpace(optionalStringValue(comment.TaskID)); taskID != "" {
			entry["task_id"] = taskID
		}
		if projectID := strings.TrimSpace(optionalStringValue(comment.ProjectID)); projectID != "" {
			entry["project_id"] = projectID
		}
		if comment.Attachment != nil {
			attachment := map[string]any{}
			if value := strings.TrimSpace(comment.Attachment.FileName); value != "" {
				attachment["file_name"] = value
			}
			if value := strings.TrimSpace(comment.Attachment.FileType); value != "" {
				attachment["file_type"] = value
			}
			if value := strings.TrimSpace(comment.Attachment.FileURL); value != "" {
				attachment["file_url"] = value
			}
			if value := strings.TrimSpace(comment.Attachment.ResourceType); value != "" {
				attachment["resource_type"] = value
			}
			if len(attachment) > 0 {
				entry["attachment"] = attachment
			}
		}
		payload = append(payload, entry)
	}
	return payload
}

func todoistTaskArtifactMeta(task todoist.Task, projectNames map[string]string, comments []todoist.Comment) (*string, error) {
	project := ""
	if task.ProjectID != nil {
		project = strings.TrimSpace(projectNames[strings.TrimSpace(*task.ProjectID)])
	}
	section := ""
	if task.SectionID != nil {
		section = strings.TrimSpace(*task.SectionID)
	}
	payload := map[string]any{
		"labels":        append([]string(nil), task.Labels...),
		"priority":      task.Priority,
		"project":       project,
		"project_id":    strings.TrimSpace(optionalStringValue(task.ProjectID)),
		"section":       section,
		"section_id":    strings.TrimSpace(optionalStringValue(task.SectionID)),
		"comment_count": task.CommentCount,
		"url":           strings.TrimSpace(task.URL),
	}
	if commentMeta := todoistCommentMeta(comments); len(commentMeta) > 0 {
		payload["comments"] = commentMeta
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	return &text, nil
}

func todoistDueUpdateRequest(followUpAt *string) (todoist.UpdateTaskRequest, error) {
	if followUpAt == nil {
		return todoist.UpdateTaskRequest{}, nil
	}
	clean := strings.TrimSpace(*followUpAt)
	if clean == "" {
		clear := "no due date"
		return todoist.UpdateTaskRequest{DueString: &clear}, nil
	}
	dueAt, err := time.Parse(time.RFC3339, clean)
	if err != nil {
		return todoist.UpdateTaskRequest{}, fmt.Errorf("follow_up_at must be a valid RFC3339 timestamp: %w", err)
	}
	normalized := dueAt.UTC().Format(time.RFC3339)
	return todoist.UpdateTaskRequest{DueDateTime: &normalized}, nil
}

func applyTodoistCreateDue(req *todoist.CreateTaskRequest, followUpAt *string) error {
	if req == nil || followUpAt == nil {
		return nil
	}
	clean := strings.TrimSpace(*followUpAt)
	if clean == "" {
		return nil
	}
	dueAt, err := time.Parse(time.RFC3339, clean)
	if err != nil {
		return fmt.Errorf("follow_up_at must be a valid RFC3339 timestamp: %w", err)
	}
	req.DueDateTime = dueAt.UTC().Format(time.RFC3339)
	return nil
}

func todoistDoneState(raw string) bool {
	return strings.EqualFold(strings.TrimSpace(raw), store.ItemStateDone)
}

func (a *App) todoistAccountForSphere(sphere string, strict bool) (store.ExternalAccount, error) {
	accounts, err := a.store.ListExternalAccountsByProvider(store.ExternalProviderTodoist)
	if err != nil {
		return store.ExternalAccount{}, err
	}
	enabled := todoistEnabledAccounts(accounts, sphere)
	if len(enabled) > 0 {
		return enabled[0], nil
	}
	if strict {
		return store.ExternalAccount{}, errors.New("no enabled Todoist account is configured for the item sphere")
	}
	enabled = todoistEnabledAccounts(accounts, "")
	if len(enabled) == 0 {
		return store.ExternalAccount{}, errors.New("no enabled Todoist account is configured")
	}
	return enabled[0], nil
}

func (a *App) todoistRemoteForItem(item store.Item) (store.ExternalAccount, *todoist.Client, string, error) {
	if !todoistBackedItem(item) {
		return store.ExternalAccount{}, nil, "", errors.New("item is not backed by Todoist")
	}
	taskID := optionalStringValue(item.SourceRef)
	taskID = todoistTaskIDFromSourceRef(taskID)
	bindings, err := a.store.GetBindingsByItem(item.ID)
	if err != nil {
		return store.ExternalAccount{}, nil, "", err
	}
	for _, binding := range bindings {
		if !strings.EqualFold(binding.Provider, store.ExternalProviderTodoist) || !strings.EqualFold(binding.ObjectType, "task") {
			continue
		}
		account, err := a.store.GetExternalAccount(binding.AccountID)
		if err != nil {
			return store.ExternalAccount{}, nil, "", err
		}
		if !account.Enabled {
			continue
		}
		if taskID == "" {
			taskID = strings.TrimSpace(binding.RemoteID)
		}
		client, err := todoistClientForAccount(account)
		if err != nil {
			return store.ExternalAccount{}, nil, "", err
		}
		if taskID == "" {
			return store.ExternalAccount{}, nil, "", errors.New("todoist task id is required")
		}
		return account, client, taskID, nil
	}
	account, err := a.todoistAccountForSphere(item.Sphere, false)
	if err != nil {
		return store.ExternalAccount{}, nil, "", err
	}
	client, err := todoistClientForAccount(account)
	if err != nil {
		return store.ExternalAccount{}, nil, "", err
	}
	if taskID == "" {
		return store.ExternalAccount{}, nil, "", errors.New("todoist task id is required")
	}
	return account, client, taskID, nil
}

func (a *App) syncTodoistTaskArtifact(item store.Item, task todoist.Task, projectNames map[string]string, comments []todoist.Comment) error {
	title := strings.TrimSpace(task.Content)
	kind := store.ArtifactKindExternalTask
	refURL := optionalTrimmedString(task.URL)
	metaJSON, err := todoistTaskArtifactMeta(task, projectNames, comments)
	if err != nil {
		return err
	}

	createArtifact := func() (store.Artifact, error) {
		return a.store.CreateArtifact(kind, nil, refURL, optionalTrimmedString(title), metaJSON)
	}

	if item.ArtifactID == nil {
		artifact, err := createArtifact()
		if err != nil {
			return err
		}
		return a.store.UpdateItemArtifact(item.ID, &artifact.ID)
	}

	err = a.store.UpdateArtifact(*item.ArtifactID, store.ArtifactUpdate{
		Kind:     &kind,
		RefURL:   refURL,
		Title:    optionalTrimmedString(title),
		MetaJSON: metaJSON,
	})
	if errors.Is(err, sql.ErrNoRows) {
		artifact, createErr := createArtifact()
		if createErr != nil {
			return createErr
		}
		return a.store.UpdateItemArtifact(item.ID, &artifact.ID)
	}
	return err
}

func (a *App) syncTodoistItemCompletion(item store.Item) error {
	_, client, taskID, err := a.todoistRemoteForItem(item)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), todoistItemSyncTimeout)
	defer cancel()
	return client.CompleteTask(ctx, taskID)
}

func (a *App) syncTodoistItemFollowUp(item store.Item, followUpAt *string) error {
	_, client, taskID, err := a.todoistRemoteForItem(item)
	if err != nil {
		return err
	}
	req, err := todoistDueUpdateRequest(followUpAt)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), todoistItemSyncTimeout)
	defer cancel()
	_, err = client.UpdateTask(ctx, taskID, req)
	return err
}

func (a *App) todoistProjectIDForWorkspace(workspaceID *int64, mappings []store.ExternalContainerMapping, projects []todoist.Project) string {
	if workspaceID == nil {
		return ""
	}
	projectIDs := todoistProjectIDByName(projects)
	for _, mapping := range mappings {
		if !strings.EqualFold(mapping.Provider, store.ExternalProviderTodoist) || !strings.EqualFold(mapping.ContainerType, "project") {
			continue
		}
		if mapping.WorkspaceID == nil || *mapping.WorkspaceID != *workspaceID {
			continue
		}
		return projectIDs[strings.ToLower(strings.TrimSpace(mapping.ContainerRef))]
	}
	return ""
}

func (a *App) createTodoistBackedItem(req itemCreateRequest) (store.Item, error) {
	derivedSphere, err := a.store.ActiveSphere()
	if err != nil {
		return store.Item{}, err
	}
	if req.WorkspaceID != nil {
		workspace, err := a.store.GetWorkspace(*req.WorkspaceID)
		if err != nil {
			return store.Item{}, err
		}
		derivedSphere = workspace.Sphere
	} else if req.Sphere != nil && strings.TrimSpace(*req.Sphere) != "" {
		derivedSphere = strings.TrimSpace(*req.Sphere)
	}

	account, err := a.todoistAccountForSphere(derivedSphere, true)
	if err != nil {
		return store.Item{}, err
	}
	client, err := todoistClientForAccount(account)
	if err != nil {
		return store.Item{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), todoistItemSyncTimeout)
	defer cancel()

	mappings, err := a.store.ListContainerMappings(store.ExternalProviderTodoist)
	if err != nil {
		return store.Item{}, err
	}
	projects, err := client.ListProjects(ctx)
	if err != nil {
		return store.Item{}, err
	}
	createReq := todoist.CreateTaskRequest{
		Content:   strings.TrimSpace(req.Title),
		ProjectID: a.todoistProjectIDForWorkspace(req.WorkspaceID, mappings, projects),
	}
	if err := applyTodoistCreateDue(&createReq, req.FollowUpAt); err != nil {
		return store.Item{}, err
	}
	task, err := client.CreateTask(ctx, createReq)
	if err != nil {
		return store.Item{}, err
	}
	if todoistDoneState(req.State) {
		if err := client.CompleteTask(ctx, strings.TrimSpace(task.ID)); err != nil {
			return store.Item{}, err
		}
		task.IsCompleted = true
	}

	item, err := a.persistTodoistTask(account, task, nil, mappings, todoistProjectNameByID(projects))
	if err != nil {
		return store.Item{}, err
	}

	var updates store.ItemUpdate
	hasUpdates := false
	if req.WorkspaceID != nil && item.WorkspaceID == nil {
		workspaceID := *req.WorkspaceID
		updates.WorkspaceID = &workspaceID
		hasUpdates = true
	}
	if req.ActorID != nil {
		updates.ActorID = req.ActorID
		hasUpdates = true
	}
	if req.VisibleAfter != nil {
		updates.VisibleAfter = req.VisibleAfter
		hasUpdates = true
	}
	if req.State != "" && !todoistDoneState(req.State) && !strings.EqualFold(req.State, item.State) {
		state := req.State
		updates.State = &state
		hasUpdates = true
	}
	if hasUpdates {
		if err := a.store.UpdateItem(item.ID, updates); err != nil {
			return store.Item{}, err
		}
		return a.store.GetItem(item.ID)
	}
	return item, nil
}
