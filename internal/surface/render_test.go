package surface

import (
	"strings"
	"testing"
)

func TestProtocolMarkdownIncludesRequiredMarkers(t *testing.T) {
	body := ProtocolBodyMarkdown()
	if !strings.Contains(body, "Tabura Codex Protocol") {
		t.Fatalf("ProtocolBodyMarkdown missing title")
	}
	if !strings.Contains(body, "canvas_session_open") {
		t.Fatalf("ProtocolBodyMarkdown missing MCP tool list")
	}

	block := ProtocolBlockMarkdown()
	if !strings.Contains(block, ProtocolBlockBeginMarker) {
		t.Fatalf("ProtocolBlockMarkdown missing begin marker")
	}
	if !strings.Contains(block, ProtocolBlockEndMarker) {
		t.Fatalf("ProtocolBlockMarkdown missing end marker")
	}
}

func TestDefaultAgentsMarkdownHasTopHeader(t *testing.T) {
	agents := DefaultAgentsMarkdown()
	if !strings.HasPrefix(agents, "# AGENTS") {
		t.Fatalf("DefaultAgentsMarkdown should start with # AGENTS")
	}
}

func TestInterfacesMarkdownIncludesKnownRoutesAndTools(t *testing.T) {
	doc := InterfacesMarkdown()
	if !strings.Contains(doc, "POST /mcp") {
		t.Fatalf("InterfacesMarkdown missing MCP route")
	}
	if !strings.Contains(doc, "GET /api/runtime") {
		t.Fatalf("InterfacesMarkdown missing runtime route")
	}
	if !strings.Contains(doc, "`delegate_to_model`") {
		t.Fatalf("InterfacesMarkdown missing delegate_to_model tool")
	}
}

func TestMCPToolNamesCSVIncludesBacktickedNames(t *testing.T) {
	csv := MCPToolNamesCSV()
	if !strings.Contains(csv, "`canvas_session_open`") {
		t.Fatalf("MCPToolNamesCSV missing canvas_session_open")
	}
	if !strings.Contains(csv, "`delegate_to_model_cancel_all`") {
		t.Fatalf("MCPToolNamesCSV missing delegate_to_model_cancel_all")
	}
}
