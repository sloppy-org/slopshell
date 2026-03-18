package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/krystophny/tabura/internal/store"
)

func TestWorkspaceFocusAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	anchor, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	focusPath := filepath.Join(t.TempDir(), "plasma")
	if err := os.MkdirAll(focusPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(focusPath): %v", err)
	}
	focus, err := app.store.CreateWorkspace("Plasma", focusPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(focus): %v", err)
	}

	rrPost := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspace/focus", map[string]any{
		"workspace_id": focus.ID,
	})
	if rrPost.Code != http.StatusOK {
		t.Fatalf("POST /api/workspace/focus status = %d, want 200: %s", rrPost.Code, rrPost.Body.String())
	}
	postPayload := decodeJSONDataResponse(t, rrPost)
	if got := int64(postPayload["anchor"].(map[string]any)["id"].(float64)); got != anchor.ID {
		t.Fatalf("anchor id = %d, want %d", got, anchor.ID)
	}
	if got := int64(postPayload["focus"].(map[string]any)["id"].(float64)); got != focus.ID {
		t.Fatalf("focus id = %d, want %d", got, focus.ID)
	}
	if explicit, _ := postPayload["explicit"].(bool); !explicit {
		t.Fatalf("explicit = %#v, want true", postPayload["explicit"])
	}

	rrDelete := doAuthedJSONRequest(t, app.Router(), http.MethodDelete, "/api/workspace/focus", nil)
	if rrDelete.Code != http.StatusOK {
		t.Fatalf("DELETE /api/workspace/focus status = %d, want 200: %s", rrDelete.Code, rrDelete.Body.String())
	}
	deletePayload := decodeJSONDataResponse(t, rrDelete)
	if got := int64(deletePayload["focus"].(map[string]any)["id"].(float64)); got != anchor.ID {
		t.Fatalf("cleared focus id = %d, want %d", got, anchor.ID)
	}
	if explicit, _ := deletePayload["explicit"].(bool); explicit {
		t.Fatalf("explicit after clear = %#v, want false", deletePayload["explicit"])
	}
}

func TestWorkspaceFocusBroadcastsWebsocketChanges(t *testing.T) {
	app := newAuthedTestApp(t)

	anchor, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.store.GetOrCreateChatSessionForWorkspace(anchor.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace: %v", err)
	}
	focus, err := app.store.CreateWorkspace("Plasma", filepath.Join(t.TempDir(), "plasma"))
	if err != nil {
		t.Fatalf("CreateWorkspace(focus): %v", err)
	}

	conn, clientConn, cleanup := newParticipantTestWSConn(t)
	defer cleanup()
	app.hub.registerChat(session.ID, conn)
	defer app.hub.unregisterChat(session.ID, conn)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/workspace/focus", map[string]any{
		"workspace_id": focus.ID,
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/workspace/focus status = %d, want 200", rr.Code)
	}

	payload := waitForWSJSONMessageType(t, clientConn, 2*time.Second, "workspace_focus_changed")
	if got := int64(payload["anchor"].(map[string]any)["id"].(float64)); got != anchor.ID {
		t.Fatalf("ws anchor id = %d, want %d", got, anchor.ID)
	}
	if got := int64(payload["focus"].(map[string]any)["id"].(float64)); got != focus.ID {
		t.Fatalf("ws focus id = %d, want %d", got, focus.ID)
	}
}

func TestFocusedWorkspaceShellCommandUsesFocusCWD(t *testing.T) {
	app := newAuthedTestApp(t)

	anchor, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.store.GetOrCreateChatSessionForWorkspace(anchor.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace: %v", err)
	}
	focusPath := filepath.Join(t.TempDir(), "focused")
	if err := os.MkdirAll(focusPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(focusPath): %v", err)
	}
	focus, err := app.store.CreateWorkspace("Focused", focusPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(focus): %v", err)
	}
	if err := app.setFocusedWorkspace(focus.ID); err != nil {
		t.Fatalf("setFocusedWorkspace: %v", err)
	}

	message, payload, err := app.executeSystemAction(session.ID, session, &SystemAction{
		Action: "shell",
		Params: map[string]interface{}{"command": "pwd"},
	})
	if err != nil {
		t.Fatalf("executeSystemAction(shell): %v", err)
	}
	if got := strFromAny(payload["cwd"]); got != focusPath {
		t.Fatalf("shell cwd = %q, want %q", got, focusPath)
	}
	if !containsLine(message, focusPath) {
		t.Fatalf("shell output = %q, want line %q", message, focusPath)
	}
}

func TestFocusedWorkspaceLeavesChatSessionAnchored(t *testing.T) {
	app := newAuthedTestApp(t)

	anchor, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.store.GetOrCreateChatSessionForWorkspace(anchor.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace: %v", err)
	}
	focusPath := filepath.Join(t.TempDir(), "focused")
	if err := os.MkdirAll(focusPath, 0o755); err != nil {
		t.Fatalf("MkdirAll(focusPath): %v", err)
	}
	focus, err := app.store.CreateWorkspace("Focused", focusPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(focus): %v", err)
	}
	if err := app.setFocusedWorkspace(focus.ID); err != nil {
		t.Fatalf("setFocusedWorkspace: %v", err)
	}

	if _, _, err := app.executeSystemAction(session.ID, session, &SystemAction{
		Action: "shell",
		Params: map[string]interface{}{"command": "pwd"},
	}); err != nil {
		t.Fatalf("executeSystemAction(shell): %v", err)
	}

	reloadedSession, err := app.store.GetChatSession(session.ID)
	if err != nil {
		t.Fatalf("GetChatSession(): %v", err)
	}
	if reloadedSession.WorkspaceID != anchor.ID {
		t.Fatalf("chat session workspace = %d, want anchor %d", reloadedSession.WorkspaceID, anchor.ID)
	}
	active, err := app.store.ActiveWorkspace()
	if err != nil {
		t.Fatalf("ActiveWorkspace(): %v", err)
	}
	if active.ID != anchor.ID {
		t.Fatalf("active workspace = %d, want anchor %d", active.ID, anchor.ID)
	}
}

func TestExplicitWorkspaceActionOverridesFocusWithoutChangingIt(t *testing.T) {
	app := newAuthedTestApp(t)

	anchor, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.store.GetOrCreateChatSessionForWorkspace(anchor.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace: %v", err)
	}
	alpha, err := app.store.CreateWorkspace("Alpha", filepath.Join(t.TempDir(), "alpha"))
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha): %v", err)
	}
	beta, err := app.store.CreateWorkspace("Beta", filepath.Join(t.TempDir(), "beta"))
	if err != nil {
		t.Fatalf("CreateWorkspace(beta): %v", err)
	}
	if _, err := app.store.CreateItem("Beta follow-up", store.ItemOptions{WorkspaceID: &beta.ID}); err != nil {
		t.Fatalf("CreateItem(beta): %v", err)
	}
	if err := app.setFocusedWorkspace(alpha.ID); err != nil {
		t.Fatalf("setFocusedWorkspace(alpha): %v", err)
	}

	message, payload, err := app.executeSystemAction(session.ID, session, &SystemAction{
		Action: "list_workspace_items",
		Params: map[string]interface{}{"workspace": "Beta"},
	})
	if err != nil {
		t.Fatalf("executeSystemAction(list_workspace_items): %v", err)
	}
	if got := int64(payload["workspace_id"].(int64)); got != beta.ID {
		t.Fatalf("payload workspace_id = %d, want %d", got, beta.ID)
	}
	if !strings.Contains(message, "Open items for workspace Beta") {
		t.Fatalf("message = %q, want Beta listing", message)
	}
	focusedID, err := app.store.FocusedWorkspaceID()
	if err != nil {
		t.Fatalf("FocusedWorkspaceID(): %v", err)
	}
	if focusedID != alpha.ID {
		t.Fatalf("FocusedWorkspaceID() = %d, want %d", focusedID, alpha.ID)
	}
}

func TestIntentPromptSystemCommandsIncludeFocusActions(t *testing.T) {
	prompt := buildIntentLLMSystemPrompt()
	if !strings.Contains(prompt, "focus_workspace") {
		t.Fatalf("prompt missing focus_workspace: %q", prompt)
	}
	if !strings.Contains(prompt, "clear_focus") {
		t.Fatalf("prompt missing clear_focus: %q", prompt)
	}
}

type appSessionStartRequest struct {
	CWD      string
	ThreadID string
}

func setupAppSessionBindingServer(t *testing.T) (*httptest.Server, *[]appSessionStartRequest) {
	t.Helper()
	var mu sync.Mutex
	starts := make([]appSessionStartRequest, 0, 4)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		defer conn.Close()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("decode app-server message: %v", err)
			}
			switch strings.TrimSpace(strFromAny(msg["method"])) {
			case "initialize":
				_ = conn.WriteJSON(map[string]any{
					"id":     msg["id"],
					"result": map[string]any{"userAgent": "binding-test"},
				})
			case "thread/start":
				params, _ := msg["params"].(map[string]any)
				start := appSessionStartRequest{
					CWD:      strings.TrimSpace(strFromAny(params["cwd"])),
					ThreadID: strings.TrimSpace(strFromAny(params["threadId"])),
				}
				mu.Lock()
				starts = append(starts, start)
				index := len(starts)
				mu.Unlock()
				threadID := start.ThreadID
				if threadID == "" {
					threadID = "thread-new-" + strconv.Itoa(index)
				}
				_ = conn.WriteJSON(map[string]any{
					"id": msg["id"],
					"result": map[string]any{
						"thread": map[string]any{"id": threadID},
					},
				})
			}
		}
	}))
	return server, &starts
}

func TestGetOrCreateAppSessionFollowsFocusedWorkspaceThreadBinding(t *testing.T) {
	server, starts := setupAppSessionBindingServer(t)
	defer server.Close()

	app, err := New(t.TempDir(), "", "", "ws"+strings.TrimPrefix(server.URL, "http"), "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Shutdown(context.Background())
	})

	anchor, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensureDefaultWorkspace: %v", err)
	}
	session, err := app.store.GetOrCreateChatSessionForWorkspace(anchor.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(anchor): %v", err)
	}
	alphaPath := filepath.Join(t.TempDir(), "alpha")
	betaPath := filepath.Join(t.TempDir(), "beta")
	for _, dir := range []string{alphaPath, betaPath} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll(%q): %v", dir, err)
		}
	}
	alpha, err := app.store.CreateWorkspace("Alpha", alphaPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(alpha): %v", err)
	}
	beta, err := app.store.CreateWorkspace("Beta", betaPath)
	if err != nil {
		t.Fatalf("CreateWorkspace(beta): %v", err)
	}
	alphaSession, err := app.store.GetOrCreateChatSessionForWorkspace(alpha.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(alpha): %v", err)
	}
	betaSession, err := app.store.GetOrCreateChatSessionForWorkspace(beta.ID)
	if err != nil {
		t.Fatalf("GetOrCreateChatSessionForWorkspace(beta): %v", err)
	}
	if err := app.store.UpdateChatSessionThread(alphaSession.ID, "thread-alpha"); err != nil {
		t.Fatalf("UpdateChatSessionThread(alpha): %v", err)
	}
	if err := app.store.UpdateChatSessionThread(betaSession.ID, "thread-beta"); err != nil {
		t.Fatalf("UpdateChatSessionThread(beta): %v", err)
	}

	profile := appServerModelProfile{Model: "gpt-test"}

	if err := app.setFocusedWorkspace(alpha.ID); err != nil {
		t.Fatalf("setFocusedWorkspace(alpha): %v", err)
	}
	alphaAppSession, bindingSessionID, resumed, err := app.getOrCreateAppSession(session.ID, "", profile)
	if err != nil {
		t.Fatalf("getOrCreateAppSession(alpha): %v", err)
	}
	if !resumed {
		t.Fatal("resumed(alpha) = false, want true")
	}
	if bindingSessionID != alphaSession.ID {
		t.Fatalf("binding session id = %q, want %q", bindingSessionID, alphaSession.ID)
	}
	if alphaAppSession.ThreadID() != "thread-alpha" {
		t.Fatalf("alpha thread id = %q, want %q", alphaAppSession.ThreadID(), "thread-alpha")
	}

	if err := app.setFocusedWorkspace(beta.ID); err != nil {
		t.Fatalf("setFocusedWorkspace(beta): %v", err)
	}
	betaAppSession, bindingSessionID, resumed, err := app.getOrCreateAppSession(session.ID, "", profile)
	if err != nil {
		t.Fatalf("getOrCreateAppSession(beta): %v", err)
	}
	if !resumed {
		t.Fatal("resumed(beta) = false, want true")
	}
	if bindingSessionID != betaSession.ID {
		t.Fatalf("binding session id = %q, want %q", bindingSessionID, betaSession.ID)
	}
	if betaAppSession.ThreadID() != "thread-beta" {
		t.Fatalf("beta thread id = %q, want %q", betaAppSession.ThreadID(), "thread-beta")
	}

	if len(*starts) != 2 {
		t.Fatalf("thread/start calls = %d, want 2", len(*starts))
	}
	if (*starts)[0].CWD != alphaPath || (*starts)[0].ThreadID != "thread-alpha" {
		t.Fatalf("first thread/start = %#v, want cwd=%q thread=%q", (*starts)[0], alphaPath, "thread-alpha")
	}
	if (*starts)[1].CWD != betaPath || (*starts)[1].ThreadID != "thread-beta" {
		t.Fatalf("second thread/start = %#v, want cwd=%q thread=%q", (*starts)[1], betaPath, "thread-beta")
	}
}

func containsLine(text, want string) bool {
	for _, line := range splitLines(text) {
		if line == want {
			return true
		}
	}
	return false
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Split(text, "\n")
}
