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
			"POST /api/login",
			"POST /api/logout",
		},
	},
	{
		Title: "Runtime and chat session management",
		Routes: []string{
			"GET /api/runtime",
			"PATCH /api/runtime/preferences",
			"POST /api/runtime/yolo",
			"POST /api/runtime/disclaimer-ack",
			"GET /api/plugins",
			"POST /api/plugins/meeting-partner/decide",
			"GET /api/extensions",
			"POST /api/extensions/commands/{command_id}",
			"GET /api/projects",
			"GET /api/projects/activity",
			"POST /api/projects",
			"POST /api/projects/{project_id}/activate",
			"POST /api/projects/{project_id}/persist",
			"POST /api/projects/{project_id}/discard",
			"POST /api/projects/{project_id}/chat-model",
			"GET /api/projects/{project_id}/context",
			"GET /api/projects/{project_id}/files",
			"GET /api/projects/{project_id}/welcome",
			"GET /api/projects/{project_id}/companion/config",
			"PUT /api/projects/{project_id}/companion/config",
			"GET /api/projects/{project_id}/companion/state",
			"GET /api/projects/{project_id}/transcript",
			"GET /api/projects/{project_id}/summary",
			"GET /api/projects/{project_id}/references",
			"GET /api/projects/{project_id}/meeting-items",
			"POST /api/projects/{project_id}/meeting-items",
			"POST /api/chat/sessions",
			"GET /api/chat/sessions/{session_id}/history",
			"GET /api/chat/sessions/{session_id}/activity",
			"POST /api/chat/sessions/{session_id}/messages",
			"POST /api/chat/sessions/{session_id}/commands",
			"POST /api/chat/sessions/{session_id}/cancel",
			"POST /api/ink/submit",
			"POST /api/review/submit",
			"POST /api/bugs/report",
		},
	},
	{
		Title: "Domain model API",
		Routes: []string{
			"GET /api/workspaces",
			"POST /api/workspaces",
			"GET /api/workspaces/{workspace_id}",
			"PUT /api/workspaces/{workspace_id}",
			"DELETE /api/workspaces/{workspace_id}",
			"GET /api/external-accounts",
			"POST /api/external-accounts",
			"PUT /api/external-accounts/{account_id}",
			"DELETE /api/external-accounts/{account_id}",
			"GET /api/container-mappings",
			"POST /api/container-mappings",
			"DELETE /api/container-mappings/{mapping_id}",
			"GET /api/actors",
			"POST /api/actors",
			"GET /api/actors/{actor_id}",
			"DELETE /api/actors/{actor_id}",
			"GET /api/artifacts",
			"POST /api/artifacts",
			"GET /api/artifacts/{artifact_id}",
			"GET /api/artifacts/{artifact_id}/items",
			"DELETE /api/artifacts/{artifact_id}",
			"GET /api/items",
			"POST /api/items",
			"GET /api/items/inbox",
			"GET /api/items/waiting",
			"GET /api/items/someday",
			"GET /api/items/done",
			"GET /api/items/counts",
			"POST /api/items/sync/github",
			"POST /api/items/sync/github/reviews",
			"GET /api/items/{item_id}",
			"GET /api/items/{item_id}/artifacts",
			"POST /api/items/{item_id}/artifacts",
			"DELETE /api/items/{item_id}/artifacts/{artifact_id}",
			"PUT /api/items/{item_id}",
			"DELETE /api/items/{item_id}",
			"PUT /api/items/{item_id}/state",
			"PUT /api/items/{item_id}/assign",
			"PUT /api/items/{item_id}/unassign",
			"PUT /api/items/{item_id}/complete",
			"PUT /api/items/{item_id}/workspace",
			"PUT /api/items/{item_id}/project",
			"POST /api/items/{item_id}/triage",
			"GET /api/items/{item_id}/print",
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
	{
		Title: "Participant and STT APIs",
		Routes: []string{
			"GET /api/participant/config",
			"PUT /api/participant/config",
			"GET /api/participant/status",
			"GET /api/participant/sessions",
			"GET /api/participant/sessions/{id}/transcript",
			"GET /api/participant/sessions/{id}/search",
			"GET /api/participant/sessions/{id}/export",
			"POST /api/stt/transcribe",
			"GET /api/stt/config",
			"PUT /api/stt/config",
			"GET /api/stt/replacements",
			"PUT /api/stt/replacements",
			"GET /api/hotword/status",
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
