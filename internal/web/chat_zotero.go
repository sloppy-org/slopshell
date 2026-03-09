package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
	"github.com/krystophny/tabura/internal/zotero"
)

const zoteroSyncTimeout = 15 * time.Second

type zoteroAccountConfig struct {
	DBPath             string `json:"db_path"`
	HomePath           string `json:"home_path"`
	CitationExportPath string `json:"citation_export_path"`
}

type zoteroSyncResult struct {
	ReferenceCount   int
	AttachmentCount  int
	AnnotationCount  int
	ReadingItemCount int
	Skipped          int
}

func parseInlineZoteroIntent(text string) *SystemAction {
	if normalizeItemCommandText(text) != "sync zotero" {
		return nil
	}
	return &SystemAction{Action: "sync_zotero", Params: map[string]interface{}{}}
}

func zoteroActionFailurePrefix(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "sync_zotero":
		return "I couldn't sync Zotero: "
	default:
		return "I couldn't resolve the Zotero request: "
	}
}

func decodeZoteroAccountConfig(account store.ExternalAccount) (zoteroAccountConfig, error) {
	var cfg zoteroAccountConfig
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return zoteroAccountConfig{}, fmt.Errorf("decode zotero config: %w", err)
	}
	return cfg, nil
}

func zoteroReaderForAccount(account store.ExternalAccount) (*zotero.Reader, zoteroAccountConfig, error) {
	cfg, err := decodeZoteroAccountConfig(account)
	if err != nil {
		return nil, zoteroAccountConfig{}, err
	}
	if strings.TrimSpace(cfg.DBPath) != "" {
		reader, err := zotero.OpenReader(cfg.DBPath)
		return reader, cfg, err
	}
	reader, err := zotero.OpenDefaultReader(cfg.HomePath)
	return reader, cfg, err
}

func (a *App) activeZoteroAccounts() ([]store.ExternalAccount, error) {
	accounts, err := a.store.ListExternalAccountsByProvider(store.ExternalProviderZotero)
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
		return nil, errors.New("no enabled Zotero account is configured")
	}
	return enabled, nil
}

func zoteroCollectionMapping(mappings []store.ExternalContainerMapping, collectionName string) *store.ExternalContainerMapping {
	cleanName := strings.ToLower(strings.TrimSpace(collectionName))
	if cleanName == "" {
		return nil
	}
	for i := range mappings {
		mapping := &mappings[i]
		if !strings.EqualFold(mapping.Provider, store.ExternalProviderZotero) {
			continue
		}
		if !strings.EqualFold(mapping.ContainerType, "collection") {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(mapping.ContainerRef), cleanName) {
			return mapping
		}
	}
	return nil
}

func zoteroCollectionNamesByKey(collections []zotero.Collection) map[string]string {
	out := make(map[string]string, len(collections))
	for _, collection := range collections {
		key := strings.TrimSpace(collection.Key)
		if key == "" {
			continue
		}
		out[key] = strings.TrimSpace(collection.Name)
	}
	return out
}

func zoteroCollectionNames(item zotero.Item, collectionNames map[string]string) []string {
	out := make([]string, 0, len(item.Collections))
	for _, key := range item.Collections {
		name := strings.TrimSpace(collectionNames[strings.TrimSpace(key)])
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func (a *App) zoteroCollectionMappingForAccount(account store.ExternalAccount, mappings []store.ExternalContainerMapping, collectionNames []string) (*store.ExternalContainerMapping, error) {
	for _, collectionName := range collectionNames {
		mapping := zoteroCollectionMapping(mappings, collectionName)
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

func (a *App) zoteroProjectHintFromTags(tags []string) *string {
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

func zoteroTimestampPointer(raw string) *string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, clean)
		if err != nil {
			continue
		}
		value := parsed.UTC().Format(time.RFC3339Nano)
		return &value
	}
	return nil
}

func zoteroAuthorNames(creators []zotero.Creator) []string {
	out := make([]string, 0, len(creators))
	for _, creator := range creators {
		switch {
		case strings.TrimSpace(creator.Name) != "":
			out = append(out, strings.TrimSpace(creator.Name))
		case strings.TrimSpace(creator.FirstName) != "" || strings.TrimSpace(creator.LastName) != "":
			out = append(out, strings.TrimSpace(strings.TrimSpace(creator.FirstName)+" "+strings.TrimSpace(creator.LastName)))
		}
	}
	return out
}

func zoteroReferenceArtifactMeta(item zotero.Item, collectionNames []string) (*string, error) {
	payload := map[string]any{
		"title":        strings.TrimSpace(item.Title),
		"authors":      zoteroAuthorNames(item.Creators),
		"year":         strings.TrimSpace(item.Year),
		"journal":      strings.TrimSpace(item.Journal),
		"doi":          strings.TrimSpace(item.DOI),
		"isbn":         strings.TrimSpace(item.ISBN),
		"citation_key": strings.TrimSpace(item.CitationKey),
		"abstract":     strings.TrimSpace(item.Abstract),
		"item_type":    strings.TrimSpace(item.ItemType),
		"tags":         append([]string(nil), item.Tags...),
		"collections":  append([]string(nil), collectionNames...),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	return &text, nil
}

func zoteroAttachmentArtifactMeta(attachment zotero.Attachment, referenceArtifactID int64, referenceKey string) (*string, error) {
	payload := map[string]any{
		"attachment_key":        strings.TrimSpace(attachment.Key),
		"reference_key":         strings.TrimSpace(referenceKey),
		"reference_artifact_id": referenceArtifactID,
		"content_type":          strings.TrimSpace(attachment.ContentType),
		"date_modified":         strings.TrimSpace(attachment.DateModified),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	return &text, nil
}

func zoteroAnnotationArtifactMeta(annotation zotero.Annotation, referenceArtifactID int64, referenceKey, attachmentKey string) (*string, error) {
	payload := map[string]any{
		"annotation_key":        strings.TrimSpace(annotation.Key),
		"attachment_key":        strings.TrimSpace(attachmentKey),
		"reference_key":         strings.TrimSpace(referenceKey),
		"reference_artifact_id": referenceArtifactID,
		"type":                  strings.TrimSpace(annotation.AnnotationType),
		"text":                  strings.TrimSpace(annotation.Text),
		"comment":               strings.TrimSpace(annotation.Comment),
		"color":                 strings.TrimSpace(annotation.Color),
		"page_label":            strings.TrimSpace(annotation.PageLabel),
		"position":              strings.TrimSpace(annotation.Position),
		"date_modified":         strings.TrimSpace(annotation.DateModified),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	return &text, nil
}

func zoteroReadingTitle(item zotero.Item) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = strings.TrimSpace(item.Key)
	}
	return "Read: " + title
}

func zoteroReadingSourceRef(itemKey string) string {
	return "reference:" + strings.TrimSpace(itemKey)
}

func zoteroReadingItemWanted(item zotero.Item) bool {
	switch strings.ToLower(strings.TrimSpace(item.ItemType)) {
	case "journalarticle", "book", "booksection", "conferencepaper", "preprint", "report", "thesis":
		return true
	default:
		return false
	}
}

func zoteroReadingItemState(item zotero.Item, annotations []zotero.Annotation) string {
	readTag := false
	unreadTag := false
	for _, tag := range item.Tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "unread", "to-read", "reading-queue":
			unreadTag = true
		case "read", "done":
			readTag = true
		}
	}
	if unreadTag {
		return store.ItemStateInbox
	}
	if readTag || len(annotations) > 0 {
		return store.ItemStateDone
	}
	return store.ItemStateInbox
}

func (a *App) linkZoteroArtifactWorkspace(artifactID int64, mapping *store.ExternalContainerMapping) {
	if mapping == nil || mapping.WorkspaceID == nil {
		return
	}
	_ = a.store.LinkArtifactToWorkspace(*mapping.WorkspaceID, artifactID)
}

func (a *App) upsertZoteroReferenceArtifact(account store.ExternalAccount, item zotero.Item, collectionNames []string, mapping *store.ExternalContainerMapping) (store.Artifact, error) {
	metaJSON, err := zoteroReferenceArtifactMeta(item, collectionNames)
	if err != nil {
		return store.Artifact{}, err
	}
	kind := store.ArtifactKindReference
	title := strings.TrimSpace(item.Title)
	remoteID := strings.TrimSpace(item.Key)
	remoteUpdatedAt := zoteroTimestampPointer(item.DateModified)
	if binding, err := a.store.GetBindingByRemote(account.ID, store.ExternalProviderZotero, "reference", remoteID); err == nil {
		if binding.ArtifactID != nil {
			err = a.store.UpdateArtifact(*binding.ArtifactID, store.ArtifactUpdate{
				Kind:     &kind,
				Title:    optionalTrimmedString(title),
				MetaJSON: metaJSON,
			})
			if err == nil {
				if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
					AccountID:       account.ID,
					Provider:        store.ExternalProviderZotero,
					ObjectType:      "reference",
					RemoteID:        remoteID,
					ArtifactID:      binding.ArtifactID,
					ContainerRef:    firstStringPointer(collectionNames),
					RemoteUpdatedAt: remoteUpdatedAt,
				}); err != nil {
					return store.Artifact{}, err
				}
				artifact, err := a.store.GetArtifact(*binding.ArtifactID)
				if err != nil {
					return store.Artifact{}, err
				}
				a.linkZoteroArtifactWorkspace(artifact.ID, mapping)
				return artifact, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return store.Artifact{}, err
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Artifact{}, err
	}

	artifact, err := a.store.CreateArtifact(kind, nil, nil, optionalTrimmedString(title), metaJSON)
	if err != nil {
		return store.Artifact{}, err
	}
	if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        store.ExternalProviderZotero,
		ObjectType:      "reference",
		RemoteID:        remoteID,
		ArtifactID:      &artifact.ID,
		ContainerRef:    firstStringPointer(collectionNames),
		RemoteUpdatedAt: remoteUpdatedAt,
	}); err != nil {
		return store.Artifact{}, err
	}
	a.linkZoteroArtifactWorkspace(artifact.ID, mapping)
	return artifact, nil
}

func (a *App) upsertZoteroAttachmentArtifact(account store.ExternalAccount, reader *zotero.Reader, attachment zotero.Attachment, referenceArtifactID int64, referenceKey string, mapping *store.ExternalContainerMapping) (store.Artifact, error) {
	metaJSON, err := zoteroAttachmentArtifactMeta(attachment, referenceArtifactID, referenceKey)
	if err != nil {
		return store.Artifact{}, err
	}
	kind := store.ArtifactKindPDF
	title := strings.TrimSpace(attachment.Title)
	refURL := optionalTrimmedString(reader.AttachmentFileURL(attachment))
	remoteUpdatedAt := zoteroTimestampPointer(attachment.DateModified)
	if binding, err := a.store.GetBindingByRemote(account.ID, store.ExternalProviderZotero, "attachment", strings.TrimSpace(attachment.Key)); err == nil {
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
					Provider:        store.ExternalProviderZotero,
					ObjectType:      "attachment",
					RemoteID:        strings.TrimSpace(attachment.Key),
					ArtifactID:      binding.ArtifactID,
					ContainerRef:    optionalTrimmedString(referenceKey),
					RemoteUpdatedAt: remoteUpdatedAt,
				}); err != nil {
					return store.Artifact{}, err
				}
				artifact, err := a.store.GetArtifact(*binding.ArtifactID)
				if err != nil {
					return store.Artifact{}, err
				}
				a.linkZoteroArtifactWorkspace(artifact.ID, mapping)
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
		Provider:        store.ExternalProviderZotero,
		ObjectType:      "attachment",
		RemoteID:        strings.TrimSpace(attachment.Key),
		ArtifactID:      &artifact.ID,
		ContainerRef:    optionalTrimmedString(referenceKey),
		RemoteUpdatedAt: remoteUpdatedAt,
	}); err != nil {
		return store.Artifact{}, err
	}
	a.linkZoteroArtifactWorkspace(artifact.ID, mapping)
	return artifact, nil
}

func (a *App) upsertZoteroAnnotationArtifact(account store.ExternalAccount, annotation zotero.Annotation, referenceArtifactID int64, referenceKey, attachmentKey string, mapping *store.ExternalContainerMapping) (store.Artifact, error) {
	metaJSON, err := zoteroAnnotationArtifactMeta(annotation, referenceArtifactID, referenceKey, attachmentKey)
	if err != nil {
		return store.Artifact{}, err
	}
	kind := store.ArtifactKindAnnotation
	title := strings.TrimSpace(annotation.Text)
	if title == "" {
		title = strings.TrimSpace(annotation.Comment)
	}
	remoteUpdatedAt := zoteroTimestampPointer(annotation.DateModified)
	if binding, err := a.store.GetBindingByRemote(account.ID, store.ExternalProviderZotero, "annotation", strings.TrimSpace(annotation.Key)); err == nil {
		if binding.ArtifactID != nil {
			err = a.store.UpdateArtifact(*binding.ArtifactID, store.ArtifactUpdate{
				Kind:     &kind,
				Title:    optionalTrimmedString(title),
				MetaJSON: metaJSON,
			})
			if err == nil {
				if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
					AccountID:       account.ID,
					Provider:        store.ExternalProviderZotero,
					ObjectType:      "annotation",
					RemoteID:        strings.TrimSpace(annotation.Key),
					ArtifactID:      binding.ArtifactID,
					ContainerRef:    optionalTrimmedString(referenceKey),
					RemoteUpdatedAt: remoteUpdatedAt,
				}); err != nil {
					return store.Artifact{}, err
				}
				artifact, err := a.store.GetArtifact(*binding.ArtifactID)
				if err != nil {
					return store.Artifact{}, err
				}
				a.linkZoteroArtifactWorkspace(artifact.ID, mapping)
				return artifact, nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return store.Artifact{}, err
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return store.Artifact{}, err
	}

	artifact, err := a.store.CreateArtifact(kind, nil, nil, optionalTrimmedString(title), metaJSON)
	if err != nil {
		return store.Artifact{}, err
	}
	if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        store.ExternalProviderZotero,
		ObjectType:      "annotation",
		RemoteID:        strings.TrimSpace(annotation.Key),
		ArtifactID:      &artifact.ID,
		ContainerRef:    optionalTrimmedString(referenceKey),
		RemoteUpdatedAt: remoteUpdatedAt,
	}); err != nil {
		return store.Artifact{}, err
	}
	a.linkZoteroArtifactWorkspace(artifact.ID, mapping)
	return artifact, nil
}

func (a *App) persistZoteroReadingItem(account store.ExternalAccount, artifact store.Artifact, item zotero.Item, mapping *store.ExternalContainerMapping, inferredProjectID *string, annotations []zotero.Annotation) (store.Item, error) {
	title := zoteroReadingTitle(item)
	source := store.ExternalProviderZotero
	sourceRef := zoteroReadingSourceRef(item.Key)
	desiredState := zoteroReadingItemState(item, annotations)
	remoteUpdatedAt := zoteroTimestampPointer(item.DateModified)
	if existing, err := a.store.GetItemBySource(source, sourceRef); err == nil {
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
		if existing.State != desiredState {
			if err := a.store.SyncItemStateBySource(source, sourceRef, desiredState); err != nil {
				return store.Item{}, err
			}
		}
		updated, err := a.store.GetItem(existing.ID)
		if err != nil {
			return store.Item{}, err
		}
		if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
			AccountID:       account.ID,
			Provider:        store.ExternalProviderZotero,
			ObjectType:      "reading_item",
			RemoteID:        strings.TrimSpace(item.Key),
			ItemID:          &updated.ID,
			ArtifactID:      &artifact.ID,
			ContainerRef:    firstStringPointer(item.Tags),
			RemoteUpdatedAt: remoteUpdatedAt,
		}); err != nil {
			return store.Item{}, err
		}
		return updated, nil
	}

	opts := store.ItemOptions{
		State:      desiredState,
		ArtifactID: &artifact.ID,
		Source:     &source,
		SourceRef:  &sourceRef,
	}
	if mapping != nil && mapping.WorkspaceID != nil {
		opts.WorkspaceID = mapping.WorkspaceID
	}
	if projectID := mappedProjectUpdateWithFallback(mapping, inferredProjectID); projectID != nil {
		opts.ProjectID = projectID
	}
	if opts.WorkspaceID == nil {
		sphere := account.Sphere
		opts.Sphere = &sphere
	}
	created, err := a.store.CreateItem(title, opts)
	if err != nil {
		return store.Item{}, err
	}
	if _, err := a.store.UpsertExternalBinding(store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        store.ExternalProviderZotero,
		ObjectType:      "reading_item",
		RemoteID:        strings.TrimSpace(item.Key),
		ItemID:          &created.ID,
		ArtifactID:      &artifact.ID,
		ContainerRef:    firstStringPointer(item.Tags),
		RemoteUpdatedAt: remoteUpdatedAt,
	}); err != nil {
		return store.Item{}, err
	}
	return created, nil
}

func firstStringPointer(values []string) *string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return &clean
		}
	}
	return nil
}

func (a *App) executeSyncZoteroAction() (string, map[string]interface{}, error) {
	accounts, err := a.activeZoteroAccounts()
	if err != nil {
		return "", nil, err
	}
	mappings, err := a.store.ListContainerMappings(store.ExternalProviderZotero)
	if err != nil {
		return "", nil, err
	}

	result := zoteroSyncResult{}
	for _, account := range accounts {
		reader, cfg, err := zoteroReaderForAccount(account)
		if errors.Is(err, zotero.ErrDatabaseNotFound) {
			result.Skipped++
			continue
		}
		if err != nil {
			return "", nil, err
		}

		func() {
			defer reader.Close()
			ctx, cancel := context.WithTimeout(context.Background(), zoteroSyncTimeout)
			defer cancel()

			items, syncErr := reader.ListItems(ctx)
			if syncErr != nil {
				err = syncErr
				return
			}
			if exportPath := strings.TrimSpace(cfg.CitationExportPath); exportPath != "" {
				citationKeys, citationErr := reader.ResolveCitationKeys(exportPath)
				if citationErr != nil && !errors.Is(citationErr, os.ErrNotExist) {
					err = citationErr
					return
				}
				for i := range items {
					if key := strings.TrimSpace(citationKeys[items[i].Key]); key != "" {
						items[i].CitationKey = key
					}
				}
			}
			collections, syncErr := reader.ListCollections(ctx)
			if syncErr != nil {
				err = syncErr
				return
			}
			collectionNamesByKey := zoteroCollectionNamesByKey(collections)

			for _, item := range items {
				names := zoteroCollectionNames(item, collectionNamesByKey)
				mapping, syncErr := a.zoteroCollectionMappingForAccount(account, mappings, names)
				if syncErr != nil {
					err = syncErr
					return
				}
				inferredProjectID := a.zoteroProjectHintFromTags(item.Tags)
				referenceArtifact, syncErr := a.upsertZoteroReferenceArtifact(account, item, names, mapping)
				if syncErr != nil {
					err = syncErr
					return
				}
				result.ReferenceCount++

				attachments, syncErr := reader.ListAttachments(ctx, item.Key)
				if syncErr != nil {
					err = syncErr
					return
				}
				annotationByAttachment := map[string][]zotero.Annotation{}
				for _, attachment := range attachments {
					annotations, annErr := reader.ListAnnotations(ctx, attachment.Key)
					if annErr != nil {
						err = annErr
						return
					}
					annotationByAttachment[strings.TrimSpace(attachment.Key)] = annotations
				}

				var readingItem *store.Item
				if zoteroReadingItemWanted(item) {
					allAnnotations := make([]zotero.Annotation, 0)
					for _, annotations := range annotationByAttachment {
						allAnnotations = append(allAnnotations, annotations...)
					}
					persisted, syncErr := a.persistZoteroReadingItem(account, referenceArtifact, item, mapping, inferredProjectID, allAnnotations)
					if syncErr != nil {
						err = syncErr
						return
					}
					readingItem = &persisted
					result.ReadingItemCount++
				}

				for _, attachment := range attachments {
					if !strings.Contains(strings.ToLower(strings.TrimSpace(attachment.ContentType)), "pdf") {
						continue
					}
					attachmentArtifact, syncErr := a.upsertZoteroAttachmentArtifact(account, reader, attachment, referenceArtifact.ID, item.Key, mapping)
					if syncErr != nil {
						err = syncErr
						return
					}
					result.AttachmentCount++
					if readingItem != nil {
						if linkErr := a.store.LinkItemArtifact(readingItem.ID, attachmentArtifact.ID, "related"); linkErr != nil {
							err = linkErr
							return
						}
					}
					for _, annotation := range annotationByAttachment[strings.TrimSpace(attachment.Key)] {
						annotationArtifact, syncErr := a.upsertZoteroAnnotationArtifact(account, annotation, referenceArtifact.ID, item.Key, attachment.Key, mapping)
						if syncErr != nil {
							err = syncErr
							return
						}
						result.AnnotationCount++
						if readingItem != nil {
							if linkErr := a.store.LinkItemArtifact(readingItem.ID, annotationArtifact.ID, "related"); linkErr != nil {
								err = linkErr
								return
							}
						}
					}
				}
			}
		}()
		if err != nil {
			return "", nil, err
		}
	}

	if result.ReferenceCount == 0 && result.Skipped > 0 {
		return "Skipped Zotero sync because no Zotero database was found.", map[string]interface{}{
			"type":             "sync_zotero",
			"reference_count":  0,
			"attachment_count": 0,
			"annotation_count": 0,
			"reading_items":    0,
			"skipped_accounts": result.Skipped,
		}, nil
	}
	return fmt.Sprintf(
			"Synced %d Zotero reference(s), %d PDF attachment(s), %d annotation(s), and %d reading item(s).",
			result.ReferenceCount,
			result.AttachmentCount,
			result.AnnotationCount,
			result.ReadingItemCount,
		), map[string]interface{}{
			"type":             "sync_zotero",
			"reference_count":  result.ReferenceCount,
			"attachment_count": result.AttachmentCount,
			"annotation_count": result.AnnotationCount,
			"reading_items":    result.ReadingItemCount,
			"skipped_accounts": result.Skipped,
		}, nil
}

func (a *App) executeZoteroAction(action *SystemAction) (string, map[string]interface{}, error) {
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "sync_zotero":
		return a.executeSyncZoteroAction()
	default:
		return "", nil, fmt.Errorf("unsupported zotero action: %s", action.Action)
	}
}
