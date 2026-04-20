package web

import (
	"regexp"
	"strings"
)

type localAssistantToolFamily string

const (
	localAssistantToolFamilyNone      localAssistantToolFamily = ""
	localAssistantToolFamilyCanvas    localAssistantToolFamily = "canvas"
	localAssistantToolFamilyWorkspace localAssistantToolFamily = "workspace"
	localAssistantToolFamilyShell     localAssistantToolFamily = "shell"
	localAssistantToolFamilyMail      localAssistantToolFamily = "mail"
	localAssistantToolFamilyCalendar  localAssistantToolFamily = "calendar"
	localAssistantToolFamilyItems     localAssistantToolFamily = "items"
	localAssistantToolFamilyRuntime   localAssistantToolFamily = "runtime"
)

func normalizeLocalAssistantAddress(text string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	lower := strings.ToLower(clean)
	for _, name := range []string{"slopshell", "computer"} {
		for _, prefix := range []string{name + " ", name + ",", name + ":", name + ";"} {
			if strings.HasPrefix(lower, prefix) {
				return strings.TrimSpace(clean[len(prefix):])
			}
		}
		if lower == name {
			return ""
		}
	}
	return clean
}

func selectLocalAssistantToolFamily(text string) localAssistantToolFamily {
	lower := strings.ToLower(normalizeLocalAssistantAddress(text))
	if lower == "" {
		return localAssistantToolFamilyNone
	}
	switch {
	case containsAnyLocalAssistantKeyword(lower,
		"go silent", "be silent", "stop talking", "dialogue mode", "meeting mode", "live dialogue", "cancel work", "show status", "show busy state",
		"sei still", "schweig", "stumm", "dialogmodus", "meetingmodus", "arbeit abbrechen", "zeige status", "zeige beschäftigt",
	):
		return localAssistantToolFamilyRuntime
	case containsAnyLocalAssistantKeyword(lower,
		"mail", "email", "inbox", "message", "archive", "unread", "label", "folder",
		"posteingang", "nachricht", "archiv", "ungelesen", "ordner",
	):
		return localAssistantToolFamilyMail
	case containsAnyLocalAssistantKeyword(lower,
		"calendar", "event", "invite", "schedule",
		"kalender", "termin", "einladung", "planen",
	):
		return localAssistantToolFamilyCalendar
	case containsAnyLocalAssistantKeyword(lower,
		"item", "task", "todo", "delegate", "snooze", "waiting", "someday",
		"aufgabe", "deleg", "später", "warten", "irgendwann",
	):
		return localAssistantToolFamilyItems
	case containsAnyLocalAssistantKeyword(lower,
		"shell", "terminal", "bash", "zsh", "shell command", "terminal command",
		"use shell", "use the shell", "run this command", "execute this command", "open terminal",
		"kommandozeile", "konsole", "shell-befehl", "terminalbefehl",
	):
		return localAssistantToolFamilyShell
	case containsAnyLocalAssistantKeyword(lower,
		"canvas", "draw ", "render ", "display ", "show ", "open ",
		"zeichne", "zeige", "öffne", "oeffne", "darstell", "rendere", "skizziere",
	) && containsAnyLocalAssistantKeyword(lower,
		"canvas", "artifact", "diagram", "flowchart", "block diagram", "process map", "workflow", "state machine", "architecture", "schematic", "readme", "file", "document", "pdf", "image", "text", "chart", "overview",
		"diagramm", "flussdiagramm", "blockdiagramm", "ablaufdiagramm", "prozess", "workflow", "zustandsdiagramm", "architektur", "schema", "schaubild", "datei", "dokument", "bild", "skizze", "schematik", "übersicht", "uebersicht",
	):
		return localAssistantToolFamilyCanvas
	case containsAnyLocalAssistantKeyword(lower,
		"directory", "folder", "path", "read file", "list files", "what files", "readme", "open file", "find file",
		"verzeichnis", "ordner", "pfad", "lies datei", "liste dateien", "welche dateien", "öffne datei", "oeffne datei", "finde datei",
	):
		return localAssistantToolFamilyWorkspace
	default:
		return localAssistantToolFamilyNone
	}
}

func containsAnyLocalAssistantKeyword(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

var localAssistantExplicitPathRe = regexp.MustCompile(`(?i)(?:` + "`" + `([^` + "`" + `]+)` + "`" + `|\"([^\"]+)\"|'([^']+)'|\b([a-z0-9][a-z0-9._/-]*\.[a-z0-9][a-z0-9._-]*)\b)`)

func localAssistantDirectOpenFileHint(text string, family localAssistantToolFamily) string {
	if family != localAssistantToolFamilyCanvas && family != localAssistantToolFamilyWorkspace {
		return ""
	}
	lower := strings.ToLower(normalizeLocalAssistantAddress(text))
	if lower == "" {
		return ""
	}
	if !containsAnyLocalAssistantKeyword(lower,
		"open ", "show ", "display ", "render ", "zeige", "öffne", "oeffne", "darstell", "rendere",
	) {
		return ""
	}
	if match := localAssistantExplicitPathRe.FindStringSubmatch(strings.TrimSpace(text)); len(match) > 0 {
		for _, candidate := range match[1:] {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" {
				return candidate
			}
		}
	}
	if strings.Contains(lower, "readme") {
		return "README"
	}
	return ""
}
