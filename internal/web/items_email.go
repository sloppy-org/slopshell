package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
	tabsync "github.com/krystophny/tabura/internal/sync"
)

const (
	emailSyncDefaultRecentDays = 30
	emailSyncDefaultMaxResults = 200
	emailBindingObjectType     = "email"
)

type emailSyncProvider interface {
	ListMessages(context.Context, email.SearchOptions) ([]string, error)
	GetMessages(context.Context, []string, string) ([]*providerdata.EmailMessage, error)
	Close() error
}

type emailFollowUpRuleConfig struct {
	Text    string `json:"text"`
	Subject string `json:"subject"`
	From    string `json:"from"`
	To      string `json:"to"`
}

func (r *emailFollowUpRuleConfig) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		r.Text = strings.TrimSpace(text)
		r.Subject = ""
		r.From = ""
		r.To = ""
		return nil
	}
	type rawRule emailFollowUpRuleConfig
	var raw rawRule
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*r = emailFollowUpRuleConfig{
		Text:    strings.TrimSpace(raw.Text),
		Subject: strings.TrimSpace(raw.Subject),
		From:    strings.TrimSpace(raw.From),
		To:      strings.TrimSpace(raw.To),
	}
	return nil
}

func (r emailFollowUpRuleConfig) empty() bool {
	return r.Text == "" && r.Subject == "" && r.From == "" && r.To == ""
}

type emailSyncAccountConfig struct {
	Host            string                    `json:"host"`
	Port            int                       `json:"port"`
	Username        string                    `json:"username"`
	TLS             bool                      `json:"tls"`
	StartTLS        bool                      `json:"starttls"`
	TokenPath       string                    `json:"token_path"`
	TokenFile       string                    `json:"token_file"`
	CredentialsPath string                    `json:"credentials_path"`
	CredentialsFile string                    `json:"credentials_file"`
	SyncMaxResults  int64                     `json:"sync_max_results"`
	SyncWindowDays  int                       `json:"sync_window_days"`
	FollowUpRules   []emailFollowUpRuleConfig `json:"follow_up_rules"`
}

type emailSyncResult struct {
	MessageCount int
	ItemCount    int
}

func decodeEmailSyncAccountConfig(account store.ExternalAccount) (emailSyncAccountConfig, error) {
	var cfg emailSyncAccountConfig
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return emailSyncAccountConfig{}, fmt.Errorf("decode %s email sync config: %w", account.Provider, err)
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.TokenPath = strings.TrimSpace(cfg.TokenPath)
	cfg.TokenFile = strings.TrimSpace(cfg.TokenFile)
	cfg.CredentialsPath = strings.TrimSpace(cfg.CredentialsPath)
	cfg.CredentialsFile = strings.TrimSpace(cfg.CredentialsFile)
	filteredRules := make([]emailFollowUpRuleConfig, 0, len(cfg.FollowUpRules))
	for _, rule := range cfg.FollowUpRules {
		if rule.empty() {
			continue
		}
		filteredRules = append(filteredRules, rule)
	}
	cfg.FollowUpRules = filteredRules
	return cfg, nil
}

func emailSyncConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".tabura"
	}
	return filepath.Join(home, ".config", "tabura")
}

func emailConfigPath(configDir, explicitPath, fileName string) string {
	switch {
	case strings.TrimSpace(explicitPath) != "":
		clean := filepath.Clean(strings.TrimSpace(explicitPath))
		if filepath.IsAbs(clean) {
			return clean
		}
		return filepath.Join(configDir, clean)
	case strings.TrimSpace(fileName) != "":
		return filepath.Join(configDir, strings.TrimSpace(fileName))
	default:
		return ""
	}
}

func gmailTokenPathForAccount(account store.ExternalAccount, cfg emailSyncAccountConfig) string {
	configDir := emailSyncConfigDir()
	if path := emailConfigPath(configDir, cfg.TokenPath, ""); path != "" {
		return path
	}
	if strings.TrimSpace(cfg.TokenFile) != "" {
		return filepath.Join(configDir, "tokens", strings.TrimSpace(cfg.TokenFile))
	}
	return store.ExternalAccountTokenPath(configDir, account.Provider, account.Label)
}

func gmailCredentialsPathForAccount(cfg emailSyncAccountConfig) string {
	configDir := emailSyncConfigDir()
	if path := emailConfigPath(configDir, cfg.CredentialsPath, cfg.CredentialsFile); path != "" {
		return path
	}
	return filepath.Join(configDir, "gmail_credentials.json")
}

func (a *App) emailSyncProviderForAccount(ctx context.Context, account store.ExternalAccount, cfg emailSyncAccountConfig) (emailSyncProvider, error) {
	if a != nil && a.newEmailSyncProvider != nil {
		return a.newEmailSyncProvider(ctx, account)
	}
	switch account.Provider {
	case store.ExternalProviderGmail:
		return email.NewGmailWithFiles(gmailCredentialsPathForAccount(cfg), gmailTokenPathForAccount(account, cfg))
	case store.ExternalProviderIMAP:
		if cfg.Host == "" {
			return nil, errors.New("imap host is required")
		}
		if cfg.Username == "" {
			return nil, errors.New("imap username is required")
		}
		password, _, err := a.store.ResolveExternalAccountPasswordForAccount(ctx, account)
		if err != nil {
			return nil, err
		}
		useTLS := cfg.TLS || cfg.Port == 993
		return email.NewIMAPClient(account.Label, cfg.Host, cfg.Port, cfg.Username, password, useTLS, cfg.StartTLS), nil
	default:
		return nil, fmt.Errorf("email sync does not support provider %s", account.Provider)
	}
}

func emailSyncMaxResults(cfg emailSyncAccountConfig) int64 {
	if cfg.SyncMaxResults > 0 {
		return cfg.SyncMaxResults
	}
	return emailSyncDefaultMaxResults
}

func emailSyncSince(now time.Time, latestRemoteUpdatedAt *string, cfg emailSyncAccountConfig) time.Time {
	days := cfg.SyncWindowDays
	if days <= 0 {
		days = emailSyncDefaultRecentDays
	}
	since := now.AddDate(0, 0, -days)
	if latestRemoteUpdatedAt == nil || strings.TrimSpace(*latestRemoteUpdatedAt) == "" {
		return since
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*latestRemoteUpdatedAt))
	if err != nil {
		return since
	}
	if parsed.Before(since) {
		return since
	}
	return parsed.Add(-time.Minute)
}

func collectEmailMessageIDs(target map[string]struct{}, ids []string) {
	for _, id := range ids {
		clean := strings.TrimSpace(id)
		if clean == "" {
			continue
		}
		target[clean] = struct{}{}
	}
}

func sortedEmailMessageIDs(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func listEmailMessagesWithFallback(ctx context.Context, provider emailSyncProvider, opts email.SearchOptions) ([]string, error) {
	ids, err := provider.ListMessages(ctx, opts)
	if err == nil || strings.TrimSpace(opts.Folder) == "" {
		return ids, err
	}
	opts.Folder = ""
	return provider.ListMessages(ctx, opts)
}

func followUpRuleSearchOptions(rule emailFollowUpRuleConfig, maxResults int64) email.SearchOptions {
	opts := email.DefaultSearchOptions().WithMaxResults(maxResults)
	if rule.Text != "" {
		opts = opts.WithText(rule.Text)
	}
	if rule.Subject != "" {
		opts = opts.WithSubject(rule.Subject)
	}
	if rule.From != "" {
		opts = opts.WithFrom(rule.From)
	}
	if rule.To != "" {
		opts = opts.WithTo(rule.To)
	}
	return opts
}

func emailFollowUpMessageIDs(ctx context.Context, provider emailSyncProvider, cfg emailSyncAccountConfig) (map[string]struct{}, error) {
	maxResults := emailSyncMaxResults(cfg)
	out := make(map[string]struct{})

	unreadOpts := email.DefaultSearchOptions().
		WithFolder("INBOX").
		WithIsRead(false).
		WithMaxResults(maxResults)
	unreadIDs, err := listEmailMessagesWithFallback(ctx, provider, unreadOpts)
	if err != nil {
		return nil, err
	}
	collectEmailMessageIDs(out, unreadIDs)

	flaggedIDs, err := provider.ListMessages(ctx, email.DefaultSearchOptions().WithIsFlagged(true).WithMaxResults(maxResults))
	if err != nil {
		return nil, err
	}
	collectEmailMessageIDs(out, flaggedIDs)

	for _, rule := range cfg.FollowUpRules {
		ruleIDs, err := provider.ListMessages(ctx, followUpRuleSearchOptions(rule, maxResults))
		if err != nil {
			return nil, err
		}
		collectEmailMessageIDs(out, ruleIDs)
	}
	return out, nil
}

func emailMessageTitle(message *providerdata.EmailMessage) string {
	title := strings.TrimSpace(message.Subject)
	if title != "" {
		return title
	}
	if sender := strings.TrimSpace(message.Sender); sender != "" {
		return sender
	}
	return "Email"
}

func emailMessageRemoteUpdatedAt(message *providerdata.EmailMessage) *string {
	if message == nil || message.Date.IsZero() {
		return nil
	}
	value := message.Date.UTC().Format(time.RFC3339)
	return &value
}

func emailMessageContainerRef(message *providerdata.EmailMessage) *string {
	if message == nil {
		return nil
	}
	for _, label := range message.Labels {
		clean := strings.TrimSpace(label)
		if clean == "" {
			continue
		}
		return &clean
	}
	return nil
}

func emailArtifactMetaJSON(message *providerdata.EmailMessage, senderActor *store.Actor) (string, error) {
	payload := map[string]any{
		"thread_id":  strings.TrimSpace(message.ThreadID),
		"subject":    strings.TrimSpace(message.Subject),
		"sender":     strings.TrimSpace(message.Sender),
		"recipients": append([]string(nil), message.Recipients...),
		"labels":     append([]string(nil), message.Labels...),
		"is_read":    message.IsRead,
	}
	if !message.Date.IsZero() {
		payload["date"] = message.Date.UTC().Format(time.RFC3339)
	}
	if snippet := strings.TrimSpace(message.Snippet); snippet != "" {
		payload["snippet"] = snippet
	}
	if senderActor != nil {
		payload["sender_actor_id"] = senderActor.ID
		if senderActor.Email != nil {
			payload["sender_email"] = *senderActor.Email
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func copyInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func (a *App) syncManagedEmailAccount(ctx context.Context, account store.ExternalAccount) (int, error) {
	messageCount, err := a.syncEmailAccount(ctx, account)
	if err != nil {
		return 0, err
	}
	if _, err := a.syncContactAccount(ctx, account); err != nil {
		return 0, err
	}
	return messageCount, nil
}

func (a *App) upsertMessageSenderActor(account store.ExternalAccount, message *providerdata.EmailMessage) (*store.Actor, error) {
	if message == nil {
		return nil, nil
	}
	contact := parseSenderContact(message.Sender)
	return a.upsertContactActor(account, contact)
}

func (a *App) persistEmailMessage(ctx context.Context, sink tabsync.Sink, account store.ExternalAccount, message *providerdata.EmailMessage, followUp bool) (emailPersistedMessage, error) {
	if message == nil || strings.TrimSpace(message.ID) == "" {
		return emailPersistedMessage{}, nil
	}
	senderActor, err := a.upsertMessageSenderActor(account, message)
	if err != nil {
		return emailPersistedMessage{}, err
	}
	title := emailMessageTitle(message)
	metaJSON, err := emailArtifactMetaJSON(message, senderActor)
	if err != nil {
		return emailPersistedMessage{}, err
	}
	binding := store.ExternalBinding{
		AccountID:       account.ID,
		Provider:        account.Provider,
		ObjectType:      emailBindingObjectType,
		RemoteID:        strings.TrimSpace(message.ID),
		ContainerRef:    emailMessageContainerRef(message),
		RemoteUpdatedAt: emailMessageRemoteUpdatedAt(message),
	}
	artifact, err := sink.UpsertArtifact(ctx, store.Artifact{
		Kind:     store.ArtifactKindEmail,
		Title:    &title,
		MetaJSON: &metaJSON,
	}, binding)
	if err != nil {
		return emailPersistedMessage{}, err
	}
	persisted := emailPersistedMessage{
		Message:  message,
		Artifact: artifact,
	}
	existingBinding, err := a.store.GetBindingByRemote(account.ID, account.Provider, emailBindingObjectType, strings.TrimSpace(message.ID))
	if err != nil {
		return emailPersistedMessage{}, err
	}
	persisted.ItemID = copyInt64Pointer(existingBinding.ItemID)
	if !followUp {
		return persisted, nil
	}
	if existingBinding.ItemID != nil {
		item, err := a.store.GetItem(*existingBinding.ItemID)
		if err != nil {
			return emailPersistedMessage{}, err
		}
		if item.State == store.ItemStateDone {
			return persisted, nil
		}
	}

	source := account.Provider
	sourceRef := "message:" + strings.TrimSpace(message.ID)
	artifactID := artifact.ID
	var actorID *int64
	if senderActor != nil {
		actorID = &senderActor.ID
	}
	_, err = sink.UpsertItem(ctx, store.Item{
		Title:      title,
		State:      store.ItemStateInbox,
		ArtifactID: &artifactID,
		ActorID:    actorID,
		Source:     &source,
		SourceRef:  &sourceRef,
	}, binding)
	if err != nil {
		return emailPersistedMessage{}, err
	}
	updatedBinding, err := a.store.GetBindingByRemote(account.ID, account.Provider, emailBindingObjectType, strings.TrimSpace(message.ID))
	if err != nil {
		return emailPersistedMessage{}, err
	}
	persisted.ItemID = copyInt64Pointer(updatedBinding.ItemID)
	persisted.FollowUpItem = true
	return persisted, nil
}

func (a *App) syncEmailAccountWithProvider(ctx context.Context, account store.ExternalAccount, provider emailSyncProvider) (emailSyncResult, error) {
	cfg, err := decodeEmailSyncAccountConfig(account)
	if err != nil {
		return emailSyncResult{}, err
	}

	latestRemoteUpdatedAt, err := a.store.LatestBindingRemoteUpdatedAt(account.ID, account.Provider, emailBindingObjectType)
	if err != nil {
		return emailSyncResult{}, err
	}
	followUpIDs, err := emailFollowUpMessageIDs(ctx, provider, cfg)
	if err != nil {
		return emailSyncResult{}, err
	}
	messageIDs := make(map[string]struct{}, len(followUpIDs))
	for id := range followUpIDs {
		messageIDs[id] = struct{}{}
	}

	since := emailSyncSince(time.Now().UTC(), latestRemoteUpdatedAt, cfg)
	recentIDs, err := provider.ListMessages(ctx, email.DefaultSearchOptions().WithSince(since).WithMaxResults(emailSyncMaxResults(cfg)))
	if err != nil {
		return emailSyncResult{}, err
	}
	collectEmailMessageIDs(messageIDs, recentIDs)
	if len(messageIDs) == 0 {
		return emailSyncResult{}, nil
	}

	messages, err := provider.GetMessages(ctx, sortedEmailMessageIDs(messageIDs), "full")
	if err != nil {
		return emailSyncResult{}, err
	}
	sink := tabsync.NewStoreSink(a.store)
	result := emailSyncResult{}
	persistedMessages := make([]emailPersistedMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil || strings.TrimSpace(message.ID) == "" {
			continue
		}
		persisted, err := a.persistEmailMessage(ctx, sink, account, message, hasEmailMessageID(followUpIDs, message.ID))
		if err != nil {
			return emailSyncResult{}, err
		}
		persistedMessages = append(persistedMessages, persisted)
		result.MessageCount++
		if persisted.FollowUpItem {
			result.ItemCount++
		}
	}
	threads, err := a.persistEmailThreads(ctx, sink, account, persistedMessages)
	if err != nil {
		return emailSyncResult{}, err
	}
	if err := a.persistEmailActionItems(account, threads); err != nil {
		return emailSyncResult{}, err
	}
	return result, nil
}

func hasEmailMessageID(values map[string]struct{}, id string) bool {
	_, ok := values[strings.TrimSpace(id)]
	return ok
}

func (a *App) syncEmailAccount(ctx context.Context, account store.ExternalAccount) (int, error) {
	cfg, err := decodeEmailSyncAccountConfig(account)
	if err != nil {
		return 0, err
	}
	provider, err := a.emailSyncProviderForAccount(ctx, account, cfg)
	if err != nil {
		return 0, err
	}
	defer provider.Close()

	result, err := a.syncEmailAccountWithProvider(ctx, account, provider)
	if err != nil {
		return 0, err
	}
	return result.MessageCount, nil
}
