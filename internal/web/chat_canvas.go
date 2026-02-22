package web

import (
	"fmt"
	"regexp"
	"strings"
)

type canvasAction struct {
	Title   string
	Kind    string
	Content string
}

var canvasShowRe = regexp.MustCompile(`(?s):::canvas_show\{([^}]*)\}\n?(.*?):::`)

func parseCanvasActions(text string) ([]canvasAction, string) {
	matches := canvasShowRe.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return nil, text
	}
	var actions []canvasAction
	cleaned := text
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		fullStart, fullEnd := m[0], m[1]
		attrsStart, attrsEnd := m[2], m[3]
		contentStart, contentEnd := m[4], m[5]

		attrs := text[attrsStart:attrsEnd]
		content := strings.TrimSpace(text[contentStart:contentEnd])

		title := extractAttr(attrs, "title")
		kind := extractAttr(attrs, "kind")
		if kind == "" {
			kind = "text"
		}

		actions = append([]canvasAction{{
			Title:   title,
			Kind:    kind,
			Content: content,
		}}, actions...)

		ref := fmt.Sprintf("[canvas: %s]", title)
		cleaned = cleaned[:fullStart] + ref + cleaned[fullEnd:]
	}
	return actions, strings.TrimSpace(cleaned)
}

var attrRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

func extractAttr(attrs, name string) string {
	for _, m := range attrRe.FindAllStringSubmatch(attrs, -1) {
		if m[1] == name {
			return m[2]
		}
	}
	return ""
}

func (a *App) executeCanvasActions(canvasSessionID string, actions []canvasAction) {
	a.mu.Lock()
	port, ok := a.tunnelPorts[canvasSessionID]
	a.mu.Unlock()
	if !ok {
		return
	}
	for _, action := range actions {
		kind := "text_artifact"
		if action.Kind == "code" {
			kind = "text_artifact"
		}
		_, _ = a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
			"session_id": canvasSessionID,
			"kind":       kind,
			"title":      action.Title,
			"text":       action.Content,
		})
	}
}
