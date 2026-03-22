package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/store"
)

const (
	taburaVersion            = "0.1.8"
	taburaBugReportOwnerRepo = "krystophny/tabura"
)

type bugReportRequest struct {
	Trigger             string          `json:"trigger"`
	Timestamp           string          `json:"timestamp"`
	PageURL             string          `json:"page_url"`
	Version             string          `json:"version"`
	BootID              string          `json:"boot_id"`
	StartedAt           string          `json:"started_at"`
	ActiveMode          string          `json:"active_mode"`
	ActiveSphere        string          `json:"active_sphere"`
	CanvasState         json.RawMessage `json:"canvas_state"`
	RecentEvents        []string        `json:"recent_events"`
	BrowserLogs         []string        `json:"browser_logs"`
	Device              map[string]any  `json:"device"`
	DialogueDiagnostics json.RawMessage `json:"dialogue_diagnostics"`
	MeetingDiagnostics  json.RawMessage `json:"meeting_diagnostics"`
	Note                string          `json:"note"`
	VoiceTranscript     string          `json:"voice_transcript"`
	ScreenshotData      string          `json:"screenshot_data_url"`
	AnnotatedDataURL    string          `json:"annotated_data_url"`
}

type bugReportBundle struct {
	Trigger             string          `json:"trigger"`
	Timestamp           string          `json:"timestamp"`
	PageURL             string          `json:"page_url,omitempty"`
	Version             string          `json:"version"`
	BootID              string          `json:"boot_id,omitempty"`
	StartedAt           string          `json:"started_at,omitempty"`
	GitSHA              string          `json:"git_sha,omitempty"`
	ActiveMode          string          `json:"active_mode,omitempty"`
	ActiveWorkspace     string          `json:"active_workspace,omitempty"`
	ActiveSphere        string          `json:"active_sphere,omitempty"`
	CanvasState         json.RawMessage `json:"canvas_state,omitempty"`
	RecentEvents        []string        `json:"recent_events,omitempty"`
	BrowserLogs         []string        `json:"browser_logs,omitempty"`
	Device              map[string]any  `json:"device,omitempty"`
	DialogueDiagnostics json.RawMessage `json:"dialogue_diagnostics,omitempty"`
	MeetingDiagnostics  json.RawMessage `json:"meeting_diagnostics,omitempty"`
	Note                string          `json:"note,omitempty"`
	VoiceTranscript     string          `json:"voice_transcript,omitempty"`
	ScreenshotPath      string          `json:"screenshot,omitempty"`
	AnnotatedPath       string          `json:"annotated_image,omitempty"`
	WorkspaceDirPath    string          `json:"workspace_dir_path,omitempty"`
	GitHubIssueURL      string          `json:"github_issue_url,omitempty"`
	GitHubIssueNo       int             `json:"github_issue_number,omitempty"`
	GitHubIssueError    string          `json:"github_issue_error,omitempty"`
	ItemID              int64           `json:"item_id,omitempty"`
	IssueLabels         []string        `json:"issue_labels,omitempty"`
}

type bugReportFile struct {
	bytes []byte
	ext   string
}

type bugReportWorkspace struct {
	Name    string
	DirPath string
	ID      *int64
	Sphere  string
}

type gitHubLabelName struct {
	Name string `json:"name"`
}

func (a *App) handleBugReportCreate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req bugReportRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	screenshot, err := decodeBugReportDataURL(req.ScreenshotData)
	if err != nil {
		http.Error(w, "screenshot_data_url must be a valid PNG or JPEG data URL", http.StatusBadRequest)
		return
	}
	var annotated *bugReportFile
	if strings.TrimSpace(req.AnnotatedDataURL) != "" {
		file, err := decodeBugReportDataURL(req.AnnotatedDataURL)
		if err != nil {
			http.Error(w, "annotated_data_url must be a valid PNG or JPEG data URL", http.StatusBadRequest)
			return
		}
		annotated = &file
	}
	workspace, err := a.resolveBugReportWorkspace()
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if sphere := normalizeBugReportSphere(req.ActiveSphere); sphere != "" {
		workspace.Sphere = sphere
		if workspace.ID != nil && *workspace.ID > 0 {
			updated, updateErr := a.store.SetWorkspaceSphere(*workspace.ID, sphere)
			if updateErr != nil {
				http.Error(w, updateErr.Error(), http.StatusConflict)
				return
			}
			workspace.ID = &updated.ID
			workspace.Name = updated.Name
			workspace.DirPath = updated.DirPath
			workspace.Sphere = updated.Sphere
		}
	}
	reportDir, reportID, err := createBugReportDir(workspace.DirPath, req.Timestamp)
	if err != nil {
		http.Error(w, "create bug report dir failed", http.StatusInternalServerError)
		return
	}
	screenshotPath := filepath.Join(reportDir, "screenshot"+screenshot.ext)
	if err := os.WriteFile(screenshotPath, screenshot.bytes, 0o644); err != nil {
		http.Error(w, "write screenshot failed", http.StatusInternalServerError)
		return
	}
	var annotatedPath string
	if annotated != nil {
		annotatedPath = filepath.Join(reportDir, "annotated"+annotated.ext)
		if err := os.WriteFile(annotatedPath, annotated.bytes, 0o644); err != nil {
			http.Error(w, "write annotated image failed", http.StatusInternalServerError)
			return
		}
	}
	timestamp := normalizeBugReportTimestamp(req.Timestamp)
	bundle := bugReportBundle{
		Trigger:             strings.TrimSpace(req.Trigger),
		Timestamp:           timestamp,
		PageURL:             strings.TrimSpace(req.PageURL),
		Version:             firstNonEmpty(strings.TrimSpace(req.Version), taburaVersion),
		BootID:              strings.TrimSpace(req.BootID),
		StartedAt:           strings.TrimSpace(req.StartedAt),
		GitSHA:              resolveGitSHA(workspace.DirPath),
		ActiveMode:          strings.TrimSpace(req.ActiveMode),
		ActiveWorkspace:     workspace.Name,
		ActiveSphere:        workspace.Sphere,
		CanvasState:         normalizeBugReportRawJSON(req.CanvasState),
		RecentEvents:        cleanBugReportLines(req.RecentEvents),
		BrowserLogs:         cleanBugReportLines(req.BrowserLogs),
		Device:              req.Device,
		DialogueDiagnostics: normalizeBugReportRawJSON(req.DialogueDiagnostics),
		MeetingDiagnostics:  normalizeBugReportRawJSON(req.MeetingDiagnostics),
		Note:                strings.TrimSpace(req.Note),
		VoiceTranscript:     strings.TrimSpace(req.VoiceTranscript),
		ScreenshotPath:      toBugReportRelativePath(workspace.DirPath, screenshotPath),
		AnnotatedPath:       toBugReportRelativePath(workspace.DirPath, annotatedPath),
		WorkspaceDirPath:    workspace.DirPath,
	}
	bundlePath := filepath.Join(reportDir, "bundle.json")
	if err := writeBugReportBundle(bundlePath, bundle); err != nil {
		http.Error(w, "write bundle failed", http.StatusInternalServerError)
		return
	}
	issueTitle := bugReportIssueTitle(bundle)
	if issueSkipReason := bugReportAutoFileSkipReason(bundle); issueSkipReason != "" {
		bundle.GitHubIssueError = issueSkipReason
	} else {
		issue, itemID, issueErr := a.createGitHubIssueFromBugReport(workspace, bundlePath, bundle)
		if issueErr != nil {
			bundle.GitHubIssueError = strings.TrimSpace(issueErr.Error())
		} else {
			bundle.GitHubIssueURL = strings.TrimSpace(issue.URL)
			bundle.GitHubIssueNo = issue.Number
			bundle.ItemID = itemID
			bundle.IssueLabels = []string{"bug", "p0"}
			issueTitle = strings.TrimSpace(issue.Title)
		}
	}
	if err := writeBugReportBundle(bundlePath, bundle); err != nil {
		http.Error(w, "update bundle failed", http.StatusInternalServerError)
		return
	}
	payload := map[string]any{
		"ok":              true,
		"report_id":       reportID,
		"bundle_path":     toBugReportRelativePath(workspace.DirPath, bundlePath),
		"screenshot_path": bundle.ScreenshotPath,
		"annotated_path":  bundle.AnnotatedPath,
		"workspace":       workspace.Name,
		"git_sha":         bundle.GitSHA,
		"issue_title":     issueTitle,
	}
	if bundle.GitHubIssueNo > 0 {
		payload["issue_number"] = bundle.GitHubIssueNo
		payload["issue_url"] = bundle.GitHubIssueURL
	}
	if bundle.ItemID > 0 {
		payload["item_id"] = bundle.ItemID
	}
	if bundle.GitHubIssueError != "" {
		payload["issue_error"] = bundle.GitHubIssueError
	}
	writeJSON(w, payload)
}

func writeBugReportBundle(path string, bundle bugReportBundle) error {
	bundleJSON, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, bundleJSON, 0o644)
}

func (a *App) resolveBugReportWorkspace() (bugReportWorkspace, error) {
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return bugReportWorkspace{}, err
	}
	if workspace := activeExplicitWorkspace(workspaces); workspace != nil {
		id := workspace.ID
		return bugReportWorkspace{Name: workspace.Name, DirPath: workspace.DirPath, ID: &id, Sphere: workspace.Sphere}, nil
	}
	if root := strings.TrimSpace(a.localProjectDir); root != "" {
		name := filepath.Base(root)
		if strings.TrimSpace(name) == "" || name == "." || name == string(filepath.Separator) {
			name = "local"
		}
		return bugReportWorkspace{Name: name, DirPath: root}, nil
	}
	if workspace, ok, err := a.resolveTaburaBugReportWorkspace(); err != nil {
		return bugReportWorkspace{}, err
	} else if ok {
		return workspace, nil
	}
	return bugReportWorkspace{}, errors.New("bug report requires an active workspace or local project")
}

func (a *App) createGitHubIssueFromBugReport(workspace bugReportWorkspace, bundlePath string, bundle bugReportBundle) (ghIssueListItem, int64, error) {
	workspaceID, err := a.ensureBugReportWorkspaceID(workspace)
	if err != nil {
		return ghIssueListItem{}, 0, err
	}
	githubCWD := resolveBugReportGitHubCommandDir(workspace.DirPath)
	if err := a.ensureGitHubLabels(githubCWD, taburaBugReportOwnerRepo, map[string]struct {
		Color       string
		Description string
	}{
		"bug": {Color: "d73a4a", Description: "Something isn't working"},
		"p0":  {Color: "b60205", Description: "Highest priority"},
	}); err != nil {
		return ghIssueListItem{}, 0, err
	}
	issue, err := a.createGitHubIssueInWorkspaceWithRepo(
		githubCWD,
		taburaBugReportOwnerRepo,
		bugReportIssueTitle(bundle),
		bugReportIssueBody(bundle, toBugReportRelativePath(workspace.DirPath, bundlePath)),
		[]string{"bug", "p0"},
		nil,
	)
	if err != nil {
		return ghIssueListItem{}, 0, err
	}
	item, err := a.store.CreateItem(strings.TrimSpace(issue.Title), store.ItemOptions{
		WorkspaceID: workspaceID,
		Source:      optionalTrimmedString("github"),
		SourceRef:   optionalTrimmedString(githubIssueSourceRef(taburaBugReportOwnerRepo, issue.Number)),
	})
	if err != nil {
		return ghIssueListItem{}, 0, err
	}
	if err := a.syncGitHubIssueArtifact(item, taburaBugReportOwnerRepo, issue); err != nil {
		return ghIssueListItem{}, 0, err
	}
	return issue, item.ID, nil
}

func (a *App) ensureBugReportWorkspaceID(workspace bugReportWorkspace) (*int64, error) {
	if workspace.ID != nil && *workspace.ID > 0 {
		id := *workspace.ID
		return &id, nil
	}
	existing, err := a.store.GetWorkspaceByPath(workspace.DirPath)
	switch {
	case err == nil:
		if sphere := normalizeBugReportSphere(workspace.Sphere); sphere != "" && sphere != existing.Sphere {
			updated, updateErr := a.store.SetWorkspaceSphere(existing.ID, sphere)
			if updateErr != nil {
				return nil, updateErr
			}
			existing = updated
		}
		id := existing.ID
		return &id, nil
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return nil, err
	}
	if sphere := normalizeBugReportSphere(workspace.Sphere); sphere != "" {
		created, err := a.store.CreateWorkspace(workspace.Name, workspace.DirPath, sphere)
		if err != nil {
			return nil, err
		}
		id := created.ID
		return &id, nil
	}
	created, err := a.store.CreateWorkspace(workspace.Name, workspace.DirPath)
	if err != nil {
		return nil, err
	}
	id := created.ID
	return &id, nil
}

func normalizeBugReportSphere(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case store.SphereWork:
		return store.SphereWork
	case store.SpherePrivate:
		return store.SpherePrivate
	default:
		return ""
	}
}

func (a *App) resolveTaburaBugReportWorkspace() (bugReportWorkspace, bool, error) {
	workspaceID, err := a.store.FindWorkspaceByGitRemote(taburaBugReportOwnerRepo)
	if err != nil {
		return bugReportWorkspace{}, false, err
	}
	if workspaceID != nil && *workspaceID > 0 {
		workspace, err := a.store.GetWorkspace(*workspaceID)
		if err != nil {
			return bugReportWorkspace{}, false, err
		}
		return bugReportWorkspace{
			Name:    workspace.Name,
			DirPath: workspace.DirPath,
			ID:      workspaceID,
			Sphere:  workspace.Sphere,
		}, true, nil
	}
	repoRoot := resolveCanonicalGitHubRepoRoot(taburaBugReportOwnerRepo)
	if repoRoot == "" {
		return bugReportWorkspace{}, false, nil
	}
	return bugReportWorkspace{
		Name:    "Tabura",
		DirPath: repoRoot,
		Sphere:  store.SphereWork,
	}, true, nil
}

func (a *App) ensureGitHubLabels(cwd, ownerRepo string, wanted map[string]struct {
	Color       string
	Description string
}) error {
	runner := a.ghCommandRunner
	if runner == nil {
		runner = runGitHubCLI
	}
	ctx, cancel := context.WithTimeout(context.Background(), githubIssueListTimeout)
	defer cancel()
	listArgs := withGitHubRepoArg([]string{"label", "list", "--json", "name", "--limit", "200"}, ownerRepo)
	raw, err := runner(ctx, cwd, listArgs...)
	if err != nil {
		return err
	}
	var labels []gitHubLabelName
	if err := json.Unmarshal([]byte(raw), &labels); err != nil {
		return fmt.Errorf("invalid github label response: %w", err)
	}
	existing := map[string]struct{}{}
	for _, label := range labels {
		if clean := strings.ToLower(strings.TrimSpace(label.Name)); clean != "" {
			existing[clean] = struct{}{}
		}
	}
	for name, spec := range wanted {
		if _, ok := existing[strings.ToLower(strings.TrimSpace(name))]; ok {
			continue
		}
		createArgs := withGitHubRepoArg([]string{"label", "create", name}, ownerRepo)
		createArgs = append(createArgs, "--color", spec.Color, "--description", spec.Description)
		_, err := runner(ctx, cwd, createArgs...)
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return err
		}
	}
	return nil
}

func withGitHubRepoArg(args []string, ownerRepo string) []string {
	clean := normalizeBugReportGitHubOwnerRepo(ownerRepo)
	if clean == "" {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, args...)
	out = append(out, "--repo", clean)
	return out
}

func resolveBugReportGitHubCommandDir(workspaceDir string) string {
	if repoRoot := resolveCanonicalGitHubRepoRoot(taburaBugReportOwnerRepo); repoRoot != "" {
		return repoRoot
	}
	return resolveGitRepoRoot(workspaceDir)
}

func resolveCanonicalGitHubRepoRoot(ownerRepo string) string {
	target := normalizeBugReportGitHubOwnerRepo(ownerRepo)
	if target == "" {
		return ""
	}
	for _, candidate := range githubCommandDirCandidates("") {
		repoRoot := resolveGitRepoRoot(candidate)
		if repoRoot == "" {
			continue
		}
		if resolveBugReportGitRemoteOwnerRepo(repoRoot) == target {
			return repoRoot
		}
	}
	return ""
}

func resolveBugReportGitRemoteOwnerRepo(dir string) string {
	clean := strings.TrimSpace(dir)
	if clean == "" {
		return ""
	}
	cmd := exec.Command("git", "-C", clean, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return normalizeBugReportGitHubOwnerRepo(string(out))
}

func bugReportIssueTitle(bundle bugReportBundle) string {
	if summary := bugReportSummary(bundle); summary != "" {
		return truncateText("Bug report: "+summary, 96)
	}
	return "Bug report: interaction failure"
}

func bugReportCanvasStateField(raw json.RawMessage, keys ...string) string {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	for _, key := range keys {
		if clean := strings.TrimSpace(fmt.Sprint(payload[key])); clean != "" && clean != "<nil>" {
			return clean
		}
	}
	return ""
}

func bugReportCanvasArtifactTitle(raw json.RawMessage) string {
	return bugReportCanvasStateField(raw, "artifact_title", "active_artifact_title", "title")
}

func bugReportCanvasStateBool(raw json.RawMessage, key string) bool {
	value := strings.TrimSpace(strings.ToLower(bugReportCanvasStateField(raw, key)))
	return value == "true"
}

func bugReportSummary(bundle bugReportBundle) string {
	for _, candidate := range []string{
		firstSentence(bundle.Note),
		firstSentence(bundle.VoiceTranscript),
		bugReportLogSummary(bundle.BrowserLogs),
		bugReportRecentEventSummary(bundle.RecentEvents),
		bugReportStructuredSummary(bundle),
	} {
		clean := strings.TrimSpace(candidate)
		if clean == "" {
			continue
		}
		return clean
	}
	return ""
}

func bugReportAutoFileSkipReason(bundle bugReportBundle) string {
	if bugReportHasActionableSummary(bundle) {
		return ""
	}
	return "auto-filing skipped: add a short note or capture clearer interaction context"
}

func bugReportHasActionableSummary(bundle bugReportBundle) bool {
	for _, candidate := range []string{
		firstSentence(bundle.Note),
		firstSentence(bundle.VoiceTranscript),
		bugReportLogSummary(bundle.BrowserLogs),
		bugReportRecentEventSummary(bundle.RecentEvents),
		bugReportCanvasArtifactTitle(bundle.CanvasState),
		bugReportStructuredInteraction(bundle.CanvasState),
		bugReportJSON(bundle.DialogueDiagnostics),
		bugReportJSON(bundle.MeetingDiagnostics),
	} {
		if strings.TrimSpace(candidate) != "" {
			return true
		}
	}
	return false
}

func bugReportLogSummary(lines []string) string {
	for idx := len(lines) - 1; idx >= 0; idx-- {
		clean := normalizeBugReportEvidenceLine(lines[idx])
		if clean == "" {
			continue
		}
		clean = trimBugReportLogLevel(clean)
		if clean == "" {
			continue
		}
		return firstSentence(clean)
	}
	return ""
}

func bugReportRecentEventSummary(lines []string) string {
	for idx := len(lines) - 1; idx >= 0; idx-- {
		clean := strings.ToLower(normalizeBugReportEvidenceLine(lines[idx]))
		if clean == "" {
			continue
		}
		if strings.Contains(clean, "bug report") || strings.Contains(clean, "report bug") {
			continue
		}
		return firstSentence(normalizeBugReportEvidenceLine(lines[idx]))
	}
	return ""
}

func bugReportStructuredSummary(bundle bugReportBundle) string {
	artifact := bugReportCanvasArtifactTitle(bundle.CanvasState)
	mode := strings.TrimSpace(bundle.ActiveMode)
	workspace := strings.TrimSpace(bundle.ActiveWorkspace)
	trigger := strings.TrimSpace(bundle.Trigger)
	interaction := bugReportStructuredInteraction(bundle.CanvasState)
	switch {
	case mode != "" && artifact != "":
		return fmt.Sprintf("%s interaction failed while viewing %s", mode, artifact)
	case artifact != "":
		return fmt.Sprintf("interaction failed while viewing %s", artifact)
	case mode != "" && workspace != "" && interaction != "":
		return fmt.Sprintf("%s interaction failed in %s %s", mode, workspace, interaction)
	case workspace != "" && interaction != "":
		return fmt.Sprintf("interaction failed in %s %s", workspace, interaction)
	case mode != "" && interaction != "":
		return fmt.Sprintf("%s interaction failed %s", mode, interaction)
	case interaction != "":
		return fmt.Sprintf("interaction failed %s", interaction)
	case mode != "" && workspace != "":
		return fmt.Sprintf("%s interaction failed in %s", mode, workspace)
	case mode != "":
		return fmt.Sprintf("%s interaction failed", mode)
	case workspace != "":
		return fmt.Sprintf("interaction failed in %s", workspace)
	case trigger != "":
		return fmt.Sprintf("interaction failed after %s", trigger)
	default:
		return ""
	}
}

func bugReportStructuredInteraction(raw json.RawMessage) string {
	switch {
	case bugReportCanvasStateField(raw, "workspace_browser_path") != "":
		return fmt.Sprintf("while browsing %s", bugReportCanvasStateField(raw, "workspace_browser_path"))
	case bugReportCanvasStateField(raw, "item_sidebar_view") != "":
		return fmt.Sprintf("while viewing %s sidebar", bugReportCanvasStateField(raw, "item_sidebar_view"))
	case bugReportCanvasStateBool(raw, "text_input_visible"):
		return "while typing"
	case bugReportCanvasStateBool(raw, "pr_review_mode"):
		return "during PR review"
	case bugReportCanvasStateBool(raw, "overlay_visible"):
		return "with the overlay open"
	}

	surface := bugReportCanvasStateField(raw, "interaction_surface")
	tool := bugReportCanvasStateField(raw, "interaction_tool")
	switch {
	case surface == "annotate" && tool == "pointer":
		// These are the default canvas states and are less useful than richer UI context.
	case surface != "" && tool != "":
		return fmt.Sprintf("on %s with %s", surface, tool)
	case surface != "":
		return fmt.Sprintf("on %s", surface)
	case tool != "":
		return fmt.Sprintf("with %s", tool)
	}

	if origin := bugReportCanvasStateField(raw, "last_input_origin"); origin != "" {
		return fmt.Sprintf("after %s input", origin)
	}
	return ""
}

func normalizeBugReportEvidenceLine(raw string) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if clean == "" {
		return ""
	}
	if clean = trimBugReportTimestamp(clean); clean == "" {
		return ""
	}
	return clean
}

func trimBugReportTimestamp(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	firstSpace := strings.IndexByte(clean, ' ')
	if firstSpace <= 0 {
		return clean
	}
	if _, err := time.Parse(time.RFC3339Nano, clean[:firstSpace]); err == nil {
		return strings.TrimSpace(clean[firstSpace+1:])
	}
	return clean
}

func trimBugReportLogLevel(raw string) string {
	clean := strings.TrimSpace(raw)
	for _, prefix := range []string{"error:", "warn:", "warning:", "log:", "info:", "debug:"} {
		if strings.HasPrefix(strings.ToLower(clean), prefix) {
			return strings.TrimSpace(clean[len(prefix):])
		}
	}
	return clean
}

func normalizeBugReportGitHubOwnerRepo(raw string) string {
	clean := strings.TrimSpace(strings.ToLower(raw))
	if clean == "" {
		return ""
	}
	clean = strings.TrimSuffix(clean, ".git")
	if idx := strings.Index(clean, "#"); idx >= 0 {
		clean = clean[:idx]
	}
	clean = strings.Trim(clean, "/")
	switch {
	case strings.HasPrefix(clean, "git@github.com:"):
		clean = strings.TrimPrefix(clean, "git@github.com:")
	case strings.HasPrefix(clean, "ssh://git@github.com/"):
		clean = strings.TrimPrefix(clean, "ssh://git@github.com/")
	case strings.HasPrefix(clean, "https://github.com/"):
		clean = strings.TrimPrefix(clean, "https://github.com/")
	case strings.HasPrefix(clean, "http://github.com/"):
		clean = strings.TrimPrefix(clean, "http://github.com/")
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func firstSentence(raw string) string {
	clean := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if clean == "" {
		return ""
	}
	for _, sep := range []string{". ", "! ", "? ", "\n"} {
		if idx := strings.Index(clean, sep); idx > 0 {
			clean = clean[:idx]
			break
		}
	}
	return strings.Trim(clean, " .!?\t\r\n")
}

func truncateText(raw string, max int) string {
	clean := strings.TrimSpace(raw)
	if max <= 0 || len(clean) <= max {
		return clean
	}
	cut := strings.TrimSpace(clean[:max])
	cut = strings.TrimRight(cut, ".,:;!-")
	return cut + "..."
}

func bugReportIssueBody(bundle bugReportBundle, bundlePath string) string {
	var b strings.Builder
	summary := bugReportSummary(bundle)
	if summary != "" {
		b.WriteString("## Summary\n\n")
		b.WriteString(summary)
		b.WriteString("\n\n")
	}
	b.WriteString("## Context\n\n")
	for _, line := range []string{
		bugReportContextLine("Trigger", bundle.Trigger),
		bugReportContextLine("Active mode", bundle.ActiveMode),
		bugReportContextLine("Sphere", bundle.ActiveSphere),
		bugReportContextLine("Workspace", bundle.ActiveWorkspace),
		bugReportContextLine("Page", bundle.PageURL),
		bugReportContextLine("Version", bundle.Version),
		bugReportContextLine("Git SHA", bundle.GitSHA),
		bugReportContextLine("Canvas artifact", bugReportCanvasArtifactTitle(bundle.CanvasState)),
		bugReportContextLine("Interaction surface", bugReportCanvasStateField(bundle.CanvasState, "interaction_surface")),
		bugReportContextLine("Interaction tool", bugReportCanvasStateField(bundle.CanvasState, "interaction_tool")),
		bugReportContextLine("Text input visible", bugReportCanvasStateField(bundle.CanvasState, "text_input_visible")),
		bugReportContextLine("PR review mode", bugReportCanvasStateField(bundle.CanvasState, "pr_review_mode")),
		bugReportContextLine("Overlay visible", bugReportCanvasStateField(bundle.CanvasState, "overlay_visible")),
		bugReportContextLine("Last input origin", bugReportCanvasStateField(bundle.CanvasState, "last_input_origin")),
		bugReportContextLine("Item sidebar view", bugReportCanvasStateField(bundle.CanvasState, "item_sidebar_view")),
		bugReportContextLine("Workspace browser path", bugReportCanvasStateField(bundle.CanvasState, "workspace_browser_path")),
	} {
		if line != "" {
			b.WriteString(line)
		}
	}
	if note := strings.TrimSpace(bundle.Note); note != "" {
		b.WriteString("\n## Note\n\n")
		b.WriteString(note)
		b.WriteString("\n")
	}
	if deviceJSON := bugReportJSON(bundle.Device); deviceJSON != "" {
		b.WriteString("\n## Device\n\n```json\n")
		b.WriteString(deviceJSON)
		b.WriteString("\n```\n")
	}
	if diagnosticsJSON := bugReportJSON(bundle.DialogueDiagnostics); diagnosticsJSON != "" {
		b.WriteString("\n## Dialogue diagnostics\n\n```json\n")
		b.WriteString(diagnosticsJSON)
		b.WriteString("\n```\n")
	}
	if diagnosticsJSON := bugReportJSON(bundle.MeetingDiagnostics); diagnosticsJSON != "" {
		b.WriteString("\n## Meeting diagnostics\n\n```json\n")
		b.WriteString(diagnosticsJSON)
		b.WriteString("\n```\n")
	}
	if len(bundle.RecentEvents) > 0 {
		b.WriteString("\n## Recent events\n\n")
		for _, event := range bundle.RecentEvents {
			b.WriteString("- ")
			b.WriteString(event)
			b.WriteString("\n")
		}
	}
	if len(bundle.BrowserLogs) > 0 {
		b.WriteString("\n## Browser logs\n\n```text\n")
		for _, line := range bundle.BrowserLogs {
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("```\n")
	}
	if transcript := strings.TrimSpace(bundle.VoiceTranscript); transcript != "" {
		b.WriteString("\n## Voice transcript\n\n")
		b.WriteString(transcript)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func bugReportContextLine(label, value string) string {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return ""
	}
	return fmt.Sprintf("- %s: `%s`\n", label, clean)
}

func bugReportJSON(value any) string {
	if value == nil {
		return ""
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil || string(encoded) == "null" {
		return ""
	}
	return string(encoded)
}

func decodeBugReportDataURL(raw string) (bugReportFile, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return bugReportFile{}, errors.New("missing data URL")
	}
	comma := strings.IndexByte(clean, ',')
	if comma <= 0 {
		return bugReportFile{}, errors.New("invalid data URL")
	}
	header := clean[:comma]
	payload := clean[comma+1:]
	if !strings.HasPrefix(strings.ToLower(header), "data:image/") || !strings.Contains(strings.ToLower(header), ";base64") {
		return bugReportFile{}, errors.New("unsupported data URL")
	}
	var ext string
	switch {
	case strings.HasPrefix(strings.ToLower(header), "data:image/png"):
		ext = ".png"
	case strings.HasPrefix(strings.ToLower(header), "data:image/jpeg"), strings.HasPrefix(strings.ToLower(header), "data:image/jpg"):
		ext = ".jpg"
	default:
		return bugReportFile{}, errors.New("unsupported image type")
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return bugReportFile{}, err
	}
	if len(decoded) == 0 {
		return bugReportFile{}, errors.New("empty image")
	}
	return bugReportFile{bytes: decoded, ext: ext}, nil
}

func normalizeBugReportTimestamp(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return time.Now().UTC().Format(time.RFC3339)
	}
	if parsed, err := time.Parse(time.RFC3339, clean); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	return clean
}

func normalizeBugReportRawJSON(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil
	}
	return trimmed
}

func cleanBugReportLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		clean := strings.TrimSpace(line)
		if clean == "" {
			continue
		}
		out = append(out, clean)
	}
	return out
}

func createBugReportDir(workspaceDir, rawTimestamp string) (string, string, error) {
	timestamp := normalizeBugReportTimestamp(rawTimestamp)
	stamp := strings.NewReplacer(":", "", "-", "", ".", "").Replace(timestamp)
	stamp = strings.TrimSuffix(stamp, "Z")
	stamp = strings.ReplaceAll(stamp, "T", "-")
	if stamp == "" {
		stamp = time.Now().UTC().Format("20060102-150405")
	}
	suffix, err := randomBugReportSuffix()
	if err != nil {
		return "", "", err
	}
	reportID := stamp + "-" + suffix
	dir := filepath.Join(workspaceDir, ".tabura", "artifacts", "bugs", reportID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	return dir, reportID, nil
}

func randomBugReportSuffix() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func toBugReportRelativePath(workspaceDir, fullPath string) string {
	clean := strings.TrimSpace(fullPath)
	if clean == "" {
		return ""
	}
	rel, err := filepath.Rel(workspaceDir, clean)
	if err != nil {
		return filepath.ToSlash(clean)
	}
	return filepath.ToSlash(rel)
}

func resolveGitSHA(dir string) string {
	clean := strings.TrimSpace(dir)
	if clean == "" {
		return ""
	}
	cmd := exec.Command("git", "-C", clean, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}
