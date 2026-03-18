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
	{
		Name:        "workspace_list",
		Description: "List workspaces, optionally filtered by the legacy work/private top-level context parameter.",
		Properties: map[string]ToolProperty{
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context filter.",
				Enum:        []string{"work", "private"},
			},
		},
	},
	{
		Name:        "workspace_activate",
		Description: "Set the active workspace.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to activate.",
			},
		},
	},
	{
		Name:        "workspace_get",
		Description: "Get workspace details and open item counts.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to inspect.",
			},
		},
	},
	{
		Name:        "workspace_watch_start",
		Description: "Enable watch mode for a workspace.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to watch.",
			},
			"poll_interval_seconds": {
				Type:        "integer",
				Description: "Optional polling interval in seconds.",
			},
			"config_json": {
				Type:        "string",
				Description: "Optional JSON config for worker selection.",
			},
		},
	},
	{
		Name:        "workspace_watch_stop",
		Description: "Disable watch mode for a workspace.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to stop watching.",
			},
		},
	},
	{
		Name:        "workspace_watch_status",
		Description: "Get persisted watch status for a workspace.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to inspect.",
			},
		},
	},
	{
		Name:        "item_list",
		Description: "List items, optionally filtered by state, workspace, legacy work/private top-level context, or source.",
		Properties: map[string]ToolProperty{
			"state": {
				Type:        "string",
				Description: "Optional item state filter.",
				Enum:        []string{"inbox", "waiting", "someday", "done"},
			},
			"workspace_id": {
				Type:        "integer",
				Description: "Optional workspace id filter. Use 0 for unassigned items.",
			},
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context filter.",
				Enum:        []string{"work", "private"},
			},
			"source": {
				Type:        "string",
				Description: "Optional source filter.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of items to return.",
			},
		},
	},
	{
		Name:        "item_get",
		Description: "Get an item with linked workspace, actor, and artifact details.",
		Required:    []string{"item_id"},
		Properties: map[string]ToolProperty{
			"item_id": {
				Type:        "integer",
				Description: "Item id to inspect.",
			},
		},
	},
	{
		Name:        "item_create",
		Description: "Create a new item with optional workspace, artifact, actor, and timing links.",
		Required:    []string{"title"},
		Properties: map[string]ToolProperty{
			"title": {
				Type:        "string",
				Description: "Item title.",
			},
			"state": {
				Type:        "string",
				Description: "Optional initial state. Defaults to inbox.",
				Enum:        []string{"inbox", "waiting", "someday", "done"},
			},
			"workspace_id": {
				Type:        "integer",
				Description: "Optional workspace id.",
			},
			"artifact_id": {
				Type:        "integer",
				Description: "Optional primary artifact id.",
			},
			"actor_id": {
				Type:        "integer",
				Description: "Optional actor id.",
			},
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context override.",
				Enum:        []string{"work", "private"},
			},
			"visible_after": {
				Type:        "string",
				Description: "Optional RFC3339 visibility timestamp.",
			},
			"follow_up_at": {
				Type:        "string",
				Description: "Optional RFC3339 follow-up timestamp.",
			},
			"source": {
				Type:        "string",
				Description: "Optional source provider name.",
			},
			"source_ref": {
				Type:        "string",
				Description: "Optional provider-specific source reference.",
			},
		},
	},
	{
		Name:        "item_triage",
		Description: "Triage an item to done, later, delegate, someday, or delete.",
		Required:    []string{"item_id", "action"},
		Properties: map[string]ToolProperty{
			"item_id": {
				Type:        "integer",
				Description: "Item id to triage.",
			},
			"action": {
				Type:        "string",
				Description: "Triage action.",
				Enum:        []string{"done", "later", "delegate", "someday", "delete"},
			},
			"actor_id": {
				Type:        "integer",
				Description: "Required when action=delegate.",
			},
			"visible_after": {
				Type:        "string",
				Description: "Required when action=later, in RFC3339 format.",
			},
		},
	},
	{
		Name:        "item_assign",
		Description: "Assign an item to an actor.",
		Required:    []string{"item_id", "actor_id"},
		Properties: map[string]ToolProperty{
			"item_id": {
				Type:        "integer",
				Description: "Item id to assign.",
			},
			"actor_id": {
				Type:        "integer",
				Description: "Actor id to assign.",
			},
		},
	},
	{
		Name:        "item_update",
		Description: "Update an item's title, state, links, source, or timing fields.",
		Required:    []string{"item_id"},
		Properties: map[string]ToolProperty{
			"item_id": {
				Type:        "integer",
				Description: "Item id to update.",
			},
			"title": {
				Type:        "string",
				Description: "Optional updated title.",
			},
			"state": {
				Type:        "string",
				Description: "Optional updated state.",
				Enum:        []string{"inbox", "waiting", "someday", "done"},
			},
			"workspace_id": {
				Type:        "integer",
				Description: "Optional workspace id. Use 0 to clear.",
			},
			"artifact_id": {
				Type:        "integer",
				Description: "Optional primary artifact id. Use 0 to clear.",
			},
			"actor_id": {
				Type:        "integer",
				Description: "Optional actor id. Use 0 to clear.",
			},
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context override.",
				Enum:        []string{"work", "private"},
			},
			"visible_after": {
				Type:        "string",
				Description: "Optional RFC3339 visibility timestamp.",
			},
			"follow_up_at": {
				Type:        "string",
				Description: "Optional RFC3339 follow-up timestamp.",
			},
			"source": {
				Type:        "string",
				Description: "Optional provider source name.",
			},
			"source_ref": {
				Type:        "string",
				Description: "Optional provider source reference.",
			},
		},
	},
	{
		Name:        "artifact_get",
		Description: "Get an artifact with linked items and readable local text content when available.",
		Required:    []string{"artifact_id"},
		Properties: map[string]ToolProperty{
			"artifact_id": {
				Type:        "integer",
				Description: "Artifact id to inspect.",
			},
		},
	},
	{
		Name:        "artifact_list",
		Description: "List artifacts, optionally filtered by kind or workspace.",
		Properties: map[string]ToolProperty{
			"kind": {
				Type:        "string",
				Description: "Optional artifact kind filter.",
				Enum:        []string{"email", "email_thread", "document", "pdf", "markdown", "image", "github_issue", "github_pr", "external_task", "transcript", "plan_note", "idea_note"},
			},
			"workspace_id": {
				Type:        "integer",
				Description: "Optional workspace id filter.",
			},
			"linked_only": {
				Type:        "boolean",
				Description: "Only include explicitly linked workspace artifacts.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of artifacts to return.",
			},
		},
	},
	{
		Name:        "actor_list",
		Description: "List actors.",
	},
	{
		Name:        "actor_create",
		Description: "Create an actor.",
		Required:    []string{"name", "kind"},
		Properties: map[string]ToolProperty{
			"name": {
				Type:        "string",
				Description: "Actor display name.",
			},
			"kind": {
				Type:        "string",
				Description: "Actor kind.",
				Enum:        []string{"human", "agent"},
			},
		},
	},
	{
		Name:        "calendar_list",
		Description: "List Google calendars available through Tabura's shared Google auth.",
	},
	{
		Name:        "calendar_events",
		Description: "List upcoming Google Calendar events with optional calendar, query, day-window, and limit filters.",
		Properties: map[string]ToolProperty{
			"calendar_id": {
				Type:        "string",
				Description: "Optional Google Calendar id. When omitted, events from all calendars are returned.",
			},
			"days": {
				Type:        "integer",
				Description: "Optional number of days forward from now to include. Defaults to 30.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of events to return. Defaults to 100.",
			},
			"query": {
				Type:        "string",
				Description: "Optional free-text search query applied by Google Calendar.",
			},
		},
	},
	{
		Name:        "mail_account_list",
		Description: "List enabled email accounts available through Tabura.",
		Properties: map[string]ToolProperty{
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context filter.",
				Enum:        []string{"work", "private"},
			},
		},
	},
	{
		Name:        "mail_label_list",
		Description: "List labels or folders for a mail account.",
		Required:    []string{"account_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
		},
	},
	{
		Name:        "mail_message_list",
		Description: "List messages from a mail account with mailbox filters and optional paging.",
		Required:    []string{"account_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"folder": {
				Type:        "string",
				Description: "Optional folder or label scope.",
			},
			"text": {
				Type:        "string",
				Description: "Optional free-text search.",
			},
			"subject": {
				Type:        "string",
				Description: "Optional subject filter.",
			},
			"from": {
				Type:        "string",
				Description: "Optional sender filter.",
			},
			"to": {
				Type:        "string",
				Description: "Optional recipient filter.",
			},
			"days": {
				Type:        "integer",
				Description: "Optional rolling day window from now.",
			},
			"after": {
				Type:        "string",
				Description: "Optional RFC3339 lower bound.",
			},
			"before": {
				Type:        "string",
				Description: "Optional RFC3339 upper bound.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of messages to return.",
			},
			"page_token": {
				Type:        "string",
				Description: "Optional provider page token for the next slice.",
			},
			"include_spam_trash": {
				Type:        "boolean",
				Description: "Include junk/spam/trash when supported.",
			},
			"has_attachment": {
				Type:        "boolean",
				Description: "Filter by attachment presence.",
			},
			"is_read": {
				Type:        "boolean",
				Description: "Filter by read state.",
			},
			"is_flagged": {
				Type:        "boolean",
				Description: "Filter by flagged state.",
			},
		},
	},
	{
		Name:        "mail_message_get",
		Description: "Get one full message from a mail account.",
		Required:    []string{"account_id", "message_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"message_id": {
				Type:        "string",
				Description: "Provider message id.",
			},
		},
	},
	{
		Name:        "mail_action",
		Description: "Apply one mailbox action to one or more messages.",
		Required:    []string{"account_id", "action"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"action": {
				Type:        "string",
				Description: "Mailbox action.",
				Enum:        []string{"mark_read", "mark_unread", "archive", "move_to_inbox", "trash", "delete", "move_to_folder", "apply_label", "archive_label"},
			},
			"message_id": {
				Type:        "string",
				Description: "Optional single provider message id.",
			},
			"message_ids": {
				Type:        "array",
				Description: "Optional list of provider message ids.",
			},
			"folder": {
				Type:        "string",
				Description: "Required for move_to_folder.",
			},
			"label": {
				Type:        "string",
				Description: "Required for apply_label or archive_label.",
			},
			"archive": {
				Type:        "boolean",
				Description: "Optional archive hint for apply_label.",
			},
		},
	},
	{
		Name:        "mail_server_filter_list",
		Description: "List provider-native server filters or rules when supported.",
		Required:    []string{"account_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
		},
	},
	{
		Name:        "mail_server_filter_upsert",
		Description: "Create or update a provider-native server filter when supported.",
		Required:    []string{"account_id", "filter"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"filter_id": {
				Type:        "string",
				Description: "Optional override filter id for updates.",
			},
			"filter": {
				Type:        "object",
				Description: "Server filter payload.",
			},
		},
	},
	{
		Name:        "mail_server_filter_delete",
		Description: "Delete a provider-native server filter when supported.",
		Required:    []string{"account_id", "filter_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"filter_id": {
				Type:        "string",
				Description: "Provider filter id.",
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
			"GET /api/live-policy",
			"POST /api/live-policy",
			"GET /api/plugins",
			"POST /api/plugins/meeting-partner/decide",
			"GET /api/extensions",
			"POST /api/extensions/commands/{command_id}",
			"GET /api/runtime/workspaces",
			"GET /api/runtime/workspaces/activity",
			"POST /api/runtime/workspaces",
			"POST /api/runtime/workspaces/{workspace_id}/activate",
			"POST /api/runtime/workspaces/{workspace_id}/persist",
			"POST /api/runtime/workspaces/{workspace_id}/discard",
			"POST /api/runtime/workspaces/{workspace_id}/chat-model",
			"GET /api/runtime/workspaces/{workspace_id}/snapshot",
			"GET /api/runtime/workspaces/{workspace_id}/welcome",
			"GET /api/workspaces/{workspace_id}/files",
			"GET /api/workspaces/{workspace_id}/companion/config",
			"PUT /api/workspaces/{workspace_id}/companion/config",
			"GET /api/workspaces/{workspace_id}/companion/state",
			"GET /api/workspaces/{workspace_id}/transcript",
			"GET /api/workspaces/{workspace_id}/summary",
			"GET /api/workspaces/{workspace_id}/references",
			"GET /api/workspaces/{workspace_id}/meeting-items",
			"POST /api/workspaces/{workspace_id}/meeting-items",
			"POST /api/chat/sessions",
			"GET /api/chat/sessions/{session_id}/history",
			"GET /api/chat/sessions/{session_id}/activity",
			"POST /api/chat/sessions/{session_id}/messages",
			"POST /api/chat/sessions/{session_id}/commands",
			"POST /api/chat/sessions/{session_id}/cancel",
			"GET /api/chat/sessions/{session_id}/dictation",
			"DELETE /api/chat/sessions/{session_id}/dictation",
			"POST /api/chat/sessions/{session_id}/dictation/start",
			"POST /api/chat/sessions/{session_id}/dictation/append",
			"PUT /api/chat/sessions/{session_id}/dictation/draft",
			"POST /api/ink/submit",
			"POST /api/review/submit",
			"POST /api/scan/upload",
			"POST /api/scan/confirm",
			"POST /api/bugs/report",
			"GET /api/mail/accounts",
			"POST /api/mail/drafts",
			"POST /api/mail/drafts/reply",
			"POST /api/mail/drafts/reply-all",
			"POST /api/mail/drafts/forward",
			"GET /api/mail/drafts/{artifact_id}",
			"PUT /api/mail/drafts/{artifact_id}",
			"POST /api/mail/drafts/{artifact_id}/send",
			"POST /api/mail/drafts/{artifact_id}/polish",
			"POST /api/text/polish",
			"GET /api/external-accounts/{account_id}/mail/labels",
			"GET /api/external-accounts/{account_id}/mail/messages",
			"GET /api/external-accounts/{account_id}/mail/messages/{message_id}",
			"POST /api/external-accounts/{account_id}/mail/actions",
			"GET /api/external-accounts/{account_id}/mail-rules",
			"POST /api/external-accounts/{account_id}/mail-rules",
			"PUT /api/external-accounts/{account_id}/mail-rules/{rule_id}",
			"DELETE /api/external-accounts/{account_id}/mail-rules/{rule_id}",
			"POST /api/external-accounts/{account_id}/mail-triage/preview",
			"GET /api/external-accounts/{account_id}/mail-triage/report",
			"POST /api/external-accounts/{account_id}/mail-triage/evaluate",
			"POST /api/external-accounts/{account_id}/mail-triage/arm",
			"POST /api/external-accounts/{account_id}/mail-triage/apply",
			"GET /api/external-accounts/{account_id}/mail-triage/manual/reviews",
			"POST /api/external-accounts/{account_id}/mail-triage/manual/reviews",
			"POST /api/external-accounts/{account_id}/mail-triage/manual/reviews/{review_id}/undo",
			"GET /api/external-accounts/{account_id}/mail-server-filters",
			"POST /api/external-accounts/{account_id}/mail-server-filters",
			"PUT /api/external-accounts/{account_id}/mail-server-filters/{filter_id}",
			"DELETE /api/external-accounts/{account_id}/mail-server-filters/{filter_id}",
		},
	},
	{
		Title: "Domain model API",
		Routes: []string{
			"GET /api/workspaces",
			"GET /api/workspaces/busy",
			"GET /api/workspace/focus",
			"GET /api/watches",
			"POST /api/workspaces",
			"POST /api/workspace/focus",
			"GET /api/workspaces/{workspace_id}",
			"PUT /api/workspaces/{workspace_id}",
			"GET /api/workspaces/{workspace_id}/watch",
			"POST /api/workspaces/{workspace_id}/watch",
			"DELETE /api/workspaces/{workspace_id}/watch",
			"DELETE /api/workspace/focus",
			"DELETE /api/workspaces/{workspace_id}",
			"GET /api/time-entries",
			"GET /api/time-entries/summary",
			"POST /api/time-entries/stamp-in",
			"POST /api/time-entries/stamp-out",
			"GET /api/spheres/{sphere}/accounts",
			"POST /api/spheres/{sphere}/accounts",
			"DELETE /api/spheres/{sphere}/accounts/{account_id}",
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
			"GET /api/labels",
			"POST /api/dialogue/diagnostics",
			"GET /api/artifacts",
			"POST /api/artifacts",
			"GET /api/artifacts/taxonomy",
			"GET /api/artifacts/{artifact_id}",
			"POST /api/artifacts/{artifact_id}/extract-figures",
			"POST /api/artifacts/{artifact_id}/materialize",
			"GET /api/artifacts/{artifact_id}/items",
			"DELETE /api/artifacts/{artifact_id}",
			"GET /api/batches",
			"GET /api/batches/{batch_id}",
			"GET /api/batches/{batch_id}/artifact",
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
			"POST /api/items/{item_id}/dispatch-review",
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
			"GET /ws/turn/{session_id}",
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
