package web

import (
	"fmt"
	"slices"
	"strings"
)

type localAssistantToolKind string

const (
	localAssistantToolKindShell                localAssistantToolKind = "shell"
	localAssistantToolKindSystemAction         localAssistantToolKind = "system_action"
	localAssistantToolKindMCP                  localAssistantToolKind = "mcp"
	localAssistantToolKindWorkspaceRead        localAssistantToolKind = "workspace_read"
	localAssistantToolKindCanvasWriteText      localAssistantToolKind = "canvas_write_text"
	localAssistantToolKindWebSearchUnavailable localAssistantToolKind = "web_search_unavailable"
)

type localAssistantExecutableTool struct {
	ModelName    string
	Kind         localAssistantToolKind
	InternalName string
	DefaultArgs  map[string]any
	Definition   map[string]any
}

type localAssistantToolCatalog struct {
	Family              localAssistantToolFamily
	RenderGeneratedText bool
	Definitions         []map[string]any
	ToolsByName         map[string]localAssistantExecutableTool
}

func (a *App) buildLocalAssistantToolCatalog(state localAssistantTurnState, family localAssistantToolFamily, userText string) (localAssistantToolCatalog, error) {
	out := localAssistantToolCatalog{
		Family:              family,
		RenderGeneratedText: family == localAssistantToolFamilyCanvas && localAssistantCanvasShouldRenderGeneratedText(userText),
		Definitions:         nil,
		ToolsByName:         map[string]localAssistantExecutableTool{},
	}
	for _, tool := range localAssistantCoreTools(state, family) {
		out.add(tool)
	}
	if !localAssistantFamilyNeedsMCP(family) || strings.TrimSpace(state.mcpURL) == "" {
		return out, nil
	}
	mcpTools, err := mcpToolsListURL(state.mcpURL)
	if err != nil {
		return localAssistantToolCatalog{}, err
	}
	for _, tool := range localAssistantMCPToolsForFamily(state, mcpTools, family) {
		out.add(tool)
	}
	return out, nil
}

func localAssistantCoreTools(state localAssistantTurnState, family localAssistantToolFamily) []localAssistantExecutableTool {
	switch family {
	case localAssistantToolFamilyCanvas:
		return []localAssistantExecutableTool{
			localAssistantWorkspaceReadTool(),
			localAssistantSystemActionTool("open_file_canvas"),
			localAssistantSystemActionTool("navigate_canvas"),
		}
	case localAssistantToolFamilyWorkspace:
		return []localAssistantExecutableTool{
			localAssistantWorkspaceReadTool(),
			localAssistantSystemActionTool("open_file_canvas"),
		}
	case localAssistantToolFamilyShell:
		return []localAssistantExecutableTool{
			localAssistantShellTool(),
			localAssistantSystemActionTool("open_file_canvas"),
		}
	case localAssistantToolFamilyRuntime:
		return []localAssistantExecutableTool{
			localAssistantSystemActionTool("toggle_silent"),
			localAssistantSystemActionTool("toggle_live_dialogue"),
			localAssistantSystemActionTool("show_status"),
			localAssistantSystemActionTool("show_busy_state"),
			localAssistantSystemActionTool("cancel_work"),
		}
	case localAssistantToolFamilyWeb:
		return []localAssistantExecutableTool{
			localAssistantWebSearchUnavailableTool(),
		}
	default:
		return nil
	}
}

func localAssistantFamilyNeedsMCP(family localAssistantToolFamily) bool {
	switch family {
	case localAssistantToolFamilyMail, localAssistantToolFamilyCalendar, localAssistantToolFamilyItems:
		return true
	default:
		return false
	}
}

func localAssistantWorkspaceReadTool() localAssistantExecutableTool {
	return localAssistantExecutableTool{
		ModelName:    "workspace_read",
		Kind:         localAssistantToolKindWorkspaceRead,
		InternalName: "workspace_read",
		Definition: map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "workspace_read",
				"description": "Inspect workspace files without using a shell. Use this to list top-level entries, read a file, or find a matching file path.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"operation": map[string]any{
							"type":        "string",
							"description": "One of list_top_level, read_file, or find_file.",
						},
						"path": map[string]any{
							"type":        "string",
							"description": "Relative or absolute workspace path for read_file.",
						},
						"query": map[string]any{
							"type":        "string",
							"description": "Loose file query for find_file, such as README or local tools.",
						},
						"max_results": map[string]any{
							"type":        "integer",
							"description": "Optional limit for find_file results.",
						},
					},
				},
			},
		},
	}
}

func localAssistantCanvasWriteTextTool(state localAssistantTurnState) localAssistantExecutableTool {
	if strings.TrimSpace(state.canvasID) == "" {
		return localAssistantExecutableTool{}
	}
	return localAssistantExecutableTool{
		ModelName:    "canvas_write_text",
		Kind:         localAssistantToolKindCanvasWriteText,
		InternalName: "canvas_artifact_show",
		DefaultArgs: map[string]any{
			"session_id": state.canvasID,
			"kind":       "text",
		},
		Definition: map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "canvas_write_text",
				"description": "Show new text on canvas. Use this for notes, ASCII diagrams, schematics, flowcharts, sketches, or any generated text artifact.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title": map[string]any{
							"type":        "string",
							"description": "Optional canvas title.",
						},
						"content": map[string]any{
							"type":        "string",
							"description": "The exact text to place on canvas.",
						},
					},
					"required": []string{"content"},
				},
			},
		},
	}
}

func localAssistantShellTool() localAssistantExecutableTool {
	return localAssistantExecutableTool{
		ModelName:    "shell",
		Kind:         localAssistantToolKindShell,
		InternalName: "shell",
		Definition: map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "shell",
				"description": "Run a shell command inside the active workspace. Use this only for explicit shell or terminal requests.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{
							"type":        "string",
							"description": "Shell command to execute.",
						},
						"cwd": map[string]any{
							"type":        "string",
							"description": "Optional relative or absolute directory inside the active workspace.",
						},
					},
					"required": []string{"command"},
				},
			},
		},
	}
}

func localAssistantWebSearchUnavailableTool() localAssistantExecutableTool {
	return localAssistantExecutableTool{
		ModelName:    "web_search_unavailable",
		Kind:         localAssistantToolKindWebSearchUnavailable,
		InternalName: "web_search_unavailable",
		Definition: map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "web_search_unavailable",
				"description": "Call this when the user asks for websites, latest news, or web search. Local mode cannot browse websites yet.",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{
							"type":        "string",
							"description": "The requested website lookup or web search query.",
						},
					},
				},
			},
		},
	}
}

func localAssistantSystemActionTool(action string) localAssistantExecutableTool {
	action = strings.TrimSpace(action)
	if action == "" {
		return localAssistantExecutableTool{}
	}
	return localAssistantExecutableTool{
		ModelName:    "action__" + action,
		Kind:         localAssistantToolKindSystemAction,
		InternalName: action,
		Definition: map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "action__" + action,
				"description": localAssistantSystemActionDescription(action),
				"parameters":  localAssistantSystemActionSchema(action),
			},
		},
	}
}

func localAssistantSystemActionDescription(action string) string {
	switch action {
	case "open_file_canvas":
		return "Open an existing workspace file on canvas."
	case "navigate_canvas":
		return "Move between visible canvas pages or artifacts."
	case "toggle_silent":
		return "Toggle silent mode."
	case "toggle_live_dialogue":
		return "Toggle dialogue or meeting live mode."
	case "show_status":
		return "Show the current runtime or workspace status."
	case "show_busy_state":
		return "Show the current busy state."
	case "cancel_work":
		return "Stop the active assistant turn or work loop."
	default:
		return "Execute native Tabura action " + action + "."
	}
}

func localAssistantSystemActionSchema(action string) map[string]any {
	switch action {
	case "toggle_silent", "toggle_live_dialogue", "cancel_work", "show_busy_state", "show_status":
		return map[string]any{"type": "object"}
	case "open_file_canvas":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":   map[string]any{"type": "string", "description": "Relative or absolute path to open."},
				"file":   map[string]any{"type": "string", "description": "Alternate file path field."},
				"target": map[string]any{"type": "string", "description": "Alternate target path field."},
			},
		}
	case "navigate_canvas":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"direction": map[string]any{"type": "string", "description": "Navigation direction such as next or previous."},
				"scope":     map[string]any{"type": "string", "description": "Optional navigation scope."},
			},
			"required": []string{"direction"},
		}
	default:
		return map[string]any{"type": "object"}
	}
}

func localAssistantMCPToolsForFamily(state localAssistantTurnState, tools []mcpListedTool, family localAssistantToolFamily) []localAssistantExecutableTool {
	out := make([]localAssistantExecutableTool, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" || !localAssistantIncludeMCPToolForFamily(tool.Name, family) {
			continue
		}
		defaultArgs := localAssistantMCPDefaultArgs(state, tool.Name)
		out = append(out, localAssistantExecutableTool{
			ModelName:    localAssistantMCPModelName(tool.Name),
			Kind:         localAssistantToolKindMCP,
			InternalName: tool.Name,
			DefaultArgs:  defaultArgs,
			Definition: map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        localAssistantMCPModelName(tool.Name),
					"description": localAssistantMCPDescription(tool.Name, tool.Description, defaultArgs),
					"parameters":  localAssistantVisibleSchema(tool.InputSchema, defaultArgs),
				},
			},
		})
	}
	return out
}

func localAssistantIncludeMCPToolForFamily(name string, family localAssistantToolFamily) bool {
	switch family {
	case localAssistantToolFamilyMail:
		return strings.HasPrefix(name, "mail_") || strings.HasPrefix(name, "handoff_")
	case localAssistantToolFamilyCalendar:
		return strings.HasPrefix(name, "calendar_")
	case localAssistantToolFamilyItems:
		return strings.HasPrefix(name, "item_") || strings.HasPrefix(name, "actor_")
	default:
		return false
	}
}

func localAssistantMCPDefaultArgs(state localAssistantTurnState, name string) map[string]any {
	defaults := map[string]any{}
	switch name {
	case "temp_file_create", "temp_file_remove":
		defaults["cwd"] = state.workspaceDir
	}
	return defaults
}

func localAssistantMCPModelName(name string) string {
	return "mcp__" + sanitizeLocalAssistantToolToken(name)
}

func sanitizeLocalAssistantToolToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func localAssistantMCPDescription(name, description string, defaults map[string]any) string {
	desc := strings.TrimSpace(description)
	if len(defaults) == 0 {
		return desc
	}
	keys := make([]string, 0, len(defaults))
	for key := range defaults {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return strings.TrimSpace(desc + " Runtime-bound arguments: " + strings.Join(keys, ", ") + ".")
}

func localAssistantVisibleSchema(schema map[string]any, defaults map[string]any) map[string]any {
	out := cloneLocalAssistantMap(schema)
	if out == nil {
		out = map[string]any{"type": "object"}
	}
	props, _ := out["properties"].(map[string]any)
	if props != nil {
		props = cloneLocalAssistantMap(props)
		for key := range defaults {
			delete(props, key)
		}
		if len(props) > 0 {
			out["properties"] = props
		} else {
			delete(out, "properties")
		}
	}
	switch required := out["required"].(type) {
	case []any:
		filtered := make([]any, 0, len(required))
		for _, item := range required {
			key := strings.TrimSpace(fmt.Sprint(item))
			if key == "" {
				continue
			}
			if _, hidden := defaults[key]; hidden {
				continue
			}
			filtered = append(filtered, key)
		}
		if len(filtered) > 0 {
			out["required"] = filtered
		} else {
			delete(out, "required")
		}
	case []string:
		filtered := make([]string, 0, len(required))
		for _, key := range required {
			if _, hidden := defaults[key]; hidden {
				continue
			}
			filtered = append(filtered, key)
		}
		if len(filtered) > 0 {
			out["required"] = filtered
		} else {
			delete(out, "required")
		}
	}
	out["type"] = "object"
	return out
}

func cloneLocalAssistantMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = cloneLocalAssistantMap(typed)
		default:
			out[key] = value
		}
	}
	return out
}

func mergeLocalAssistantToolArguments(defaults, args map[string]any) map[string]any {
	out := make(map[string]any, len(defaults)+len(args))
	for key, value := range args {
		out[key] = value
	}
	for key, value := range defaults {
		out[key] = value
	}
	return out
}

func buildLocalAssistantToolPolicy(catalog localAssistantToolCatalog) string {
	if len(catalog.Definitions) == 0 {
		return "No tools are available in this turn. Answer directly."
	}
	lines := []string{
		"Tool protocol:",
		"- Either answer with plain text or return JSON only in this exact form: {\"tool_calls\":[{\"name\":\"tool_name\",\"arguments\":{...}}]}.",
		"- Never mix prose with tool JSON.",
		"- After tool results arrive, either call another tool with the same JSON shape or answer plainly.",
	}
	if catalog.Family == localAssistantToolFamilyCanvas {
		lines = append(lines, localAssistantCanvasPolicyLines(catalog.RenderGeneratedText)...)
	} else {
		lines = append(lines, localAssistantFamilyPolicyLines(catalog.Family)...)
	}
	lines = append(lines, localAssistantToolCatalogLines(catalog)...)
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func localAssistantFamilyPolicyLines(family localAssistantToolFamily) []string {
	switch family {
	case localAssistantToolFamilyCanvas:
		return localAssistantCanvasPolicyLines(false)
	case localAssistantToolFamilyWorkspace:
		return []string{
			"- This is a workspace file request.",
			"- The first valid response must use the workspace tool when inspection is needed.",
			"- Use workspace_read instead of shell for listing files, reading files, or finding a path.",
			"- Use action__open_file_canvas only when the user wants an existing file shown on canvas.",
		}
	case localAssistantToolFamilyShell:
		return []string{
			"- This is an explicit terminal or shell request.",
			"- Use shell directly. Do not invent higher-level wrappers.",
		}
	case localAssistantToolFamilyMail:
		return []string{"- This is a mail request. Use only the listed mail tools."}
	case localAssistantToolFamilyCalendar:
		return []string{"- This is a calendar request. Use only the listed calendar tools."}
	case localAssistantToolFamilyItems:
		return []string{"- This is an items or task request. Use only the listed item tools."}
	case localAssistantToolFamilyRuntime:
		return []string{"- This is a runtime control request. Use only the listed runtime action tools."}
	case localAssistantToolFamilyWeb:
		return []string{"- This is a web request. Call web_search_unavailable, then explain the limitation briefly."}
	default:
		return nil
	}
}

func localAssistantCanvasPolicyLines(directRender bool) []string {
	if directRender {
		return []string{
			"- This is a canvas request.",
			"- The user wants new generated content on canvas.",
			"- Reply with the exact canvas text only. Do not return tool JSON, promises, or explanations.",
			"- Do not add any intro or outro. Write only the artifact body that should appear on canvas.",
			"- Use action__open_file_canvas only for an existing workspace file.",
			"- Use workspace_read first only if you genuinely need to inspect or find a file path.",
		}
	}
	return []string{
		"- This is a canvas request.",
		"- The first valid response must use a tool, not a promise or explanation.",
		"- Use action__open_file_canvas only for an existing workspace file.",
		"- Use workspace_read first only if you genuinely need to inspect or find a file path.",
	}
}

func localAssistantCanvasShouldRenderGeneratedText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(normalizeLocalAssistantAddress(text)))
	if lower == "" {
		return false
	}
	if containsAnyLocalAssistantKeyword(lower,
		"readme", "pdf", "image", "document", "file", "path",
		"datei", "dokument", "bild", "pfad",
	) {
		return false
	}
	return containsAnyLocalAssistantKeyword(lower,
		"canvas", "draw ", "render ", "display ", "show ", "sketch", "diagram", "flowchart", "schematic",
		"zeichne", "rendere", "darstell", "skizziere", "diagramm", "flussdiagramm", "schema", "schaubild",
	)
}

func localAssistantToolCatalogLines(catalog localAssistantToolCatalog) []string {
	if len(catalog.ToolsByName) == 0 {
		return nil
	}
	names := make([]string, 0, len(catalog.ToolsByName))
	for name := range catalog.ToolsByName {
		names = append(names, name)
	}
	slices.Sort(names)
	lines := []string{"Available tools in this turn:"}
	for _, name := range names {
		tool := catalog.ToolsByName[name]
		lines = append(lines, fmt.Sprintf("- %s: %s", name, localAssistantToolSummary(tool)))
	}
	return lines
}

func localAssistantToolSummary(tool localAssistantExecutableTool) string {
	function, _ := tool.Definition["function"].(map[string]any)
	desc := strings.TrimSpace(fmt.Sprint(function["description"]))
	desc = strings.ReplaceAll(desc, "\n", " ")
	desc = strings.Join(strings.Fields(desc), " ")
	if desc == "" || desc == "<nil>" {
		return "Execute this tool when needed."
	}
	return desc
}

func (c *localAssistantToolCatalog) add(tool localAssistantExecutableTool) {
	if c == nil || strings.TrimSpace(tool.ModelName) == "" || tool.Definition == nil {
		return
	}
	c.Definitions = append(c.Definitions, tool.Definition)
	c.ToolsByName[tool.ModelName] = tool
}
