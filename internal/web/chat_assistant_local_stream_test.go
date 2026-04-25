package web

import (
	"strings"
	"testing"
)

// TestDecodeLocalAssistantStreamingPayloadPreservesBulletNewlines guards the
// SSE stream decoder against swallowing whitespace-only chunks. Local OpenAI
// compatible servers frequently emit a lone "\n" as its own content delta
// between bullets or paragraphs; dropping those collapses the assistant's
// formatting when the web/slsh clients render the final message.
func TestDecodeLocalAssistantStreamingPayloadPreservesBulletNewlines(t *testing.T) {
	body := strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"- First bullet."}}]}`,
		`data: {"choices":[{"delta":{"content":"\n"}}]}`,
		`data: {"choices":[{"delta":{"content":"- Second bullet."}}]}`,
		`data: {"choices":[{"delta":{"content":"\n"}}]}`,
		`data: {"choices":[{"delta":{"content":"- Third bullet."}}]}`,
		`data: {"choices":[{"finish_reason":"stop"}]}`,
		`data: [DONE]`,
		``,
	}, "\n")

	msg, finish, err := decodeLocalAssistantStreamingPayload(strings.NewReader(body), false, nil)
	if err != nil {
		t.Fatalf("decodeLocalAssistantStreamingPayload: %v", err)
	}
	if finish != "stop" {
		t.Fatalf("finish reason = %q, want stop", finish)
	}
	want := "- First bullet.\n- Second bullet.\n- Third bullet."
	if msg.Content != want {
		t.Fatalf("message content = %q, want %q", msg.Content, want)
	}
}
