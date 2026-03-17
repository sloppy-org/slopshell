package web

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
	tabsync "github.com/krystophny/tabura/internal/sync"
)

const (
	emailHistoryDefaultPageSize    = 250
	emailHistoryDefaultPagesPerRun = 3
	emailHistoryMailboxContainer   = "__mailbox__"
	emailHistoryStateKey           = "history_sync"
	emailHistoryContinueDelay      = 2 * time.Second
)

type emailHistorySyncState struct {
	CurrentContainer string   `json:"current_container,omitempty"`
	Cursor           string   `json:"cursor,omitempty"`
	Completed        []string `json:"completed,omitempty"`
	Complete         bool     `json:"complete,omitempty"`
}

type emailHistoryContainer struct {
	Key    string
	Folder string
	Name   string
}

type emailHistoryLabelProvider interface {
	ListLabels(context.Context) ([]providerdata.Label, error)
}

func emailHistoryPageSize(cfg emailSyncAccountConfig) int64 {
	if cfg.HistoryPageSize > 0 {
		return int64(cfg.HistoryPageSize)
	}
	return emailHistoryDefaultPageSize
}

func emailHistoryPagesPerRun(cfg emailSyncAccountConfig) int {
	if cfg.HistoryPagesPerRun > 0 {
		return cfg.HistoryPagesPerRun
	}
	return emailHistoryDefaultPagesPerRun
}

func decodeEmailHistorySyncState(account store.ExternalAccount) (emailHistorySyncState, error) {
	var state emailHistorySyncState
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return state, nil
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return state, err
	}
	if payload, ok := config[emailHistoryStateKey]; ok {
		data, err := json.Marshal(payload)
		if err != nil {
			return state, err
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return emailHistorySyncState{}, err
		}
	}
	state.CurrentContainer = strings.TrimSpace(state.CurrentContainer)
	state.Cursor = strings.TrimSpace(state.Cursor)
	state.Completed = compactEmailHistoryKeys(state.Completed)
	return state, nil
}

func compactEmailHistoryKeys(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}

func (a *App) updateEmailHistorySyncState(account *store.ExternalAccount, state emailHistorySyncState) error {
	if a == nil || a.store == nil || account == nil {
		return nil
	}
	var config map[string]any
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw != "" && raw != "{}" {
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return err
		}
	}
	if config == nil {
		config = map[string]any{}
	}
	state.CurrentContainer = strings.TrimSpace(state.CurrentContainer)
	state.Cursor = strings.TrimSpace(state.Cursor)
	state.Completed = compactEmailHistoryKeys(state.Completed)
	if state.Complete {
		state.CurrentContainer = ""
		state.Cursor = ""
	}
	config[emailHistoryStateKey] = map[string]any{
		"current_container": state.CurrentContainer,
		"cursor":            state.Cursor,
		"completed":         append([]string(nil), state.Completed...),
		"complete":          state.Complete,
	}
	if err := a.store.UpdateExternalAccount(account.ID, store.ExternalAccountUpdate{Config: config}); err != nil {
		return err
	}
	updated, err := a.store.GetExternalAccount(account.ID)
	if err != nil {
		return err
	}
	*account = updated
	return nil
}

func emailHistoryContainerKey(label providerdata.Label) string {
	if clean := strings.TrimSpace(label.ID); clean != "" {
		return clean
	}
	return strings.TrimSpace(label.Name)
}

func emailHistoryFolderWeight(name string) int {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case lower == "", lower == "inbox", lower == "posteingang":
		return 0
	case strings.Contains(lower, "sent"), strings.Contains(lower, "gesendet"):
		return 10
	case strings.Contains(lower, "archive"), strings.Contains(lower, "archiv"):
		return 20
	case strings.Contains(lower, "draft"), strings.Contains(lower, "entw"):
		return 30
	case strings.Contains(lower, "trash"), strings.Contains(lower, "deleted"), strings.Contains(lower, "gelösch"):
		return 40
	case strings.Contains(lower, "junk"), strings.Contains(lower, "spam"):
		return 50
	default:
		return 25
	}
}

func emailHistorySkipFolder(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case lower == "":
		return false
	case strings.Contains(lower, "kalender"), strings.Contains(lower, "calendar"):
		return true
	case strings.Contains(lower, "kontakte"), strings.Contains(lower, "contacts"):
		return true
	case strings.Contains(lower, "aufgaben"), strings.Contains(lower, "tasks"):
		return true
	case strings.Contains(lower, "journal"), strings.Contains(lower, "notizen"), strings.Contains(lower, "notes"):
		return true
	default:
		return false
	}
}

func emailHistoryContainers(ctx context.Context, account store.ExternalAccount, provider emailSyncProvider) ([]emailHistoryContainer, error) {
	switch account.Provider {
	case store.ExternalProviderGmail, store.ExternalProviderExchange:
		return []emailHistoryContainer{{
			Key:  emailHistoryMailboxContainer,
			Name: "Mailbox",
		}}, nil
	}
	labelProvider, ok := provider.(emailHistoryLabelProvider)
	if !ok {
		return nil, nil
	}
	labels, err := labelProvider.ListLabels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]emailHistoryContainer, 0, len(labels))
	seen := map[string]struct{}{}
	for _, label := range labels {
		key := emailHistoryContainerKey(label)
		name := strings.TrimSpace(label.Name)
		if key == "" || emailHistorySkipFolder(name) {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, emailHistoryContainer{
			Key:    key,
			Folder: key,
			Name:   name,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		leftWeight := emailHistoryFolderWeight(firstNonEmpty(out[i].Name, out[i].Folder, out[i].Key))
		rightWeight := emailHistoryFolderWeight(firstNonEmpty(out[j].Name, out[j].Folder, out[j].Key))
		if leftWeight != rightWeight {
			return leftWeight < rightWeight
		}
		leftName := strings.ToLower(firstNonEmpty(out[i].Name, out[i].Folder, out[i].Key))
		rightName := strings.ToLower(firstNonEmpty(out[j].Name, out[j].Folder, out[j].Key))
		return leftName < rightName
	})
	return out, nil
}

func nextEmailHistoryContainer(containers []emailHistoryContainer, state emailHistorySyncState) (emailHistoryContainer, bool) {
	if len(containers) == 0 {
		return emailHistoryContainer{}, false
	}
	completed := map[string]struct{}{}
	for _, key := range state.Completed {
		completed[key] = struct{}{}
	}
	if state.CurrentContainer != "" {
		if _, ok := completed[state.CurrentContainer]; !ok {
			for _, container := range containers {
				if container.Key == state.CurrentContainer {
					return container, true
				}
			}
		}
	}
	for _, container := range containers {
		if _, ok := completed[container.Key]; ok {
			continue
		}
		return container, true
	}
	return emailHistoryContainer{}, false
}

func emailHistoryStatePending(state emailHistorySyncState, containers []emailHistoryContainer) bool {
	if state.Complete {
		return false
	}
	_, ok := nextEmailHistoryContainer(containers, state)
	return ok
}

func (a *App) emailHistoryPending(account store.ExternalAccount) bool {
	state, err := decodeEmailHistorySyncState(account)
	if err != nil {
		return false
	}
	return !state.Complete
}

func (a *App) emailContainerRepairPending(account store.ExternalAccount) bool {
	if a == nil || a.store == nil {
		return false
	}
	bindings, err := a.store.ListBindingsMissingContainerRef(account.ID, account.Provider, emailBindingObjectType, 1)
	if err != nil {
		return false
	}
	return len(bindings) > 0
}

func (a *App) emailInboxSyncPending(account store.ExternalAccount) bool {
	state, err := decodeEmailInboxSyncState(account)
	if err != nil {
		return false
	}
	return state.Enabled && state.HasMore
}

func (a *App) emailSourceContinuation(_ context.Context, account store.ExternalAccount) (time.Duration, bool) {
	if a.emailHistoryPending(account) || a.emailContainerRepairPending(account) || a.emailInboxSyncPending(account) {
		return emailHistoryContinueDelay, true
	}
	return 0, false
}

func (a *App) persistEmailMessagesBatch(ctx context.Context, sink tabsync.Sink, account store.ExternalAccount, mappings []store.ExternalContainerMapping, followUpIDs map[string]struct{}, messages []*providerdata.EmailMessage) (emailSyncResult, []emailPersistedMessage, error) {
	result := emailSyncResult{}
	persistedMessages := make([]emailPersistedMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil || strings.TrimSpace(message.ID) == "" {
			continue
		}
		persisted, err := a.persistEmailMessage(ctx, sink, account, message, mappings, hasEmailMessageID(followUpIDs, message.ID))
		if err != nil {
			return emailSyncResult{}, nil, err
		}
		persistedMessages = append(persistedMessages, persisted)
		result.MessageCount++
		if persisted.FollowUpItem {
			result.ItemCount++
		}
	}
	threads, err := a.persistEmailThreads(ctx, sink, account, mappings, persistedMessages)
	if err != nil {
		return emailSyncResult{}, nil, err
	}
	if err := a.persistEmailActionItems(account, threads); err != nil {
		return emailSyncResult{}, nil, err
	}
	return result, persistedMessages, nil
}

func (a *App) syncEmailHistoryBackfill(ctx context.Context, account *store.ExternalAccount, provider emailSyncProvider, cfg emailSyncAccountConfig, sink tabsync.Sink, mappings []store.ExternalContainerMapping, followUpIDs map[string]struct{}, seen map[string]struct{}) (emailSyncResult, bool, error) {
	pager, ok := provider.(email.MessagePageProvider)
	if !ok {
		return emailSyncResult{}, false, nil
	}
	state, err := decodeEmailHistorySyncState(*account)
	if err != nil {
		return emailSyncResult{}, false, err
	}
	containers, err := emailHistoryContainers(ctx, *account, provider)
	if err != nil {
		return emailSyncResult{}, false, err
	}
	if len(containers) == 0 {
		state.Complete = true
		if err := a.updateEmailHistorySyncState(account, state); err != nil {
			return emailSyncResult{}, false, err
		}
		return emailSyncResult{}, false, nil
	}
	result := emailSyncResult{}
	pagesRemaining := emailHistoryPagesPerRun(cfg)
	if pagesRemaining <= 0 {
		pagesRemaining = 1
	}
	pageSize := emailHistoryPageSize(cfg)
	if pageSize <= 0 {
		pageSize = emailHistoryDefaultPageSize
	}
	for pagesRemaining > 0 {
		container, ok := nextEmailHistoryContainer(containers, state)
		if !ok {
			state.Complete = true
			if err := a.updateEmailHistorySyncState(account, state); err != nil {
				return emailSyncResult{}, false, err
			}
			return result, false, nil
		}
		opts := email.DefaultSearchOptions().WithMaxResults(pageSize)
		if container.Folder != "" {
			opts = opts.WithFolder(container.Folder)
		}
		page, err := pager.ListMessagesPage(ctx, opts, state.Cursor)
		if err != nil {
			return result, true, err
		}
		if len(page.IDs) == 0 && strings.TrimSpace(page.NextPageToken) == "" {
			state.CurrentContainer = ""
			state.Cursor = ""
			state.Completed = append(state.Completed, container.Key)
			if !emailHistoryStatePending(state, containers) {
				state.Complete = true
			}
			if err := a.updateEmailHistorySyncState(account, state); err != nil {
				return emailSyncResult{}, true, err
			}
			pagesRemaining--
			continue
		}
		pageIDs := make([]string, 0, len(page.IDs))
		for _, id := range page.IDs {
			clean := strings.TrimSpace(id)
			if clean == "" {
				continue
			}
			if _, ok := seen[clean]; ok {
				continue
			}
			seen[clean] = struct{}{}
			pageIDs = append(pageIDs, clean)
		}
		if len(pageIDs) == 0 {
			state.CurrentContainer = container.Key
			state.Cursor = strings.TrimSpace(page.NextPageToken)
			if state.Cursor == "" {
				state.CurrentContainer = ""
				state.Completed = append(state.Completed, container.Key)
				if !emailHistoryStatePending(state, containers) {
					state.Complete = true
				}
			}
			if err := a.updateEmailHistorySyncState(account, state); err != nil {
				return result, true, err
			}
			pagesRemaining--
			continue
		}
		messages, err := provider.GetMessages(ctx, pageIDs, "full")
		if err != nil {
			return result, true, err
		}
		batchResult, _, err := a.persistEmailMessagesBatch(ctx, sink, *account, mappings, followUpIDs, messages)
		if err != nil {
			return result, true, err
		}
		result.MessageCount += batchResult.MessageCount
		result.ItemCount += batchResult.ItemCount
		state.CurrentContainer = container.Key
		state.Cursor = strings.TrimSpace(page.NextPageToken)
		if state.Cursor == "" {
			state.CurrentContainer = ""
			state.Completed = append(state.Completed, container.Key)
			if !emailHistoryStatePending(state, containers) {
				state.Complete = true
			}
		}
		if err := a.updateEmailHistorySyncState(account, state); err != nil {
			return result, true, err
		}
		pagesRemaining--
	}
	return result, emailHistoryStatePending(state, containers), nil
}
