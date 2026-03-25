package web

import "strings"

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
	localAssistantToolFamilyWeb       localAssistantToolFamily = "web"
)

func normalizeLocalAssistantAddress(text string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}
	lower := strings.ToLower(clean)
	for _, name := range []string{"tabura", "sloppy", "computer"} {
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
		"website", "web search", "browse ", "latest ", "news", "search the web",
		"webseite", "im web", "im internet", "neueste ", "nachrichten",
	):
		return localAssistantToolFamilyWeb
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
		"canvas", "artifact", "diagram", "flowchart", "schematic", "readme", "file", "document", "pdf", "image", "text",
		"diagramm", "flussdiagramm", "schema", "schaubild", "datei", "dokument", "bild", "skizze", "schematik",
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
