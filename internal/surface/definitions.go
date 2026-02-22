package surface

import "strings"

const (
	ProtocolBlockBeginMarker = "<!-- TABURA_PROTOCOL:BEGIN -->"
	ProtocolBlockEndMarker   = "<!-- TABURA_PROTOCOL:END -->"
)

type Tool struct {
	Name        string
	Description string
	Required    []string
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
		Name:        "canvas_mark_set",
		Description: "Create or update a mark (selection/annotation) on the active artifact.",
		Required:    []string{"session_id", "intent", "type", "target_kind", "target"},
	},
	{
		Name:        "canvas_mark_delete",
		Description: "Delete a mark by id.",
		Required:    []string{"session_id", "mark_id"},
	},
	{
		Name:        "canvas_marks_list",
		Description: "List marks for a session, optionally filtered by artifact/intent.",
		Required:    []string{"session_id"},
	},
	{
		Name:        "canvas_mark_focus",
		Description: "Set or clear currently focused mark.",
		Required:    []string{"session_id"},
	},
	{
		Name:        "canvas_commit",
		Description: "Commit draft marks to persistent annotations and write sidecar/PDF annotations.",
		Required:    []string{"session_id"},
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
		Title: "Canvas/files",
		Routes: []string{
			"GET /api/canvas/{session_id}/snapshot",
			"POST /api/canvas/{session_id}/commit",
			"GET /api/files/{session_id}/*",
		},
	},
	{
		Title: "Mail interaction endpoints",
		Routes: []string{
			"POST /api/mail/action-capabilities",
			"POST /api/mail/read",
			"POST /api/mail/mark-read",
			"POST /api/mail/action",
			"POST /api/mail/draft-reply",
			"POST /api/mail/draft-intent",
			"POST /api/mail/stt",
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
