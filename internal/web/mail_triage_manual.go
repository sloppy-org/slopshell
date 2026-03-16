package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/krystophny/tabura/internal/email"
	"github.com/krystophny/tabura/internal/mailtriage"
	"github.com/krystophny/tabura/internal/providerdata"
	"github.com/krystophny/tabura/internal/store"
)

type mailTriageManualReviewRequest struct {
	MessageID string `json:"message_id"`
	Folder    string `json:"folder"`
	Action    string `json:"action"`
}

func normalizeMailTriageManualAction(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "inbox":
		return "inbox"
	case "cc":
		return "cc"
	case "archive":
		return "archive"
	case "trash":
		return "trash"
	default:
		return ""
	}
}

func (a *App) handleMailTriageManualReviewsList(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	accountID, err := parseURLInt64Param(r, "account_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	account, err := a.store.GetExternalAccount(accountID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	limit := 50
	folder := strings.TrimSpace(r.URL.Query().Get("folder"))
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, convErr := strconv.Atoi(raw)
		if convErr != nil || value <= 0 {
			writeAPIError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = value
	}
	reviews, err := a.store.ListMailTriageReviews(account.ID, limit)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	reviewedMessageIDs, err := a.store.ListMailTriageReviewedMessageIDs(account.ID, folder, 5000)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	allReviews, err := a.store.ListMailTriageReviews(account.ID, 1000)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	input := make([]mailtriage.ReviewedExample, 0, len(allReviews))
	for _, review := range allReviews {
		input = append(input, mailtriage.ReviewedExample{
			Sender:  strings.TrimSpace(review.Sender),
			Subject: strings.TrimSpace(review.Subject),
			Folder:  strings.TrimSpace(review.Folder),
			Action:  strings.TrimSpace(review.Action),
		})
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"account":   account,
		"reviews":   reviews,
		"count":     len(reviews),
		"folder":    folder,
		"reviewed_message_ids": reviewedMessageIDs,
		"distilled": mailtriage.DistillReviewedExamples(input),
	})
}

func (a *App) handleMailTriageManualReviewCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()

	var req mailTriageManualReviewRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	action := normalizeMailTriageManualAction(req.Action)
	if action == "" {
		writeAPIError(w, http.StatusBadRequest, "action must be inbox, cc, archive, or trash")
		return
	}
	messageID := strings.TrimSpace(req.MessageID)
	if messageID == "" {
		writeAPIError(w, http.StatusBadRequest, "message_id is required")
		return
	}
	message, err := provider.GetMessage(r.Context(), messageID, "full")
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}

	appliedAction := ""
	succeeded := 1
	switch action {
	case "inbox":
		appliedAction = "inbox"
		if !mailTriageFolderImpliesInbox(strings.TrimSpace(req.Folder)) {
			succeeded, err = provider.MoveToInbox(r.Context(), []string{messageID})
		}
	case "cc":
		appliedAction = "cc"
		err = applyMailTriageAction(r.Context(), account, provider, mailtriage.ActionCC, "", []string{messageID})
		if err == nil {
			succeeded = 1
		}
	case "archive":
		appliedAction = "archive"
		succeeded, err = provider.Archive(r.Context(), []string{messageID})
	case "trash":
		appliedAction = "trash"
		succeeded, err = provider.Trash(r.Context(), []string{messageID})
	}
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}

	review, err := a.store.CreateMailTriageReview(store.MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: messageID,
		Folder:    strings.TrimSpace(req.Folder),
		Subject:   strings.TrimSpace(message.Subject),
		Sender:    strings.TrimSpace(message.Sender),
		Action:    action,
	})
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}

	writeAPIData(w, http.StatusOK, map[string]any{
		"account":        account,
		"review":         review,
		"applied_action": appliedAction,
		"succeeded":      succeeded,
	})
}

func (a *App) handleMailTriageManualReviewUndo(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	account, provider, err := a.emailProviderForRoute(r.Context(), r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer provider.Close()

	reviewID, err := parseURLInt64Param(r, "review_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	review, err := a.store.GetMailTriageReview(reviewID)
	if err != nil {
		writeDomainStoreError(w, err)
		return
	}
	if review.AccountID != account.ID {
		writeAPIError(w, http.StatusBadRequest, "review does not belong to this account")
		return
	}
	message, succeeded, err := undoMailTriageReviewWithStore(r.Context(), a.store, account, provider, review)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := a.store.DeleteMailTriageReview(review.ID); err != nil {
		writeDomainStoreError(w, err)
		return
	}
	writeAPIData(w, http.StatusOK, map[string]any{
		"account":   account,
		"review":    review,
		"succeeded": succeeded,
		"message":   message,
	})
}

func mailTriageFolderImpliesInbox(folder string) bool {
	switch strings.ToLower(strings.TrimSpace(folder)) {
	case "inbox", "posteingang":
		return true
	default:
		return false
	}
}

func undoMailTriageReview(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, review store.MailTriageReview) (*providerdata.EmailMessage, int, error) {
	return undoMailTriageReviewWithStore(ctx, nil, account, provider, review)
}

func undoMailTriageReviewWithStore(ctx context.Context, st *store.Store, account store.ExternalAccount, provider email.EmailProvider, review store.MailTriageReview) (*providerdata.EmailMessage, int, error) {
	action := normalizeMailTriageManualAction(review.Action)
	if action == "" {
		return nil, 0, errors.New("invalid review action")
	}
	if message, err := provider.GetMessage(ctx, strings.TrimSpace(review.MessageID), "full"); err == nil && message != nil && mailTriageFolderMatches(strings.TrimSpace(review.Folder), message.Labels) {
		return message, 0, nil
	}
	if st != nil {
		if resolvedID, containerRef, ok := resolveMailTriageUndoRemoteIDFromStore(st, account, review, action); ok {
			if mailTriageFolderMatches(strings.TrimSpace(review.Folder), []string{containerRef}) {
				if message, err := provider.GetMessage(ctx, resolvedID, "full"); err == nil && message != nil {
					return message, 0, nil
				}
				if restored, _, err := resolveMailTriageUndoMessage(ctx, provider, review, strings.TrimSpace(review.Folder)); err == nil && restored != nil {
					return restored, 0, nil
				}
				return nil, 0, errors.New("unable to resolve restored message after undo")
			}
			if err := restoreMailTriageReviewToOriginalFolder(ctx, provider, review, resolvedID); err != nil {
				return nil, 0, err
			}
			if restored, _, err := resolveMailTriageUndoMessage(ctx, provider, review, strings.TrimSpace(review.Folder)); err == nil && restored != nil {
				return restored, 1, nil
			}
			return nil, 1, nil
		}
	}
	if action == "inbox" && mailTriageFolderImpliesInbox(review.Folder) {
		message, err := provider.GetMessage(ctx, strings.TrimSpace(review.MessageID), "full")
		if err != nil {
			return nil, 0, nil
		}
		return message, 0, nil
	}

	currentFolder := mailTriageCurrentFolderForAction(account.Provider, action)
	currentMessage, currentID, err := resolveMailTriageUndoMessage(ctx, provider, review, currentFolder)
	if err != nil {
		return nil, 0, err
	}
	if currentID == "" {
		return nil, 0, errors.New("unable to resolve message for undo")
	}
	if err := restoreMailTriageReviewToOriginalFolder(ctx, provider, review, currentID); err != nil {
		return nil, 0, err
	}
	restored, _, err := resolveMailTriageUndoMessage(ctx, provider, review, strings.TrimSpace(review.Folder))
	if err == nil && restored != nil {
		return restored, 1, nil
	}
	return currentMessage, 1, nil
}

func resolveMailTriageUndoRemoteIDFromStore(st *store.Store, account store.ExternalAccount, review store.MailTriageReview, action string) (string, string, bool) {
	if st == nil {
		return "", "", false
	}
	binding, err := st.GetBindingByRemote(account.ID, account.Provider, "email", strings.TrimSpace(review.MessageID))
	if err != nil {
		return "", "", false
	}
	candidates := []store.ExternalBinding{binding}
	if binding.ArtifactID != nil {
		if bindings, err := st.GetBindingsByArtifact(*binding.ArtifactID); err == nil {
			for _, candidate := range bindings {
				if candidate.AccountID == account.ID && candidate.Provider == account.Provider && candidate.ObjectType == "email" {
					candidates = append(candidates, candidate)
				}
			}
		}
	}
	originalFolder := strings.TrimSpace(review.Folder)
	for _, candidate := range candidates {
		if candidate.ContainerRef != nil && mailTriageFolderMatches(originalFolder, []string{*candidate.ContainerRef}) {
			return strings.TrimSpace(candidate.RemoteID), strings.TrimSpace(*candidate.ContainerRef), true
		}
	}
	currentFolder := mailTriageCurrentFolderForAction(account.Provider, action)
	for _, candidate := range candidates {
		if candidate.ContainerRef != nil && mailTriageFolderMatches(currentFolder, []string{*candidate.ContainerRef}) {
			return strings.TrimSpace(candidate.RemoteID), strings.TrimSpace(*candidate.ContainerRef), true
		}
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.RemoteID) != "" {
			return strings.TrimSpace(candidate.RemoteID), strings.TrimSpace(valueOrEmpty(candidate.ContainerRef)), true
		}
	}
	return "", "", false
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func restoreMailTriageReviewToOriginalFolder(ctx context.Context, provider email.EmailProvider, review store.MailTriageReview, messageID string) error {
	if mailTriageFolderImpliesInbox(review.Folder) {
		_, err := provider.MoveToInbox(ctx, []string{messageID})
		return err
	}
	if folderProvider, ok := provider.(email.NamedFolderProvider); ok {
		_, err := folderProvider.MoveToFolder(ctx, []string{messageID}, strings.TrimSpace(review.Folder))
		return err
	}
	return errors.New("undo is not supported for this provider and folder")
}

func resolveMailTriageUndoMessage(ctx context.Context, provider email.EmailProvider, review store.MailTriageReview, expectedFolder string) (*providerdata.EmailMessage, string, error) {
	if message, err := provider.GetMessage(ctx, strings.TrimSpace(review.MessageID), "full"); err == nil && message != nil {
		if mailTriageMessageMatchesReview(review, message, expectedFolder) {
			return message, strings.TrimSpace(message.ID), nil
		}
	}
	subject := strings.TrimSpace(review.Subject)
	if subject == "" {
		return nil, "", errors.New("missing review subject for undo lookup")
	}
	ids, err := provider.ListMessages(ctx, email.DefaultSearchOptions().WithFolder(expectedFolder).WithSubject(subject).WithMaxResults(25))
	if err != nil {
		return nil, "", err
	}
	if len(ids) == 0 {
		return nil, "", errors.New("undo lookup found no matching messages")
	}
	messages, err := provider.GetMessages(ctx, ids, "full")
	if err != nil {
		return nil, "", err
	}
	var subjectMatches []*providerdata.EmailMessage
	for _, message := range messages {
		if message == nil || !mailTriageSubjectMatches(review.Subject, message.Subject) {
			continue
		}
		if mailTriageFolderMatches(expectedFolder, message.Labels) && mailTriageSenderMatches(review.Sender, message.Sender) {
			return message, strings.TrimSpace(message.ID), nil
		}
		subjectMatches = append(subjectMatches, message)
	}
	if len(subjectMatches) == 1 {
		return subjectMatches[0], strings.TrimSpace(subjectMatches[0].ID), nil
	}
	return nil, "", errors.New("undo lookup could not uniquely resolve message")
}

func mailTriageCurrentFolderForAction(provider, action string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case string(store.ExternalProviderExchangeEWS):
		switch action {
		case "inbox":
			return "Posteingang"
		case "cc":
			return "CC"
		case "archive":
			return "Archive"
		case "trash":
			return "Gelöschte Elemente"
		}
	default:
		switch action {
		case "inbox":
			return "inbox"
		case "cc":
			return "CC"
		case "archive":
			return "archive"
		case "trash":
			return "trash"
		}
	}
	return ""
}

func mailTriageMessageMatchesReview(review store.MailTriageReview, message *providerdata.EmailMessage, expectedFolder string) bool {
	if message == nil {
		return false
	}
	return mailTriageSubjectMatches(review.Subject, message.Subject) &&
		mailTriageSenderMatches(review.Sender, message.Sender) &&
		mailTriageFolderMatches(expectedFolder, message.Labels)
}

func mailTriageSubjectMatches(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func mailTriageSenderMatches(left, right string) bool {
	cleanLeft := strings.ToLower(strings.TrimSpace(left))
	cleanRight := strings.ToLower(strings.TrimSpace(right))
	if cleanLeft == cleanRight {
		return true
	}
	return cleanLeft != "" && cleanRight != "" && (strings.Contains(cleanLeft, cleanRight) || strings.Contains(cleanRight, cleanLeft))
}

func mailTriageFolderMatches(expected string, labels []string) bool {
	cleanExpected := strings.ToLower(strings.TrimSpace(expected))
	if cleanExpected == "" {
		return true
	}
	for _, label := range labels {
		if strings.EqualFold(strings.TrimSpace(label), cleanExpected) || strings.EqualFold(strings.TrimSpace(label), expected) {
			return true
		}
	}
	return false
}
