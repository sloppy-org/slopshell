package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/evernote"
	"github.com/krystophny/tabura/internal/store"
)

const evernoteSyncPageSize = 100

type evernoteAccountConfig struct {
	BaseURL string `json:"base_url"`
}

type evernoteSyncResult struct {
	NoteCount int
	TaskCount int
}

func parseInlineEvernoteIntent(text string) *SystemAction {
	if normalizeItemCommandText(text) != "sync evernote" {
		return nil
	}
	return &SystemAction{Action: "sync_evernote", Params: map[string]interface{}{}}
}

func evernoteActionFailurePrefix(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "sync_evernote":
		return "I couldn't sync Evernote: "
	default:
		return "I couldn't resolve the Evernote request: "
	}
}

func decodeEvernoteAccountConfig(account store.ExternalAccount) (evernoteAccountConfig, error) {
	var cfg evernoteAccountConfig
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return evernoteAccountConfig{}, fmt.Errorf("decode evernote config: %w", err)
	}
	return cfg, nil
}

func evernoteClientForAccount(account store.ExternalAccount) (*evernote.Client, error) {
	cfg, err := decodeEvernoteAccountConfig(account)
	if err != nil {
		return nil, err
	}
	opts := []evernote.Option{}
	if strings.TrimSpace(cfg.BaseURL) != "" {
		opts = append(opts, evernote.WithBaseURL(cfg.BaseURL))
	}
	return evernote.NewClientFromEnv(account.Label, opts...)
}

func (a *App) activeEvernoteAccounts() ([]store.ExternalAccount, error) {
	accounts, err := a.store.ListExternalAccountsByProvider(store.ExternalProviderEvernote)
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
		return nil, errors.New("no enabled Evernote account is configured")
	}
	return enabled, nil
}

func evernoteNotebookNameByID(notebooks []evernote.Notebook) map[string]string {
	out := make(map[string]string, len(notebooks))
	for _, notebook := range notebooks {
		id := strings.TrimSpace(notebook.ID)
		if id == "" {
			continue
		}
		out[id] = strings.TrimSpace(notebook.Name)
	}
	return out
}

func evernoteNotebookMapping(mappings []store.ExternalContainerMapping, notebookName string) *store.ExternalContainerMapping {
	cleanName := strings.ToLower(strings.TrimSpace(notebookName))
	if cleanName == "" {
		return nil
	}
	for i := range mappings {
		mapping := &mappings[i]
		if !strings.EqualFold(mapping.Provider, store.ExternalProviderEvernote) {
			continue
		}
		if !strings.EqualFold(mapping.ContainerType, "notebook") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(mapping.ContainerRef), cleanName) {
			return mapping
		}
	}
	return nil
}

func (a *App) evernoteNotebookMappingForAccount(account store.ExternalAccount, mappings []store.ExternalContainerMapping, notebookName string) (*store.ExternalContainerMapping, error) {
	mapping := evernoteNotebookMapping(mappings, notebookName)
	if mapping == nil {
		return nil, nil
	}
	if mapping.Sphere != nil && !strings.EqualFold(strings.TrimSpace(*mapping.Sphere), account.Sphere) {
		return nil, nil
	}
	if mapping.WorkspaceID == nil {
		return mapping, nil
	}
	workspace, err := a.store.GetWorkspace(*mapping.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(workspace.Sphere, account.Sphere) {
		return nil, nil
	}
	return mapping, nil
}

func (a *App) evernoteProjectHintFromTags(tags []string) *string {
	for _, tag := range tags {
		project, err := a.hubFindProjectByName(tag)
		if err != nil {
			continue
		}
		projectID := project.ID
		return &projectID
	}
	return nil
}

func evernoteNoteArtifactMeta(note evernote.Note, notebookName string) (*string, error) {
	payload := map[string]any{
		"notebook":         strings.TrimSpace(notebookName),
		"tags":             append([]string(nil), note.TagNames...),
		"updated_at":       strings.TrimSpace(note.UpdatedAt),
		"created_at":       strings.TrimSpace(note.CreatedAt),
		"content_text":     strings.TrimSpace(note.ContentText),
		"content_markdown": strings.TrimSpace(note.ContentMarkdown),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	return &text, nil
}

func evernoteNoteRefURL(note evernote.Note) *string {
	for _, key := range []string{"url", "web_url", "webUrl"} {
		if value, ok := note.Raw[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" {
				return &text
			}
		}
	}
	return nil
}

func evernoteTaskSourceRef(noteID string, index int) string {
	return fmt.Sprintf("note:%s:task:%d", strings.TrimSpace(noteID), index)
}

func evernoteTaskState(task evernote.Task) string {
	if task.Checked {
		return store.ItemStateDone
	}
	return store.ItemStateInbox
}

func (a *App) linkEvernoteArtifactWorkspace(artifactID int64, mapping *store.ExternalContainerMapping) {
	if mapping == nil || mapping.WorkspaceID == nil {
		return
	}
	_ = a.store.LinkArtifactToWorkspace(*mapping.WorkspaceID, artifactID)
}

func (a *App) upsertEvernoteArtifact(account store.ExternalAccount, note evernote.Note, notebookName string) (store.Artifact, error) {
	metaJSON, err := evernoteNoteArtifactMeta(note, notebookName)
	if err != nil {
		return store.Artifact{}, err
	}
	title := strings.TrimSpace(note.Title)
	refURL := evernoteNoteRefURL(note)
	kind := store.ArtifactKindExternalNote

	if binding, err := a.store.GetBindingByRemote(account.ID, store.ExternalProviderEvernote, "note", strings.TrimSpace(note.ID)); err == nil {
		if binding.ArtifactID != nil {
			err = a.store.UpdateArtifact(*binding.ArtifactID, store.ArtifactUpdate{
				Kind:     &kind,
				RefURL:   refURL,
				Title:    optionalTrimmedString(title),
				MetaJSON: metaJSON,
			})
			if err == nil {
				if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
					AccountID:       account.ID,
					Provider:        store.ExternalProviderEvernote,
					ObjectType:      "note",
					RemoteID:        strings.TrimSpace(note.ID),
					ArtifactID:      binding.ArtifactID,
					ContainerRef:    optionalStringPointer(notebookName),
					RemoteUpdatedAt: optionalStringPointer(note.UpdatedAt),
				}); err != nil {
					return store.Artifact{}, err
				}
				return a.store.GetArtifact(*binding.ArtifactID)
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return store.Artifact{}, err
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Artifact{}, err
	}

	artifact, err := a.store.CreateArtifact(kind, nil, refURL, optionalTrimmedString(title), metaJSON)
	if err != nil {
		return store.Artifact{}, err
	}
	if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        store.ExternalProviderEvernote,
		ObjectType:      "note",
		RemoteID:        strings.TrimSpace(note.ID),
		ArtifactID:      &artifact.ID,
		ContainerRef:    optionalStringPointer(notebookName),
		RemoteUpdatedAt: optionalStringPointer(note.UpdatedAt),
	}); err != nil {
		return store.Artifact{}, err
	}
	return artifact, nil
}

func evernoteTaskProjectID(mapping *store.ExternalContainerMapping, inferredProjectID *string) *string {
	if mapping != nil && mapping.ProjectID != nil {
		projectID := strings.TrimSpace(*mapping.ProjectID)
		return &projectID
	}
	if inferredProjectID == nil {
		return nil
	}
	projectID := strings.TrimSpace(*inferredProjectID)
	if projectID == "" {
		return nil
	}
	return &projectID
}

func mappedProjectUpdateWithFallback(mapping *store.ExternalContainerMapping, fallback *string) *string {
	if mapping != nil {
		if mapping.ProjectID != nil {
			return mappedProjectUpdate(mapping)
		}
		if fallback == nil {
			return nil
		}
		projectID := strings.TrimSpace(*fallback)
		if projectID == "" {
			return nil
		}
		return &projectID
	}
	if fallback == nil {
		return nil
	}
	projectID := strings.TrimSpace(*fallback)
	return &projectID
}

func (a *App) persistEvernoteTask(account store.ExternalAccount, artifact store.Artifact, task evernote.Task, sourceRef string, mapping *store.ExternalContainerMapping, inferredProjectID *string, remoteUpdatedAt *string) (store.Item, error) {
	title := strings.TrimSpace(task.Text)
	if title == "" {
		return store.Item{}, errors.New("evernote task text is required")
	}
	desiredState := evernoteTaskState(task)
	if existing, err := a.store.GetItemBySource(store.ExternalProviderEvernote, sourceRef); err == nil {
		updates := store.ItemUpdate{
			Title:      &title,
			ArtifactID: &artifact.ID,
		}
		if mapping != nil {
			updates.WorkspaceID = mappedWorkspaceUpdate(mapping)
		}
		if projectID := mappedProjectUpdateWithFallback(mapping, inferredProjectID); projectID != nil {
			updates.ProjectID = projectID
		}
		if mapping == nil || mapping.WorkspaceID == nil {
			sphere := account.Sphere
			updates.Sphere = &sphere
		}
		if err := a.store.UpdateItem(existing.ID, updates); err != nil {
			return store.Item{}, err
		}
		item, err := a.store.GetItem(existing.ID)
		if err != nil {
			return store.Item{}, err
		}
		switch {
		case desiredState == store.ItemStateDone && item.State != store.ItemStateDone:
			if err := a.store.CompleteItemBySource(store.ExternalProviderEvernote, sourceRef); err != nil {
				return store.Item{}, err
			}
			item, err = a.store.GetItem(existing.ID)
			if err != nil {
				return store.Item{}, err
			}
		case desiredState == store.ItemStateInbox && item.State == store.ItemStateDone:
			if err := a.store.SyncItemStateBySource(store.ExternalProviderEvernote, sourceRef, store.ItemStateInbox); err != nil {
				return store.Item{}, err
			}
			item, err = a.store.GetItem(existing.ID)
			if err != nil {
				return store.Item{}, err
			}
		}
		if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
			AccountID:       account.ID,
			Provider:        store.ExternalProviderEvernote,
			ObjectType:      "task",
			RemoteID:        sourceRef,
			ItemID:          &item.ID,
			ArtifactID:      &artifact.ID,
			RemoteUpdatedAt: remoteUpdatedAt,
		}); err != nil {
			return store.Item{}, err
		}
		return item, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Item{}, err
	}

	opts := store.ItemOptions{
		State:      desiredState,
		ProjectID:  evernoteTaskProjectID(mapping, inferredProjectID),
		Sphere:     &account.Sphere,
		ArtifactID: &artifact.ID,
		Source:     optionalStringPointer(store.ExternalProviderEvernote),
		SourceRef:  &sourceRef,
	}
	if mapping != nil && mapping.WorkspaceID != nil {
		opts.WorkspaceID = mapping.WorkspaceID
	}
	item, err := a.store.CreateItem(title, opts)
	if err != nil {
		return store.Item{}, err
	}
	if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        store.ExternalProviderEvernote,
		ObjectType:      "task",
		RemoteID:        sourceRef,
		ItemID:          &item.ID,
		ArtifactID:      &artifact.ID,
		RemoteUpdatedAt: remoteUpdatedAt,
	}); err != nil {
		return store.Item{}, err
	}
	return item, nil
}

func (a *App) persistEvernoteNote(account store.ExternalAccount, note evernote.Note, notebookName string, mappings []store.ExternalContainerMapping) (evernoteSyncResult, error) {
	mapping, err := a.evernoteNotebookMappingForAccount(account, mappings, notebookName)
	if err != nil {
		return evernoteSyncResult{}, err
	}
	artifact, err := a.upsertEvernoteArtifact(account, note, notebookName)
	if err != nil {
		return evernoteSyncResult{}, err
	}
	a.linkEvernoteArtifactWorkspace(artifact.ID, mapping)
	inferredProjectID := a.evernoteProjectHintFromTags(note.TagNames)

	result := evernoteSyncResult{NoteCount: 1}
	remoteUpdatedAt := optionalStringPointer(note.UpdatedAt)
	for i, task := range note.Tasks {
		if strings.TrimSpace(task.Text) == "" {
			continue
		}
		sourceRef := evernoteTaskSourceRef(note.ID, i+1)
		if _, err := a.persistEvernoteTask(account, artifact, task, sourceRef, mapping, inferredProjectID, remoteUpdatedAt); err != nil {
			return evernoteSyncResult{}, err
		}
		result.TaskCount++
	}
	return result, nil
}

func listAllEvernoteNotes(ctx context.Context, client *evernote.Client, updatedAfter string) ([]evernote.NoteSummary, error) {
	var all []evernote.NoteSummary
	offset := 0
	for {
		notes, err := client.ListNotes(ctx, "", evernote.ListNotesOptions{
			UpdatedAfter: updatedAfter,
			Limit:        evernoteSyncPageSize,
			Offset:       offset,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, notes...)
		if len(notes) < evernoteSyncPageSize {
			return all, nil
		}
		offset += len(notes)
	}
}

func (a *App) executeSyncEvernoteAction() (string, map[string]interface{}, error) {
	accounts, err := a.activeEvernoteAccounts()
	if err != nil {
		return "", nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := evernoteSyncResult{}
	for _, account := range accounts {
		synced, err := a.syncEvernoteAccount(ctx, account)
		if err != nil {
			return "", nil, err
		}
		result.NoteCount += synced.NoteCount
		result.TaskCount += synced.TaskCount
	}

	return fmt.Sprintf("Synced %d Evernote note(s) and %d task item(s).", result.NoteCount, result.TaskCount), map[string]interface{}{
		"type":       "sync_evernote",
		"note_count": result.NoteCount,
		"task_count": result.TaskCount,
	}, nil
}

func (a *App) executeEvernoteAction(_ store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "sync_evernote":
		return a.executeSyncEvernoteAction()
	default:
		return "", nil, fmt.Errorf("unsupported evernote action: %s", action.Action)
	}
}

func (a *App) syncEvernoteAccount(ctx context.Context, account store.ExternalAccount) (evernoteSyncResult, error) {
	mappings, err := a.store.ListContainerMappings(store.ExternalProviderEvernote)
	if err != nil {
		return evernoteSyncResult{}, err
	}
	client, err := evernoteClientForAccount(account)
	if err != nil {
		return evernoteSyncResult{}, err
	}
	notebooks, err := client.ListNotebooks(ctx)
	if err != nil {
		return evernoteSyncResult{}, err
	}
	notebookNames := evernoteNotebookNameByID(notebooks)
	updatedAfter := ""
	if latest, err := a.store.LatestBindingRemoteUpdatedAt(account.ID, store.ExternalProviderEvernote, "note"); err != nil {
		return evernoteSyncResult{}, err
	} else if latest != nil {
		updatedAfter = *latest
	}
	notes, err := listAllEvernoteNotes(ctx, client, updatedAfter)
	if err != nil {
		return evernoteSyncResult{}, err
	}
	result := evernoteSyncResult{}
	for _, summary := range notes {
		note, err := client.GetNote(ctx, summary.ID)
		if err != nil {
			return evernoteSyncResult{}, err
		}
		notebookName := strings.TrimSpace(notebookNames[strings.TrimSpace(note.NotebookID)])
		synced, err := a.persistEvernoteNote(account, note, notebookName, mappings)
		if err != nil {
			return evernoteSyncResult{}, err
		}
		result.NoteCount += synced.NoteCount
		result.TaskCount += synced.TaskCount
	}
	return result, nil
}
