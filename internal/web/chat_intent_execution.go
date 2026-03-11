package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

func (a *App) executeSystemActionPlan(sessionID string, session store.ChatSession, userText string, actions []*SystemAction) (string, []map[string]interface{}, error) {
	actions = enforceRoutingPolicy(userText, actions)
	if len(actions) == 0 {
		return "", nil, errors.New("action plan is empty")
	}
	if guardMessage, guardPayloads, blocked := a.guardDangerousSystemActionPlan(sessionID, userText, actions); blocked {
		return guardMessage, guardPayloads, nil
	}
	if guardMessage, guardPayloads, blocked := a.guardArtifactSystemActionPlan(sessionID, userText, actions); blocked {
		return guardMessage, guardPayloads, nil
	}
	return a.executeSystemActionPlanUnsafe(sessionID, session, userText, actions)
}

func (a *App) executeSystemActionPlanUnsafe(sessionID string, session store.ChatSession, userText string, actions []*SystemAction) (string, []map[string]interface{}, error) {
	if len(actions) == 0 {
		return "", nil, errors.New("action plan is empty")
	}
	messages := make([]string, 0, len(actions))
	payloads := make([]map[string]interface{}, 0, len(actions))
	lastShellPath := ""
	targetProject, targetErr := a.systemActionTargetProject(session)
	targetCWD := ""
	if targetErr == nil {
		targetCWD = strings.TrimSpace(targetProject.RootPath)
		if targetCWD == "" {
			targetCWD = strings.TrimSpace(a.cwdForProjectKey(targetProject.ProjectKey))
		}
	}
	requestHints := extractOpenRequestHints(userText)
	for _, action := range actions {
		if action == nil {
			continue
		}
		resolved := &SystemAction{Action: action.Action, Params: map[string]interface{}{}}
		for key, value := range action.Params {
			resolved.Params[key] = value
		}
		if resolved.Action == "open_file_canvas" {
			path := systemActionOpenPath(resolved.Params)
			if strings.EqualFold(strings.TrimSpace(path), systemActionLastShellPathPlaceholder) {
				if strings.TrimSpace(lastShellPath) == "" {
					return "", nil, errors.New("open_file_canvas requires a resolved shell path")
				}
				resolved.Params["path"] = preferTopLevelSiblingPath(targetCWD, lastShellPath)
			}
		}
		message, payload, err := a.executeSystemAction(sessionID, session, resolved)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(message) != "" {
			messages = append(messages, strings.TrimSpace(message))
		}
		if payload != nil {
			payloads = append(payloads, payload)
			payloadType := strings.TrimSpace(fmt.Sprint(payload["type"]))
			payloadOutput := strings.TrimSpace(fmt.Sprint(payload["output"]))
			if strings.EqualFold(payloadType, "shell") && payloadOutput != "" && payloadOutput != "<nil>" {
				lastShellPath = selectBestShellPathFromOutput(targetCWD, payloadOutput, requestHints)
			}
		}
	}
	if len(messages) == 0 {
		messages = append(messages, "Done.")
	}
	return strings.Join(messages, "\n\n"), payloads, nil
}

func (a *App) systemActionTargetProject(session store.ChatSession) (store.Project, error) {
	projectKey := strings.TrimSpace(session.ProjectKey)
	if projectKey != "" {
		project, err := a.store.GetProjectByProjectKey(projectKey)
		if err == nil && !isHubProject(project) {
			return project, nil
		}
	}
	return a.hubPrimaryProject()
}

func truncateSystemActionOutput(text string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = systemActionShellOutputLimit
	}
	if len(text) <= maxBytes {
		return text
	}
	if maxBytes <= 24 {
		return text[:maxBytes]
	}
	return text[:maxBytes] + "\n...(truncated)"
}

type shellCommandExecution struct {
	Output   string
	ExitCode int
	TimedOut bool
	RunErr   error
}

func executeShellCommand(command string, cwd string) shellCommandExecution {
	commandCtx, cancel := context.WithTimeout(context.Background(), systemActionShellTimeout)
	defer cancel()

	cmd := exec.CommandContext(commandCtx, "bash", "-lc", command)
	cmd.Dir = cwd

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	runErr := cmd.Run()

	rawOutput := strings.TrimSpace(output.String())
	rawOutput = truncateSystemActionOutput(rawOutput, systemActionShellOutputLimit)
	if rawOutput == "" {
		rawOutput = "(no output)"
	}

	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		return shellCommandExecution{
			Output:   rawOutput,
			ExitCode: -1,
			TimedOut: true,
			RunErr:   runErr,
		}
	}

	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	return shellCommandExecution{
		Output:   rawOutput,
		ExitCode: exitCode,
		TimedOut: false,
		RunErr:   runErr,
	}
}

func suggestShellCommandRetry(command string, output string) (string, string, bool) {
	cleanCommand := strings.TrimSpace(command)
	if cleanCommand == "" || !strings.Contains(cleanCommand, "jq") {
		return "", "", false
	}
	cleanOutput := strings.TrimSpace(output)
	if cleanOutput == "" {
		return "", "", false
	}
	lowerOutput := strings.ToLower(cleanOutput)
	if !strings.Contains(lowerOutput, "jq: error: syntax error") || !strings.Contains(lowerOutput, "compile error") {
		return "", "", false
	}
	lines := strings.Split(cleanOutput, "\n")
	for _, line := range lines {
		candidate := strings.TrimSpace(line)
		if candidate == "" || !strings.HasPrefix(candidate, ".") {
			continue
		}
		if !(strings.HasSuffix(candidate, "}") || strings.HasSuffix(candidate, "]")) {
			continue
		}
		fixedCandidate := strings.TrimSuffix(strings.TrimSuffix(candidate, "}"), "]")
		if strings.TrimSpace(fixedCandidate) == "" || fixedCandidate == candidate {
			continue
		}
		fixedCommand := strings.Replace(cleanCommand, candidate, fixedCandidate, 1)
		if fixedCommand == cleanCommand {
			continue
		}
		return fixedCommand, fmt.Sprintf("fixed jq filter typo (%s -> %s)", candidate, fixedCandidate), true
	}
	return "", "", false
}

func (a *App) executeSystemAction(sessionID string, session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	if action == nil {
		return "", nil, errors.New("system action is required")
	}
	switch action.Action {
	case "switch_project":
		targetName := systemActionStringParam(action.Params, "name")
		project, err := a.hubFindProjectByName(targetName)
		if err != nil {
			return "", nil, err
		}
		activated, err := a.activateProject(project.ID)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Switched to %s.", activated.Name), map[string]interface{}{
			"type":       "switch_project",
			"project_id": activated.ID,
		}, nil
	case "switch_workspace":
		workspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionWorkspaceRef(action.Params))
		if err != nil {
			return "", nil, err
		}
		if err := a.setActiveWorkspaceTracked(workspace.ID, "workspace_switch"); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Switched to workspace %s.", workspace.Name), map[string]interface{}{
			"type":         "switch_workspace",
			"workspace_id": workspace.ID,
			"name":         workspace.Name,
			"dir_path":     workspace.DirPath,
		}, nil
	case "cursor_open_item", "cursor_triage_item", "cursor_open_path":
		return a.executeCursorAction(context.Background(), session, action)
	case "triage_item_by_title":
		return a.executeTitledItemAction(context.Background(), session, action)
	case "list_workspaces":
		return a.executeListWorkspacesAction(session, action)
	case "create_workspace":
		return a.executeCreateWorkspaceAction(session, action)
	case "workspace_watch_start", "workspace_watch_stop", "workspace_watch_status":
		return a.executeWorkspaceWatchAction(session, action)
	case "batch_work", "batch_configure", "review_policy", "batch_limit", "batch_status":
		return a.executeBatchAction(session, action)
	case "assign_workspace_project", "show_workspace_project", "create_project", "list_project_workspaces", "sync_project":
		return a.executeProjectAction(session, action)
	case "list_workspace_items":
		workspace, err := a.resolveWorkspaceReference(session.ProjectKey, systemActionWorkspaceRef(action.Params))
		if err != nil {
			return "", nil, err
		}
		items, err := a.listOpenWorkspaceItems(workspace.ID)
		if err != nil {
			return "", nil, err
		}
		itemIDs := make([]int64, 0, len(items))
		titles := make([]string, 0, len(items))
		for _, item := range items {
			itemIDs = append(itemIDs, item.ID)
			titles = append(titles, item.Title)
		}
		return summarizeWorkspaceItems(workspace, items), map[string]interface{}{
			"type":         "list_workspace_items",
			"workspace_id": workspace.ID,
			"name":         workspace.Name,
			"dir_path":     workspace.DirPath,
			"item_ids":     itemIDs,
			"item_titles":  titles,
			"item_count":   len(items),
		}, nil
	case "create_workspace_from_git":
		workspace, repoURL, err := a.createWorkspaceFromGit(systemActionGitRepoURL(action.Params), systemActionGitTargetPath(action.Params))
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Created workspace %s at %s.", workspace.Name, workspace.DirPath), map[string]interface{}{
			"type":         "create_workspace_from_git",
			"workspace_id": workspace.ID,
			"name":         workspace.Name,
			"dir_path":     workspace.DirPath,
			"repo_url":     repoURL,
		}, nil
	case "rename_workspace":
		return a.executeRenameWorkspaceAction(session, action)
	case "delete_workspace":
		return a.executeDeleteWorkspaceAction(session, action)
	case "show_workspace_details":
		return a.executeShowWorkspaceDetailsAction(session, action)
	case "switch_model":
		targetProject, err := a.systemActionTargetProject(session)
		if err != nil {
			return "", nil, err
		}
		updated, err := a.updateProjectChatModel(
			targetProject.ID,
			systemActionStringParam(action.Params, "alias"),
			systemActionStringParam(action.Params, "effort"),
		)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Model for %s set to %s (%s).", updated.Name, updated.ChatModel, updated.ChatModelReasoningEffort), map[string]interface{}{
			"type":       "switch_model",
			"project_id": updated.ID,
			"alias":      updated.ChatModel,
			"effort":     updated.ChatModelReasoningEffort,
		}, nil
	case "toggle_silent":
		return "Toggled silent mode.", map[string]interface{}{"type": "toggle_silent"}, nil
	case "toggle_live_dialogue":
		return "Toggled Live Dialogue.", map[string]interface{}{"type": "toggle_live_dialogue"}, nil
	case "cancel_work":
		activeCanceled, queuedCanceled := a.cancelChatWork(sessionID)
		return fmt.Sprintf("Canceled %d running task(s).", activeCanceled+queuedCanceled), nil, nil
	case "show_status":
		status, err := a.fetchCodexStatusMessage(session.ProjectKey)
		if err != nil {
			return "", nil, err
		}
		return status, nil, nil
	case "capture_idea":
		return a.captureIdeaItem(session, action)
	case "refine_idea_note":
		return a.refineConversationIdea(session, action)
	case "promote_idea":
		return a.previewIdeaPromotion(session, action)
	case "apply_idea_promotion":
		return a.applyIdeaPromotion(session, action)
	case "make_item", "delegate_item", "snooze_item", "split_items":
		return a.createConversationItem(sessionID, session, action)
	case "reassign_workspace", "reassign_project", "clear_workspace", "clear_project":
		return a.executeItemReassignmentAction(session, action)
	case "link_workspace_artifact":
		return a.linkWorkspaceArtifact(session, action)
	case "list_linked_artifacts":
		return a.listLinkedArtifacts(session, action)
	case "review_someday", "triage_someday", "promote_someday", "toggle_someday_review_nudge":
		return a.executeSomedayAction(session, action)
	case "show_filtered_items":
		return a.executeFilteredItemViewAction(action)
	case "sync_sources":
		return a.executeSourceSyncAction(session, action)
	case "map_todoist_project", "sync_todoist", "create_todoist_task":
		return a.executeTodoistAction(session, action)
	case "sync_evernote":
		return a.executeEvernoteAction(session, action)
	case "sync_bear", "promote_bear_checklist":
		return a.executeBearAction(session, action)
	case "sync_zotero":
		return a.executeZoteroAction(action)
	case "create_github_issue", "create_github_issue_split":
		return a.createGitHubIssueFromConversation(sessionID, session, action)
	case "shell":
		targetProject, err := a.systemActionTargetProject(session)
		if err != nil {
			return "", nil, err
		}
		command := systemActionShellCommand(action.Params)
		if command == "" {
			return "", nil, errors.New("shell command is required")
		}
		cwd := strings.TrimSpace(targetProject.RootPath)
		if cwd == "" {
			cwd = strings.TrimSpace(a.cwdForProjectKey(targetProject.ProjectKey))
		}
		if cwd == "" {
			return "", nil, errors.New("shell cwd is not available")
		}
		execResult := executeShellCommand(command, cwd)
		if execResult.TimedOut {
			return fmt.Sprintf("Shell command timed out after %s.\n\n%s", systemActionShellTimeout, execResult.Output), map[string]interface{}{
				"type":       "shell",
				"command":    command,
				"cwd":        cwd,
				"exit_code":  -1,
				"timed_out":  true,
				"output":     execResult.Output,
				"project_id": targetProject.ID,
			}, nil
		}
		if execResult.RunErr != nil && execResult.ExitCode == 0 {
			return "", nil, execResult.RunErr
		}
		if execResult.ExitCode != 0 {
			if fixedCommand, fixReason, retry := suggestShellCommandRetry(command, execResult.Output); retry {
				retryResult := executeShellCommand(fixedCommand, cwd)
				if !retryResult.TimedOut && retryResult.RunErr == nil && retryResult.ExitCode == 0 {
					return fmt.Sprintf("Shell command auto-corrected (%s).\n\n%s", fixReason, retryResult.Output), map[string]interface{}{
						"type":                "shell",
						"command":             fixedCommand,
						"original_command":    command,
						"cwd":                 cwd,
						"exit_code":           0,
						"output":              retryResult.Output,
						"project_id":          targetProject.ID,
						"auto_corrected":      true,
						"auto_correct_reason": fixReason,
					}, nil
				}
			}
			return fmt.Sprintf("Shell command failed (exit %d).\n\n%s", execResult.ExitCode, execResult.Output), map[string]interface{}{
				"type":       "shell",
				"command":    command,
				"cwd":        cwd,
				"exit_code":  execResult.ExitCode,
				"output":     execResult.Output,
				"project_id": targetProject.ID,
			}, nil
		}
		return execResult.Output, map[string]interface{}{
			"type":       "shell",
			"command":    command,
			"cwd":        cwd,
			"exit_code":  execResult.ExitCode,
			"output":     execResult.Output,
			"project_id": targetProject.ID,
		}, nil
	case "open_file_canvas":
		targetProject, err := a.systemActionTargetProject(session)
		if err != nil {
			return "", nil, err
		}
		rawPath := systemActionOpenPath(action.Params)
		if rawPath == "" {
			return "", nil, errors.New("open_file_canvas path is required")
		}
		cwd := strings.TrimSpace(targetProject.RootPath)
		if cwd == "" {
			cwd = strings.TrimSpace(a.cwdForProjectKey(targetProject.ProjectKey))
		}
		if cwd == "" {
			return "", nil, errors.New("open_file_canvas cwd is not available")
		}
		absPath, canvasTitle, err := resolveCanvasFilePath(cwd, rawPath)
		if err != nil {
			return "", nil, err
		}
		info, err := os.Stat(absPath)
		if err != nil {
			return "", nil, err
		}
		if info.IsDir() {
			return "", nil, fmt.Errorf("path %q is a directory", rawPath)
		}
		canvasSessionID := strings.TrimSpace(a.canvasSessionIDForProject(targetProject))
		if canvasSessionID == "" {
			return "", nil, errors.New("canvas session is not available")
		}
		port, ok := a.tunnels.getPort(canvasSessionID)
		if !ok {
			return "", nil, fmt.Errorf("no active MCP tunnel for project %q", targetProject.Name)
		}
		if isPresentationFilePath(absPath) {
			renderedPath, err := a.renderPresentationArtifact(cwd, absPath)
			if err != nil {
				return "", nil, err
			}
			if _, err := a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
				"session_id": canvasSessionID,
				"kind":       "pdf",
				"title":      canvasTitle,
				"path":       renderedPath,
			}); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("Opened %s on canvas as PDF.", canvasTitle), map[string]interface{}{
				"type":          "open_file_canvas",
				"path":          canvasTitle,
				"project_id":    targetProject.ID,
				"rendered_path": renderedPath,
			}, nil
		}
		if shouldRenderDocumentArtifact(cwd, absPath) {
			renderedPath, err := a.renderDocumentArtifact(cwd, absPath)
			if err != nil {
				return "", nil, err
			}
			if _, err := a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
				"session_id": canvasSessionID,
				"kind":       "pdf",
				"title":      canvasTitle,
				"path":       renderedPath,
			}); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("Opened %s on canvas as PDF.", canvasTitle), map[string]interface{}{
				"type":          "open_file_canvas",
				"path":          canvasTitle,
				"project_id":    targetProject.ID,
				"rendered_path": renderedPath,
			}, nil
		}
		if isPDFFilePath(absPath) {
			if _, err := a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
				"session_id": canvasSessionID,
				"kind":       "pdf",
				"title":      canvasTitle,
				"path":       canvasTitle,
			}); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("Opened %s on canvas.", canvasTitle), map[string]interface{}{
				"type":       "open_file_canvas",
				"path":       canvasTitle,
				"project_id": targetProject.ID,
			}, nil
		}
		if info.Size() > systemActionOpenFileSizeLimit {
			return "", nil, fmt.Errorf("file %q exceeds %d bytes", rawPath, systemActionOpenFileSizeLimit)
		}
		contentBytes, err := os.ReadFile(absPath)
		if err != nil {
			return "", nil, err
		}
		if _, err := a.mcpToolsCall(port, "canvas_artifact_show", map[string]interface{}{
			"session_id":       canvasSessionID,
			"kind":             "text",
			"title":            canvasTitle,
			"markdown_or_text": string(contentBytes),
		}); err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Opened %s on canvas.", canvasTitle), map[string]interface{}{
			"type":       "open_file_canvas",
			"path":       canvasTitle,
			"project_id": targetProject.ID,
		}, nil
	case "show_calendar":
		return a.executeCalendarAction(session, action)
	case "show_briefing":
		return a.executeBriefingAction(session, action)
	case "print_item":
		return a.executePrintItemAction(sessionID, session, action)
	default:
		return "", nil, fmt.Errorf("unsupported action: %s", action.Action)
	}
}
