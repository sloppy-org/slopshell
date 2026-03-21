package web

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

const testPNGDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO5W8xkAAAAASUVORK5CYII="

func TestHandleBugReportCreateWritesBundleUnderWorkspaceArtifacts(t *testing.T) {
	app := newAuthedTestApp(t)
	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	addGitRemote(t, workspaceDir, "https://github.com/owner/tabula.git")
	workspace, err := app.store.CreateWorkspace("Tabura", workspaceDir, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	var ghCalls [][]string
	wantCWD := resolveBugReportGitHubCommandDir(workspaceDir)
	app.ghCommandRunner = func(_ context.Context, cwd string, args ...string) (string, error) {
		ghCalls = append(ghCalls, append([]string{cwd}, args...))
		if cwd != wantCWD {
			t.Fatalf("gh cwd = %q, want %q", cwd, wantCWD)
		}
		if len(args) >= 3 && args[0] == "label" && args[1] == "list" {
			return `[{"name":"bug"}]`, nil
		}
		if len(args) >= 3 && args[0] == "label" && args[1] == "create" && args[2] == "p0" {
			return "", nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
			return "https://github.com/krystophny/tabura/issues/77\n", nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "view" {
			return `{"number":77,"title":"Bug report: The indicator froze after the tap","url":"https://github.com/krystophny/tabura/issues/77","state":"OPEN","labels":[{"name":"bug"},{"name":"p0"}],"assignees":[]}`, nil
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/bugs/report", map[string]any{
		"trigger":       "button",
		"timestamp":     "2026-03-08T15:04:05Z",
		"page_url":      "http://127.0.0.1:8420/",
		"version":       "0.1.8",
		"boot_id":       "boot-123",
		"started_at":    "2026-03-08T14:00:00Z",
		"active_mode":   "pen",
		"active_sphere": store.SphereWork,
		"canvas_state":  map[string]any{"has_artifact": true, "artifact_title": "README.md"},
		"recent_events": []string{"tap at (12,18)", "report bug"},
		"browser_logs":  []string{"warn: render slow"},
		"device": map[string]any{
			"ua":                   "Mozilla/5.0 Example",
			"platform":             "macOS",
			"os_version":           "14.4.0",
			"browser_version":      "123.0.6312.59",
			"viewport":             "1280x720",
			"screen":               "1440x900",
			"timezone":             "Europe/Vienna",
			"hardware_concurrency": float64(8),
		},
		"meeting_diagnostics": map[string]any{
			"live_policy":         "meeting",
			"turn_policy_profile": "balanced",
			"participant_status": map[string]any{
				"directed_speech_gate": map[string]any{"decision": "target_speaker_follow_up"},
				"interaction_policy":   map[string]any{"decision": "respond"},
			},
		},
		"note":                "The indicator froze after the tap.",
		"voice_transcript":    "it stops responding after the second tap",
		"screenshot_data_url": testPNGDataURL,
		"annotated_data_url":  testPNGDataURL,
	})
	if rr.Code != 200 {
		t.Fatalf("POST /api/bugs/report status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	bundlePath := strFromAny(payload["bundle_path"])
	screenshotPath := strFromAny(payload["screenshot_path"])
	annotatedPath := strFromAny(payload["annotated_path"])
	if !strings.HasPrefix(bundlePath, ".tabura/artifacts/bugs/") {
		t.Fatalf("bundle_path = %q, want .tabura/artifacts/bugs/... path", bundlePath)
	}
	if !strings.HasSuffix(screenshotPath, "screenshot.png") {
		t.Fatalf("screenshot_path = %q, want screenshot.png suffix", screenshotPath)
	}
	if !strings.HasSuffix(annotatedPath, "annotated.png") {
		t.Fatalf("annotated_path = %q, want annotated.png suffix", annotatedPath)
	}
	if got := intFromAny(payload["issue_number"], 0); got != 77 {
		t.Fatalf("issue_number = %d, want 77", got)
	}
	if got := strFromAny(payload["issue_url"]); got != "https://github.com/krystophny/tabura/issues/77" {
		t.Fatalf("issue_url = %q", got)
	}
	if got := strFromAny(payload["issue_title"]); got != "Bug report: The indicator froze after the tap" {
		t.Fatalf("issue_title = %q", got)
	}
	bundleBytes, err := os.ReadFile(filepath.Join(workspaceDir, filepath.FromSlash(bundlePath)))
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle map[string]any
	if err := json.Unmarshal(bundleBytes, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if got := strFromAny(bundle["active_workspace"]); got != "Tabura" {
		t.Fatalf("active_workspace = %q, want %q", got, "Tabura")
	}
	if got := strFromAny(bundle["active_sphere"]); got != store.SphereWork {
		t.Fatalf("active_sphere = %q, want %q", got, store.SphereWork)
	}
	if got := strFromAny(bundle["active_mode"]); got != "pen" {
		t.Fatalf("active_mode = %q, want %q", got, "pen")
	}
	if got := strFromAny(bundle["note"]); got != "The indicator froze after the tap." {
		t.Fatalf("note = %q, want note to round-trip", got)
	}
	if got := strFromAny(bundle["voice_transcript"]); got != "it stops responding after the second tap" {
		t.Fatalf("voice_transcript = %q, want transcript to round-trip", got)
	}
	meetingDiagnostics, ok := bundle["meeting_diagnostics"].(map[string]any)
	if !ok {
		t.Fatalf("meeting_diagnostics = %#v, want object", bundle["meeting_diagnostics"])
	}
	if got := strFromAny(meetingDiagnostics["live_policy"]); got != "meeting" {
		t.Fatalf("meeting_diagnostics.live_policy = %q, want meeting", got)
	}
	participantStatus, ok := meetingDiagnostics["participant_status"].(map[string]any)
	if !ok {
		t.Fatalf("meeting_diagnostics.participant_status = %#v, want object", meetingDiagnostics["participant_status"])
	}
	directedSpeechGate, ok := participantStatus["directed_speech_gate"].(map[string]any)
	if !ok {
		t.Fatalf("participant_status.directed_speech_gate = %#v, want object", participantStatus["directed_speech_gate"])
	}
	if got := strFromAny(directedSpeechGate["decision"]); got != "target_speaker_follow_up" {
		t.Fatalf("participant_status.directed_speech_gate.decision = %q, want target_speaker_follow_up", got)
	}
	device, ok := bundle["device"].(map[string]any)
	if !ok {
		t.Fatalf("device = %#v, want object", bundle["device"])
	}
	if got := strFromAny(device["platform"]); got != "macOS" {
		t.Fatalf("device.platform = %q, want %q", got, "macOS")
	}
	if got := strFromAny(device["os_version"]); got != "14.4.0" {
		t.Fatalf("device.os_version = %q, want %q", got, "14.4.0")
	}
	if got := strFromAny(device["timezone"]); got != "Europe/Vienna" {
		t.Fatalf("device.timezone = %q, want %q", got, "Europe/Vienna")
	}
	if got := strFromAny(bundle["screenshot"]); got != screenshotPath {
		t.Fatalf("bundle screenshot = %q, want %q", got, screenshotPath)
	}
	if got := strFromAny(bundle["annotated_image"]); got != annotatedPath {
		t.Fatalf("bundle annotated_image = %q, want %q", got, annotatedPath)
	}
	if got := strFromAny(bundle["github_issue_url"]); got != "https://github.com/krystophny/tabura/issues/77" {
		t.Fatalf("bundle github_issue_url = %q", got)
	}
	if got := intFromAny(bundle["github_issue_number"], 0); got != 77 {
		t.Fatalf("bundle github_issue_number = %d, want 77", got)
	}
	if got := strFromAny(bundle["git_sha"]); !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(got) {
		t.Fatalf("git_sha = %q, want 40 hex chars", got)
	}
	canvasState, ok := bundle["canvas_state"].(map[string]any)
	if !ok {
		t.Fatalf("canvas_state = %#v, want object", bundle["canvas_state"])
	}
	if got := strFromAny(canvasState["artifact_title"]); got != "README.md" {
		t.Fatalf("canvas_state.artifact_title = %q, want %q", got, "README.md")
	}
	item, err := app.store.GetItemBySource("github", "krystophny/tabura#77")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("item.WorkspaceID = %v, want %d", item.WorkspaceID, workspace.ID)
	}
	if item.ArtifactID == nil {
		t.Fatal("expected GitHub issue artifact to be linked")
	}
	artifact, err := app.store.GetArtifact(*item.ArtifactID)
	if err != nil {
		t.Fatalf("GetArtifact() error: %v", err)
	}
	if artifact.Kind != store.ArtifactKindGitHubIssue {
		t.Fatalf("artifact.Kind = %q, want %q", artifact.Kind, store.ArtifactKindGitHubIssue)
	}
	createCall := ""
	for _, call := range ghCalls {
		if len(call) > 2 && call[1] == "issue" && call[2] == "create" {
			createCall = strings.Join(call[1:], " ")
			break
		}
	}
	if createCall == "" {
		t.Fatal("missing gh issue create call")
	}
	for _, needle := range []string{
		"--repo krystophny/tabura",
		"--label bug",
		"--label p0",
		"--title Bug report: The indicator froze after the tap",
		"## Evidence",
		"Sphere",
		"## Device",
		".tabura/artifacts/bugs/",
		"\"platform\": \"macOS\"",
		"\"os_version\": \"14.4.0\"",
	} {
		if !strings.Contains(createCall, needle) {
			t.Fatalf("create call = %q, missing %q", createCall, needle)
		}
	}
}

func TestHandleBugReportCreateFallsBackToWorkspaceSphere(t *testing.T) {
	app := newAuthedTestApp(t)
	workspaceDir := t.TempDir()
	initGitRepo(t, workspaceDir)
	addGitRemote(t, workspaceDir, "https://github.com/owner/tabula.git")
	workspace, err := app.store.CreateWorkspace("Tabura", workspaceDir, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	wantCWD := resolveBugReportGitHubCommandDir(workspaceDir)
	app.ghCommandRunner = func(_ context.Context, cwd string, args ...string) (string, error) {
		if cwd != wantCWD {
			t.Fatalf("gh cwd = %q, want %q", cwd, wantCWD)
		}
		if len(args) >= 3 && args[0] == "label" && args[1] == "list" {
			return `[{"name":"bug"},{"name":"p0"}]`, nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
			return "https://github.com/krystophny/tabura/issues/88\n", nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "view" {
			return `{"number":88,"title":"Bug report: Sphere fallback","url":"https://github.com/krystophny/tabura/issues/88","state":"OPEN","labels":[{"name":"bug"},{"name":"p0"}],"assignees":[]}`, nil
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/bugs/report", map[string]any{
		"note":                "Sphere fallback.",
		"screenshot_data_url": testPNGDataURL,
	})
	if rr.Code != 200 {
		t.Fatalf("POST /api/bugs/report status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	bundlePath := strFromAny(payload["bundle_path"])
	bundleBytes, err := os.ReadFile(filepath.Join(workspaceDir, filepath.FromSlash(bundlePath)))
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle map[string]any
	if err := json.Unmarshal(bundleBytes, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if got := strFromAny(bundle["active_sphere"]); got != store.SphereWork {
		t.Fatalf("active_sphere = %q, want %q", got, store.SphereWork)
	}
}

func TestHandleBugReportCreateUsesDefaultWorkspace(t *testing.T) {
	t.Chdir(t.TempDir())
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/bugs/report", map[string]any{
		"screenshot_data_url": testPNGDataURL,
	})
	if rr.Code != 200 {
		t.Fatalf("POST /api/bugs/report status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleBugReportCreateUsesLocalProjectFallback(t *testing.T) {
	dataDir := t.TempDir()
	localProjectDir := t.TempDir()
	initGitRepo(t, localProjectDir)
	addGitRemote(t, localProjectDir, "https://github.com/owner/tabula.git")
	app, err := New(dataDir, localProjectDir, "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := app.store.AddAuthSession(testAuthToken); err != nil {
		t.Fatalf("AddAuthSession() error: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})
	wantCWD := resolveBugReportGitHubCommandDir(localProjectDir)
	app.ghCommandRunner = func(_ context.Context, cwd string, args ...string) (string, error) {
		if cwd != wantCWD {
			t.Fatalf("gh cwd = %q, want %q", cwd, wantCWD)
		}
		if len(args) >= 3 && args[0] == "label" && args[1] == "list" {
			return `[{"name":"bug"},{"name":"p0"}]`, nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
			return "https://github.com/krystophny/tabura/issues/91\n", nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "view" {
			return `{"number":91,"title":"Bug report: Local project fallback","url":"https://github.com/krystophny/tabura/issues/91","state":"OPEN","labels":[{"name":"bug"},{"name":"p0"}],"assignees":[]}`, nil
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/bugs/report", map[string]any{
		"active_sphere":       store.SphereWork,
		"note":                "Local project fallback.",
		"screenshot_data_url": testPNGDataURL,
	})
	if rr.Code != 200 {
		t.Fatalf("POST /api/bugs/report status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	workspace, err := app.store.GetWorkspaceByPath(localProjectDir)
	if err != nil {
		t.Fatalf("GetWorkspaceByPath() error: %v", err)
	}
	if workspace.Sphere != store.SphereWork {
		t.Fatalf("workspace.Sphere = %q, want %q", workspace.Sphere, store.SphereWork)
	}
	item, err := app.store.GetItemBySource("github", "krystophny/tabura#91")
	if err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	if item.WorkspaceID == nil || *item.WorkspaceID != workspace.ID {
		t.Fatalf("item.WorkspaceID = %v, want %d", item.WorkspaceID, workspace.ID)
	}
}

func TestHandleBugReportCreateUsesCanonicalRepoFromNonGitWorkspace(t *testing.T) {
	app := newAuthedTestApp(t)
	workspaceDir := t.TempDir()
	workspace, err := app.store.CreateWorkspace("Hub", workspaceDir, store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	var calls [][]string
	wantCWD := resolveBugReportGitHubCommandDir(workspaceDir)
	app.ghCommandRunner = func(_ context.Context, cwd string, args ...string) (string, error) {
		calls = append(calls, append([]string{cwd}, args...))
		if cwd != wantCWD {
			t.Fatalf("gh cwd = %q, want %q", cwd, wantCWD)
		}
		if len(args) >= 3 && args[0] == "label" && args[1] == "list" {
			return `[{"name":"bug"},{"name":"p0"}]`, nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
			return "https://github.com/krystophny/tabura/issues/104\n", nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "view" {
			return `{"number":104,"title":"Bug report: Submission should work outside git repos","url":"https://github.com/krystophny/tabura/issues/104","state":"OPEN","labels":[{"name":"bug"},{"name":"p0"}],"assignees":[]}`, nil
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/bugs/report", map[string]any{
		"note":                "Submission should work outside git repos.",
		"screenshot_data_url": testPNGDataURL,
	})
	if rr.Code != 200 {
		t.Fatalf("POST /api/bugs/report status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	if got := intFromAny(payload["issue_number"], 0); got != 104 {
		t.Fatalf("issue_number = %d, want 104", got)
	}
	if got := strFromAny(payload["issue_url"]); got != "https://github.com/krystophny/tabura/issues/104" {
		t.Fatalf("issue_url = %q", got)
	}
	if _, err := app.store.GetItemBySource("github", "krystophny/tabura#104"); err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
	createCall := ""
	for _, call := range calls {
		if len(call) > 2 && call[1] == "issue" && call[2] == "create" {
			createCall = strings.Join(call[1:], " ")
			break
		}
	}
	if !strings.Contains(createCall, "--repo krystophny/tabura") {
		t.Fatalf("create call = %q, want canonical repo flag", createCall)
	}
}

func TestHandleBugReportCreateFallsBackToTaburaRepoWithoutWorkspace(t *testing.T) {
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)
	addGitRemote(t, repoDir, "https://github.com/krystophny/tabura.git")
	t.Chdir(repoDir)

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := app.store.AddAuthSession(testAuthToken); err != nil {
		t.Fatalf("AddAuthSession() error: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	wantCWD := resolveBugReportGitHubCommandDir("")
	app.ghCommandRunner = func(_ context.Context, cwd string, args ...string) (string, error) {
		if cwd != wantCWD {
			t.Fatalf("gh cwd = %q, want %q", cwd, wantCWD)
		}
		if len(args) >= 3 && args[0] == "label" && args[1] == "list" {
			return `[{"name":"bug"},{"name":"p0"}]`, nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "create" {
			return "https://github.com/krystophny/tabura/issues/118\n", nil
		}
		if len(args) >= 2 && args[0] == "issue" && args[1] == "view" {
			return `{"number":118,"title":"Bug report: Repo fallback","url":"https://github.com/krystophny/tabura/issues/118","state":"OPEN","labels":[{"name":"bug"},{"name":"p0"}],"assignees":[]}`, nil
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/bugs/report", map[string]any{
		"note":                "Repo fallback.",
		"screenshot_data_url": testPNGDataURL,
	})
	if rr.Code != 200 {
		t.Fatalf("POST /api/bugs/report status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	bundlePath := strFromAny(payload["bundle_path"])
	if !strings.HasPrefix(bundlePath, ".tabura/artifacts/bugs/") {
		t.Fatalf("bundle_path = %q, want .tabura/artifacts/bugs/... path", bundlePath)
	}
	// Bug report was filed against the default workspace (no local project dir configured)
	if _, err := app.store.GetItemBySource("github", "krystophny/tabura#118"); err != nil {
		t.Fatalf("GetItemBySource() error: %v", err)
	}
}

func TestHandleBugReportCreateSucceedsWhenIssueCreationFails(t *testing.T) {
	app := newAuthedTestApp(t)
	workspaceDir := t.TempDir()
	workspace, err := app.store.CreateWorkspace("Hub", workspaceDir, store.SpherePrivate)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	if err := app.store.SetActiveWorkspace(workspace.ID); err != nil {
		t.Fatalf("SetActiveWorkspace() error: %v", err)
	}
	app.ghCommandRunner = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "label" && args[1] == "list" {
			return "", errors.New("gh label list failed: permission denied")
		}
		t.Fatalf("unexpected gh invocation: %v", args)
		return "", nil
	}

	rr := doAuthedJSONRequest(t, app.Router(), "POST", "/api/bugs/report", map[string]any{
		"note":                "Submission should not fail when workspace is not a git repo.",
		"screenshot_data_url": testPNGDataURL,
	})
	if rr.Code != 200 {
		t.Fatalf("POST /api/bugs/report status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	bundlePath := strFromAny(payload["bundle_path"])
	if bundlePath == "" {
		t.Fatalf("bundle_path missing in payload: %#v", payload)
	}
	if got := strFromAny(payload["issue_url"]); got != "" {
		t.Fatalf("issue_url = %q, want empty on github failure", got)
	}
	if got := intFromAny(payload["issue_number"], 0); got != 0 {
		t.Fatalf("issue_number = %d, want 0 on github failure", got)
	}
	issueErr := strFromAny(payload["issue_error"])
	if !strings.Contains(strings.ToLower(issueErr), "permission denied") {
		t.Fatalf("issue_error = %q, want github failure", issueErr)
	}
	bundleBytes, err := os.ReadFile(filepath.Join(workspaceDir, filepath.FromSlash(bundlePath)))
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle map[string]any
	if err := json.Unmarshal(bundleBytes, &bundle); err != nil {
		t.Fatalf("decode bundle: %v", err)
	}
	if got := strFromAny(bundle["github_issue_error"]); got == "" {
		t.Fatalf("github_issue_error missing in bundle: %#v", bundle)
	}
	if got := strFromAny(bundle["github_issue_url"]); got != "" {
		t.Fatalf("github_issue_url = %q, want empty on github failure", got)
	}
}

func TestBugReportIssueTitleUsesBrowserLogFallback(t *testing.T) {
	bundle := bugReportBundle{
		BrowserLogs: []string{
			"2026-03-11T11:07:21Z error: TypeError: Cannot read properties of undefined (reading 'click')",
		},
		ActiveMode:      "pen",
		ActiveWorkspace: "2026/03/11",
	}

	title := bugReportIssueTitle(bundle)
	if title != "Bug report: TypeError: Cannot read properties of undefined (reading 'click')" {
		t.Fatalf("bugReportIssueTitle() = %q", title)
	}
	body := bugReportIssueBody(bundle, ".tabura/artifacts/bugs/20260311-110721-568aa357/bundle.json")
	if !strings.Contains(body, "## Summary\n\nTypeError: Cannot read properties of undefined (reading 'click')") {
		t.Fatalf("bugReportIssueBody() missing browser log summary:\n%s", body)
	}
}

func TestBugReportIssueTitleUsesStructuredFallbackWithoutFreeText(t *testing.T) {
	bundle := bugReportBundle{
		ActiveMode:      "pen",
		ActiveWorkspace: "2026/03/11",
	}

	title := bugReportIssueTitle(bundle)
	if title != "Bug report: pen interaction failed in 2026/03/11" {
		t.Fatalf("bugReportIssueTitle() = %q", title)
	}
	body := bugReportIssueBody(bundle, ".tabura/artifacts/bugs/20260311-110721-568aa357/bundle.json")
	if !strings.Contains(body, "## Summary\n\npen interaction failed in 2026/03/11") {
		t.Fatalf("bugReportIssueBody() missing structured summary:\n%s", body)
	}
}

func TestBugReportIssueTitleUsesCanvasStateFallbackForDefaultWorkspace(t *testing.T) {
	canvasState, err := json.Marshal(map[string]any{
		"interaction_surface":    "canvas",
		"interaction_tool":       "annotate",
		"last_input_origin":      "pen",
		"workspace_browser_path": "docs/interaction-grammar.md",
	})
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	bundle := bugReportBundle{
		ActiveWorkspace: "default",
		CanvasState:     canvasState,
	}

	title := bugReportIssueTitle(bundle)
	if title != "Bug report: interaction failed in default while browsing docs/interaction-grammar.md" {
		t.Fatalf("bugReportIssueTitle() = %q", title)
	}
	body := bugReportIssueBody(bundle, ".tabura/artifacts/bugs/20260321-161441-a83c6a31/bundle.json")
	for _, needle := range []string{
		"## Summary\n\ninteraction failed in default while browsing docs/interaction-grammar.md",
		"- Interaction surface: `canvas`",
		"- Interaction tool: `annotate`",
		"- Last input origin: `pen`",
		"- Workspace browser path: `docs/interaction-grammar.md`",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("bugReportIssueBody() missing %q:\n%s", needle, body)
		}
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "tabura@example.com"},
		{"git", "config", "user.name", "Tabura Test"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, string(out))
		}
	}
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	commitCommands := [][]string{
		{"git", "add", "README.md"},
		{"git", "commit", "-m", "init"},
	}
	for _, args := range commitCommands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), err, string(out))
		}
	}
}

func addGitRemote(t *testing.T, dir, remoteURL string) {
	t.Helper()
	cmd := exec.Command("git", "remote", "add", "origin", remoteURL)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote add origin failed: %v\n%s", err, string(out))
	}
}
