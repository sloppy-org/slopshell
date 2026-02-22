package web

import (
	"context"
	"errors"
	"testing"
)

func TestNormalizeAssistantError(t *testing.T) {
	t.Run("deadline exceeded", func(t *testing.T) {
		got := normalizeAssistantError(context.DeadlineExceeded)
		if got != "assistant request timed out" {
			t.Fatalf("expected timeout message, got %q", got)
		}
	})

	t.Run("raw io timeout", func(t *testing.T) {
		got := normalizeAssistantError(errors.New("read tcp 127.0.0.1:10->127.0.0.1:20: i/o timeout"))
		if got != "assistant request timed out" {
			t.Fatalf("expected timeout message, got %q", got)
		}
	})

	t.Run("empty error", func(t *testing.T) {
		got := normalizeAssistantError(errors.New(" "))
		if got != "assistant request failed" {
			t.Fatalf("expected generic error message, got %q", got)
		}
	})

	t.Run("passthrough", func(t *testing.T) {
		want := "connection refused"
		got := normalizeAssistantError(errors.New(want))
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})
}
