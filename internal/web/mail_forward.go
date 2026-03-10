package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/store"
)

type mailDraftForwardRequest struct {
	ItemID int64 `json:"item_id"`
}

func (a *App) handleMailDraftForward(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req mailDraftForwardRequest
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
	account, cfg, seed, err := a.forwardDraftSeed(r.Context(), item)
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
	remote, err := draftProvider.CreateDraft(r.Context(), seed)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	payload, err := a.persistMailDraft(project, account, remote, seed, "")
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusCreated, map[string]any{"draft": payload})
}

func (a *App) forwardDraftSeed(ctx context.Context, item store.Item) (store.ExternalAccount, emailSyncAccountConfig, email.DraftInput, error) {
	seed, err := a.resolveMailItemSeedContext(item)
	if err != nil {
		return store.ExternalAccount{}, emailSyncAccountConfig{}, email.DraftInput{}, err
	}
	input := email.DraftInput{
		From: firstNonEmpty(seed.config.FromAddress, seed.config.SMTPUsername, seed.config.Username),
	}
	if seed.artifact != nil {
		meta := parseSidebarArtifactMeta(stringFromPointer(seed.artifact.MetaJSON))
		if subject := strings.TrimSpace(stringAny(meta["subject"])); subject != "" {
			input.Subject = mailForwardSubject(subject)
		}
		if threadID := strings.TrimSpace(stringAny(meta["thread_id"])); threadID != "" {
			input.ThreadID = threadID
		}
		input.Body = forwardBodyFromArtifactMeta(meta)
	}
	remoteMessageID := seed.remoteMessageID
	if strings.TrimSpace(input.Subject) == "" || strings.TrimSpace(input.Body) == "" {
		provider, provErr := a.emailProviderForAccount(ctx, seed.account, seed.config)
		if provErr == nil {
			defer provider.Close()
			if message, getErr := provider.GetMessage(ctx, remoteMessageID, "full"); getErr == nil && message != nil {
				if strings.TrimSpace(input.Subject) == "" {
					input.Subject = mailForwardSubject(message.Subject)
				}
				if strings.TrimSpace(input.Body) == "" {
					messageBody := strings.TrimSpace(stringFromPointer(message.BodyText))
					if messageBody == "" {
						messageBody = strings.TrimSpace(message.Snippet)
					}
					input.Body = formatForwardQuote(message.Sender, message.Subject, message.Date.Format("Mon, 2 Jan 2006 15:04:05"), messageBody)
				}
				if strings.TrimSpace(input.ThreadID) == "" {
					input.ThreadID = strings.TrimSpace(message.ThreadID)
				}
			}
		}
	}
	return seed.account, seed.config, input, nil
}

func mailForwardSubject(subject string) string {
	clean := strings.TrimSpace(subject)
	if clean == "" {
		return "Fwd:"
	}
	lower := strings.ToLower(clean)
	if strings.HasPrefix(lower, "fwd:") || strings.HasPrefix(lower, "fw:") {
		return clean
	}
	return "Fwd: " + clean
}

func forwardBodyFromArtifactMeta(meta map[string]any) string {
	messages, _ := meta["messages"].([]any)
	if len(messages) > 0 {
		last, _ := messages[len(messages)-1].(map[string]any)
		sender := strings.TrimSpace(stringAny(last["sender"]))
		date := strings.TrimSpace(stringAny(last["date"]))
		subject := strings.TrimSpace(stringAny(meta["subject"]))
		body := strings.TrimSpace(stringAny(last["body"]))
		if body == "" {
			body = strings.TrimSpace(stringAny(last["snippet"]))
		}
		return formatForwardQuote(sender, subject, date, body)
	}
	sender := strings.TrimSpace(stringAny(meta["sender"]))
	date := strings.TrimSpace(stringAny(meta["date"]))
	subject := strings.TrimSpace(stringAny(meta["subject"]))
	body := strings.TrimSpace(stringAny(meta["body"]))
	if body == "" {
		body = strings.TrimSpace(stringAny(meta["snippet"]))
	}
	return formatForwardQuote(sender, subject, date, body)
}

func formatForwardQuote(sender, subject, date, body string) string {
	var parts []string
	parts = append(parts, "---------- Forwarded message ----------")
	if sender != "" {
		parts = append(parts, "From: "+sender)
	}
	if date != "" {
		parts = append(parts, "Date: "+date)
	}
	if subject != "" {
		parts = append(parts, "Subject: "+subject)
	}
	parts = append(parts, "")
	parts = append(parts, strings.TrimSpace(body))
	return strings.Join(parts, "\n")
}

func (a *App) appendSentMessageToThread(ctx mailDraftContext) {
	threadID := strings.TrimSpace(ctx.meta.ThreadID)
	if threadID == "" {
		return
	}
	threadArtifact := a.findThreadArtifactByBinding(ctx.account.ID, ctx.account.Provider, threadID)
	if threadArtifact == nil {
		return
	}
	meta := parseSidebarArtifactMeta(stringFromPointer(threadArtifact.MetaJSON))
	messages, _ := meta["messages"].([]any)
	sentMessage := map[string]any{
		"sender":     firstNonEmpty(ctx.config.FromAddress, ctx.config.SMTPUsername, ctx.config.Username),
		"recipients": ctx.meta.To,
		"subject":    ctx.meta.Subject,
		"body":       ctx.body,
		"date":       time.Now().Format("2006-01-02 15:04"),
		"sent":       "true",
	}
	messages = append(messages, sentMessage)
	meta["messages"] = messages
	if mc, ok := meta["message_count"].(float64); ok {
		meta["message_count"] = mc + 1
	} else {
		meta["message_count"] = float64(len(messages))
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return
	}
	metaStr := string(metaJSON)
	_ = a.store.UpdateArtifact(threadArtifact.ID, store.ArtifactUpdate{
		MetaJSON: &metaStr,
	})
}

func (a *App) findThreadArtifactByBinding(accountID int64, provider, threadID string) *store.Artifact {
	if strings.TrimSpace(threadID) == "" {
		return nil
	}
	binding, err := a.store.GetBindingByRemote(accountID, provider, emailThreadBindingObjectType, strings.TrimSpace(threadID))
	if err != nil {
		return nil
	}
	if binding.ArtifactID == nil || *binding.ArtifactID <= 0 {
		return nil
	}
	artifact, err := a.store.GetArtifact(*binding.ArtifactID)
	if err != nil {
		return nil
	}
	return &artifact
}
