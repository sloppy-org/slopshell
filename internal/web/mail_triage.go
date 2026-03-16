package web

import (
	"context"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/mailtriage"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type mailTriagePreviewRequest struct {
	MessageIDs     []string `json:"message_ids"`
	Folder         string   `json:"folder"`
	MaxResults     int64    `json:"max_results"`
	Phase          string   `json:"phase"`
	Apply          bool     `json:"apply"`
	IncludeBody    bool     `json:"include_body"`
	PrimaryBaseURL string   `json:"primary_base_url"`
	PrimaryModel   string   `json:"primary_model"`
	AuditBaseURL   string   `json:"audit_base_url"`
	AuditModel     string   `json:"audit_model"`
}

type mailTriageApplyRequest struct {
	Decisions []mailTriageApplyDecision `json:"decisions"`
}

type mailTriageApplyDecision struct {
	MessageID    string            `json:"message_id"`
	Action       mailtriage.Action `json:"action"`
	ArchiveLabel string            `json:"archive_label,omitempty"`
}

type mailServerFilterUpsertRequest struct {
	Filter email.ServerFilter `json:"filter"`
}

type mailTriageApplyResult struct {
	MessageID string            `json:"message_id"`
	Action    mailtriage.Action `json:"action"`
	Status    string            `json:"status"`
	Error     string            `json:"error,omitempty"`
}

func (a *App) handleMailTriagePreview(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	var req mailTriagePreviewRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	classifier, err := a.mailTriageClassifier(req.PrimaryBaseURL, req.PrimaryModel)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var audit mailtriage.Classifier
	if strings.TrimSpace(req.AuditBaseURL) != "" || strings.TrimSpace(req.AuditModel) != "" {
		audit, err = a.mailTriageClassifier(req.AuditBaseURL, req.AuditModel)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	messages, err := a.loadMailTriageMessages(r.Context(), account, provider, req)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	engine := mailtriage.Engine{
		Primary: classifier,
		Audit:   audit,
		Policy:  mailtriage.DefaultPolicy(parseMailTriagePhase(req.Phase)),
	}
	results, err := engine.Evaluate(r.Context(), messages)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	applied := []mailTriageApplyResult(nil)
	if req.Apply && parseMailTriagePhase(req.Phase) == mailtriage.PhaseAutoApply {
		applied = a.applyMailTriageEvaluations(r.Context(), account, provider, results)
	}
	capabilities := email.ServerFilterCapabilities{Provider: account.Provider}
	if filterProvider, ok := provider.(email.ServerFilterProvider); ok {
		capabilities = filterProvider.ServerFilterCapabilities()
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"account":                    account,
		"results":                    results,
		"applied":                    applied,
		"server_filter_capabilities": capabilities,
	})
}

func (a *App) handleMailTriageApply(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	var req mailTriageApplyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	results := a.applyMailTriageDecisions(r.Context(), account, provider, req.Decisions)
	writeAPIData(w, http.StatusOK, map[string]any{"results": results})
}

func (a *App) handleMailServerFiltersList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "server filters are not supported for this account")
		return
	}
	filters, err := filterProvider.ListServerFilters(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"account":      account,
		"filters":      filters,
		"capabilities": filterProvider.ServerFilterCapabilities(),
	})
}

func (a *App) handleMailServerFilterUpsert(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	_, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "server filters are not supported for this account")
		return
	}
	var req mailServerFilterUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	filterID := strings.TrimSpace(chi.URLParam(r, "filter_id"))
	if filterID != "" {
		req.Filter.ID = filterID
	}
	filter, err := filterProvider.UpsertServerFilter(r.Context(), req.Filter)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{"filter": filter})
}

func (a *App) handleMailServerFilterDelete(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	_, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()
	filterProvider, ok := provider.(email.ServerFilterProvider)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "server filters are not supported for this account")
		return
	}
	filterID := strings.TrimSpace(chi.URLParam(r, "filter_id"))
	if filterID == "" {
		writeAPIError(w, http.StatusBadRequest, "filter_id is required")
		return
	}
	if err := filterProvider.DeleteServerFilter(r.Context(), filterID); err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeNoContent(w)
}

func (a *App) emailProviderForRoute(ctx context.Context, r *http.Request) (store.ExternalAccount, email.EmailProvider, error) {
	accountID, err := parseURLInt64Param(r, "account_id")
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	account, err := a.store.GetExternalAccount(accountID)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	cfg, err := decodeEmailSyncAccountConfig(account)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	provider, err := a.emailProviderForAccount(ctx, account, cfg)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, provider, nil
}

func (a *App) mailTriageClassifier(baseURL, model string) (mailtriage.Classifier, error) {
	resolvedBaseURL := strings.TrimSpace(baseURL)
	if resolvedBaseURL == "" {
		resolvedBaseURL = strings.TrimSpace(a.intentLLMURL)
	}
	if resolvedBaseURL == "" {
		return nil, errBadRequest("mail triage classifier base URL is required")
	}
	resolvedModel := strings.TrimSpace(model)
	if resolvedModel == "" {
		resolvedModel = a.localIntentLLMModel()
	}
	return mailtriage.OpenAIClassifier{
		BaseURL: resolvedBaseURL,
		Model:   resolvedModel,
	}, nil
}

func (a *App) loadMailTriageMessages(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, req mailTriagePreviewRequest) ([]mailtriage.Message, error) {
	ids := compactStringList(req.MessageIDs)
	if len(ids) == 0 {
		opts := email.DefaultSearchOptions().WithMaxResults(req.MaxResults)
		folder := strings.TrimSpace(req.Folder)
		if folder == "" {
			folder = "inbox"
		}
		opts = opts.WithFolder(folder)
		var err error
		ids, err = provider.ListMessages(ctx, opts)
		if err != nil {
			return nil, err
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	messages, err := provider.GetMessages(ctx, ids, "")
	if err != nil {
		return nil, err
	}
	cfg, _ := decodeEmailSyncAccountConfig(account)
	accountAddress := firstNonEmpty(cfg.FromAddress, cfg.Username, account.AccountName)
	examples, err := a.mailTriageExamples(account.ID)
	if err != nil {
		return nil, err
	}
	out := make([]mailtriage.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		out = append(out, toMailTriageMessage(account, accountAddress, req.IncludeBody, message, examples))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ReceivedAt.After(out[j].ReceivedAt)
	})
	return out, nil
}

func (a *App) mailTriageExamples(accountID int64) ([]mailtriage.Example, error) {
	reviews, err := a.store.ListMailTriageReviews(accountID, 12)
	if err != nil {
		return nil, err
	}
	out := make([]mailtriage.Example, 0, len(reviews))
	for _, review := range reviews {
		out = append(out, mailtriage.Example{
			Sender:  strings.TrimSpace(review.Sender),
			Subject: strings.TrimSpace(review.Subject),
			Folder:  strings.TrimSpace(review.Folder),
			Action:  strings.TrimSpace(review.Action),
		})
	}
	return out, nil
}

func toMailTriageMessage(account store.ExternalAccount, accountAddress string, includeBody bool, message *providerdata.EmailMessage, examples []mailtriage.Example) mailtriage.Message {
	body := ""
	if includeBody {
		if message.BodyText != nil {
			body = strings.TrimSpace(*message.BodyText)
		}
	}
	return mailtriage.Message{
		ID:             strings.TrimSpace(message.ID),
		Provider:       account.Provider,
		AccountLabel:   account.Label,
		AccountAddress: strings.TrimSpace(accountAddress),
		ThreadID:       strings.TrimSpace(message.ThreadID),
		Subject:        strings.TrimSpace(message.Subject),
		Sender:         strings.TrimSpace(message.Sender),
		Recipients:     compactStringList(message.Recipients),
		Labels:         compactStringList(message.Labels),
		Snippet:        strings.TrimSpace(message.Snippet),
		Body:           body,
		HasAttachments: len(message.Attachments) > 0,
		IsRead:         message.IsRead,
		IsFlagged:      message.IsFlagged,
		ReceivedAt:     message.Date,
		Examples:       slices.Clone(examples),
	}
}

func parseMailTriagePhase(raw string) mailtriage.Phase {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(mailtriage.PhaseShadow):
		return mailtriage.PhaseShadow
	case string(mailtriage.PhaseAutoApply):
		return mailtriage.PhaseAutoApply
	default:
		return mailtriage.PhaseManualReview
	}
}

func (a *App) applyMailTriageEvaluations(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, evals []mailtriage.Evaluation) []mailTriageApplyResult {
	decisions := make([]mailTriageApplyDecision, 0, len(evals))
	for _, eval := range evals {
		if eval.Disposition != mailtriage.DispositionAutoApply {
			continue
		}
		decisions = append(decisions, mailTriageApplyDecision{
			MessageID:    eval.Message.ID,
			Action:       eval.Primary.Action,
			ArchiveLabel: eval.Primary.ArchiveLabel,
		})
	}
	return a.applyMailTriageDecisions(ctx, account, provider, decisions)
}

func (a *App) applyMailTriageDecisions(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, decisions []mailTriageApplyDecision) []mailTriageApplyResult {
	type key struct {
		action mailtriage.Action
		label  string
	}
	groups := map[key][]string{}
	order := make([]key, 0, len(decisions))
	for _, decision := range decisions {
		messageID := strings.TrimSpace(decision.MessageID)
		if messageID == "" {
			continue
		}
		groupKey := key{action: decision.Action, label: strings.TrimSpace(decision.ArchiveLabel)}
		if _, ok := groups[groupKey]; !ok {
			order = append(order, groupKey)
		}
		groups[groupKey] = append(groups[groupKey], messageID)
	}
	results := make([]mailTriageApplyResult, 0, len(decisions))
	for _, groupKey := range order {
		ids := groups[groupKey]
		err := applyMailTriageAction(ctx, account, provider, groupKey.action, groupKey.label, ids)
		status := "ok"
		errText := ""
		if err != nil {
			status = "error"
			errText = err.Error()
		}
		for _, id := range ids {
			results = append(results, mailTriageApplyResult{
				MessageID: id,
				Action:    groupKey.action,
				Status:    status,
				Error:     errText,
			})
		}
	}
	return results
}

func applyMailTriageAction(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, action mailtriage.Action, archiveLabel string, messageIDs []string) error {
	switch action {
	case mailtriage.ActionInbox:
		_, err := provider.MoveToInbox(ctx, messageIDs)
		return err
	case mailtriage.ActionTrash:
		_, err := provider.Trash(ctx, messageIDs)
		return err
	case mailtriage.ActionCC:
		if folderProvider, ok := provider.(email.NamedFolderProvider); ok {
			_, err := folderProvider.MoveToFolder(ctx, messageIDs, "CC")
			return err
		}
		if labelProvider, ok := provider.(email.NamedLabelProvider); ok {
			_, err := labelProvider.ApplyNamedLabel(ctx, messageIDs, "CC", true)
			return err
		}
		return errBadRequest("cc triage is not supported for this provider")
	case mailtriage.ActionArchive:
		if label := strings.TrimSpace(archiveLabel); label != "" {
			if folderProvider, ok := provider.(email.NamedFolderProvider); ok {
				target := label
				if account.Provider == store.ExternalProviderExchangeEWS {
					target = "Archive/" + label
				}
				_, err := folderProvider.MoveToFolder(ctx, messageIDs, target)
				return err
			}
			if labelProvider, ok := provider.(email.NamedLabelProvider); ok {
				_, err := labelProvider.ApplyNamedLabel(ctx, messageIDs, label, true)
				return err
			}
		}
		_, err := provider.Archive(ctx, messageIDs)
		return err
	default:
		return errBadRequest("unsupported triage action")
	}
}

func compactStringList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		key := strings.ToLower(clean)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
	}
	return out
}
