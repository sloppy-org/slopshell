package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	colorReset  = "\x1b[0m"
	colorDim    = "\x1b[2m"
	colorCyan   = "\x1b[36m"
	colorGreen  = "\x1b[32m"
	colorYellow = "\x1b[33m"
	colorRed    = "\x1b[31m"
	colorGray   = "\x1b[90m"
)

type renderer struct {
	out         io.Writer
	jsonMode    bool
	colors      bool
	lastTurn    string
	lastPreview string
	progressOn  bool
	didFinal    bool
}

func newRenderer(out io.Writer, jsonMode, colors bool) *renderer {
	return &renderer{out: out, jsonMode: jsonMode, colors: colors}
}

// finishProgressLine terminates any in-place progress preview so subsequent
// output starts on a clean line.
func (r *renderer) finishProgressLine() {
	if !r.progressOn {
		return
	}
	fmt.Fprintln(r.out)
	r.progressOn = false
	r.lastPreview = ""
}

func (r *renderer) colorize(color, text string) string {
	if !r.colors || color == "" {
		return text
	}
	return color + text + colorReset
}

func (r *renderer) didEmitFinalText() bool { return r.didFinal }

func (r *renderer) writeEvent(event map[string]any) {
	if !r.jsonMode {
		return
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return
	}
	fmt.Fprintln(r.out, string(encoded))
}

func (r *renderer) onTurnStarted(event map[string]any) {
	r.lastTurn, _ = event["turn_id"].(string)
	r.lastPreview = ""
	if r.jsonMode {
		r.writeEvent(event)
		return
	}
	r.finishProgressLine()
	// Deliberately no visible marker here: the first assistant delta (or
	// system_action / final) is the first thing the user should see. A
	// "thinking" label is misleading when reasoning is disabled.
}

func (r *renderer) onAssistantDelta(event map[string]any) {
	if r.jsonMode {
		r.writeEvent(event)
		return
	}
	msg, _ := event["message"].(string)
	trimmed := strings.TrimSpace(msg)
	if trimmed == "" {
		return
	}
	preview := previewLine(trimmed, 120)
	if preview == r.lastPreview {
		return
	}
	r.lastPreview = preview
	line := "   " + preview
	if r.colors {
		// Overwrite the current progress line in place so streaming growth
		// shows up as a single evolving preview instead of spamming the
		// terminal with near-duplicate lines.
		fmt.Fprint(r.out, "\r\x1b[2K"+r.colorize(colorGray, line))
		r.progressOn = true
		return
	}
	fmt.Fprintln(r.out, r.colorize(colorGray, line))
}

func (r *renderer) onAssistantFinal(event map[string]any) {
	msg, _ := event["message"].(string)
	trimmed := strings.TrimSpace(msg)
	r.didFinal = true
	if r.jsonMode {
		r.writeEvent(event)
		return
	}
	r.finishProgressLine()
	if trimmed == "" {
		return
	}
	fmt.Fprintln(r.out, r.colorize(colorGreen, "assistant: ")+trimmed)
}

func (r *renderer) onSystemAction(event map[string]any) {
	if r.jsonMode {
		r.writeEvent(event)
		return
	}
	r.finishProgressLine()
	action, _ := event["action"].(map[string]any)
	name := "(unknown)"
	if action != nil {
		name, _ = action["name"].(string)
	}
	summary, _ := event["summary"].(string)
	if summary == "" && action != nil {
		summary, _ = action["summary"].(string)
	}
	line := "tool: " + strings.TrimSpace(name)
	if summary != "" {
		line += " — " + summary
	}
	fmt.Fprintln(r.out, r.colorize(colorYellow, line))
}

func (r *renderer) onRenderChat(event map[string]any) {
	if r.jsonMode {
		r.writeEvent(event)
		return
	}
	md, _ := event["markdown"].(string)
	trimmed := strings.TrimSpace(md)
	if trimmed == "" {
		return
	}
	preview := previewLine(trimmed, 120)
	if preview == r.lastPreview {
		return
	}
	r.lastPreview = preview
	line := "   " + preview
	if r.colors {
		fmt.Fprint(r.out, "\r\x1b[2K"+r.colorize(colorGray, line))
		r.progressOn = true
		return
	}
	fmt.Fprintln(r.out, r.colorize(colorGray, line))
}

func (r *renderer) onChatCleared(event map[string]any) {
	if r.jsonMode {
		r.writeEvent(event)
		return
	}
	r.finishProgressLine()
	fmt.Fprintln(r.out, r.colorize(colorDim, "chat cleared"))
}

func (r *renderer) onChatCompacted(event map[string]any) {
	if r.jsonMode {
		r.writeEvent(event)
		return
	}
	r.finishProgressLine()
	fmt.Fprintln(r.out, r.colorize(colorDim, "thread compacted"))
}

func (r *renderer) onOther(event map[string]any) {
	if r.jsonMode {
		r.writeEvent(event)
	}
}

func (r *renderer) renderHistoryMessage(role, text string) {
	if r.jsonMode {
		r.writeEvent(map[string]any{"type": "history", "role": role, "message": text})
		return
	}
	prefix := role
	switch strings.ToLower(role) {
	case "assistant":
		prefix = r.colorize(colorGreen, "assistant: ")
	case "user":
		prefix = r.colorize(colorCyan, "you: ")
	case "system":
		prefix = r.colorize(colorDim, "system: ")
	default:
		prefix = role + ": "
	}
	fmt.Fprintln(r.out, prefix+text)
}

func previewLine(text string, max int) string {
	oneline := strings.ReplaceAll(text, "\n", " ")
	oneline = strings.Join(strings.Fields(oneline), " ")
	if len([]rune(oneline)) <= max {
		return oneline
	}
	runes := []rune(oneline)
	return string(runes[:max-1]) + "…"
}
