package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/store"
)

const artifactKindEmailDraft store.ArtifactKind = "email_draft"

type mailDraftArtifactMeta struct {
	AccountID        int64    `json:"account_id"`
	AccountLabel     string   `json:"account_label"`
	Provider         string   `json:"provider"`
	RemoteDraftID    string   `json:"remote_draft_id,omitempty"`
	ReplyToMessageID string   `json:"reply_to_message_id,omitempty"`
	ThreadID         string   `json:"thread_id,omitempty"`
	Status           string   `json:"status"`
	To               []string `json:"to,omitempty"`
	Cc               []string `json:"cc,omitempty"`
	Bcc              []string `json:"bcc,omitempty"`
	Subject          string   `json:"subject,omitempty"`
}

type mailDraftCreateRequest struct {
	AccountID int64    `json:"account_id"`
	To        []string `json:"to"`
	Cc        []string `json:"cc"`
	Bcc       []string `json:"bcc"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
}

type mailDraftReplyRequest struct {
	ItemID int64 `json:"item_id"`
}

type mailDraftUpdateRequest struct {
	To      []string `json:"to"`
	Cc      []string `json:"cc"`
	Bcc     []string `json:"bcc"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

type mailDraftPayload struct {
	ArtifactID       int64                 `json:"artifact_id"`
	ItemID           int64                 `json:"item_id"`
	AccountID        int64                 `json:"account_id"`
	AccountLabel     string                `json:"account_label"`
	Provider         string                `json:"provider"`
	RemoteDraftID    string                `json:"remote_draft_id,omitempty"`
	ReplyToMessageID string                `json:"reply_to_message_id,omitempty"`
	ThreadID         string                `json:"thread_id,omitempty"`
	Status           string                `json:"status"`
	To               []string              `json:"to"`
	Cc               []string              `json:"cc"`
	Bcc              []string              `json:"bcc"`
	Subject          string                `json:"subject"`
	Body             string                `json:"body"`
	Title            string                `json:"title"`
	RefPath          string                `json:"ref_path"`
	Artifact         store.Artifact        `json:"artifact"`
	Item             *store.Item           `json:"item,omitempty"`
	Meta             mailDraftArtifactMeta `json:"meta"`
}

type mailDraftContext struct {
	project  store.Project
	artifact store.Artifact
	item     *store.Item
	account  store.ExternalAccount
	config   emailSyncAccountConfig
	meta     mailDraftArtifactMeta
	body     string
	absPath  string
}

func (a *App) handleMailDraftCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req mailDraftCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	project, err := a.activeMailDraftProject()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	account, cfg, err := a.resolveMailDraftAccount(r.Context(), req.AccountID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider, draftProvider, err := a.mailDraftProviderForAccount(r.Context(), account, cfg)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	input := email.DraftInput{
		From:    firstNonEmpty(cfg.FromAddress, cfg.SMTPUsername, cfg.Username),
		To:      req.To,
		Cc:      req.Cc,
		Bcc:     req.Bcc,
		Subject: req.Subject,
		Body:    req.Body,
	}
	remote, err := draftProvider.CreateDraft(r.Context(), input)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	payload, err := a.persistMailDraft(project, account, remote, input, "")
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusCreated, map[string]any{"draft": payload})
}

func (a *App) handleMailDraftReply(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req mailDraftReplyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	item, err := a.store.GetItem(req.ItemID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	project, err := a.projectForItem(item)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	account, cfg, remoteMessageID, seed, err := a.replyDraftSeed(r.Context(), item)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	provider, draftProvider, err := a.mailDraftProviderForAccount(r.Context(), account, cfg)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	remote, err := draftProvider.CreateReplyDraft(r.Context(), remoteMessageID, seed)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	payload, err := a.persistMailDraft(project, account, remote, seed, remoteMessageID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusCreated, map[string]any{"draft": payload})
}

func (a *App) handleMailDraftGet(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, err := a.loadMailDraftContext(artifactID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{"draft": ctx.payload()})
}

func (a *App) handleMailDraftUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, err := a.loadMailDraftContext(artifactID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	var req mailDraftUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	provider, draftProvider, err := a.mailDraftProviderForAccount(r.Context(), ctx.account, ctx.config)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	input := email.DraftInput{
		From:      firstNonEmpty(ctx.config.FromAddress, ctx.config.SMTPUsername, ctx.config.Username),
		To:        req.To,
		Cc:        req.Cc,
		Bcc:       req.Bcc,
		Subject:   req.Subject,
		Body:      req.Body,
		ThreadID:  ctx.meta.ThreadID,
		ReplyToID: ctx.meta.ReplyToMessageID,
	}
	remote, err := draftProvider.UpdateDraft(r.Context(), ctx.meta.RemoteDraftID, input)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	payload, err := a.updateMailDraft(ctx, remote, input, ctx.meta.Status)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{"draft": payload})
}

func (a *App) handleMailDraftSend(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, err := a.loadMailDraftContext(artifactID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	provider, draftProvider, err := a.mailDraftProviderForAccount(r.Context(), ctx.account, ctx.config)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	input, err := email.NormalizeDraftSendInput(email.DraftInput{
		From:      firstNonEmpty(ctx.config.FromAddress, ctx.config.SMTPUsername, ctx.config.Username),
		To:        ctx.meta.To,
		Cc:        ctx.meta.Cc,
		Bcc:       ctx.meta.Bcc,
		Subject:   ctx.meta.Subject,
		Body:      ctx.body,
		ThreadID:  ctx.meta.ThreadID,
		ReplyToID: ctx.meta.ReplyToMessageID,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := draftProvider.SendDraft(r.Context(), ctx.meta.RemoteDraftID, input); err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	payload, err := a.updateMailDraft(ctx, email.Draft{ThreadID: ctx.meta.ThreadID}, input, "sent")
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	a.appendSentMessageToThread(ctx)
	if ctx.item != nil {
		_ = a.store.TriageItemDone(ctx.item.ID)
	}
	writeAPIData(w, http.StatusOK, map[string]any{"draft": payload})
}

func (a *App) activeMailDraftProject() (store.Project, error) {
	projectID, err := a.store.ActiveProjectID()
	if err != nil {
		return store.Project{}, err
	}
	if strings.TrimSpace(projectID) == "" {
		return store.Project{}, errors.New("mail draft requires an active project")
	}
	return a.store.GetProject(projectID)
}

func (a *App) resolveMailDraftAccount(ctx context.Context, accountID int64) (store.ExternalAccount, emailSyncAccountConfig, error) {
	if accountID > 0 {
		account, err := a.store.GetExternalAccount(accountID)
		if err != nil {
			return store.ExternalAccount{}, emailSyncAccountConfig{}, err
		}
		cfg, err := decodeEmailSyncAccountConfig(account)
		return account, cfg, err
	}
	sphere, err := a.store.ActiveSphere()
	if err != nil {
		return store.ExternalAccount{}, emailSyncAccountConfig{}, err
	}
	accounts, err := a.store.ListExternalAccounts(sphere)
	if err != nil {
		return store.ExternalAccount{}, emailSyncAccountConfig{}, err
	}
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		switch account.Provider {
		case store.ExternalProviderGmail, store.ExternalProviderIMAP, store.ExternalProviderExchange:
			cfg, err := decodeEmailSyncAccountConfig(account)
			return account, cfg, err
		}
	}
	return store.ExternalAccount{}, emailSyncAccountConfig{}, errors.New("no enabled mail account is configured for the active sphere")
}

func (a *App) mailDraftProviderForAccount(ctx context.Context, account store.ExternalAccount, cfg emailSyncAccountConfig) (email.EmailProvider, email.DraftProvider, error) {
	provider, err := a.emailProviderForAccount(ctx, account, cfg)
	if err != nil {
		return nil, nil, err
	}
	draftProvider, ok := provider.(email.DraftProvider)
	if !ok {
		provider.Close()
		return nil, nil, fmt.Errorf("mail provider %s does not support drafts", account.Provider)
	}
	return provider, draftProvider, nil
}

func (a *App) persistMailDraft(project store.Project, account store.ExternalAccount, remote email.Draft, input email.DraftInput, replyToMessageID string) (mailDraftPayload, error) {
	normalized, err := email.NormalizeDraftInput(input)
	if err != nil {
		return mailDraftPayload{}, err
	}
	relPath, absPath := newMailDraftPath(project.RootPath)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return mailDraftPayload{}, err
	}
	if err := os.WriteFile(absPath, []byte(strings.TrimSpace(normalized.Body)+"\n"), 0644); err != nil {
		return mailDraftPayload{}, err
	}
	meta := mailDraftArtifactMeta{
		AccountID:        account.ID,
		AccountLabel:     account.Label,
		Provider:         account.Provider,
		RemoteDraftID:    strings.TrimSpace(remote.ID),
		ReplyToMessageID: strings.TrimSpace(replyToMessageID),
		ThreadID:         firstNonEmpty(remote.ThreadID, normalized.ThreadID),
		Status:           "draft",
		To:               normalized.To,
		Cc:               normalized.Cc,
		Bcc:              normalized.Bcc,
		Subject:          strings.TrimSpace(normalized.Subject),
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return mailDraftPayload{}, err
	}
	title := mailDraftTitle(meta.Subject)
	artifact, err := a.store.CreateArtifact(artifactKindEmailDraft, mailDraftStringPointer(relPath), nil, mailDraftStringPointer(title), mailDraftStringPointer(string(metaJSON)))
	if err != nil {
		return mailDraftPayload{}, err
	}
	projectID := project.ID
	source := account.Provider
	sourceRef := strings.TrimSpace(remote.ID)
	item, err := a.store.CreateItem(title, store.ItemOptions{
		ProjectID:  &projectID,
		ArtifactID: &artifact.ID,
		Source:     &source,
		SourceRef:  &sourceRef,
	})
	if err != nil {
		return mailDraftPayload{}, err
	}
	ctx, err := a.loadMailDraftContext(artifact.ID)
	if err != nil {
		return mailDraftPayload{}, err
	}
	ctx.item = &item
	return ctx.payload(), nil
}

func (a *App) updateMailDraft(ctx mailDraftContext, remote email.Draft, input email.DraftInput, status string) (mailDraftPayload, error) {
	normalized, err := email.NormalizeDraftInput(input)
	if err != nil {
		return mailDraftPayload{}, err
	}
	if err := os.WriteFile(ctx.absPath, []byte(strings.TrimSpace(normalized.Body)+"\n"), 0644); err != nil {
		return mailDraftPayload{}, err
	}
	nextMeta := ctx.meta
	nextMeta.RemoteDraftID = strings.TrimSpace(remote.ID)
	if nextMeta.RemoteDraftID == "" && status == "sent" {
		nextMeta.RemoteDraftID = ""
	}
	nextMeta.ThreadID = firstNonEmpty(remote.ThreadID, nextMeta.ThreadID, normalized.ThreadID)
	nextMeta.Status = firstNonEmpty(status, nextMeta.Status)
	nextMeta.To = normalized.To
	nextMeta.Cc = normalized.Cc
	nextMeta.Bcc = normalized.Bcc
	nextMeta.Subject = strings.TrimSpace(normalized.Subject)
	metaJSON, err := json.Marshal(nextMeta)
	if err != nil {
		return mailDraftPayload{}, err
	}
	title := mailDraftTitle(nextMeta.Subject)
	if err := a.store.UpdateArtifact(ctx.artifact.ID, store.ArtifactUpdate{
		Title:    &title,
		MetaJSON: mailDraftStringPointer(string(metaJSON)),
	}); err != nil {
		return mailDraftPayload{}, err
	}
	if ctx.item != nil {
		update := store.ItemUpdate{Title: &title}
		if source := ctx.item.Source; source != nil && strings.TrimSpace(nextMeta.RemoteDraftID) != "" {
			update.Source = source
			update.SourceRef = mailDraftStringPointer(nextMeta.RemoteDraftID)
		}
		if err := a.store.UpdateItem(ctx.item.ID, update); err != nil {
			return mailDraftPayload{}, err
		}
	}
	return a.loadMailDraftPayload(ctx.artifact.ID)
}

func (a *App) loadMailDraftContext(artifactID int64) (mailDraftContext, error) {
	artifact, err := a.store.GetArtifact(artifactID)
	if err != nil {
		return mailDraftContext{}, err
	}
	if strings.TrimSpace(string(artifact.Kind)) != string(artifactKindEmailDraft) {
		return mailDraftContext{}, sql.ErrNoRows
	}
	meta, err := decodeMailDraftMeta(artifact.MetaJSON)
	if err != nil {
		return mailDraftContext{}, err
	}
	account, err := a.store.GetExternalAccount(meta.AccountID)
	if err != nil {
		return mailDraftContext{}, err
	}
	cfg, err := decodeEmailSyncAccountConfig(account)
	if err != nil {
		return mailDraftContext{}, err
	}
	items, err := a.store.ListArtifactItems(artifact.ID)
	if err != nil {
		return mailDraftContext{}, err
	}
	var item *store.Item
	for i := range items {
		if items[i].ArtifactID != nil && *items[i].ArtifactID == artifact.ID {
			item = &items[i]
			break
		}
	}
	project, err := a.projectForArtifact(item, artifact)
	if err != nil {
		return mailDraftContext{}, err
	}
	absPath := filepath.Join(project.RootPath, strings.TrimSpace(stringFromPointer(artifact.RefPath)))
	bodyBytes, err := os.ReadFile(absPath)
	if err != nil {
		return mailDraftContext{}, err
	}
	return mailDraftContext{
		project:  project,
		artifact: artifact,
		item:     item,
		account:  account,
		config:   cfg,
		meta:     meta,
		body:     strings.TrimSpace(string(bodyBytes)),
		absPath:  absPath,
	}, nil
}

func (a *App) loadMailDraftPayload(artifactID int64) (mailDraftPayload, error) {
	ctx, err := a.loadMailDraftContext(artifactID)
	if err != nil {
		return mailDraftPayload{}, err
	}
	return ctx.payload(), nil
}

func (ctx mailDraftContext) payload() mailDraftPayload {
	payload := mailDraftPayload{
		ArtifactID:       ctx.artifact.ID,
		AccountID:        ctx.meta.AccountID,
		AccountLabel:     ctx.meta.AccountLabel,
		Provider:         ctx.meta.Provider,
		RemoteDraftID:    ctx.meta.RemoteDraftID,
		ReplyToMessageID: ctx.meta.ReplyToMessageID,
		ThreadID:         ctx.meta.ThreadID,
		Status:           ctx.meta.Status,
		To:               append([]string(nil), ctx.meta.To...),
		Cc:               append([]string(nil), ctx.meta.Cc...),
		Bcc:              append([]string(nil), ctx.meta.Bcc...),
		Subject:          ctx.meta.Subject,
		Body:             ctx.body,
		Title:            mailDraftTitle(ctx.meta.Subject),
		RefPath:          stringFromPointer(ctx.artifact.RefPath),
		Artifact:         ctx.artifact,
		Meta:             ctx.meta,
	}
	if ctx.item != nil {
		itemCopy := *ctx.item
		payload.ItemID = itemCopy.ID
		payload.Item = &itemCopy
	}
	return payload
}

func (a *App) projectForItem(item store.Item) (store.Project, error) {
	if item.ProjectID != nil && strings.TrimSpace(*item.ProjectID) != "" {
		return a.store.GetProject(strings.TrimSpace(*item.ProjectID))
	}
	return a.activeMailDraftProject()
}

func (a *App) projectForArtifact(item *store.Item, artifact store.Artifact) (store.Project, error) {
	if item != nil {
		return a.projectForItem(*item)
	}
	return a.activeMailDraftProject()
}

type mailItemSeedContext struct {
	account         store.ExternalAccount
	config          emailSyncAccountConfig
	artifact        *store.Artifact
	remoteMessageID string
}

func (a *App) resolveMailItemSeedContext(item store.Item) (mailItemSeedContext, error) {
	var artifact *store.Artifact
	if item.ArtifactID != nil && *item.ArtifactID > 0 {
		loadedArtifact, artifactErr := a.store.GetArtifact(*item.ArtifactID)
		if artifactErr == nil {
			artifact = &loadedArtifact
		}
	}
	bindings, err := a.store.GetBindingsByItem(item.ID)
	if err != nil {
		return mailItemSeedContext{}, err
	}
	if artifact != nil {
		artifactBindings, artifactErr := a.store.GetBindingsByArtifact(artifact.ID)
		if artifactErr == nil {
			bindings = append(bindings, artifactBindings...)
		}
	}
	binding := mailReplyBindingForItem(bindings, item)
	if binding == nil {
		return mailItemSeedContext{}, errors.New("item is not linked to a remote mail message or thread")
	}
	account, err := a.store.GetExternalAccount(binding.AccountID)
	if err != nil {
		return mailItemSeedContext{}, err
	}
	cfg, err := decodeEmailSyncAccountConfig(account)
	if err != nil {
		return mailItemSeedContext{}, err
	}
	return mailItemSeedContext{
		account:         account,
		config:          cfg,
		artifact:        artifact,
		remoteMessageID: strings.TrimSpace(binding.RemoteID),
	}, nil
}

func (a *App) replyDraftSeed(ctx context.Context, item store.Item) (store.ExternalAccount, emailSyncAccountConfig, string, email.DraftInput, error) {
	seed, err := a.resolveMailItemSeedContext(item)
	if err != nil {
		return store.ExternalAccount{}, emailSyncAccountConfig{}, "", email.DraftInput{}, err
	}
	input := email.DraftInput{
		From: firstNonEmpty(seed.config.FromAddress, seed.config.SMTPUsername, seed.config.Username),
	}
	remoteMessageID := seed.remoteMessageID
	if seed.artifact != nil {
		meta := parseSidebarArtifactMeta(stringFromPointer(seed.artifact.MetaJSON))
		if sender, messageID := replySeedFromArtifactMeta(meta); sender != "" || messageID != "" {
			if sender != "" {
				input.To = []string{sender}
			}
			if messageID != "" {
				remoteMessageID = messageID
			}
		}
		if subject := strings.TrimSpace(stringAny(meta["subject"])); subject != "" {
			input.Subject = mailReplySubject(subject)
		}
		if threadID := strings.TrimSpace(stringAny(meta["thread_id"])); threadID != "" {
			input.ThreadID = threadID
		}
	}
	if strings.TrimSpace(remoteMessageID) == "" {
		return store.ExternalAccount{}, emailSyncAccountConfig{}, "", email.DraftInput{}, errors.New("item is not linked to a remote mail message")
	}
	if len(input.To) == 0 || strings.TrimSpace(input.Subject) == "" {
		provider, err := a.emailProviderForAccount(ctx, seed.account, seed.config)
		if err == nil {
			defer provider.Close()
			if message, getErr := provider.GetMessage(ctx, remoteMessageID, "full"); getErr == nil && message != nil {
				if len(input.To) == 0 && strings.TrimSpace(message.Sender) != "" {
					input.To = []string{strings.TrimSpace(message.Sender)}
				}
				if strings.TrimSpace(input.Subject) == "" {
					input.Subject = mailReplySubject(message.Subject)
				}
				if strings.TrimSpace(input.ThreadID) == "" {
					input.ThreadID = strings.TrimSpace(message.ThreadID)
				}
			}
		}
	}
	return seed.account, seed.config, remoteMessageID, input, nil
}

func decodeMailDraftMeta(raw *string) (mailDraftArtifactMeta, error) {
	var meta mailDraftArtifactMeta
	if strings.TrimSpace(stringFromPointer(raw)) == "" {
		return meta, errors.New("mail draft metadata is missing")
	}
	if err := json.Unmarshal([]byte(stringFromPointer(raw)), &meta); err != nil {
		return mailDraftArtifactMeta{}, err
	}
	if strings.TrimSpace(meta.Status) == "" {
		meta.Status = "draft"
	}
	return meta, nil
}

func newMailDraftPath(root string) (string, string) {
	name := fmt.Sprintf("draft-%d.md", time.Now().UnixNano())
	rel := filepath.ToSlash(filepath.Join(".tabura", "artifacts", "mail", name))
	return rel, filepath.Join(root, rel)
}

func mailDraftTitle(subject string) string {
	if clean := strings.TrimSpace(subject); clean != "" {
		return clean
	}
	return "Draft email"
}

func mailDraftStringPointer(value string) *string {
	clean := strings.TrimSpace(value)
	return &clean
}

func mailReplySubject(subject string) string {
	clean := strings.TrimSpace(subject)
	if clean == "" {
		return "Re:"
	}
	if strings.HasPrefix(strings.ToLower(clean), "re:") {
		return clean
	}
	return "Re: " + clean
}

func stringAny(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func parseSidebarArtifactMeta(raw string) map[string]any {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(clean), &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func replySeedFromArtifactMeta(meta map[string]any) (string, string) {
	if sender := strings.TrimSpace(stringAny(meta["sender"])); sender != "" {
		return sender, ""
	}
	messages, _ := meta["messages"].([]any)
	for i := len(messages) - 1; i >= 0; i-- {
		entry, _ := messages[i].(map[string]any)
		sender := strings.TrimSpace(stringAny(entry["sender"]))
		messageID := strings.TrimSpace(stringAny(entry["id"]))
		if sender != "" || messageID != "" {
			return sender, messageID
		}
	}
	return "", ""
}
