package cerebras

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL         = "https://api.cerebras.ai"
	DefaultModel           = "gpt-oss-120b"
	DefaultReasoningEffort = "medium"
	defaultMaxTokens       = 512
	defaultBackoff         = 5 * time.Minute
)

var (
	ErrQuotaExhausted = errors.New("cerebras quota exhausted")
	ErrUnavailable    = errors.New("cerebras unavailable")
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionRequest struct {
	Messages    []Message
	MaxTokens   int
	Temperature float64
}

type CompletionResponse struct {
	Text       string
	Confidence string
	TokensUsed int
	Latency    time.Duration
}

type Client struct {
	BaseURL          string
	APIKey           string
	Model            string
	ReasoningEffort  string
	HTTPClient       *http.Client
	UnavailableAfter time.Duration

	now func() time.Time

	mu               sync.Mutex
	quotaExhausted   bool
	exhaustedAt      time.Time
	unavailableUntil time.Time
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type structuredCompletion struct {
	Text       string `json:"text"`
	Confidence string `json:"confidence"`
}

func NewClient(baseURL, apiKey, model, reasoningEffort string) *Client {
	return &Client{
		BaseURL:          strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:           strings.TrimSpace(apiKey),
		Model:            strings.TrimSpace(model),
		ReasoningEffort:  normalizeReasoningEffort(reasoningEffort),
		UnavailableAfter: defaultBackoff,
		now:              time.Now,
	}
}

func (c *Client) IsAvailable() bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.BaseURL) == "" || strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.Model) == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maybeResetQuotaLocked()
	if c.quotaExhausted {
		return false
	}
	if !c.unavailableUntil.IsZero() && c.now().Before(c.unavailableUntil) {
		return false
	}
	return true
}

func (c *Client) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	if c == nil {
		return nil, ErrUnavailable
	}
	if !c.IsAvailable() {
		if c.isQuotaExhausted() {
			return nil, ErrQuotaExhausted
		}
		return nil, ErrUnavailable
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("cerebras request requires at least one message")
	}

	requestBody, err := json.Marshal(map[string]any{
		"model":            c.Model,
		"messages":         req.Messages,
		"max_tokens":       resolveMaxTokens(req.MaxTokens),
		"temperature":      req.Temperature,
		"reasoning_effort": c.reasoningEffort(),
		"response_format": map[string]any{
			"type": "json_object",
		},
	})
	if err != nil {
		return nil, err
	}

	startedAt := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/chat/completions", bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		c.markUnavailable()
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		c.markQuotaExhausted()
		return nil, ErrQuotaExhausted
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp.StatusCode >= http.StatusInternalServerError {
			c.markUnavailable()
		}
		return nil, fmt.Errorf("cerebras completion failed: status %d", resp.StatusCode)
	}

	var decoded chatCompletionResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Choices) == 0 {
		return nil, errors.New("cerebras completion missing choices")
	}
	text, confidence := parseStructuredContent(decoded.Choices[0].Message.Content)
	if text == "" {
		return nil, errors.New("cerebras completion missing text")
	}
	return &CompletionResponse{
		Text:       text,
		Confidence: confidence,
		TokensUsed: decoded.Usage.TotalTokens,
		Latency:    time.Since(startedAt),
	}, nil
}

func (c *Client) reasoningEffort() string {
	if c == nil {
		return DefaultReasoningEffort
	}
	if effort := normalizeReasoningEffort(c.ReasoningEffort); effort != "" {
		return effort
	}
	return DefaultReasoningEffort
}

func normalizeReasoningEffort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "minimal", "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func parseStructuredContent(raw string) (string, string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ""
	}
	var parsed structuredCompletion
	if json.Unmarshal([]byte(trimmed), &parsed) == nil {
		text := strings.TrimSpace(parsed.Text)
		if text != "" {
			return text, normalizeConfidence(parsed.Confidence)
		}
	}
	if start := strings.Index(trimmed, "{"); start >= 0 {
		if end := strings.LastIndex(trimmed, "}"); end > start {
			var embedded structuredCompletion
			if json.Unmarshal([]byte(trimmed[start:end+1]), &embedded) == nil {
				text := strings.TrimSpace(embedded.Text)
				if text != "" {
					return text, normalizeConfidence(embedded.Confidence)
				}
			}
		}
	}
	return trimmed, "medium"
}

func normalizeConfidence(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high":
		return "high"
	case "medium":
		return "medium"
	case "low":
		return "low"
	default:
		return ""
	}
}

func resolveMaxTokens(raw int) int {
	if raw > 0 {
		return raw
	}
	return defaultMaxTokens
}

func (c *Client) markQuotaExhausted() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.quotaExhausted = true
	c.exhaustedAt = c.now().UTC()
}

func (c *Client) maybeResetQuotaLocked() {
	if !c.quotaExhausted {
		return
	}
	now := c.now().UTC()
	yearA, monthA, dayA := now.Date()
	yearB, monthB, dayB := c.exhaustedAt.UTC().Date()
	if yearA == yearB && monthA == monthB && dayA == dayB {
		return
	}
	c.quotaExhausted = false
	c.exhaustedAt = time.Time{}
}

func (c *Client) isQuotaExhausted() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maybeResetQuotaLocked()
	return c.quotaExhausted
}

func (c *Client) markUnavailable() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	backoff := c.UnavailableAfter
	if backoff <= 0 {
		backoff = defaultBackoff
	}
	c.unavailableUntil = c.now().Add(backoff)
}
