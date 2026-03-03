package surface

import "strings"

const (
	ProtocolBlockBeginMarker = "<!-- TABURA_PROTOCOL:BEGIN -->"
	ProtocolBlockEndMarker   = "<!-- TABURA_PROTOCOL:END -->"
)

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
	{
		Name: "delegate_to_model",
		Description: "Start a delegated task asynchronously via the Codex app-server and return immediately with a job id. " +
			"Use delegate_to_model_status to stream progress and delegate_to_model_cancel to stop the job.",
		Required: []string{"prompt"},
		Properties: map[string]ToolProperty{
			"prompt": {
				Type:        "string",
				Description: "The task or question for the delegate model.",
			},
			"model": {
				Type:        "string",
				Description: "Model to use. Aliases: 'spark' (gpt-5.3-codex-spark), 'codex' (gpt-5.3-codex), 'gpt' (gpt-5.2). Defaults to 'codex'.",
				Enum:        []string{"spark", "codex", "gpt"},
			},
			"reasoning_effort": {
				Type:        "string",
				Description: "Reasoning effort for the delegate model. Allowed: low, medium, high, xhigh.",
				Enum:        []string{"low", "medium", "high", "xhigh"},
			},
			"context": {
				Type:        "string",
				Description: "Summary of the conversation so far, giving the delegate model background.",
			},
			"system_prompt": {
				Type:        "string",
				Description: "Task-specific instructions for the delegate model (e.g. 'You are a code reviewer. Be thorough.').",
			},
			"cwd": {
				Type:        "string",
				Description: "Working directory for the delegate model. Defaults to the active project directory.",
			},
			"timeout_seconds": {
				Type:        "integer",
				Description: "Maximum time in seconds for the delegate to complete. Defaults to 3600 (1 hour).",
			},
		},
	},
	{
		Name:        "delegate_to_model_status",
		Description: "Read asynchronous delegated task status and incremental progress events.",
		Required:    []string{"job_id"},
		Properties: map[string]ToolProperty{
			"job_id": {
				Type:        "string",
				Description: "Job id returned by delegate_to_model.",
			},
			"after_seq": {
				Type:        "integer",
				Description: "Only return events with seq greater than this value. Use 0 to fetch from the beginning.",
			},
			"max_events": {
				Type:        "integer",
				Description: "Maximum number of progress events to return. Defaults to 20.",
			},
		},
	},
	{
		Name:        "delegate_to_model_cancel",
		Description: "Cancel an asynchronous delegated task started with delegate_to_model.",
		Required:    []string{"job_id"},
		Properties: map[string]ToolProperty{
			"job_id": {
				Type:        "string",
				Description: "Job id returned by delegate_to_model.",
			},
		},
	},
	{
		Name:        "delegate_to_model_active_count",
		Description: "Return count of running delegated jobs, optionally scoped by cwd prefix.",
		Properties: map[string]ToolProperty{
			"cwd_prefix": {
				Type:        "string",
				Description: "Optional absolute cwd prefix to scope active job count.",
			},
		},
	},
	{
		Name:        "delegate_to_model_cancel_all",
		Description: "Cancel all running delegated jobs, optionally scoped by cwd prefix.",
		Properties: map[string]ToolProperty{
			"cwd_prefix": {
				Type:        "string",
				Description: "Optional absolute cwd prefix to scope canceled jobs.",
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
			"POST /api/chat/sessions/{session_id}/cancel-delegates",
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
