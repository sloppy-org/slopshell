package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const defaultPromptTimeout = 90 * time.Second

type Client struct {
	URL    string
	Dialer *websocket.Dialer
}

type PromptRequest struct {
	CWD     string
	Prompt  string
	Model   string
	Timeout time.Duration
}

type PromptResponse struct {
	ThreadID string
	TurnID   string
	Message  string
}

type StreamEvent struct {
	Type     string
	ThreadID string
	TurnID   string
	Message  string
	Delta    string
	Error    string
}

func NewClient(rawURL string) (*Client, error) {
	normalized, err := NormalizeURL(rawURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		URL:    normalized,
		Dialer: websocket.DefaultDialer,
	}, nil
}

func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("app_server_url is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid app_server_url")
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", fmt.Errorf("app_server_url must use ws or wss")
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("app_server_url must include host")
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("app_server_url host must be loopback")
	}
	path := strings.TrimSpace(u.Path)
	if path != "" && path != "/" {
		return "", fmt.Errorf("app_server_url path must be empty or /")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("app_server_url must not include query or fragment")
	}
	if path == "/" {
		u.Path = ""
	}
	return u.String(), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c *Client) SendPrompt(ctx context.Context, req PromptRequest) (*PromptResponse, error) {
	return c.SendPromptStream(ctx, req, nil)
}

func (c *Client) SendPromptStream(ctx context.Context, req PromptRequest, onEvent func(StreamEvent)) (*PromptResponse, error) {
	if c == nil {
		return nil, errors.New("app-server client is nil")
	}
	if strings.TrimSpace(c.URL) == "" {
		return nil, errors.New("app-server URL is empty")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("prompt is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = defaultPromptTimeout
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, c.URL, nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := c.writeJSON(ctx, conn, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"clientInfo": map[string]interface{}{
				"name":    "tabula-web",
				"title":   "Tabula Web",
				"version": "0.0.5",
			},
			"capabilities": map[string]interface{}{
				"experimentalApi": true,
			},
		},
	}); err != nil {
		return nil, err
	}
	if _, err := c.waitForResponse(ctx, conn, 1); err != nil {
		return nil, fmt.Errorf("initialize failed: %w", err)
	}

	if err := c.writeJSON(ctx, conn, map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
	}); err != nil {
		return nil, err
	}

	threadParams := map[string]interface{}{
		"cwd":                    strings.TrimSpace(req.CWD),
		"approvalPolicy":         "never",
		"sandbox":                "danger-full-access",
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
		"ephemeral":              false,
	}
	if strings.TrimSpace(req.Model) != "" {
		threadParams["model"] = strings.TrimSpace(req.Model)
	}
	if err := c.writeJSON(ctx, conn, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "thread/start",
		"params":  threadParams,
	}); err != nil {
		return nil, err
	}
	threadResp, err := c.waitForResponse(ctx, conn, 2)
	if err != nil {
		return nil, fmt.Errorf("thread/start failed: %w", err)
	}
	threadID := parseThreadID(threadResp)
	if threadID == "" {
		return nil, errors.New("thread/start missing thread id")
	}
	if onEvent != nil {
		onEvent(StreamEvent{Type: "thread_started", ThreadID: threadID})
	}

	if err := c.writeJSON(ctx, conn, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "turn/start",
		"params": map[string]interface{}{
			"threadId": threadID,
			"input": []map[string]interface{}{{
				"type":          "text",
				"text":          prompt,
				"text_elements": []interface{}{},
			}},
		},
	}); err != nil {
		return nil, err
	}

	turnID, message, err := c.readTurnUntilComplete(ctx, conn, threadID, onEvent)
	if err != nil {
		return nil, err
	}
	return &PromptResponse{
		ThreadID: threadID,
		TurnID:   turnID,
		Message:  message,
	}, nil
}

func (c *Client) writeJSON(ctx context.Context, conn *websocket.Conn, payload interface{}) error {
	if err := setWriteDeadline(ctx, conn); err != nil {
		return err
	}
	return conn.WriteJSON(payload)
}

func (c *Client) waitForResponse(ctx context.Context, conn *websocket.Conn, id int) (map[string]interface{}, error) {
	for {
		msg, err := readJSON(ctx, conn)
		if err != nil {
			return nil, err
		}
		msgID, hasID := jsonRPCID(msg)
		if !hasID || msgID != id {
			continue
		}
		if errObj, ok := msg["error"].(map[string]interface{}); ok && errObj != nil {
			return nil, fmt.Errorf("rpc error: %s", strings.TrimSpace(fmt.Sprint(errObj["message"])))
		}
		return msg, nil
	}
}

func (c *Client) readTurnUntilComplete(ctx context.Context, conn *websocket.Conn, threadID string, onEvent func(StreamEvent)) (string, string, error) {
	turnResponseSeen := false
	turnID := ""
	message := ""
	previousMessage := ""
	turnCompleted := false

	for {
		msg, err := readJSON(ctx, conn)
		if err != nil {
			return "", "", err
		}

		if msgID, hasID := jsonRPCID(msg); hasID && msgID == 3 {
			if errObj, ok := msg["error"].(map[string]interface{}); ok && errObj != nil {
				errText := strings.TrimSpace(fmt.Sprint(errObj["message"]))
				if onEvent != nil {
					onEvent(StreamEvent{Type: "error", ThreadID: threadID, TurnID: turnID, Error: errText})
				}
				return "", "", fmt.Errorf("turn/start rpc error: %s", errText)
			}
			turnResponseSeen = true
			if result, _ := msg["result"].(map[string]interface{}); result != nil {
				if turn, _ := result["turn"].(map[string]interface{}); turn != nil {
					if id, _ := turn["id"].(string); strings.TrimSpace(id) != "" {
						turnID = id
					}
				}
			}
			if onEvent != nil {
				onEvent(StreamEvent{Type: "turn_started", ThreadID: threadID, TurnID: turnID})
			}
			continue
		}

		method, _ := msg["method"].(string)
		params, _ := msg["params"].(map[string]interface{})
		switch method {
		case "item/completed":
			if item, _ := params["item"].(map[string]interface{}); item != nil {
				if typ, _ := item["type"].(string); typ == "agentMessage" {
					if text, _ := item["text"].(string); strings.TrimSpace(text) != "" {
						message = text
					}
				}
			}
		case "turn/completed":
			turnCompleted = true
			if turn, _ := params["turn"].(map[string]interface{}); turn != nil {
				if id, _ := turn["id"].(string); strings.TrimSpace(id) != "" {
					turnID = id
				}
			}
			if onEvent != nil {
				onEvent(StreamEvent{Type: "turn_completed", ThreadID: threadID, TurnID: turnID, Message: strings.TrimSpace(message)})
			}
		case "codex/event/agent_message":
			if msgObj, _ := params["msg"].(map[string]interface{}); msgObj != nil {
				if text, _ := msgObj["message"].(string); strings.TrimSpace(text) != "" {
					message = text
				}
			}
		case "codex/event/task_complete":
			if msgObj, _ := params["msg"].(map[string]interface{}); msgObj != nil {
				if text, _ := msgObj["last_agent_message"].(string); strings.TrimSpace(text) != "" {
					message = text
				}
			}
		case "error":
			if params != nil {
				errText := strings.TrimSpace(fmt.Sprint(params["message"]))
				if errText != "" && errText != "<nil>" {
					if onEvent != nil {
						onEvent(StreamEvent{Type: "error", ThreadID: threadID, TurnID: turnID, Error: errText})
					}
					return "", "", errors.New(errText)
				}
			}
		}

		trimmed := strings.TrimSpace(message)
		if onEvent != nil && trimmed != "" && trimmed != previousMessage {
			onEvent(StreamEvent{
				Type:     "assistant_message",
				ThreadID: threadID,
				TurnID:   turnID,
				Message:  trimmed,
				Delta:    computeSuffixDelta(previousMessage, trimmed),
			})
			previousMessage = trimmed
		}

		if turnResponseSeen && turnCompleted {
			final := strings.TrimSpace(message)
			if final == "" {
				if onEvent != nil {
					onEvent(StreamEvent{Type: "error", ThreadID: threadID, TurnID: turnID, Error: "app-server returned an empty assistant message"})
				}
				return turnID, "", errors.New("app-server returned an empty assistant message")
			}
			return turnID, final, nil
		}
	}
}

func computeSuffixDelta(previous, current string) string {
	if previous == "" {
		return current
	}
	i := 0
	max := len(previous)
	if len(current) < max {
		max = len(current)
	}
	for i < max && previous[i] == current[i] {
		i++
	}
	return current[i:]
}

func parseThreadID(msg map[string]interface{}) string {
	result, _ := msg["result"].(map[string]interface{})
	if result == nil {
		return ""
	}
	thread, _ := result["thread"].(map[string]interface{})
	if thread == nil {
		return ""
	}
	id, _ := thread["id"].(string)
	return strings.TrimSpace(id)
}

func jsonRPCID(msg map[string]interface{}) (int, bool) {
	id, ok := msg["id"]
	if !ok {
		return 0, false
	}
	switch v := id.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	case string:
		// this client uses integer IDs only
		return 0, false
	default:
		return 0, false
	}
}

func readJSON(ctx context.Context, conn *websocket.Conn) (map[string]interface{}, error) {
	if err := setReadDeadline(ctx, conn); err != nil {
		return nil, err
	}
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func setReadDeadline(ctx context.Context, conn *websocket.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetReadDeadline(deadline)
	}
	return conn.SetReadDeadline(time.Now().Add(defaultPromptTimeout))
}

func setWriteDeadline(ctx context.Context, conn *websocket.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		return conn.SetWriteDeadline(deadline)
	}
	return conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
}
