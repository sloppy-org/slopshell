package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
	tabsync "github.com/krystophny/tabura/internal/sync"
)

const (
	sourceSyncDefaultInterval = 5 * time.Minute
	sourceSyncStaleAfter      = 24 * time.Hour
	sourceSyncCommandTimeout  = 45 * time.Second
)

type sourceSyncRunner interface {
	RunOnce(ctx context.Context) (tabsync.RunResult, error)
	RunNow(ctx context.Context) (tabsync.RunResult, error)
}

type accountSyncProvider struct {
	name        string
	syncAccount func(context.Context, store.ExternalAccount) (int, error)
	onSynced    func(store.ExternalAccount, int)
}

func (p *accountSyncProvider) Name() string {
	return p.name
}

func (p *accountSyncProvider) Sync(ctx context.Context, account store.ExternalAccount, _ tabsync.Sink) error {
	if p == nil || p.syncAccount == nil {
		return nil
	}
	count, err := p.syncAccount(ctx, account)
	if err != nil {
		return err
	}
	if count > 0 && p.onSynced != nil {
		p.onSynced(account, count)
	}
	return nil
}

func parseInlineSourceSyncIntent(text string) *SystemAction {
	switch normalizeItemCommandText(text) {
	case "sync now", "sync all", "sync everything", "sync all sources":
		return &SystemAction{Action: "sync_sources", Params: map[string]interface{}{}}
	default:
		return nil
	}
}

func sourceSyncActionFailurePrefix(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "sync_sources":
		return "I couldn't sync external sources: "
	default:
		return "I couldn't sync external sources: "
	}
}

func (a *App) newSourceSyncRunner() sourceSyncRunner {
	if a == nil || a.store == nil {
		return nil
	}
	engine := tabsync.NewEngine(a.store, a.store, nil, tabsync.Options{
		DefaultInterval: sourceSyncDefaultInterval,
		StaleAfter:      sourceSyncStaleAfter,
	})
	for _, provider := range []*accountSyncProvider{
		{
			name:        store.ExternalProviderGmail,
			syncAccount: a.syncManagedEmailAccount,
			onSynced:    a.handleSourceSyncCount,
		},
		{
			name:        store.ExternalProviderIMAP,
			syncAccount: a.syncEmailAccount,
			onSynced:    a.handleSourceSyncCount,
		},
		{
			name:        store.ExternalProviderExchange,
			syncAccount: a.syncContactAccount,
		},
		{
			name:        store.ExternalProviderTodoist,
			syncAccount: a.syncTodoistAccount,
			onSynced:    a.handleSourceSyncCount,
		},
		{
			name: store.ExternalProviderEvernote,
			syncAccount: func(ctx context.Context, account store.ExternalAccount) (int, error) {
				result, err := a.syncEvernoteAccount(ctx, account)
				return result.NoteCount + result.TaskCount, err
			},
			onSynced: a.handleSourceSyncCount,
		},
		{
			name: store.ExternalProviderBear,
			syncAccount: func(ctx context.Context, account store.ExternalAccount) (int, error) {
				result, err := a.syncBearAccount(ctx, account)
				return result.NoteCount, err
			},
			onSynced: a.handleSourceSyncCount,
		},
		{
			name: store.ExternalProviderZotero,
			syncAccount: func(ctx context.Context, account store.ExternalAccount) (int, error) {
				result, err := a.syncZoteroAccount(ctx, account)
				return result.ReferenceCount + result.AttachmentCount + result.AnnotationCount + result.ReadingItemCount, err
			},
			onSynced: a.handleSourceSyncCount,
		},
	} {
		engine.Register(provider)
	}
	return engine
}

func (a *App) handleSourceSyncCount(account store.ExternalAccount, count int) {
	if a == nil || count <= 0 {
		return
	}
	a.broadcastItemsIngested(count, account.Provider)
}

func (a *App) startSourcePoller() {
	if a == nil || a.shutdownCtx == nil || a.sourceSync == nil {
		return
	}
	a.workerWG.Add(1)
	go func() {
		defer a.workerWG.Done()
		a.runSourcePoller(a.shutdownCtx)
	}()
}

func (a *App) runSourcePoller(ctx context.Context) {
	if a == nil || a.sourceSync == nil {
		return
	}
	for {
		result, err := a.sourceSync.RunOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("source poller: %v", err)
			if err := sleepSourcePoller(ctx, sourceSyncDefaultInterval); err != nil {
				return
			}
			continue
		}
		if err := sleepSourcePoller(ctx, nextSourceSyncDelay(result.NextDelay)); err != nil {
			return
		}
	}
}

func nextSourceSyncDelay(delay time.Duration) time.Duration {
	if delay <= 0 {
		return sourceSyncDefaultInterval
	}
	return delay
}

func sleepSourcePoller(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (a *App) syncSourcesNow(ctx context.Context) (tabsync.RunResult, error) {
	if a == nil || a.sourceSync == nil {
		return tabsync.RunResult{}, fmt.Errorf("no external source poller is configured")
	}
	return a.sourceSync.RunNow(ctx)
}

func summarizeSourceSyncResult(result tabsync.RunResult) string {
	if len(result.Accounts) == 0 {
		return "No enabled external sources were configured."
	}
	synced := 0
	skipped := 0
	failed := 0
	for _, account := range result.Accounts {
		switch {
		case account.Err != nil:
			failed++
		case account.Skipped:
			skipped++
		default:
			synced++
		}
	}
	message := fmt.Sprintf("Polled %d external source account(s)", len(result.Accounts))
	message += fmt.Sprintf("; %d synced", synced)
	if skipped > 0 {
		message += fmt.Sprintf(", %d skipped", skipped)
	}
	if failed > 0 {
		message += fmt.Sprintf(", %d failed", failed)
	}
	return message + "."
}

func sourceSyncResultPayload(result tabsync.RunResult) map[string]interface{} {
	payload := map[string]interface{}{
		"type":          "sync_sources",
		"account_count": len(result.Accounts),
	}
	accounts := make([]map[string]interface{}, 0, len(result.Accounts))
	synced := 0
	skipped := 0
	failed := 0
	for _, account := range result.Accounts {
		entry := map[string]interface{}{
			"account_id": account.AccountID,
			"provider":   account.Provider,
			"label":      account.Label,
			"skipped":    account.Skipped,
		}
		if account.Reason != "" {
			entry["reason"] = account.Reason
		}
		if account.Err != nil {
			entry["error"] = account.Err.Error()
			failed++
		} else if account.Skipped {
			skipped++
		} else {
			synced++
		}
		accounts = append(accounts, entry)
	}
	payload["synced_accounts"] = synced
	payload["skipped_accounts"] = skipped
	payload["failed_accounts"] = failed
	payload["accounts"] = accounts
	return payload
}

func (a *App) executeSourceSyncAction(_ store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "sync_sources":
		ctx, cancel := context.WithTimeout(context.Background(), sourceSyncCommandTimeout)
		defer cancel()
		result, err := a.syncSourcesNow(ctx)
		if err != nil {
			return "", nil, err
		}
		return summarizeSourceSyncResult(result), sourceSyncResultPayload(result), nil
	default:
		return "", nil, fmt.Errorf("unsupported source sync action: %s", action.Action)
	}
}

func (a *App) broadcastItemsIngested(count int, source string) {
	if a == nil || count <= 0 {
		return
	}
	encoded, err := json.Marshal(map[string]interface{}{
		"type":   "items_ingested",
		"count":  count,
		"source": strings.TrimSpace(source),
	})
	if err != nil {
		return
	}
	a.hub.forEachChatConn(func(conn *chatWSConn) {
		_ = conn.writeText(encoded)
	})
}
