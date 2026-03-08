package web

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

var (
	artifactLinkPattern        = regexp.MustCompile(`(?i)^link\s+(.+?)\s+from\s+(.+?)\s+to\s+(.+?)$`)
	showLinkedArtifactsPattern = regexp.MustCompile(`(?i)^(?:show|list)\s+linked\s+artifacts(?:\s+for\s+(.+?))?$`)
)

func parseInlineArtifactLinkIntent(text string) *SystemAction {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if match := artifactLinkPattern.FindStringSubmatch(trimmed); len(match) == 4 {
		artifactRef := cleanArtifactReference(match[1])
		sourceWorkspace := cleanArtifactReference(match[2])
		targetWorkspace := cleanArtifactReference(match[3])
		if artifactRef != "" && sourceWorkspace != "" && targetWorkspace != "" {
			return &SystemAction{
				Action: "link_workspace_artifact",
				Params: map[string]interface{}{
					"artifact":         artifactRef,
					"source_workspace": sourceWorkspace,
					"target_workspace": targetWorkspace,
				},
			}
		}
	}
	if match := showLinkedArtifactsPattern.FindStringSubmatch(trimmed); len(match) == 2 {
		params := map[string]interface{}{}
		if workspaceRef := cleanArtifactReference(match[1]); workspaceRef != "" {
			params["workspace"] = workspaceRef
		}
		return &SystemAction{Action: "list_linked_artifacts", Params: params}
	}
	return nil
}

func cleanArtifactReference(raw string) string {
	text := strings.TrimSpace(raw)
	text = strings.Trim(text, " \t\r\n!?,:;.")
	text = strings.Trim(text, `"'`)
	return strings.TrimSpace(text)
}

func systemActionArtifactRef(params map[string]interface{}) string {
	for _, key := range []string{"artifact", "title", "path", "name"} {
		value := strings.TrimSpace(fmt.Sprint(params[key]))
		if value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func optionalArtifactString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func artifactMatchesReference(artifact store.Artifact, ref string) bool {
	if strings.EqualFold(optionalArtifactString(artifact.Title), ref) {
		return true
	}
	if strings.EqualFold(optionalArtifactString(artifact.RefPath), ref) {
		return true
	}
	if strings.EqualFold(filepath.Base(optionalArtifactString(artifact.RefPath)), ref) {
		return true
	}
	if strings.EqualFold(optionalArtifactString(artifact.RefURL), ref) {
		return true
	}
	return false
}

func (a *App) resolveWorkspaceArtifactReference(workspaceID int64, raw string) (store.Artifact, error) {
	ref := cleanArtifactReference(raw)
	if ref == "" {
		return store.Artifact{}, fmt.Errorf("artifact reference is required")
	}
	artifacts, err := a.store.ListArtifactsForWorkspace(workspaceID)
	if err != nil {
		return store.Artifact{}, err
	}
	matches := make([]store.Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifactMatchesReference(artifact, ref) {
			matches = append(matches, artifact)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return store.Artifact{}, fmt.Errorf("artifact %q is ambiguous", ref)
	}
	return store.Artifact{}, fmt.Errorf("artifact %q not found", ref)
}

func (a *App) linkWorkspaceArtifact(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	sourceWorkspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionStringParam(action.Params, "source_workspace"))
	if err != nil {
		return "", nil, err
	}
	targetWorkspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionStringParam(action.Params, "target_workspace"))
	if err != nil {
		return "", nil, err
	}
	artifact, err := a.resolveWorkspaceArtifactReference(sourceWorkspace.ID, systemActionArtifactRef(action.Params))
	if err != nil {
		return "", nil, err
	}
	if err := a.store.LinkArtifactToWorkspace(targetWorkspace.ID, artifact.ID); err != nil {
		return "", nil, err
	}
	title := optionalArtifactString(artifact.Title)
	if title == "" {
		title = filepath.Base(optionalArtifactString(artifact.RefPath))
	}
	if title == "" {
		title = fmt.Sprintf("artifact %d", artifact.ID)
	}
	return fmt.Sprintf("Linked %s from %s to %s.", title, sourceWorkspace.Name, targetWorkspace.Name), map[string]interface{}{
		"type":                "link_workspace_artifact",
		"artifact_id":         artifact.ID,
		"artifact_title":      title,
		"source_workspace_id": sourceWorkspace.ID,
		"source_workspace":    sourceWorkspace.Name,
		"target_workspace_id": targetWorkspace.ID,
		"target_workspace":    targetWorkspace.Name,
	}, nil
}

func linkedArtifactDisplayTitle(artifact store.Artifact) string {
	title := optionalArtifactString(artifact.Title)
	if title != "" {
		return title
	}
	refPath := optionalArtifactString(artifact.RefPath)
	if refPath != "" {
		return filepath.Base(refPath)
	}
	refURL := optionalArtifactString(artifact.RefURL)
	if refURL != "" {
		return refURL
	}
	return fmt.Sprintf("artifact %d", artifact.ID)
}

func (a *App) listLinkedArtifacts(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	workspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionWorkspaceRef(action.Params))
	if err != nil {
		return "", nil, err
	}
	artifacts, err := a.store.ListLinkedArtifacts(workspace.ID)
	if err != nil {
		return "", nil, err
	}
	if len(artifacts) == 0 {
		return fmt.Sprintf("No linked artifacts for workspace %s.", workspace.Name), map[string]interface{}{
			"type":         "list_linked_artifacts",
			"workspace_id": workspace.ID,
			"workspace":    workspace.Name,
			"artifact_ids": []int64{},
			"count":        0,
		}, nil
	}
	titles := make([]string, 0, len(artifacts))
	ids := make([]int64, 0, len(artifacts))
	lines := make([]string, 0, len(artifacts)+1)
	lines = append(lines, fmt.Sprintf("Linked artifacts for workspace %s:", workspace.Name))
	for _, artifact := range artifacts {
		title := linkedArtifactDisplayTitle(artifact)
		if homeWorkspaceID, err := a.store.InferWorkspaceForArtifact(artifact); err == nil && homeWorkspaceID != nil {
			if homeWorkspace, err := a.store.GetWorkspace(*homeWorkspaceID); err == nil {
				title = fmt.Sprintf("%s (from %s)", title, homeWorkspace.Name)
			}
		}
		lines = append(lines, "- "+title)
		titles = append(titles, title)
		ids = append(ids, artifact.ID)
	}
	return strings.Join(lines, "\n"), map[string]interface{}{
		"type":          "list_linked_artifacts",
		"workspace_id":  workspace.ID,
		"workspace":     workspace.Name,
		"artifact_ids":  ids,
		"artifact_list": titles,
		"count":         len(ids),
	}, nil
}
