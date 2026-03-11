package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "tabura.db"))
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func TestStoreAdminPasswordAndAuthLifecycle(t *testing.T) {
	s := newTestStore(t)

	if s.HasAdminPassword() {
		t.Fatalf("HasAdminPassword() = true, want false")
	}
	if err := s.SetAdminPassword("short"); err == nil {
		t.Fatalf("expected short password error")
	}
	if err := s.SetAdminPassword("very-strong-pass"); err != nil {
		t.Fatalf("SetAdminPassword() error: %v", err)
	}
	if !s.HasAdminPassword() {
		t.Fatalf("HasAdminPassword() = false, want true")
	}
	if !s.VerifyAdminPassword("very-strong-pass") {
		t.Fatalf("VerifyAdminPassword(correct) = false, want true")
	}
	if s.VerifyAdminPassword("wrong-password") {
		t.Fatalf("VerifyAdminPassword(wrong) = true, want false")
	}

	if err := s.AddAuthSession("tok-1"); err != nil {
		t.Fatalf("AddAuthSession() error: %v", err)
	}
	if !s.HasAuthSession("tok-1") {
		t.Fatalf("HasAuthSession(tok-1) = false, want true")
	}
	if err := s.SetAdminPassword("another-strong-pass"); err != nil {
		t.Fatalf("SetAdminPassword(second) error: %v", err)
	}
	if s.HasAuthSession("tok-1") {
		t.Fatalf("expected auth sessions to be cleared when admin password changes")
	}
	if err := s.AddAuthSession(""); err == nil {
		t.Fatalf("expected AddAuthSession(empty) to fail")
	}
	if err := s.DeleteAuthSession(""); err != nil {
		t.Fatalf("DeleteAuthSession(empty) should be noop: %v", err)
	}
}

func TestStoreHostAndRemoteSessionCRUD(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.AddHost(HostConfig{}); err == nil {
		t.Fatalf("expected AddHost() validation error")
	}

	h2, err := s.AddHost(HostConfig{
		Name:       "zeta",
		Hostname:   "zeta.local",
		Port:       0,
		Username:   "u2",
		KeyPath:    "/tmp/key2",
		ProjectDir: "/tmp/p2",
	})
	if err != nil {
		t.Fatalf("AddHost(zeta) error: %v", err)
	}
	if h2.Port != 22 {
		t.Fatalf("default port = %d, want 22", h2.Port)
	}
	h1, err := s.AddHost(HostConfig{
		Name:       "alpha",
		Hostname:   "alpha.local",
		Port:       2202,
		Username:   "u1",
		KeyPath:    "/tmp/key1",
		ProjectDir: "/tmp/p1",
	})
	if err != nil {
		t.Fatalf("AddHost(alpha) error: %v", err)
	}

	list, err := s.ListHosts()
	if err != nil {
		t.Fatalf("ListHosts() error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListHosts() len = %d, want 2", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "zeta" {
		t.Fatalf("ListHosts() should be name-sorted, got %#v", []string{list[0].Name, list[1].Name})
	}

	updated, err := s.UpdateHost(h1.ID, map[string]interface{}{"username": "updated-user", "port": 2222})
	if err != nil {
		t.Fatalf("UpdateHost() error: %v", err)
	}
	if updated.Username != "updated-user" || updated.Port != 2222 {
		t.Fatalf("UpdateHost() did not apply fields: %+v", updated)
	}

	if err := s.AddRemoteSession("sid-1", h1.ID); err != nil {
		t.Fatalf("AddRemoteSession(sid-1) error: %v", err)
	}
	if err := s.AddRemoteSession("sid-2", h2.ID); err != nil {
		t.Fatalf("AddRemoteSession(sid-2) error: %v", err)
	}
	remote, err := s.ListRemoteSessions()
	if err != nil {
		t.Fatalf("ListRemoteSessions() error: %v", err)
	}
	if len(remote) != 2 {
		t.Fatalf("ListRemoteSessions() len = %d, want 2", len(remote))
	}
	if err := s.DeleteRemoteSession("sid-1"); err != nil {
		t.Fatalf("DeleteRemoteSession() error: %v", err)
	}
	remote, err = s.ListRemoteSessions()
	if err != nil {
		t.Fatalf("ListRemoteSessions(after delete) error: %v", err)
	}
	if len(remote) != 1 {
		t.Fatalf("ListRemoteSessions() len after delete = %d, want 1", len(remote))
	}

	if err := s.DeleteHost(h1.ID); err != nil {
		t.Fatalf("DeleteHost() error: %v", err)
	}
	if _, err := s.GetHost(h1.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetHost(deleted) error = %v, want sql.ErrNoRows", err)
	}
}

func TestStoreProjectLifecycleAndAppState(t *testing.T) {
	s := newTestStore(t)
	rootA := filepath.Join(t.TempDir(), "workspace-a")
	rootB := filepath.Join(t.TempDir(), "workspace-b")
	rootMeeting := filepath.Join(t.TempDir(), "meeting-a")

	if _, err := s.CreateProject("", "key-a", rootA, "managed", "", "", false); err == nil {
		t.Fatalf("expected empty project name validation error")
	}

	p1, err := s.CreateProject("Project A", "key-a", rootA, "unknown-kind", "http://127.0.0.1:9420/mcp", "", false)
	if err != nil {
		t.Fatalf("CreateProject(p1) error: %v", err)
	}
	if p1.Kind != "managed" {
		t.Fatalf("project kind = %q, want managed", p1.Kind)
	}
	if p1.CanvasSessionID == "" {
		t.Fatalf("CanvasSessionID should be auto-generated")
	}

	p2, err := s.CreateProject("Project B", "key-b", rootB, "linked", "", "canvas-b", true)
	if err != nil {
		t.Fatalf("CreateProject(p2) error: %v", err)
	}
	if !p2.IsDefault {
		t.Fatalf("p2 should be default")
	}

	meeting, err := s.CreateProject("Meeting Temp", "key-meeting", rootMeeting, "meeting", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject(meeting) error: %v", err)
	}
	if meeting.Kind != "meeting" {
		t.Fatalf("meeting kind = %q, want meeting", meeting.Kind)
	}

	gotByKey, err := s.GetProjectByProjectKey("key-a")
	if err != nil {
		t.Fatalf("GetProjectByProjectKey(key-a) error: %v", err)
	}
	if gotByKey.ID != p1.ID {
		t.Fatalf("GetProjectByProjectKey returned %q, want %q", gotByKey.ID, p1.ID)
	}
	gotByPath, err := s.GetProjectByRootPath(rootB)
	if err != nil {
		t.Fatalf("GetProjectByRootPath(rootB) error: %v", err)
	}
	if gotByPath.ID != p2.ID {
		t.Fatalf("GetProjectByRootPath returned %q, want %q", gotByPath.ID, p2.ID)
	}
	gotByCanvas, err := s.GetProjectByCanvasSession("canvas-b")
	if err != nil {
		t.Fatalf("GetProjectByCanvasSession(canvas-b) error: %v", err)
	}
	if gotByCanvas.ID != p2.ID {
		t.Fatalf("GetProjectByCanvasSession returned %q, want %q", gotByCanvas.ID, p2.ID)
	}

	if err := s.UpdateProjectRuntime(p1.ID, "http://127.0.0.1:1111/mcp", "canvas-a"); err != nil {
		t.Fatalf("UpdateProjectRuntime() error: %v", err)
	}
	if err := s.UpdateProjectChatModel(p1.ID, "  SPARK  "); err != nil {
		t.Fatalf("UpdateProjectChatModel() error: %v", err)
	}
	if err := s.UpdateProjectChatModelReasoningEffort(p1.ID, "  HIGH "); err != nil {
		t.Fatalf("UpdateProjectChatModelReasoningEffort() error: %v", err)
	}
	if err := s.UpdateProjectCompanionConfig(p1.ID, `{"companion_enabled":false,"idle_surface":"black"}`); err != nil {
		t.Fatalf("UpdateProjectCompanionConfig() error: %v", err)
	}
	p1Updated, err := s.GetProject(p1.ID)
	if err != nil {
		t.Fatalf("GetProject(updated p1) error: %v", err)
	}
	if p1Updated.ChatModel != "spark" {
		t.Fatalf("ChatModel = %q, want spark", p1Updated.ChatModel)
	}
	if p1Updated.ChatModelReasoningEffort != "high" {
		t.Fatalf("ChatModelReasoningEffort = %q, want high", p1Updated.ChatModelReasoningEffort)
	}
	if got := strings.TrimSpace(p1Updated.CompanionConfigJSON); got != `{"companion_enabled":false,"idle_surface":"black"}` {
		t.Fatalf("CompanionConfigJSON = %q", got)
	}

	beforeTouch := p1Updated.LastOpenedAt
	time.Sleep(1100 * time.Millisecond)
	if err := s.TouchProject(p1.ID); err != nil {
		t.Fatalf("TouchProject() error: %v", err)
	}
	p1Touched, err := s.GetProject(p1.ID)
	if err != nil {
		t.Fatalf("GetProject(touched p1) error: %v", err)
	}
	if p1Touched.LastOpenedAt <= beforeTouch {
		t.Fatalf("TouchProject() did not advance LastOpenedAt")
	}

	projects, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects() error: %v", err)
	}
	if len(projects) != 3 {
		t.Fatalf("ListProjects() len = %d, want 3", len(projects))
	}
	if !projects[0].IsDefault {
		t.Fatalf("first listed project should be default")
	}

	if err := s.SetActiveProjectID(""); err == nil {
		t.Fatalf("expected SetActiveProjectID(empty) validation error")
	}
	if err := s.SetActiveProjectID(p2.ID); err != nil {
		t.Fatalf("SetActiveProjectID() error: %v", err)
	}
	activeID, err := s.ActiveProjectID()
	if err != nil {
		t.Fatalf("ActiveProjectID() error: %v", err)
	}
	if activeID != p2.ID {
		t.Fatalf("ActiveProjectID() = %q, want %q", activeID, p2.ID)
	}

	if err := s.UpdateProjectKind(meeting.ID, "managed"); err != nil {
		t.Fatalf("UpdateProjectKind() error: %v", err)
	}
	meetingManaged, err := s.GetProject(meeting.ID)
	if err != nil {
		t.Fatalf("GetProject(meeting managed) error: %v", err)
	}
	if meetingManaged.Kind != "managed" {
		t.Fatalf("meeting managed kind = %q, want managed", meetingManaged.Kind)
	}
}

func TestStoreProjectCompanionConfigPersistsAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "tabura.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}

	root := filepath.Join(t.TempDir(), "workspace")
	project, err := s.CreateProject("Project A", "key-a", root, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := s.UpdateProjectCompanionConfig(project.ID, `{"companion_enabled":false,"language":"de","idle_surface":"black"}`); err != nil {
		t.Fatalf("UpdateProjectCompanionConfig() error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	reopened, err := New(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer func() {
		_ = reopened.Close()
	}()
	got, err := reopened.GetProject(project.ID)
	if err != nil {
		t.Fatalf("GetProject() after reopen error: %v", err)
	}
	if strings.TrimSpace(got.CompanionConfigJSON) != `{"companion_enabled":false,"language":"de","idle_surface":"black"}` {
		t.Fatalf("CompanionConfigJSON after reopen = %q", got.CompanionConfigJSON)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected db at %s: %v", dbPath, err)
	}
}

func TestStoreChatSessionMessageAndThreading(t *testing.T) {
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "workspace-default")
	project, err := s.CreateProject("Default", root, root, "managed", "", "", true)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := s.SetActiveProjectID(project.ID); err != nil {
		t.Fatalf("SetActiveProjectID() error: %v", err)
	}

	session, err := s.GetOrCreateChatSession("  ")
	if err != nil {
		t.Fatalf("GetOrCreateChatSession(active project) error: %v", err)
	}
	if session.ProjectKey != project.ProjectKey {
		t.Fatalf("project key = %q, want %q", session.ProjectKey, project.ProjectKey)
	}
	if session.WorkspaceID <= 0 {
		t.Fatalf("workspace_id = %d, want positive id", session.WorkspaceID)
	}
	same, err := s.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession(existing) error: %v", err)
	}
	if same.ID != session.ID {
		t.Fatalf("expected same chat session id, got %q vs %q", same.ID, session.ID)
	}

	updatedSession, err := s.UpdateChatSessionMode(session.ID, "plan")
	if err != nil {
		t.Fatalf("UpdateChatSessionMode(plan) error: %v", err)
	}
	if updatedSession.Mode != "plan" {
		t.Fatalf("mode = %q, want plan", updatedSession.Mode)
	}
	updatedSession, err = s.UpdateChatSessionMode(session.ID, "review")
	if err != nil {
		t.Fatalf("UpdateChatSessionMode(review) error: %v", err)
	}
	if updatedSession.Mode != "review" {
		t.Fatalf("mode = %q, want review", updatedSession.Mode)
	}
	updatedSession, err = s.UpdateChatSessionMode(session.ID, "not-a-mode")
	if err != nil {
		t.Fatalf("UpdateChatSessionMode(invalid) error: %v", err)
	}
	if updatedSession.Mode != "chat" {
		t.Fatalf("mode = %q, want chat fallback", updatedSession.Mode)
	}

	if err := s.UpdateChatSessionThread(session.ID, "thread-1"); err != nil {
		t.Fatalf("UpdateChatSessionThread() error: %v", err)
	}
	sessionWithThread, err := s.GetChatSession(session.ID)
	if err != nil {
		t.Fatalf("GetChatSession() error: %v", err)
	}
	if sessionWithThread.AppThreadID != "thread-1" {
		t.Fatalf("AppThreadID = %q, want thread-1", sessionWithThread.AppThreadID)
	}

	msg1, err := s.AddChatMessage(session.ID, "invalid-role", "m1", "p1", "canvas")
	if err != nil {
		t.Fatalf("AddChatMessage(msg1) error: %v", err)
	}
	if msg1.Role != "user" {
		t.Fatalf("msg1 role = %q, want user", msg1.Role)
	}
	if msg1.RenderFormat != "text" {
		t.Fatalf("msg1 render format = %q, want text", msg1.RenderFormat)
	}

	msg2, err := s.AddChatMessage(session.ID, "assistant", "m2", "p2", "markdown", WithThreadKey("thread-a"))
	if err != nil {
		t.Fatalf("AddChatMessage(msg2) error: %v", err)
	}
	if msg2.ThreadKey != "thread-a" {
		t.Fatalf("msg2 thread key = %q, want thread-a", msg2.ThreadKey)
	}
	msg3, err := s.AddChatMessage(
		session.ID,
		"assistant",
		"m3",
		"p3",
		"markdown",
		WithThreadKey("thread-b"),
		WithProviderMetadata("OpenAI", "gpt-5.3-codex-spark", 321),
	)
	if err != nil {
		t.Fatalf("AddChatMessage(msg3) error: %v", err)
	}
	if msg3.Provider != "openai" {
		t.Fatalf("msg3 provider = %q, want openai", msg3.Provider)
	}
	if msg3.ProviderModel != "gpt-5.3-codex-spark" {
		t.Fatalf("msg3 provider_model = %q, want gpt-5.3-codex-spark", msg3.ProviderModel)
	}
	if msg3.ProviderLatency != 321 {
		t.Fatalf("msg3 provider_latency = %d, want 321", msg3.ProviderLatency)
	}

	defaultThreadMessages, err := s.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages(default thread) error: %v", err)
	}
	if len(defaultThreadMessages) != 1 {
		t.Fatalf("default thread message count = %d, want 1", len(defaultThreadMessages))
	}

	threadMessages, err := s.ListChatMessages(session.ID, 10, WithThreadKey("thread-a"))
	if err != nil {
		t.Fatalf("ListChatMessages(thread-a) error: %v", err)
	}
	if len(threadMessages) != 1 || threadMessages[0].ID != msg2.ID {
		t.Fatalf("thread-a message selection mismatch")
	}
	threadMessages, err = s.ListChatMessages(session.ID, 10, WithThreadKey("thread-b"))
	if err != nil {
		t.Fatalf("ListChatMessages(thread-b) error: %v", err)
	}
	if len(threadMessages) != 1 || threadMessages[0].ID != msg3.ID {
		t.Fatalf("thread-b message selection mismatch")
	}
	if threadMessages[0].Provider != "openai" {
		t.Fatalf("thread-b provider = %q, want openai", threadMessages[0].Provider)
	}

	if err := s.UpdateChatMessageContent(msg2.ID, "m2-updated", "p2-updated", "canvas", WithProviderMetadata("local", "qwen3.5-9b", 27)); err != nil {
		t.Fatalf("UpdateChatMessageContent() error: %v", err)
	}
	threadMessages, err = s.ListChatMessages(session.ID, 10, WithThreadKey("thread-a"))
	if err != nil {
		t.Fatalf("ListChatMessages(thread-a after update) error: %v", err)
	}
	if threadMessages[0].RenderFormat != "text" {
		t.Fatalf("updated message render format = %q, want text", threadMessages[0].RenderFormat)
	}
	if threadMessages[0].Provider != "local" {
		t.Fatalf("updated message provider = %q, want local", threadMessages[0].Provider)
	}
	if threadMessages[0].ProviderModel != "qwen3.5-9b" {
		t.Fatalf("updated message provider_model = %q, want qwen3.5-9b", threadMessages[0].ProviderModel)
	}
	if threadMessages[0].ProviderLatency != 27 {
		t.Fatalf("updated message provider_latency = %d, want 27", threadMessages[0].ProviderLatency)
	}
	if err := s.UpdateChatMessageContent(0, "x", "y", "markdown"); err == nil {
		t.Fatalf("expected invalid message id validation error")
	}

	if err := s.AddChatEvent(session.ID, "turn-1", "turn_started", `{"ok":true}`); err != nil {
		t.Fatalf("AddChatEvent() error: %v", err)
	}

	if err := s.ResetChatSessionThread(session.ID); err != nil {
		t.Fatalf("ResetChatSessionThread() error: %v", err)
	}
	sessionReset, err := s.GetChatSession(session.ID)
	if err != nil {
		t.Fatalf("GetChatSession(after reset) error: %v", err)
	}
	if sessionReset.AppThreadID != "" {
		t.Fatalf("expected AppThreadID to be cleared")
	}
	if err := s.ResetAllChatSessionThreads(); err != nil {
		t.Fatalf("ResetAllChatSessionThreads() error: %v", err)
	}

	if err := s.ClearChatMessages(session.ID); err != nil {
		t.Fatalf("ClearChatMessages() error: %v", err)
	}
	remaining, err := s.ListChatMessages(session.ID, 10)
	if err != nil {
		t.Fatalf("ListChatMessages(after clear) error: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining messages = %d, want 0", len(remaining))
	}
	if err := s.ClearAllChatMessages(); err != nil {
		t.Fatalf("ClearAllChatMessages() error: %v", err)
	}
	if err := s.ClearAllChatEvents(); err != nil {
		t.Fatalf("ClearAllChatEvents() error: %v", err)
	}
}

func TestStoreChatSessionsKeyToWorkspace(t *testing.T) {
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "workspace-alpha")
	project, err := s.CreateProject("Alpha", "alpha-key", root, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	session, err := s.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	if session.WorkspaceID <= 0 {
		t.Fatalf("workspace_id = %d, want positive id", session.WorkspaceID)
	}

	columns, err := s.tableColumnNames("chat_sessions")
	if err != nil {
		t.Fatalf("tableColumnNames(chat_sessions) error: %v", err)
	}
	if containsString(columns, "project_key") {
		t.Fatalf("chat_sessions columns still include project_key: %v", columns)
	}
	if !containsString(columns, "workspace_id") {
		t.Fatalf("chat_sessions columns missing workspace_id: %v", columns)
	}

	byWorkspace, err := s.GetChatSessionByWorkspaceID(session.WorkspaceID)
	if err != nil {
		t.Fatalf("GetChatSessionByWorkspaceID() error: %v", err)
	}
	if byWorkspace.ID != session.ID {
		t.Fatalf("GetChatSessionByWorkspaceID() = %q, want %q", byWorkspace.ID, session.ID)
	}

	var sessionCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_sessions WHERE workspace_id = ?`, session.WorkspaceID).Scan(&sessionCount); err != nil {
		t.Fatalf("count chat_sessions by workspace_id: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("chat session count for workspace %d = %d, want 1", session.WorkspaceID, sessionCount)
	}
}

func TestGetOrCreateChatSessionPrefersLinkedWorkspaceOverLegacyProjectPath(t *testing.T) {
	s := newTestStore(t)
	projectRoot := filepath.Join(t.TempDir(), "project-root")
	project, err := s.CreateProject("Alpha", "alpha-key", projectRoot, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	workspace, err := s.CreateWorkspace("Alpha", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	workspace, err = s.SetWorkspaceProject(workspace.ID, &project.ID)
	if err != nil {
		t.Fatalf("SetWorkspaceProject() error: %v", err)
	}

	relocatedRoot := filepath.Join(t.TempDir(), "workspace-relocated")
	if err := os.MkdirAll(relocatedRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(relocatedRoot) error: %v", err)
	}
	workspace, err = s.UpdateWorkspaceLocation(workspace.ID, workspace.Name, relocatedRoot)
	if err != nil {
		t.Fatalf("UpdateWorkspaceLocation() error: %v", err)
	}

	session, err := s.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	if session.WorkspaceID != workspace.ID {
		t.Fatalf("workspace_id = %d, want %d", session.WorkspaceID, workspace.ID)
	}

	workspaces, err := s.ListWorkspacesForProject(project.ID)
	if err != nil {
		t.Fatalf("ListWorkspacesForProject() error: %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("linked workspace count = %d, want 1", len(workspaces))
	}
	if workspaces[0].DirPath != relocatedRoot {
		t.Fatalf("linked workspace dir_path = %q, want %q", workspaces[0].DirPath, relocatedRoot)
	}
}

func TestGetOrCreateChatSessionCreatesWorkspaceForLegacyProjectFallback(t *testing.T) {
	s := newTestStore(t)
	projectRoot := filepath.Join(t.TempDir(), "project-root")
	project, err := s.CreateProject("Alpha", "alpha-key", projectRoot, "managed", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}

	session, err := s.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	if session.WorkspaceID <= 0 {
		t.Fatalf("workspace_id = %d, want positive id", session.WorkspaceID)
	}
	workspace, err := s.GetWorkspace(session.WorkspaceID)
	if err != nil {
		t.Fatalf("GetWorkspace() error: %v", err)
	}
	if workspace.DirPath != project.RootPath {
		t.Fatalf("workspace dir_path = %q, want %q", workspace.DirPath, project.RootPath)
	}
	if workspace.ProjectID == nil || *workspace.ProjectID != project.ID {
		t.Fatalf("workspace project_id = %v, want %q", workspace.ProjectID, project.ID)
	}
}

func TestStoreSchemaAndHelperNormalizers(t *testing.T) {
	s := newTestStore(t)

	columns, err := s.TableColumns()
	if err != nil {
		t.Fatalf("TableColumns() error: %v", err)
	}
	if len(columns) == 0 {
		t.Fatalf("TableColumns() should not be empty")
	}
	chatMessageCols := strings.Join(columns["chat_messages"], ",")
	if !strings.Contains(chatMessageCols, "thread_key") {
		t.Fatalf("chat_messages missing thread_key column: %q", chatMessageCols)
	}
	if !strings.Contains(chatMessageCols, "provider") || !strings.Contains(chatMessageCols, "provider_model") || !strings.Contains(chatMessageCols, "provider_latency_ms") {
		t.Fatalf("chat_messages missing provider columns: %q", chatMessageCols)
	}
	projectCols := strings.Join(columns["projects"], ",")
	if !strings.Contains(projectCols, "canvas_session_id") {
		t.Fatalf("projects missing canvas_session_id column: %q", projectCols)
	}
	workspaceCols := strings.Join(columns["workspaces"], ",")
	if !strings.Contains(workspaceCols, "canvas_session_id") {
		t.Fatalf("workspaces missing canvas_session_id column: %q", workspaceCols)
	}
	if !strings.Contains(workspaceCols, "chat_model_reasoning_effort") {
		t.Fatalf("workspaces missing chat_model_reasoning_effort column: %q", workspaceCols)
	}

	if got := normalizeProjectKind(" LINKED "); got != "linked" {
		t.Fatalf("normalizeProjectKind(linked) = %q, want linked", got)
	}
	if got := normalizeProjectKind(" Meeting "); got != "meeting" {
		t.Fatalf("normalizeProjectKind(meeting) = %q, want meeting", got)
	}
	if got := normalizeProjectKind(" TASK "); got != "task" {
		t.Fatalf("normalizeProjectKind(task) = %q, want task", got)
	}
	if got := normalizeProjectKind("weird"); got != "managed" {
		t.Fatalf("normalizeProjectKind(default) = %q, want managed", got)
	}
	if got := normalizeProjectName("  hello  "); got != "hello" {
		t.Fatalf("normalizeProjectName() = %q, want hello", got)
	}
	if got := normalizeProjectChatModel("  Spark "); got != "spark" {
		t.Fatalf("normalizeProjectChatModel() = %q, want spark", got)
	}
	if got := normalizeProjectChatModelReasoningEffort(" High "); got != "high" {
		t.Fatalf("normalizeProjectChatModelReasoningEffort() = %q, want high", got)
	}
	if got := normalizeChatMode("plan"); got != "plan" {
		t.Fatalf("normalizeChatMode(plan) = %q, want plan", got)
	}
	if got := normalizeChatMode("other"); got != "chat" {
		t.Fatalf("normalizeChatMode(default) = %q, want chat", got)
	}
	if got := normalizeChatRole("assistant"); got != "assistant" {
		t.Fatalf("normalizeChatRole(assistant) = %q, want assistant", got)
	}
	if got := normalizeChatRole("weird"); got != "user" {
		t.Fatalf("normalizeChatRole(default) = %q, want user", got)
	}
	if got := normalizeRenderFormat("canvas"); got != "text" {
		t.Fatalf("normalizeRenderFormat(canvas) = %q, want text", got)
	}
	if got := normalizeRenderFormat("unknown"); got != "markdown" {
		t.Fatalf("normalizeRenderFormat(default) = %q, want markdown", got)
	}
	if got := stringsJoin([]string{"a", "b", "c"}, ","); got != "a,b,c" {
		t.Fatalf("stringsJoin() = %q, want a,b,c", got)
	}
	if got := boolToInt(true); got != 1 {
		t.Fatalf("boolToInt(true) = %d, want 1", got)
	}
	if got := boolToInt(false); got != 0 {
		t.Fatalf("boolToInt(false) = %d, want 0", got)
	}
}

func TestStoreDeleteProjectRemovesAssociatedSessions(t *testing.T) {
	s := newTestStore(t)
	root := filepath.Join(t.TempDir(), "meeting-temp")
	project, err := s.CreateProject("Meeting Temp", "meeting-key", root, "meeting", "", "", false)
	if err != nil {
		t.Fatalf("CreateProject() error: %v", err)
	}
	if err := s.SetActiveProjectID(project.ID); err != nil {
		t.Fatalf("SetActiveProjectID() error: %v", err)
	}
	chatSession, err := s.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("GetOrCreateChatSession() error: %v", err)
	}
	if _, err := s.AddChatMessage(chatSession.ID, "assistant", "saved output", "saved output", "markdown"); err != nil {
		t.Fatalf("AddChatMessage() error: %v", err)
	}
	if err := s.AddChatEvent(chatSession.ID, "turn-1", "assistant_output", `{"ok":true}`); err != nil {
		t.Fatalf("AddChatEvent() error: %v", err)
	}
	participantSession, err := s.AddParticipantSession(project.ProjectKey, "{}")
	if err != nil {
		t.Fatalf("AddParticipantSession() error: %v", err)
	}
	if _, err := s.AddParticipantSegment(ParticipantSegment{
		SessionID: participantSession.ID,
		StartTS:   100,
		EndTS:     101,
		Text:      "text artifact only",
		Status:    "final",
	}); err != nil {
		t.Fatalf("AddParticipantSegment() error: %v", err)
	}
	if err := s.AddParticipantEvent(participantSession.ID, 0, "segment_committed", `{"text":"text artifact only"}`); err != nil {
		t.Fatalf("AddParticipantEvent() error: %v", err)
	}
	if err := s.UpsertParticipantRoomState(participantSession.ID, "summary", `["Acme"]`, `["Decision"]`); err != nil {
		t.Fatalf("UpsertParticipantRoomState() error: %v", err)
	}

	if err := s.DeleteProject(project.ID); err != nil {
		t.Fatalf("DeleteProject() error: %v", err)
	}
	if _, err := s.GetProject(project.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetProject(deleted) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetChatSession(chatSession.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetChatSession(deleted) error = %v, want sql.ErrNoRows", err)
	}
	if _, err := s.GetParticipantSession(participantSession.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetParticipantSession(deleted) error = %v, want sql.ErrNoRows", err)
	}
	activeID, err := s.ActiveProjectID()
	if err != nil {
		t.Fatalf("ActiveProjectID() error: %v", err)
	}
	if activeID != "" {
		t.Fatalf("ActiveProjectID() = %q, want empty", activeID)
	}
}
