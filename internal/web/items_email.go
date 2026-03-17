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
	emailContainerRepairBatch  = 200
	emailInboxSyncStateKey     = "inbox_sync"
)

type emailInboxSyncState struct {
	Cursor  string `json:"cursor,omitempty"`
	HasMore bool   `json:"has_more,omitempty"`
	Enabled bool   `json:"enabled,omitempty"`
}

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
	Host               string                    `json:"host"`
	Port               int                       `json:"port"`
	Username           string                    `json:"username"`
	TLS                bool                      `json:"tls"`
	StartTLS           bool                      `json:"starttls"`
	FromAddress        string                    `json:"from_address"`
	FromName           string                    `json:"from_name"`
	TokenPath          string                    `json:"token_path"`
	TokenFile          string                    `json:"token_file"`
	CredentialsPath    string                    `json:"credentials_path"`
	CredentialsFile    string                    `json:"credentials_file"`
	SMTPHost           string                    `json:"smtp_host"`
	SMTPPort           int                       `json:"smtp_port"`
	SMTPTLS            bool                      `json:"smtp_tls"`
	SMTPStartTLS       bool                      `json:"smtp_starttls"`
	SMTPUsername       string                    `json:"smtp_username"`
	SMTPCredential     string                    `json:"smtp_credential_ref"`
	DraftsMailbox      string                    `json:"drafts_mailbox"`
	SyncMaxResults     int64                     `json:"sync_max_results"`
	SyncWindowDays     int                       `json:"sync_window_days"`
	HistoryPageSize    int                       `json:"history_page_size"`
	HistoryPagesPerRun int                       `json:"history_pages_per_run"`
	FollowUpRules      []emailFollowUpRuleConfig `json:"follow_up_rules"`
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
	cfg.FromAddress = strings.TrimSpace(cfg.FromAddress)
	cfg.FromName = strings.TrimSpace(cfg.FromName)
	cfg.TokenPath = strings.TrimSpace(cfg.TokenPath)
	cfg.TokenFile = strings.TrimSpace(cfg.TokenFile)
	cfg.CredentialsPath = strings.TrimSpace(cfg.CredentialsPath)
	cfg.CredentialsFile = strings.TrimSpace(cfg.CredentialsFile)
	cfg.SMTPHost = strings.TrimSpace(cfg.SMTPHost)
	cfg.SMTPUsername = strings.TrimSpace(cfg.SMTPUsername)
	cfg.SMTPCredential = strings.TrimSpace(cfg.SMTPCredential)
	cfg.DraftsMailbox = strings.TrimSpace(cfg.DraftsMailbox)
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

func decodeEmailInboxSyncState(account store.ExternalAccount) (emailInboxSyncState, error) {
	var state emailInboxSyncState
	raw := strings.TrimSpace(account.ConfigJSON)
	if raw == "" || raw == "{}" {
		return state, nil
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return state, err
	}
	payload, ok := config[emailInboxSyncStateKey]
	if !ok || payload == nil {
		return state, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	state.Cursor = strings.TrimSpace(state.Cursor)
	return state, nil
}

func (a *App) updateEmailInboxSyncState(account *store.ExternalAccount, state emailInboxSyncState) error {
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
	state.Cursor = strings.TrimSpace(state.Cursor)
	config[emailInboxSyncStateKey] = map[string]any{
		"cursor":   state.Cursor,
		"has_more": state.HasMore,
		"enabled":  state.Enabled,
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
	return store.ExternalAccountTokenPath(configDir, account.Provider, account.AccountName)
}

func gmailCredentialsPathForAccount(cfg emailSyncAccountConfig) string {
	configDir := emailSyncConfigDir()
	if path := emailConfigPath(configDir, cfg.CredentialsPath, cfg.CredentialsFile); path != "" {
		return path
	}
	return filepath.Join(configDir, "gmail_credentials.json")
}

func (a *App) smtpPasswordForAccount(ctx context.Context, account store.ExternalAccount, cfg emailSyncAccountConfig) (string, error) {
	if strings.TrimSpace(cfg.SMTPCredential) == "" {
		password, _, err := a.store.ResolveExternalAccountPasswordForAccount(ctx, account)
		return password, err
	}
	tempAccount := account
	tempAccount.ConfigJSON = fmt.Sprintf(`{"credential_ref":%q}`, strings.TrimSpace(cfg.SMTPCredential))
	password, _, err := a.store.ResolveExternalAccountPasswordForAccount(ctx, tempAccount)
	return password, err
}

func (a *App) emailProviderForAccount(ctx context.Context, account store.ExternalAccount, cfg emailSyncAccountConfig) (email.EmailProvider, error) {
	if a != nil && a.newEmailProvider != nil {
		return a.newEmailProvider(ctx, account)
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
		client := email.NewIMAPClient(account.AccountName, cfg.Host, cfg.Port, cfg.Username, password, useTLS, cfg.StartTLS)
		smtpPassword, err := a.smtpPasswordForAccount(ctx, account, cfg)
		if err != nil && strings.TrimSpace(cfg.SMTPHost) != "" {
			return nil, err
		}
		client.ConfigureDraftTransport(email.SMTPConfig{
			Host:      firstNonEmpty(cfg.SMTPHost, cfg.Host),
			Port:      firstPositive(cfg.SMTPPort, cfg.Port),
			Username:  firstNonEmpty(cfg.SMTPUsername, cfg.Username),
			Password:  smtpPassword,
			TLS:       cfg.SMTPTLS,
			StartTLS:  cfg.SMTPStartTLS,
			From:      firstNonEmpty(cfg.FromAddress, cfg.SMTPUsername, cfg.Username),
			FromName:  cfg.FromName,
			DraftsBox: cfg.DraftsMailbox,
		})
		return client, nil
	case store.ExternalProviderExchange:
		exchangeConfig, err := decodeExchangeAccountConfig(account)
		if err != nil {
			return nil, err
		}
		return email.NewExchangeMailProvider(exchangeConfig)
	case store.ExternalProviderExchangeEWS:
		return a.exchangeEWSMailProviderForAccount(ctx, account)
	default:
		return nil, fmt.Errorf("email sync does not support provider %s", account.Provider)
	}
}

func (a *App) emailSyncProviderForAccount(ctx context.Context, account store.ExternalAccount, cfg emailSyncAccountConfig) (emailSyncProvider, error) {
	if a != nil && a.newEmailSyncProvider != nil {
		return a.newEmailSyncProvider(ctx, account)
	}
	return a.emailProviderForAccount(ctx, account, cfg)
}

func emailSyncMaxResults(cfg emailSyncAccountConfig) int64 {
	if cfg.SyncMaxResults > 0 {
		return cfg.SyncMaxResults
	}
	return emailSyncDefaultMaxResults
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
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
	out := make(map[string]struct{})
	if pager, ok := provider.(email.MessagePageProvider); ok {
		pageToken := ""
		pageSize := emailHistoryPageSize(cfg)
		for {
			page, err := pager.ListMessagesPage(ctx, email.DefaultSearchOptions().WithFolder("INBOX").WithMaxResults(pageSize), pageToken)
			if err != nil {
				break
			}
			collectEmailMessageIDs(out, page.IDs)
			if strings.TrimSpace(page.NextPageToken) == "" {
				if len(out) == 0 {
					break
				}
				return out, nil
			}
			pageToken = page.NextPageToken
		}
	}
	maxResults := emailSyncMaxResults(cfg)
	inboxOpts := email.DefaultSearchOptions().
		WithFolder("INBOX").
		WithMaxResults(maxResults)
	inboxIDs, err := listEmailMessagesWithFallback(ctx, provider, inboxOpts)
	if err != nil {
		return nil, err
	}
	collectEmailMessageIDs(out, inboxIDs)
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

func emailMessageLabelRefs(message *providerdata.EmailMessage) []string {
	if message == nil {
		return nil
	}
	seen := map[string]struct{}{}
	refs := make([]string, 0, len(message.Labels))
	for _, label := range message.Labels {
		clean := strings.TrimSpace(label)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, clean)
	}
	return refs
}

func emailMessageContainerRef(message *providerdata.EmailMessage, mappings []store.ExternalContainerMapping) *string {
	refs := emailMessageLabelRefs(message)
	if len(refs) == 0 {
		return nil
	}
	if len(mappings) == 0 {
		return &refs[0]
	}
	mappedRefs := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		ref := strings.TrimSpace(mapping.ContainerRef)
		if ref == "" {
			continue
		}
		mappedRefs[strings.ToLower(ref)] = struct{}{}
	}
	for _, ref := range refs {
		if _, ok := mappedRefs[strings.ToLower(ref)]; ok {
			return &ref
		}
	}
	ref := refs[0]
	return &ref
}

func emailMessageBody(message *providerdata.EmailMessage) string {
	if message == nil {
		return ""
	}
	if message.BodyText != nil {
		if body := strings.TrimSpace(*message.BodyText); body != "" {
			return body
		}
	}
	if snippet := strings.TrimSpace(message.Snippet); snippet != "" {
		return snippet
	}
	return ""
}

func emailArtifactMetaJSON(message *providerdata.EmailMessage, senderActor *store.Actor) (string, error) {
	payload := map[string]any{
		"thread_id":           strings.TrimSpace(message.ThreadID),
		"internet_message_id": strings.TrimSpace(message.InternetMessageID),
		"subject":             strings.TrimSpace(message.Subject),
		"sender":              strings.TrimSpace(message.Sender),
		"recipients":          append([]string(nil), message.Recipients...),
		"labels":              append([]string(nil), message.Labels...),
		"is_read":             message.IsRead,
	}
	if !message.Date.IsZero() {
		payload["date"] = message.Date.UTC().Format(time.RFC3339)
	}
	if snippet := strings.TrimSpace(message.Snippet); snippet != "" {
		payload["snippet"] = snippet
	}
	if body := emailMessageBody(message); body != "" {
		payload["body"] = body
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
	if account.Provider == store.ExternalProviderExchangeEWS {
		taskCount, err := a.syncExchangeEWSTaskAccount(ctx, account)
		if err != nil {
			return 0, err
		}
		messageCount += taskCount
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

func (a *App) persistEmailMessage(ctx context.Context, sink tabsync.Sink, account store.ExternalAccount, message *providerdata.EmailMessage, mappings []store.ExternalContainerMapping, followUp bool) (emailPersistedMessage, error) {
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
		ContainerRef:    emailMessageContainerRef(message, mappings),
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

func (a *App) reconcileEmailFollowUpBindings(account store.ExternalAccount, followUpIDs map[string]struct{}) error {
	bindings, err := a.store.ListBindingsByAccount(account.ID, account.Provider, emailBindingObjectType)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if binding.ItemID == nil {
			continue
		}
		if hasEmailMessageID(followUpIDs, binding.RemoteID) {
			continue
		}
		item, err := a.store.GetItem(*binding.ItemID)
		if err != nil {
			if errorsIsNoRows(err) {
				continue
			}
			return err
		}
		if item.State == store.ItemStateDone {
			continue
		}
		if err := a.store.UpdateItemState(item.ID, store.ItemStateDone); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) applyEmailInboxDeletions(account store.ExternalAccount, deletedIDs []string) error {
	if a == nil || a.store == nil || len(deletedIDs) == 0 {
		return nil
	}
	for _, messageID := range deletedIDs {
		clean := strings.TrimSpace(messageID)
		if clean == "" {
			continue
		}
		binding, err := a.store.GetBindingByRemote(account.ID, account.Provider, emailBindingObjectType, clean)
		if err != nil {
			if errorsIsNoRows(err) {
				continue
			}
			return err
		}
		if binding.ItemID == nil {
			continue
		}
		item, err := a.store.GetItem(*binding.ItemID)
		if err != nil {
			if errorsIsNoRows(err) {
				continue
			}
			return err
		}
		if item.State == store.ItemStateDone {
			continue
		}
		if err := a.store.UpdateItemState(item.ID, store.ItemStateDone); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) syncEmailIncrementalInbox(ctx context.Context, account *store.ExternalAccount, provider emailSyncProvider, seen map[string]struct{}) ([]string, error) {
	incremental, ok := provider.(email.FolderIncrementalSyncProvider)
	if !ok || account == nil || account.Provider != store.ExternalProviderExchangeEWS {
		return nil, nil
	}
	state, err := decodeEmailInboxSyncState(*account)
	if err != nil {
		return nil, err
	}
	result, err := incremental.SyncFolderChanges(ctx, "INBOX", state.Cursor, 200)
	if err != nil {
		return nil, err
	}
	state.Cursor = strings.TrimSpace(result.Cursor)
	state.HasMore = result.More
	state.Enabled = true
	if err := a.updateEmailInboxSyncState(account, state); err != nil {
		return nil, err
	}
	if err := a.applyEmailInboxDeletions(*account, result.DeletedIDs); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(result.IDs))
	for _, id := range result.IDs {
		clean := strings.TrimSpace(id)
		if clean == "" {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		ids = append(ids, clean)
	}
	return ids, nil
}

func (a *App) syncEmailAccountWithProvider(ctx context.Context, account store.ExternalAccount, provider emailSyncProvider) (emailSyncResult, error) {
	cfg, err := decodeEmailSyncAccountConfig(account)
	if err != nil {
		return emailSyncResult{}, err
	}
	mappings, err := a.store.ListContainerMappings(account.Provider)
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
	if err := a.reconcileEmailFollowUpBindings(account, followUpIDs); err != nil {
		return emailSyncResult{}, err
	}
	messageIDs := make(map[string]struct{})
	queuedRefreshIDs := []string(nil)
	if a != nil && a.emailRefreshes != nil {
		queuedRefreshIDs = a.emailRefreshes.list(account.ID)
		collectEmailMessageIDs(messageIDs, queuedRefreshIDs)
	}
	inboxRefreshIDs, err := a.emailInboxStateRefreshIDs(account, followUpIDs)
	if err != nil {
		return emailSyncResult{}, err
	}
	collectEmailMessageIDs(messageIDs, inboxRefreshIDs)
	containerRepairIDs, err := a.emailContainerRepairIDs(account)
	if err != nil {
		return emailSyncResult{}, err
	}
	collectEmailMessageIDs(messageIDs, containerRepairIDs)

	accountState := account
	incrementalIDs, err := a.syncEmailIncrementalInbox(ctx, &accountState, provider, messageIDs)
	if err != nil {
		return emailSyncResult{}, err
	}
	account = accountState
	collectEmailMessageIDs(messageIDs, incrementalIDs)
	if account.Provider != store.ExternalProviderExchangeEWS {
		since := emailSyncSince(time.Now().UTC(), latestRemoteUpdatedAt, cfg)
		recentIDs, err := provider.ListMessages(ctx, email.DefaultSearchOptions().WithSince(since).WithMaxResults(emailSyncMaxResults(cfg)))
		if err != nil {
			return emailSyncResult{}, err
		}
		collectEmailMessageIDs(messageIDs, recentIDs)
	}
	if len(messageIDs) == 0 {
		sink := tabsync.NewStoreSink(a.store)
		backfillResult, _, err := a.syncEmailHistoryBackfill(ctx, &account, provider, cfg, sink, mappings, followUpIDs, messageIDs)
		if err == nil && a != nil && a.emailRefreshes != nil && len(queuedRefreshIDs) > 0 {
			a.emailRefreshes.remove(account.ID, queuedRefreshIDs...)
		}
		return backfillResult, err
	}

	messages, err := provider.GetMessages(ctx, sortedEmailMessageIDs(messageIDs), "full")
	if err != nil {
		return emailSyncResult{}, err
	}
	sink := tabsync.NewStoreSink(a.store)
	result, _, err := a.persistEmailMessagesBatch(ctx, sink, account, mappings, followUpIDs, messages)
	if err != nil {
		return emailSyncResult{}, err
	}
	backfillResult, _, err := a.syncEmailHistoryBackfill(ctx, &account, provider, cfg, sink, mappings, followUpIDs, messageIDs)
	if err != nil {
		return emailSyncResult{}, err
	}
	result.MessageCount += backfillResult.MessageCount
	result.ItemCount += backfillResult.ItemCount
	if a != nil && a.emailRefreshes != nil && len(queuedRefreshIDs) > 0 {
		a.emailRefreshes.remove(account.ID, queuedRefreshIDs...)
	}
	return result, nil
}

func hasEmailMessageID(values map[string]struct{}, id string) bool {
	_, ok := values[strings.TrimSpace(id)]
	return ok
}

func emailContainerRefIsInbox(ref *string) bool {
	if ref == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(*ref)) {
	case "inbox", "posteingang":
		return true
	default:
		return false
	}
}

func (a *App) emailInboxStateRefreshIDs(account store.ExternalAccount, followUpIDs map[string]struct{}) ([]string, error) {
	if a == nil || a.store == nil || len(followUpIDs) == 0 {
		return nil, nil
	}
	bindings, err := a.store.ListBindingsByAccount(account.ID, account.Provider, emailBindingObjectType)
	if err != nil {
		return nil, err
	}
	byRemoteID := make(map[string]store.ExternalBinding, len(bindings))
	for _, binding := range bindings {
		byRemoteID[strings.TrimSpace(binding.RemoteID)] = binding
	}
	refresh := make(map[string]struct{})
	for id := range followUpIDs {
		binding, ok := byRemoteID[id]
		if !ok {
			continue
		}
		if !emailContainerRefIsInbox(binding.ContainerRef) {
			refresh[id] = struct{}{}
		}
	}
	for _, binding := range bindings {
		if !emailContainerRefIsInbox(binding.ContainerRef) {
			continue
		}
		if !hasEmailMessageID(followUpIDs, binding.RemoteID) {
			refresh[strings.TrimSpace(binding.RemoteID)] = struct{}{}
		}
	}
	return sortedEmailMessageIDs(refresh), nil
}

func (a *App) emailContainerRepairIDs(account store.ExternalAccount) ([]string, error) {
	if a == nil || a.store == nil {
		return nil, nil
	}
	bindings, err := a.store.ListBindingsMissingContainerRef(account.ID, account.Provider, emailBindingObjectType, emailContainerRepairBatch)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		if id := strings.TrimSpace(binding.RemoteID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
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
