package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/bear"
	"github.com/krystophny/tabura/internal/store"
)

type bearAccountConfig struct {
	DBPath string `json:"db_path"`
}

type bearSyncResult struct {
	NoteCount int
	Skipped   int
}

type bearNoteMeta struct {
	NoteID          string   `json:"note_id,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Created         string   `json:"created,omitempty"`
	Modified        string   `json:"modified,omitempty"`
	ContentMarkdown string   `json:"content_markdown,omitempty"`
}

func parseInlineBearIntent(text string) *SystemAction {
	switch normalizeItemCommandText(text) {
	case "sync bear":
		return &SystemAction{Action: "sync_bear", Params: map[string]interface{}{}}
	case "create items from this bear note's checklist", "create items from this bear notes checklist":
		return &SystemAction{Action: "promote_bear_checklist", Params: map[string]interface{}{}}
	default:
		return nil
	}
}

func bearActionFailurePrefix(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "sync_bear":
		return "I couldn't sync Bear: "
	case "promote_bear_checklist":
		return "I couldn't create Bear checklist items: "
	default:
		return "I couldn't resolve the Bear request: "
	}
}

func decodeBearAccountConfig(account store.ExternalAccount) (bearAccountConfig, error) {
	var cfg bearAccountConfig
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return bearAccountConfig{}, fmt.Errorf("decode bear config: %w", err)
	}
	return cfg, nil
}

func bearClientForAccount(account store.ExternalAccount) (*bear.Client, error) {
	cfg, err := decodeBearAccountConfig(account)
	if err != nil {
		return nil, err
	}
	return bear.NewClient(cfg.DBPath)
}

func (a *App) activeBearAccounts() ([]store.ExternalAccount, error) {
	accounts, err := a.store.ListExternalAccountsByProvider(store.ExternalProviderBear)
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
		return nil, errors.New("no enabled Bear account is configured")
	}
	return enabled, nil
}

func bearTagMapping(mappings []store.ExternalContainerMapping, tag string) *store.ExternalContainerMapping {
	cleanTag := strings.ToLower(strings.TrimSpace(tag))
	if cleanTag == "" {
		return nil
	}
	for i := range mappings {
		mapping := &mappings[i]
		if !strings.EqualFold(mapping.Provider, store.ExternalProviderBear) {
			continue
		}
		if !strings.EqualFold(mapping.ContainerType, "tag") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(mapping.ContainerRef), cleanTag) {
			return mapping
		}
	}
	return nil
}

func (a *App) bearTagMappingForAccount(account store.ExternalAccount, mappings []store.ExternalContainerMapping, tags []string) (*store.ExternalContainerMapping, error) {
	for _, tag := range tags {
		mapping := bearTagMapping(mappings, tag)
		if mapping == nil {
			continue
		}
		if mapping.Sphere != nil && !strings.EqualFold(strings.TrimSpace(*mapping.Sphere), account.Sphere) {
			continue
		}
		if mapping.WorkspaceID == nil {
			return mapping, nil
		}
		workspace, err := a.store.GetWorkspace(*mapping.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(workspace.Sphere, account.Sphere) {
			continue
		}
		return mapping, nil
	}
	return nil, nil
}

func (a *App) bearProjectHintFromTags(tags []string) *string {
	for _, tag := range tags {
		project, err := a.findProjectByName(tag)
		if err != nil {
			continue
		}
		workspaceID := projectIDString(project.ID)
		return &workspaceID
	}
	return nil
}

func bearNoteArtifactMeta(note bear.Note) (*string, error) {
	payload := bearNoteMeta{
		NoteID:          strings.TrimSpace(note.ID),
		Tags:            append([]string(nil), note.Tags...),
		Created:         strings.TrimSpace(note.Created),
		Modified:        strings.TrimSpace(note.Modified),
		ContentMarkdown: strings.TrimSpace(note.Markdown),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	return &text, nil
}

func parseBearNoteMeta(metaJSON *string) bearNoteMeta {
	var meta bearNoteMeta
	if metaJSON == nil || strings.TrimSpace(*metaJSON) == "" {
		return meta
	}
	_ = json.Unmarshal([]byte(*metaJSON), &meta)
	for i := range meta.Tags {
		meta.Tags[i] = strings.TrimSpace(meta.Tags[i])
	}
	return meta
}

func bearNoteTitle(note bear.Note) string {
	if title := strings.TrimSpace(note.Title); title != "" {
		return title
	}
	for _, line := range strings.Split(note.Markdown, "\n") {
		clean := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if clean != "" {
			return clean
		}
	}
	return fmt.Sprintf("Bear note %s", strings.TrimSpace(note.ID))
}

func bearNoteRefURL(note bear.Note) *string {
	noteID := strings.TrimSpace(note.ID)
	if noteID == "" {
		return nil
	}
	value := "bear://x-callback-url/open-note?id=" + url.QueryEscape(noteID)
	return &value
}

func (a *App) upsertBearArtifact(account store.ExternalAccount, note bear.Note, mapping *store.ExternalContainerMapping) (store.Artifact, error) {
	metaJSON, err := bearNoteArtifactMeta(note)
	if err != nil {
		return store.Artifact{}, err
	}
	kind := store.ArtifactKindMarkdown
	title := bearNoteTitle(note)
	refURL := bearNoteRefURL(note)
	remoteID := strings.TrimSpace(note.ID)

	if binding, err := a.store.GetBindingByRemote(account.ID, store.ExternalProviderBear, "note", remoteID); err == nil {
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
					Provider:        store.ExternalProviderBear,
					ObjectType:      "note",
					RemoteID:        remoteID,
					ArtifactID:      binding.ArtifactID,
					ContainerRef:    firstBearTagPointer(note.Tags),
					RemoteUpdatedAt: optionalStringPointer(note.Modified),
				}); err != nil {
					return store.Artifact{}, err
				}
				artifact, err := a.store.GetArtifact(*binding.ArtifactID)
				if err != nil {
					return store.Artifact{}, err
				}
				a.linkBearArtifactWorkspace(artifact.ID, mapping)
				return artifact, nil
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
		Provider:        store.ExternalProviderBear,
		ObjectType:      "note",
		RemoteID:        remoteID,
		ArtifactID:      &artifact.ID,
		ContainerRef:    firstBearTagPointer(note.Tags),
		RemoteUpdatedAt: optionalStringPointer(note.Modified),
	}); err != nil {
		return store.Artifact{}, err
	}
	a.linkBearArtifactWorkspace(artifact.ID, mapping)
	return artifact, nil
}

func firstBearTagPointer(tags []string) *string {
	for _, tag := range tags {
		if clean := strings.TrimSpace(tag); clean != "" {
			return &clean
		}
	}
	return nil
}

func (a *App) linkBearArtifactWorkspace(artifactID int64, mapping *store.ExternalContainerMapping) {
	if mapping == nil || mapping.WorkspaceID == nil {
		return
	}
	_ = a.store.LinkArtifactToWorkspace(*mapping.WorkspaceID, artifactID)
}

func bearChecklistSourceRef(noteID string, index int) string {
	return fmt.Sprintf("note:%s:task:%d", strings.TrimSpace(noteID), index)
}

func bearChecklistState(item bear.ChecklistItem) string {
	if item.Checked {
		return store.ItemStateDone
	}
	return store.ItemStateInbox
}

func (a *App) persistBearChecklistItem(account store.ExternalAccount, artifact store.Artifact, checklistItem bear.ChecklistItem, sourceRef string, mapping *store.ExternalContainerMapping, inferredWorkspaceID *string, remoteUpdatedAt *string) (store.Item, error) {
	title := strings.TrimSpace(checklistItem.Text)
	if title == "" {
		return store.Item{}, errors.New("bear checklist item text is required")
	}
	desiredState := bearChecklistState(checklistItem)
	if existing, err := a.store.GetItemBySource(store.ExternalProviderBear, sourceRef); err == nil {
		updates := store.ItemUpdate{
			Title:      &title,
			ArtifactID: &artifact.ID,
		}
		if mapping != nil {
			updates.WorkspaceID = mappedWorkspaceUpdate(mapping)
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
			if err := a.store.CompleteItemBySource(store.ExternalProviderBear, sourceRef); err != nil {
				return store.Item{}, err
			}
			item, err = a.store.GetItem(existing.ID)
			if err != nil {
				return store.Item{}, err
			}
		case desiredState == store.ItemStateInbox && item.State == store.ItemStateDone:
			if err := a.store.SyncItemStateBySource(store.ExternalProviderBear, sourceRef, store.ItemStateInbox); err != nil {
				return store.Item{}, err
			}
			item, err = a.store.GetItem(existing.ID)
			if err != nil {
				return store.Item{}, err
			}
		}
		if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
			AccountID:       account.ID,
			Provider:        store.ExternalProviderBear,
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
		Sphere:     &account.Sphere,
		ArtifactID: &artifact.ID,
		Source:     optionalStringPointer(store.ExternalProviderBear),
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
		Provider:        store.ExternalProviderBear,
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

func (a *App) resolveActiveBearNoteArtifact(workspacePath string) (*store.Artifact, bearNoteMeta, store.ExternalBinding, error) {
	canvas := a.resolveCanvasContext(workspacePath)
	if canvas == nil || strings.TrimSpace(canvas.ArtifactTitle) == "" {
		return nil, bearNoteMeta{}, store.ExternalBinding{}, errors.New("open the Bear note on canvas first")
	}
	title := strings.TrimSpace(canvas.ArtifactTitle)
	artifacts, err := a.store.ListArtifactsByKind(store.ArtifactKindMarkdown)
	if err != nil {
		return nil, bearNoteMeta{}, store.ExternalBinding{}, err
	}
	for _, artifact := range artifacts {
		if ideaNoteString(artifact.Title) != title {
			continue
		}
		bindings, err := a.store.GetBindingsByArtifact(artifact.ID)
		if err != nil {
			return nil, bearNoteMeta{}, store.ExternalBinding{}, err
		}
		for _, binding := range bindings {
			if !strings.EqualFold(binding.Provider, store.ExternalProviderBear) {
				continue
			}
			if !strings.EqualFold(binding.ObjectType, "note") {
				continue
			}
			meta := parseBearNoteMeta(artifact.MetaJSON)
			candidate := artifact
			return &candidate, meta, binding, nil
		}
	}
	return nil, bearNoteMeta{}, store.ExternalBinding{}, errors.New("active canvas artifact is not a Bear note")
}

func (a *App) executeSyncBearAction() (string, map[string]interface{}, error) {
	accounts, err := a.activeBearAccounts()
	if err != nil {
		return "", nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result := bearSyncResult{}
	for _, account := range accounts {
		synced, err := a.syncBearAccount(ctx, account)
		if err != nil {
			return "", nil, err
		}
		result.NoteCount += synced.NoteCount
		result.Skipped += synced.Skipped
	}

	if result.NoteCount == 0 && result.Skipped > 0 {
		return "Skipped Bear sync because no Bear database was found.", map[string]interface{}{
			"type":             "sync_bear",
			"note_count":       0,
			"skipped_accounts": result.Skipped,
		}, nil
	}
	return fmt.Sprintf("Synced %d Bear note(s).", result.NoteCount), map[string]interface{}{
		"type":             "sync_bear",
		"note_count":       result.NoteCount,
		"skipped_accounts": result.Skipped,
	}, nil
}

func (a *App) syncBearAccount(ctx context.Context, account store.ExternalAccount) (bearSyncResult, error) {
	mappings, err := a.store.ListContainerMappings(store.ExternalProviderBear)
	if err != nil {
		return bearSyncResult{}, err
	}
	client, err := bearClientForAccount(account)
	if err != nil {
		if errors.Is(err, bear.ErrDatabaseNotFound) {
			return bearSyncResult{Skipped: 1}, nil
		}
		return bearSyncResult{}, err
	}
	notes, err := client.ListNotes(ctx)
	if err != nil {
		if errors.Is(err, bear.ErrDatabaseNotFound) {
			return bearSyncResult{Skipped: 1}, nil
		}
		return bearSyncResult{}, err
	}
	result := bearSyncResult{}
	for _, note := range notes {
		mapping, err := a.bearTagMappingForAccount(account, mappings, note.Tags)
		if err != nil {
			return bearSyncResult{}, err
		}
		if _, err := a.upsertBearArtifact(account, note, mapping); err != nil {
			return bearSyncResult{}, err
		}
		result.NoteCount++
	}
	return result, nil
}

func (a *App) executePromoteBearChecklistAction(session store.ChatSession) (string, map[string]interface{}, error) {
	artifact, meta, binding, err := a.resolveActiveBearNoteArtifact(session.WorkspacePath)
	if err != nil {
		return "", nil, err
	}
	account, err := a.store.GetExternalAccount(binding.AccountID)
	if err != nil {
		return "", nil, err
	}
	checklist := bear.ExtractChecklist(meta.ContentMarkdown)
	if len(checklist) == 0 {
		return "", nil, errors.New("Bear note has no checklist items")
	}
	mappings, err := a.store.ListContainerMappings(store.ExternalProviderBear)
	if err != nil {
		return "", nil, err
	}
	mapping, err := a.bearTagMappingForAccount(account, mappings, meta.Tags)
	if err != nil {
		return "", nil, err
	}
	inferredWorkspaceID := a.bearProjectHintFromTags(meta.Tags)
	remoteUpdatedAt := optionalStringPointer(meta.Modified)
	noteID := strings.TrimSpace(binding.RemoteID)
	if noteID == "" {
		noteID = strings.TrimSpace(meta.NoteID)
	}
	created := 0
	for i, item := range checklist {
		sourceRef := bearChecklistSourceRef(noteID, i+1)
		if _, err := a.persistBearChecklistItem(account, *artifact, item, sourceRef, mapping, inferredWorkspaceID, remoteUpdatedAt); err != nil {
			return "", nil, err
		}
		created++
	}
	return fmt.Sprintf("Created %d item(s) from the Bear note checklist.", created), map[string]interface{}{
		"type":        "bear_checklist_promoted",
		"count":       created,
		"artifact_id": artifact.ID,
	}, nil
}

func (a *App) executeBearAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "sync_bear":
		return a.executeSyncBearAction()
	case "promote_bear_checklist":
		return a.executePromoteBearChecklistAction(session)
	default:
		return "", nil, fmt.Errorf("unsupported bear action: %s", action.Action)
	}
}
