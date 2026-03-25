package web

import (
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestSelectLocalAssistantToolFamilySupportsGermanCanvasRequests(t *testing.T) {
	tests := []struct {
		name string
		text string
		want localAssistantToolFamily
	}{
		{
			name: "german flowchart canvas",
			text: "Bitte zeichne mir wie ein Fusionsreaktor funktioniert als Flowchart auf der Canvas.",
			want: localAssistantToolFamilyCanvas,
		},
		{
			name: "english flowchart canvas",
			text: "Please draw a flowchart on the canvas showing how a fusion reactor works.",
			want: localAssistantToolFamilyCanvas,
		},
		{
			name: "english architecture overview canvas",
			text: "Draw an architecture overview on the canvas for a fusion reactor control loop.",
			want: localAssistantToolFamilyCanvas,
		},
		{
			name: "english state machine canvas",
			text: "Show a state machine on the canvas for the reactor startup sequence.",
			want: localAssistantToolFamilyCanvas,
		},
		{
			name: "german block diagram canvas",
			text: "Zeichne ein Blockdiagramm des Fusionsreaktors auf der Canvas.",
			want: localAssistantToolFamilyCanvas,
		},
		{
			name: "german open file canvas",
			text: "Öffne bitte die README auf der Canvas.",
			want: localAssistantToolFamilyCanvas,
		},
		{
			name: "english open file canvas",
			text: "Display the README on canvas.",
			want: localAssistantToolFamilyCanvas,
		},
		{
			name: "german workspace files",
			text: "Welche Dateien sind in diesem Verzeichnis?",
			want: localAssistantToolFamilyWorkspace,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectLocalAssistantToolFamily(tt.text); got != tt.want {
				t.Fatalf("selectLocalAssistantToolFamily(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestLocalAssistantCanvasShouldRenderGeneratedTextForMeetingFlowcharts(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "german flowchart canvas",
			text: "Bitte zeichne mir wie ein Fusionsreaktor funktioniert als Flowchart auf der Canvas.",
			want: true,
		},
		{
			name: "english flowchart canvas",
			text: "Please draw a flowchart on the canvas showing how a fusion reactor works.",
			want: true,
		},
		{
			name: "english process map canvas",
			text: "Draw a process map on the canvas for a fusion reactor.",
			want: true,
		},
		{
			name: "german architecture canvas",
			text: "Zeichne eine Architekturübersicht des Reaktors auf der Canvas.",
			want: true,
		},
		{
			name: "open existing readme",
			text: "Display the README on canvas.",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := localAssistantCanvasShouldRenderGeneratedText(tt.text); got != tt.want {
				t.Fatalf("localAssistantCanvasShouldRenderGeneratedText(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestNormalizeLocalAssistantAddressSupportsVisibleNames(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{text: "Tabura, was ist los?", want: "was ist los?"},
		{text: "Sloppy: draw a diagram", want: "draw a diagram"},
		{text: "Computer; open the readme", want: "open the readme"},
	}
	for _, tt := range tests {
		if got := normalizeLocalAssistantAddress(tt.text); got != tt.want {
			t.Fatalf("normalizeLocalAssistantAddress(%q) = %q, want %q", tt.text, got, tt.want)
		}
	}
}

func TestLocalAssistantDirectOpenFileHint(t *testing.T) {
	tests := []struct {
		text   string
		family localAssistantToolFamily
		want   string
	}{
		{text: "Display the README on canvas.", family: localAssistantToolFamilyCanvas, want: "README"},
		{text: "Open docs/guide.md on canvas.", family: localAssistantToolFamilyCanvas, want: "docs/guide.md"},
		{text: "Öffne \"notes/summary.md\" auf der Canvas.", family: localAssistantToolFamilyCanvas, want: "notes/summary.md"},
		{text: "Draw a flowchart on the canvas.", family: localAssistantToolFamilyCanvas, want: ""},
	}
	for _, tt := range tests {
		if got := localAssistantDirectOpenFileHint(tt.text, tt.family); got != tt.want {
			t.Fatalf("localAssistantDirectOpenFileHint(%q, %q) = %q, want %q", tt.text, tt.family, got, tt.want)
		}
	}
}

func TestLocalAssistantToolUserPromptFallsBackToLatestUserMessage(t *testing.T) {
	req := &assistantTurnRequest{
		userText: "",
		messages: []store.ChatMessage{
			{Role: "user", ContentPlain: "Bitte zeichne mir wie ein Fusionsreaktor funktioniert als Flowchart auf der Canvas."},
		},
	}
	got := localAssistantToolUserPrompt(req, "## Canvas Position Events\n1. meeting_started")
	want := "Bitte zeichne mir wie ein Fusionsreaktor funktioniert als Flowchart auf der Canvas."
	if got != want {
		t.Fatalf("localAssistantToolUserPrompt() = %q, want %q", got, want)
	}
}

func TestLocalAssistantNeedsFullPromptContextKeepsLongReferentialFollowUps(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{text: "Mach es schöner, behalte das Flussdiagramm auf der Canvas und füge Magnetspulen hinzu.", want: true},
		{text: "Make it nicer, keep it on the canvas, and add magnets plus a turbine stage.", want: true},
		{text: "Please draw a flowchart on the canvas showing how a fusion reactor works.", want: false},
	}
	for _, tt := range tests {
		if got := localAssistantNeedsFullPromptContext(tt.text); got != tt.want {
			t.Fatalf("localAssistantNeedsFullPromptContext(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestBuildLocalAssistantCanvasPromptContextIncludesCanvasAndCurrentRequest(t *testing.T) {
	req := &assistantTurnRequest{
		userText: "Make it nicer, keep it on the canvas, and add magnets.",
		messages: []store.ChatMessage{
			{Role: "user", ContentPlain: "Draw a flowchart on the canvas showing how a fusion reactor works."},
			{Role: "assistant", ContentPlain: "Shown on canvas."},
			{Role: "user", ContentPlain: "Make it nicer, keep it on the canvas, and add magnets."},
		},
		canvasCtx: &canvasContext{
			HasArtifact:  true,
			ArtifactText: "[Fusion Reactor]\n  |\n[Plasma]\n  |\n[Turbine]",
		},
	}
	got := buildLocalAssistantCanvasPromptContext(req, "fallback prompt")
	for _, snippet := range []string{
		"Current canvas content:\n[Fusion Reactor]",
		"Recent conversation:",
		"Current request:\nMake it nicer, keep it on the canvas, and add magnets.",
	} {
		if !strings.Contains(got, snippet) {
			t.Fatalf("buildLocalAssistantCanvasPromptContext() missing %q:\n%s", snippet, got)
		}
	}
}
