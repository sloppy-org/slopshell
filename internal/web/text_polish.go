package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	textPolishTimeout       = 10 * time.Second
	textPolishResponseLimit = 128 * 1024
	textPolishMaxTokens     = 1024
)

type textPolishStyle struct {
	SystemPrompt string
	MaxTokens    int
}

var textPolishStyles = map[string]textPolishStyle{
	"": {
		SystemPrompt: `You polish dictated text.

Rules:
- Fix spelling errors, especially names, titles, and organizations from the context
- Insert German Umlaute where appropriate (Muenchen->München, schoene->schöne, fuer->für, ueber->über, etc.)
- Fix clearly misheard words based on context
- Add proper punctuation and paragraphs
- Preserve the speaker's meaning exactly; do not add content they did not say
- Keep the same language as the dictated text; do not translate
- Return JSON: {"polished_body":"the corrected and formatted text"}`,
		MaxTokens: textPolishMaxTokens,
	},
	"email": {
		SystemPrompt: `You polish dictated email text.

Rules:
- Fix spelling errors, especially names, titles, and organizations from the context
- Insert German Umlaute where appropriate (Muenchen->München, schoene->schöne, fuer->für, ueber->über, etc.)
- Fix clearly misheard words based on context
- Format as a proper email with greeting, paragraphs, and closing
- Preserve the sender's meaning exactly; do not add content they did not say
- Keep the same language as the dictated text; do not translate
- Return JSON: {"polished_body":"the corrected and formatted text"}`,
		MaxTokens: textPolishMaxTokens,
	},
	"note": {
		SystemPrompt: `You polish dictated notes.

Rules:
- Fix spelling errors, especially names, titles, and organizations from the context
- Insert German Umlaute where appropriate (Muenchen->München, schoene->schöne, fuer->für, ueber->über, etc.)
- Fix clearly misheard words based on context
- Clean up into concise, readable note format
- Use bullet points or numbered lists where the speaker implied structure
- Preserve the speaker's meaning exactly; do not add content they did not say
- Keep the same language as the dictated text; do not translate
- Return JSON: {"polished_body":"the corrected and formatted text"}`,
		MaxTokens: textPolishMaxTokens,
	},
}

func resolvePolishStyle(name string) textPolishStyle {
	if style, ok := textPolishStyles[strings.ToLower(strings.TrimSpace(name))]; ok {
		return style
	}
	return textPolishStyles[""]
}

type textPolishRequest struct {
	Body    string `json:"body"`
	Context string `json:"context,omitempty"`
	Style   string `json:"style,omitempty"`
}

type textPolishResponse struct {
	PolishedBody string `json:"polished_body"`
}

func (a *App) handleTextPolish(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req textPolishRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeAPIError(w, http.StatusBadRequest, "body is required")
		return
	}
	style := resolvePolishStyle(req.Style)
	polished, err := a.polishText(r.Context(), body, strings.TrimSpace(req.Context), style)
	if err != nil {
		polished = body
	}
	writeAPIData(w, http.StatusOK, map[string]any{"polished_body": polished})
}

func (a *App) handleMailDraftPolish(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	artifactID, err := parseURLInt64Param(r, "artifact_id")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req textPolishRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		writeAPIError(w, http.StatusBadRequest, "body is required")
		return
	}
	contextText := a.mailDraftPolishContext(artifactID)
	polished, err := a.polishText(r.Context(), body, contextText, resolvePolishStyle("email"))
	if err != nil {
		polished = body
	}
	writeAPIData(w, http.StatusOK, map[string]any{"polished_body": polished})
}

func (a *App) mailDraftPolishContext(artifactID int64) string {
	ctx, err := a.loadMailDraftContext(artifactID)
	if err != nil {
		return ""
	}
	if strings.TrimSpace(ctx.meta.ReplyToMessageID) == "" {
		return ""
	}
	binding, err := a.store.GetBindingByRemote(
		ctx.meta.AccountID,
		ctx.meta.Provider,
		emailBindingObjectType,
		ctx.meta.ReplyToMessageID,
	)
	if err != nil {
		return ""
	}
	if binding.ArtifactID == nil || *binding.ArtifactID <= 0 {
		return ""
	}
	artifact, err := a.store.GetArtifact(*binding.ArtifactID)
	if err != nil {
		return ""
	}
	meta := parseSidebarArtifactMeta(stringFromPointer(artifact.MetaJSON))
	var lines []string
	if sender := strings.TrimSpace(stringAny(meta["sender"])); sender != "" {
		lines = append(lines, "From: "+sender)
	}
	if subject := strings.TrimSpace(stringAny(meta["subject"])); subject != "" {
		lines = append(lines, "Subject: "+subject)
	}
	if body := strings.TrimSpace(stringAny(meta["body"])); body != "" {
		if len(body) > 1000 {
			body = body[:1000] + "..."
		}
		lines = append(lines, "Body:\n"+body)
	}
	if len(lines) == 0 {
		return ""
	}
	return "Original email:\n" + strings.Join(lines, "\n")
}

func (a *App) polishText(ctx context.Context, body, contextText string, style textPolishStyle) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(a.intentLLMURL), "/")
	if baseURL == "" {
		return body, nil
	}
	userPrompt := body
	if contextText != "" {
		userPrompt = contextText + "\n\nDictated text to polish:\n" + body
	}
	maxTokens := style.MaxTokens
	if maxTokens <= 0 {
		maxTokens = textPolishMaxTokens
	}
	requestBody, _ := json.Marshal(map[string]any{
		"model":       a.localIntentLLMModel(),
		"temperature": 0,
		"max_tokens":  maxTokens,
		"response_format": map[string]any{
			"type": "json_object",
		},
		"chat_template_kwargs": map[string]any{
			"enable_thinking": false,
		},
		"messages": []map[string]any{
			{"role": "system", "content": style.SystemPrompt},
			{"role": "user", "content": userPrompt},
		},
	})
	requestCtx, cancel := context.WithTimeout(ctx, textPolishTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		baseURL+"/v1/chat/completions",
		bytes.NewReader(requestBody),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, textPolishResponseLimit))
		return "", fmt.Errorf("polish LLM HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var payload localIntentLLMChatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, textPolishResponseLimit)).Decode(&payload); err != nil {
		return "", err
	}
	if len(payload.Choices) == 0 {
		return "", fmt.Errorf("polish LLM returned no choices")
	}
	content := strings.TrimSpace(stripCodeFence(payload.Choices[0].Message.Content))
	if content == "" {
		return "", fmt.Errorf("polish LLM returned empty content")
	}
	var result textPolishResponse
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return "", err
	}
	polished := strings.TrimSpace(result.PolishedBody)
	if polished == "" {
		return "", fmt.Errorf("polish LLM returned empty polished body")
	}
	return polished, nil
}
