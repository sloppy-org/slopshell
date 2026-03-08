package web

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

func parseInlineWorkspaceIntent(text string) *SystemAction {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	lower := normalizeItemCommandText(trimmed)
	switch lower {
	case "show items here", "show open items here", "show items for this workspace", "what's open", "what is open", "what's open here", "what is open here":
		return &SystemAction{Action: "list_workspace_items", Params: map[string]interface{}{}}
	}

	if name, ok := cutPrefixedWorkspaceReference(trimmed, "open workspace "); ok {
		return &SystemAction{Action: "switch_workspace", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "switch to workspace "); ok {
		return &SystemAction{Action: "switch_workspace", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "switch workspace to "); ok {
		return &SystemAction{Action: "switch_workspace", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "show items for workspace "); ok {
		return &SystemAction{Action: "list_workspace_items", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "show items for "); ok && looksLikeWorkspaceReference(name) {
		return &SystemAction{Action: "list_workspace_items", Params: map[string]interface{}{"workspace": name}}
	}
	if name, ok := cutPrefixedWorkspaceReference(trimmed, "switch to "); ok && looksLikeWorkspaceReference(name) {
		return &SystemAction{Action: "switch_workspace", Params: map[string]interface{}{"workspace": name}}
	}
	return nil
}

func cutPrefixedWorkspaceReference(text, prefix string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(strings.ToLower(trimmed), prefix) {
		return "", false
	}
	value := strings.TrimSpace(trimmed[len(prefix):])
	value = strings.TrimRight(value, " \t\r\n!?,:;.")
	if value == "" {
		return "", false
	}
	return value, true
}

func looksLikeWorkspaceReference(raw string) bool {
	value := strings.TrimSpace(raw)
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, `/\~.`) {
		return true
	}
	if strings.EqualFold(value, "here") || strings.EqualFold(value, "this workspace") {
		return true
	}
	return false
}

func systemActionWorkspaceRef(params map[string]interface{}) string {
	for _, key := range []string{"workspace", "name", "path", "target"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func expandWorkspacePathReference(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return value
		}
		if value == "~" {
			return home
		}
		return filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	return value
}

func (a *App) fallbackWorkspaceForProjectKey(projectKey string) (*store.Workspace, error) {
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	for i := range workspaces {
		if workspaces[i].IsActive {
			return &workspaces[i], nil
		}
	}
	key := strings.TrimSpace(projectKey)
	if key == "" {
		return nil, nil
	}
	project, err := a.store.GetProjectByProjectKey(key)
	if err != nil {
		return nil, nil
	}
	workspaceID, err := a.store.FindWorkspaceContainingPath(project.RootPath)
	if err != nil || workspaceID == nil {
		return nil, err
	}
	workspace, err := a.store.GetWorkspace(*workspaceID)
	if err != nil {
		return nil, err
	}
	return &workspace, nil
}

func (a *App) resolveWorkspaceReference(projectKey string, raw string) (store.Workspace, error) {
	ref := strings.TrimSpace(raw)
	if ref == "" || strings.EqualFold(ref, "here") || strings.EqualFold(ref, "this workspace") {
		workspace, err := a.fallbackWorkspaceForProjectKey(projectKey)
		if err != nil {
			return store.Workspace{}, err
		}
		if workspace == nil {
			return store.Workspace{}, errors.New("no active workspace")
		}
		return *workspace, nil
	}

	expanded := expandWorkspacePathReference(ref)
	if strings.ContainsAny(expanded, `/\~.`) {
		if workspace, err := a.store.GetWorkspaceByPath(expanded); err == nil {
			return workspace, nil
		}
		if workspaceID, err := a.store.FindWorkspaceContainingPath(expanded); err == nil && workspaceID != nil {
			return a.store.GetWorkspace(*workspaceID)
		}
	}

	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return store.Workspace{}, err
	}
	var matches []store.Workspace
	for _, workspace := range workspaces {
		switch {
		case strings.EqualFold(workspace.Name, ref):
			matches = append(matches, workspace)
		case strings.EqualFold(filepath.Base(workspace.DirPath), ref):
			matches = append(matches, workspace)
		case strings.EqualFold(workspace.DirPath, expanded):
			matches = append(matches, workspace)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].IsActive != matches[j].IsActive {
				return matches[i].IsActive
			}
			return strings.ToLower(matches[i].Name) < strings.ToLower(matches[j].Name)
		})
		return store.Workspace{}, fmt.Errorf("workspace %q is ambiguous", ref)
	}
	return store.Workspace{}, fmt.Errorf("workspace %q not found", ref)
}

func (a *App) listOpenWorkspaceItems(workspaceID int64) ([]store.Item, error) {
	items, err := a.store.ListItems()
	if err != nil {
		return nil, err
	}
	out := make([]store.Item, 0, len(items))
	for _, item := range items {
		if item.WorkspaceID == nil || *item.WorkspaceID != workspaceID {
			continue
		}
		if item.State == store.ItemStateDone {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func summarizeWorkspaceItems(workspace store.Workspace, items []store.Item) string {
	if len(items) == 0 {
		return fmt.Sprintf("No open items for workspace %s.", workspace.Name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Open items for workspace %s:\n", workspace.Name)
	for _, item := range items {
		fmt.Fprintf(&b, "- %s [%s]\n", item.Title, item.State)
	}
	return strings.TrimSpace(b.String())
}

type workspacePromptContext struct {
	Workspace     store.Workspace
	OpenItemCount int
}

func (a *App) loadWorkspacePromptContext(projectKey string) *workspacePromptContext {
	workspace, err := a.fallbackWorkspaceForProjectKey(projectKey)
	if err != nil || workspace == nil {
		return nil
	}
	items, err := a.listOpenWorkspaceItems(workspace.ID)
	if err != nil {
		return nil
	}
	return &workspacePromptContext{
		Workspace:     *workspace,
		OpenItemCount: len(items),
	}
}

func prependWorkspacePromptContext(prompt string, ctx *workspacePromptContext) string {
	if ctx == nil || strings.TrimSpace(prompt) == "" {
		return prompt
	}
	var b strings.Builder
	b.WriteString("## Workspace Context\n")
	fmt.Fprintf(&b, "Active workspace: %s (%s)\n", ctx.Workspace.Name, ctx.Workspace.DirPath)
	fmt.Fprintf(&b, "Open items in this workspace: %d\n\n", ctx.OpenItemCount)
	b.WriteString(prompt)
	return b.String()
}

func (a *App) applyWorkspacePromptContext(projectKey, prompt string) string {
	return prependWorkspacePromptContext(prompt, a.loadWorkspacePromptContext(projectKey))
}
