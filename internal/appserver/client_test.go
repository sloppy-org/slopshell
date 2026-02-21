package appserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSendPrompt(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	promptSeen := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]interface{}
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode message: %v", err)
			}

			method, _ := msg["method"].(string)
			switch method {
			case "initialize":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"userAgent": "test-client",
					},
				})
			case "thread/start":
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"thread": map[string]interface{}{
							"id": "thread-test-1",
						},
					},
				})
			case "turn/start":
				params, _ := msg["params"].(map[string]interface{})
				input, _ := params["input"].([]interface{})
				if len(input) > 0 {
					first, _ := input[0].(map[string]interface{})
					promptSeen, _ = first["text"].(string)
				}
				_ = conn.WriteJSON(map[string]interface{}{
					"id": msg["id"],
					"result": map[string]interface{}{
						"turn": map[string]interface{}{
							"id": "turn-test-1",
						},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "item/completed",
					"params": map[string]interface{}{
						"item": map[string]interface{}{
							"type": "agentMessage",
							"text": "# Improved\n\nUpdated by test.",
						},
					},
				})
				_ = conn.WriteJSON(map[string]interface{}{
					"method": "turn/completed",
					"params": map[string]interface{}{
						"turn": map[string]interface{}{
							"id":     "turn-test-1",
							"status": "completed",
						},
					},
				})
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	client, err := NewClient(wsURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.SendPrompt(ctx, PromptRequest{
		CWD:    "/tmp",
		Prompt: "rewrite this markdown",
	})
	if err != nil {
		t.Fatalf("send prompt: %v", err)
	}
	if promptSeen != "rewrite this markdown" {
		t.Fatalf("expected prompt to be forwarded, got %q", promptSeen)
	}
	if resp.ThreadID != "thread-test-1" {
		t.Fatalf("expected thread-test-1, got %q", resp.ThreadID)
	}
	if resp.TurnID != "turn-test-1" {
		t.Fatalf("expected turn-test-1, got %q", resp.TurnID)
	}
	if strings.TrimSpace(resp.Message) != "# Improved\n\nUpdated by test." {
		t.Fatalf("unexpected assistant message: %q", resp.Message)
	}
}

func TestNormalizeURLRejectsNonLoopback(t *testing.T) {
	if _, err := NormalizeURL("ws://example.com:8787"); err == nil {
		t.Fatalf("expected non-loopback URL to be rejected")
	}
}
