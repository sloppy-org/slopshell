package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/krystophny/tabura/internal/mailtriage"
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

func mailTriageFolderImpliesInbox(folder string) bool {
	switch strings.ToLower(strings.TrimSpace(folder)) {
	case "inbox", "posteingang":
		return true
	default:
		return false
	}
}
