package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

type mailTriageManualReviewRequest struct {
	MessageID string `json:"message_id"`
	Folder    string `json:"folder"`
	Action    string `json:"action"`
}

func normalizeMailTriageManualAction(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "keep":
		return "keep"
	case "rescue":
		return "rescue"
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
	writeAPIData(w, http.StatusOK, map[string]any{
		"account": account,
		"reviews": reviews,
		"count":   len(reviews),
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
		writeAPIError(w, http.StatusBadRequest, "action must be keep, rescue, archive, or trash")
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
	case "rescue":
		appliedAction = "move_to_inbox"
		succeeded, err = provider.MoveToInbox(r.Context(), []string{messageID})
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
