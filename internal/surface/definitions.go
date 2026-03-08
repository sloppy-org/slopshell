package surface

import "strings"

type ToolProperty struct {
	Type        string
	Description string
	Enum        []string
}

type Tool struct {
	Name        string
	Description string
	Required    []string
	Properties  map[string]ToolProperty
}

type RouteSection struct {
	Title  string
	Routes []string
}

var MCPTools = []Tool{
	{
		Name:        "canvas_session_open",
		Description: "Open canvas session and initialize runtime status.",
		Required:    []string{"session_id"},
	},
	{
		Name:        "canvas_artifact_show",
		Description: "Show one artifact kind in canvas: text, image, pdf, or clear.",
		Required:    []string{"session_id", "kind"},
	},
	{
		Name:        "canvas_status",
		Description: "Get current session status and active artifact metadata.",
		Required:    []string{"session_id"},
	},
	{
		Name:        "canvas_import_handoff",
		Description: "Consume a generic producer handoff and render it in canvas.",
		Required:    []string{"session_id", "handoff_id"},
	},
	{
		Name:        "temp_file_create",
		Description: "Create a temporary file under .tabura/artifacts/tmp for file-backed canvas usage.",
		Properties: map[string]ToolProperty{
			"cwd": {
				Type:        "string",
				Description: "Project root to create the temp file under. Defaults to active project root.",
			},
			"prefix": {
				Type:        "string",
				Description: "Filename prefix for the generated temp file.",
			},
			"suffix": {
				Type:        "string",
				Description: "Filename suffix/extension (for example .md). Default is .md.",
			},
			"content": {
				Type:        "string",
				Description: "Optional initial file content.",
			},
		},
	},
	{
		Name:        "temp_file_remove",
		Description: "Remove a temporary file previously created under .tabura/artifacts/tmp.",
		Required:    []string{"path"},
		Properties: map[string]ToolProperty{
			"path": {
				Type:        "string",
				Description: "Relative or absolute temp file path to remove.",
			},
			"cwd": {
				Type:        "string",
				Description: "Project root for resolving relative paths. Defaults to active project root.",
			},
		},
	},
}

var MCPDaemonRoutes = []string{
	"POST /mcp",
	"GET /mcp",
	"DELETE /mcp",
	"GET /ws/canvas",
	"GET /files/*",
	"GET /health",
}

var WebRouteSections = []RouteSection{
	{
		Title: "Public pages",
		Routes: []string{
			"GET /",
			"GET /canvas",
			"GET /capture",
		},
	},
	{
		Title: "Auth and setup",
		Routes: []string{
			"GET /api/setup",
			"POST /api/setup",
			"POST /api/login",
			"POST /api/logout",
		},
	},
	{
		Title: "Runtime and chat session management",
		Routes: []string{
			"GET /api/runtime",
			"POST /api/runtime/yolo",
			"POST /api/runtime/disclaimer-ack",
			"GET /api/projects",
			"POST /api/projects",
			"POST /api/projects/{project_id}/activate",
			"GET /api/projects/{project_id}/context",
			"POST /api/chat/sessions",
			"GET /api/chat/sessions/{session_id}/history",
			"GET /api/chat/sessions/{session_id}/activity",
			"POST /api/chat/sessions/{session_id}/messages",
			"POST /api/chat/sessions/{session_id}/commands",
			"POST /api/chat/sessions/{session_id}/cancel",
		},
	},
	{
		Title: "Domain model API",
		Routes: []string{
			"GET /api/workspaces",
			"POST /api/workspaces",
			"GET /api/workspaces/{workspace_id}",
			"DELETE /api/workspaces/{workspace_id}",
			"GET /api/external-accounts",
			"POST /api/external-accounts",
			"PUT /api/external-accounts/{account_id}",
			"DELETE /api/external-accounts/{account_id}",
			"GET /api/actors",
			"POST /api/actors",
			"GET /api/actors/{actor_id}",
			"DELETE /api/actors/{actor_id}",
			"GET /api/artifacts",
			"POST /api/artifacts",
			"GET /api/artifacts/{artifact_id}",
			"DELETE /api/artifacts/{artifact_id}",
			"GET /api/items",
			"POST /api/items",
			"GET /api/items/{item_id}",
			"PUT /api/items/{item_id}",
			"DELETE /api/items/{item_id}",
			"PUT /api/items/{item_id}/state",
			"PUT /api/items/{item_id}/assign",
		},
	},
	{
		Title: "Canvas/files",
		Routes: []string{
			"GET /api/canvas/{session_id}/snapshot",
			"GET /api/files/{session_id}/*",
		},
	},
	{
		Title: "Websocket routes",
		Routes: []string{
			"GET /ws/chat/{session_id}",
			"GET /ws/canvas/{session_id}",
		},
	},
}

func MCPToolNamesCSV() string {
	names := make([]string, 0, len(MCPTools))
	for _, tool := range MCPTools {
		names = append(names, "`"+tool.Name+"`")
	}
	return strings.Join(names, ", ")
}
