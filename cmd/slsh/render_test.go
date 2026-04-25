package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRendererDedupesDeltaPreviews(t *testing.T) {
	var buf bytes.Buffer
	r := newRenderer(&buf, false, false)

	// Long running text — exceeds the 120-rune preview cap so each delta
	// produces the same truncated preview. The renderer must collapse them.
	full := strings.Repeat("I do not have access to external logs or the ability to diagnose why a previous call to GPT failed. ", 5)
	for i := 1; i <= 10; i++ {
		r.onAssistantDelta(map[string]any{"message": full[:len(full)-10+i%2]})
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 1 {
		t.Fatalf("expected at most 1 preview line for identical previews, got %d:\n%s", nonEmpty, out)
	}
}

func TestRendererStreamsDistinctPreviews(t *testing.T) {
	var buf bytes.Buffer
	r := newRenderer(&buf, false, false)

	r.onAssistantDelta(map[string]any{"message": "one small preview"})
	r.onAssistantDelta(map[string]any{"message": "another different preview"})
	r.onAssistantDelta(map[string]any{"message": "a third changed preview"})

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty != 3 {
		t.Fatalf("expected 3 distinct preview lines, got %d:\n%s", nonEmpty, out)
	}
}

func TestRendererFinalClosesInPlaceProgress(t *testing.T) {
	var buf bytes.Buffer
	// colors=true triggers in-place overwrite mode
	r := newRenderer(&buf, false, true)

	r.onAssistantDelta(map[string]any{"message": "progress one"})
	r.onAssistantDelta(map[string]any{"message": "progress two"})
	r.onAssistantFinal(map[string]any{"message": "final answer"})

	out := buf.String()
	if !strings.Contains(out, "final answer") {
		t.Fatalf("final answer missing from output:\n%q", out)
	}
	// After the final line, the progressOn flag should be false.
	if r.progressOn {
		t.Fatalf("progressOn should be cleared after final; out=%q", out)
	}
}

func TestRendererSystemActionClearsProgress(t *testing.T) {
	var buf bytes.Buffer
	r := newRenderer(&buf, false, true)

	r.onAssistantDelta(map[string]any{"message": "running thought"})
	r.onSystemAction(map[string]any{
		"action": map[string]any{"name": "shell"},
	})
	r.onAssistantFinal(map[string]any{"message": "done"})

	out := buf.String()
	if !strings.Contains(out, "tool:") {
		t.Fatalf("expected tool line in output:\n%q", out)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("expected final line in output:\n%q", out)
	}
}
