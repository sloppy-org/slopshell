package web

import (
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
			name: "german open file canvas",
			text: "Öffne bitte die README auf der Canvas.",
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
