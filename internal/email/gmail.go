package email

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/krystophny/tabura/internal/providerdata"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	people "google.golang.org/api/people/v1"
)

var gmailScopes = []string{
	gmail.GmailModifyScope,
	people.ContactsReadonlyScope,
}

func configDir() string {
	return defaultTaburaConfigDir()
}

func credentialsFile() string {
	return filepath.Join(configDir(), "gmail_credentials.json")
}

func tokenFile() string {
	return filepath.Join(configDir(), "gmail_token.json")
}

// GmailClient provides access to Gmail API with rate limiting.
type GmailClient struct {
	rateLimiter     *RateLimiter
	config          *oauth2.Config
	token           *oauth2.Token
	credentialsPath string
	tokenPath       string
	mu              sync.Mutex
}

// Compile-time check that GmailClient implements EmailProvider.
var _ EmailProvider = (*GmailClient)(nil)
var _ MessageActionProvider = (*GmailClient)(nil)

// NewGmail creates a new Gmail client.
func NewGmail() (*GmailClient, error) {
	return NewGmailWithFiles("", "")
}

// NewGmailWithFiles creates a Gmail client with explicit credential and token paths.
func NewGmailWithFiles(credentialsPath, tokenPath string) (*GmailClient, error) {
	if strings.TrimSpace(credentialsPath) == "" {
		credentialsPath = credentialsFile()
	}
	if strings.TrimSpace(tokenPath) == "" {
		tokenPath = tokenFile()
	}

	credBytes, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("configure Gmail credentials at %s first: %w", credentialsPath, err)
	}

	config, err := google.ConfigFromJSON(credBytes, gmailScopes...)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	client := &GmailClient{
		rateLimiter:     NewRateLimiter(15000),
		config:          config,
		credentialsPath: credentialsPath,
		tokenPath:       tokenPath,
	}

	// Try to load existing token
	if tokenBytes, err := os.ReadFile(tokenPath); err == nil {
		var token oauth2.Token
		if json.Unmarshal(tokenBytes, &token) == nil {
			client.token = &token
		}
	}

	return client, nil
}

// GetAuthURL returns the URL for OAuth authorization.
func (c *GmailClient) GetAuthURL() string {
	return c.config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
}

// ExchangeCode exchanges an authorization code for a token.
func (c *GmailClient) ExchangeCode(ctx context.Context, code string) error {
	token, err := c.config.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("failed to exchange code: %w", err)
	}

	c.token = token

	// Save token
	if err := os.MkdirAll(filepath.Dir(c.tokenPath), 0o755); err != nil {
		return err
	}
	tokenBytes, _ := json.MarshalIndent(token, "", "  ")
	return os.WriteFile(c.tokenPath, tokenBytes, 0o600)
}

func (c *GmailClient) getTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.token == nil {
		return nil, fmt.Errorf("gmail is not authenticated; token file %s is missing", c.tokenPath)
	}

	tokenSource := c.config.TokenSource(ctx, c.token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	if newToken.AccessToken != c.token.AccessToken {
		c.token = newToken
		tokenBytes, _ := json.MarshalIndent(newToken, "", "  ")
		_ = os.MkdirAll(filepath.Dir(c.tokenPath), 0o755)
		_ = os.WriteFile(c.tokenPath, tokenBytes, 0o600)
	}

	return tokenSource, nil
}

func (c *GmailClient) getService(ctx context.Context) (*gmail.Service, error) {
	tokenSource, err := c.getTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	return gmail.NewService(ctx, option.WithTokenSource(tokenSource))
}

// ListLabels returns all Gmail labels.
func (c *GmailClient) ListLabels(ctx context.Context) ([]providerdata.Label, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}

	c.rateLimiter.Acquire("labels.list")

	result, err := service.Users.Labels.List("me").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("failed to list labels: %w", err)
	}

	labels := make([]providerdata.Label, 0, len(result.Labels))
	for _, lbl := range result.Labels {
		labels = append(labels, providerdata.Label{
			ID:             lbl.Id,
			Name:           lbl.Name,
			Type:           lbl.Type,
			MessagesTotal:  int(lbl.MessagesTotal),
			MessagesUnread: int(lbl.MessagesUnread),
		})
	}

	return labels, nil
}

// ListMessages returns message IDs matching the search options.
func (c *GmailClient) ListMessages(ctx context.Context, opts SearchOptions) ([]string, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}

	maxResults := opts.MaxResults
	if maxResults == 0 {
		maxResults = 100
	}

	query := buildGmailQuery(opts)

	var messageIDs []string
	pageToken := ""

	for int64(len(messageIDs)) < maxResults {
		c.rateLimiter.Acquire("messages.list")

		call := service.Users.Messages.List("me").
			Context(ctx).
			MaxResults(minInt64(500, maxResults-int64(len(messageIDs)))).
			IncludeSpamTrash(opts.IncludeSpamTrash)

		if query != "" {
			call = call.Q(query)
		}
		if len(opts.LabelIDs) > 0 {
			call = call.LabelIds(opts.LabelIDs...)
		}
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		result, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("failed to list messages: %w", err)
		}

		for _, msg := range result.Messages {
			if int64(len(messageIDs)) >= maxResults {
				break
			}
			messageIDs = append(messageIDs, msg.Id)
		}

		pageToken = result.NextPageToken
		if pageToken == "" {
			break
		}
	}

	return messageIDs, nil
}

// buildGmailQuery converts SearchOptions to a Gmail query string.
func buildGmailQuery(opts SearchOptions) string {
	var parts []string

	if opts.Folder != "" {
		parts = append(parts, fmt.Sprintf("label:%s", opts.Folder))
	}

	if opts.Text != "" {
		parts = append(parts, opts.Text)
	}

	if opts.Subject != "" {
		parts = append(parts, fmt.Sprintf("subject:%s", opts.Subject))
	}

	if opts.From != "" {
		parts = append(parts, fmt.Sprintf("from:%s", opts.From))
	}

	if opts.To != "" {
		parts = append(parts, fmt.Sprintf("to:%s", opts.To))
	}

	if !opts.After.IsZero() {
		parts = append(parts, fmt.Sprintf("after:%s", opts.After.Format("2006/01/02")))
	}

	if !opts.Before.IsZero() {
		parts = append(parts, fmt.Sprintf("before:%s", opts.Before.Format("2006/01/02")))
	}

	if !opts.Since.IsZero() {
		parts = append(parts, fmt.Sprintf("after:%s", opts.Since.AddDate(0, 0, -1).Format("2006/01/02")))
	}

	if !opts.Until.IsZero() {
		parts = append(parts, fmt.Sprintf("before:%s", opts.Until.AddDate(0, 0, 1).Format("2006/01/02")))
	}

	if opts.HasAttachment != nil && *opts.HasAttachment {
		parts = append(parts, "has:attachment")
	}

	if opts.IsRead != nil {
		if *opts.IsRead {
			parts = append(parts, "-is:unread")
		} else {
			parts = append(parts, "is:unread")
		}
	}

	if opts.IsFlagged != nil {
		if *opts.IsFlagged {
			parts = append(parts, "is:starred")
		} else {
			parts = append(parts, "-is:starred")
		}
	}

	if opts.SizeGreater > 0 {
		parts = append(parts, fmt.Sprintf("larger:%d", opts.SizeGreater))
	}

	if opts.SizeLess > 0 {
		parts = append(parts, fmt.Sprintf("smaller:%d", opts.SizeLess))
	}

	return strings.Join(parts, " ")
}

// GetMessage retrieves a single message.
func (c *GmailClient) GetMessage(ctx context.Context, messageID, format string) (*providerdata.EmailMessage, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return nil, err
	}

	if format == "" {
		format = "full"
	}

	c.rateLimiter.Acquire("messages.get")

	msg, err := service.Users.Messages.Get("me", messageID).
		Context(ctx).
		Format(format).
		Do()
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	return parseGmailMessage(msg), nil
}

// GetMessages retrieves multiple messages concurrently.
func (c *GmailClient) GetMessages(ctx context.Context, messageIDs []string, format string) ([]*providerdata.EmailMessage, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}

	if format == "" {
		format = "metadata"
	}

	results := make([]*providerdata.EmailMessage, len(messageIDs))
	errors := make([]error, len(messageIDs))

	var wg sync.WaitGroup
	sem := make(chan struct{}, 50)

	for i, id := range messageIDs {
		wg.Add(1)
		go func(idx int, msgID string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			msg, err := c.GetMessage(ctx, msgID, format)
			results[idx] = msg
			errors[idx] = err
		}(i, id)
	}

	wg.Wait()

	var validResults []*providerdata.EmailMessage
	for i, msg := range results {
		if errors[i] == nil && msg != nil {
			validResults = append(validResults, msg)
		}
	}

	return validResults, nil
}

// ModifyLabels modifies labels on messages.
func (c *GmailClient) ModifyLabels(ctx context.Context, messageIDs, addLabels, removeLabels []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	service, err := c.getService(ctx)
	if err != nil {
		return 0, err
	}

	succeeded := 0
	batchSize := 50

	for i := 0; i < len(messageIDs); i += batchSize {
		end := i + batchSize
		if end > len(messageIDs) {
			end = len(messageIDs)
		}
		batch := messageIDs[i:end]

		c.rateLimiter.Acquire("messages.batchModify")

		req := &gmail.BatchModifyMessagesRequest{
			Ids:            batch,
			AddLabelIds:    addLabels,
			RemoveLabelIds: removeLabels,
		}

		err := service.Users.Messages.BatchModify("me", req).Context(ctx).Do()
		if err == nil {
			succeeded += len(batch)
		}
	}

	return succeeded, nil
}

// MarkRead marks messages as read.
func (c *GmailClient) MarkRead(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, nil, []string{"UNREAD"})
}

// MarkUnread marks messages as unread.
func (c *GmailClient) MarkUnread(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, []string{"UNREAD"}, nil)
}

// Archive removes messages from inbox.
func (c *GmailClient) Archive(ctx context.Context, messageIDs []string) (int, error) {
	return c.ModifyLabels(ctx, messageIDs, nil, []string{"INBOX"})
}

// Trash moves messages to trash.
func (c *GmailClient) Trash(ctx context.Context, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	service, err := c.getService(ctx)
	if err != nil {
		return 0, err
	}

	succeeded := 0
	for _, id := range messageIDs {
		c.rateLimiter.Acquire("messages.trash")
		_, err := service.Users.Messages.Trash("me", id).Context(ctx).Do()
		if err == nil {
			succeeded++
		}
	}

	return succeeded, nil
}

// Delete permanently deletes messages.
func (c *GmailClient) Delete(ctx context.Context, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	service, err := c.getService(ctx)
	if err != nil {
		return 0, err
	}

	succeeded := 0
	for _, id := range messageIDs {
		c.rateLimiter.Acquire("messages.delete")
		err := service.Users.Messages.Delete("me", id).Context(ctx).Do()
		if err == nil {
			succeeded++
		}
	}

	return succeeded, nil
}

// Defer applies Gmail-native deferred handling using the system SNOOZED label.
func (c *GmailClient) Defer(ctx context.Context, messageID string, untilAt time.Time) (MessageActionResult, error) {
	service, err := c.getService(ctx)
	if err != nil {
		return MessageActionResult{}, err
	}
	c.rateLimiter.Acquire("messages.modify")
	req := &gmail.ModifyMessageRequest{
		AddLabelIds:    []string{"SNOOZED"},
		RemoveLabelIds: []string{"INBOX"},
	}
	if _, err := service.Users.Messages.Modify("me", messageID, req).Context(ctx).Do(); err != nil {
		return MessageActionResult{}, err
	}
	return MessageActionResult{
		Provider:              c.ProviderName(),
		Action:                "defer",
		MessageID:             messageID,
		Status:                "ok",
		EffectiveProviderMode: "native",
		DeferredUntilAt:       untilAt.UTC().Format(time.RFC3339),
	}, nil
}

func (c *GmailClient) SupportsNativeDefer() bool {
	return true
}

// ProviderName returns the name of the provider.
func (c *GmailClient) ProviderName() string {
	return "gmail"
}

// Close releases any resources held by the client.
func (c *GmailClient) Close() error {
	return nil
}

func parseGmailMessage(msg *gmail.Message) *providerdata.EmailMessage {
	headers := make(map[string]string)
	if msg.Payload != nil {
		for _, h := range msg.Payload.Headers {
			headers[h.Name] = h.Value
		}
	}

	isRead := true
	for _, lbl := range msg.LabelIds {
		if lbl == "UNREAD" {
			isRead = false
			break
		}
	}

	email := &providerdata.EmailMessage{
		ID:       msg.Id,
		ThreadID: msg.ThreadId,
		Subject:  headers["Subject"],
		Sender:   headers["From"],
		Snippet:  msg.Snippet,
		Labels:   msg.LabelIds,
		IsRead:   isRead,
	}

	if email.Subject == "" {
		email.Subject = "(No subject)"
	}

	if to := headers["To"]; to != "" {
		email.Recipients = strings.Split(to, ",")
		for i := range email.Recipients {
			email.Recipients[i] = strings.TrimSpace(email.Recipients[i])
		}
	}

	if dateStr := headers["Date"]; dateStr != "" {
		if t, err := mail.ParseDate(dateStr); err == nil {
			email.Date = t
		}
	}
	if email.Date.IsZero() {
		email.Date = time.Now()
	}

	if msg.Payload != nil {
		email.BodyText = extractGmailBody(msg.Payload, "text/plain")
		email.BodyHTML = extractGmailBody(msg.Payload, "text/html")
	}

	return email
}

func extractGmailBody(payload *gmail.MessagePart, mimeType string) *string {
	if payload.MimeType == mimeType && payload.Body != nil && payload.Body.Data != "" {
		data, err := base64.URLEncoding.DecodeString(payload.Body.Data)
		if err == nil {
			s := string(data)
			return &s
		}
	}

	for _, part := range payload.Parts {
		if body := extractGmailBody(part, mimeType); body != nil {
			return body
		}
	}

	return nil
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
