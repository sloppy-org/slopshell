package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var (
	errAssistantError = errors.New("assistant error")
)

type clientConfig struct {
	baseURL   string
	tokenFile string
	verbose   bool
	stderr    io.Writer
}

type chatClient struct {
	base    *url.URL
	http    *http.Client
	verbose bool
	stderr  io.Writer
}

type chatSessionInfo struct {
	ID              string `json:"session_id"`
	WorkspaceID     int64  `json:"workspace_id"`
	WorkspacePath   string `json:"workspace_path"`
	Mode            string `json:"mode"`
	CanvasSessionID string `json:"canvas_session_id"`
}

func newClient(ctx context.Context, cfg clientConfig) (*chatClient, error) {
	base, err := url.Parse(strings.TrimSpace(cfg.baseURL))
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("base url must include scheme and host: %q", cfg.baseURL)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}
	client := &chatClient{
		base: base,
		http: &http.Client{
			Jar:     jar,
			Timeout: 0,
		},
		verbose: cfg.verbose,
		stderr:  cfg.stderr,
	}
	if err := client.cliLogin(ctx, cfg.tokenFile); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *chatClient) cliLogin(ctx context.Context, tokenFile string) error {
	token, err := readCLIToken(tokenFile)
	if err != nil {
		return fmt.Errorf("read cli token: %w (is the slopshell server running on this host?)", err)
	}
	payload, _ := json.Marshal(map[string]string{"token": token})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlFor("/api/cli/login"), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cli login: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cli login failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func readCLIToken(path string) (string, error) {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return "", errors.New("cli token file path unresolved; set --token-file or SLOPSHELL_CLI_TOKEN_FILE")
	}
	body, err := os.ReadFile(clean)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", fmt.Errorf("cli token file %s is empty", clean)
	}
	return token, nil
}

func (c *chatClient) urlFor(path string) string {
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String()
}

func (c *chatClient) wsURLFor(path string) string {
	u := *c.base
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	return u.String()
}

func (c *chatClient) startChatSession(ctx context.Context, projectDir, resumeID string) (*chatSessionInfo, error) {
	if strings.TrimSpace(resumeID) != "" {
		return c.attachSession(ctx, resumeID)
	}
	body := map[string]any{}
	if strings.TrimSpace(projectDir) != "" {
		body["workspace_path"] = projectDir
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlFor("/api/chat/sessions"), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create chat session: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var info chatSessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	if strings.TrimSpace(info.ID) == "" {
		return nil, errors.New("server returned empty session id")
	}
	return &info, nil
}

func (c *chatClient) attachSession(ctx context.Context, sessionID string) (*chatSessionInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor("/api/chat/sessions/"+url.PathEscape(sessionID)+"/history"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("attach session %s: status %d: %s", sessionID, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var wrapped struct {
		Session struct {
			ID            string `json:"id"`
			WorkspaceID   int64  `json:"workspace_id"`
			WorkspacePath string `json:"workspace_path"`
			Mode          string `json:"mode"`
		} `json:"session"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return nil, err
	}
	if strings.TrimSpace(wrapped.Session.ID) == "" {
		return nil, fmt.Errorf("session %q not found", sessionID)
	}
	return &chatSessionInfo{
		ID:            wrapped.Session.ID,
		WorkspaceID:   wrapped.Session.WorkspaceID,
		WorkspacePath: wrapped.Session.WorkspacePath,
		Mode:          wrapped.Session.Mode,
	}, nil
}

type historyMessage struct {
	ID           string `json:"id"`
	Role         string `json:"role"`
	ContentPlain string `json:"content_plain"`
	Content      string `json:"content"`
	CreatedAt    string `json:"created_at"`
}

func (c *chatClient) printRecentHistory(ctx context.Context, sessionID string, renderer *renderer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor("/api/chat/sessions/"+url.PathEscape(sessionID)+"/history"), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fetch history: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var wrapped struct {
		Messages []historyMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return err
	}
	keep := wrapped.Messages
	if len(keep) > 12 {
		keep = keep[len(keep)-12:]
	}
	for _, msg := range keep {
		text := strings.TrimSpace(msg.ContentPlain)
		if text == "" {
			text = strings.TrimSpace(msg.Content)
		}
		if text == "" {
			continue
		}
		renderer.renderHistoryMessage(msg.Role, text)
	}
	return nil
}

func (c *chatClient) sendCommand(ctx context.Context, sessionID, command string) (map[string]any, error) {
	payload, _ := json.Marshal(map[string]string{"command": command})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlFor("/api/chat/sessions/"+url.PathEscape(sessionID)+"/commands"), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("command %s: status %d: %s", command, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	result, _ := body["result"].(map[string]any)
	if result == nil {
		result = map[string]any{}
	}
	return result, nil
}

func (c *chatClient) sendAndWaitForFinal(ctx context.Context, sessionID, prompt string, renderer *renderer) (string, error) {
	ws, err := c.dialChatWS(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("ws dial: %w", err)
	}
	defer ws.Close()

	// Send user message
	payload, _ := json.Marshal(map[string]any{
		"text":        prompt,
		"output_mode": "silent",
		"local_only":  false,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlFor("/api/chat/sessions/"+url.PathEscape(sessionID)+"/messages"), bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("send message: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	// Drain WS events until assistant_output or error or ctx done.
	return c.consumeTurn(ctx, ws, renderer)
}

func (c *chatClient) consumeTurn(ctx context.Context, ws *websocket.Conn, renderer *renderer) (string, error) {
	errCh := make(chan error, 1)
	doneCh := make(chan string, 1)
	go func() {
		var finalText string
		var turnSeen bool
		for {
			_, data, err := ws.ReadMessage()
			if err != nil {
				renderer.finishProgressLine()
				if turnSeen && finalText != "" {
					doneCh <- finalText
					return
				}
				errCh <- err
				return
			}
			var event map[string]any
			if err := json.Unmarshal(data, &event); err != nil {
				continue
			}
			if c.verbose {
				compact, _ := json.Marshal(event)
				fmt.Fprintln(c.stderr, "ws:", string(compact))
			}
			kind, _ := event["type"].(string)
			switch kind {
			case "turn_started":
				turnSeen = true
				renderer.onTurnStarted(event)
			case "assistant_message":
				renderer.onAssistantDelta(event)
			case "assistant_output":
				if role, _ := event["role"].(string); role == "assistant" {
					text, _ := event["message"].(string)
					finalText = text
					renderer.onAssistantFinal(event)
					doneCh <- finalText
					return
				}
			case "message_persisted":
				if role, _ := event["role"].(string); role == "assistant" && finalText == "" {
					text, _ := event["message"].(string)
					if strings.TrimSpace(text) != "" {
						finalText = text
					}
				}
			case "system_action":
				renderer.onSystemAction(event)
			case "render_chat":
				renderer.onRenderChat(event)
			case "error":
				msg, _ := event["error"].(string)
				renderer.finishProgressLine()
				errCh <- fmt.Errorf("%w: %s", errAssistantError, strings.TrimSpace(msg))
				return
			case "turn_cancelled":
				renderer.finishProgressLine()
				errCh <- errors.New("turn cancelled")
				return
			case "chat_cleared":
				renderer.onChatCleared(event)
			case "chat_compacted":
				renderer.onChatCompacted(event)
			default:
				renderer.onOther(event)
			}
		}
	}()

	select {
	case <-ctx.Done():
		_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, "ctx"), time.Now().Add(500*time.Millisecond))
		return "", ctx.Err()
	case err := <-errCh:
		return "", err
	case text := <-doneCh:
		return text, nil
	}
}

func (c *chatClient) dialChatWS(ctx context.Context, sessionID string) (*websocket.Conn, error) {
	dialer := *websocket.DefaultDialer
	dialer.Jar = c.http.Jar
	dialer.HandshakeTimeout = 10 * time.Second
	ws, _, err := dialer.DialContext(ctx, c.wsURLFor("/ws/chat/"+url.PathEscape(sessionID)), nil)
	if err != nil {
		return nil, err
	}
	return ws, nil
}
