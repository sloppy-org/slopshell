package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func requireConfirmationRequired(t *testing.T, message string, payloads []map[string]interface{}, wantKind string) {
	t.Helper()
	if !strings.Contains(strings.ToLower(message), "reply `confirm`") {
		t.Fatalf("message = %q, want confirmation guidance", message)
	}
	if len(payloads) != 1 {
		t.Fatalf("payloads len=%d want 1", len(payloads))
	}
	if got := strings.TrimSpace(strFromAny(payloads[0]["type"])); got != "confirmation_required" {
		t.Fatalf("payload type=%q want confirmation_required", got)
	}
	if got := strings.TrimSpace(strFromAny(payloads[0]["confirmation_kind"])); got != wantKind {
		t.Fatalf("confirmation_kind=%q want %q", got, wantKind)
	}
}

func confirmNextAction(t *testing.T, app *App, session store.ChatSession) (string, []map[string]interface{}, bool) {
	t.Helper()
	return app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "confirm")
}

func requireConfirmationFailureMessage(t *testing.T, got string, wantErr string) {
	t.Helper()
	want := "Confirmation failed: " + wantErr
	if got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestIsDestructiveShellCommand(t *testing.T) {
	cases := []struct {
		command string
		want    bool
	}{
		{command: "ls -1", want: false},
		{command: "find . -maxdepth 2 -type f", want: false},
		{command: "rm -rf ./tmp", want: true},
		{command: "git reset --hard", want: true},
		{command: "echo hello > notes.txt", want: true},
	}
	for _, tc := range cases {
		if got := isDestructiveShellCommand(tc.command); got != tc.want {
			t.Fatalf("isDestructiveShellCommand(%q)=%v want %v", tc.command, got, tc.want)
		}
	}
}

func TestDestructivePlanRequiresConfirmationAndConfirmExecutes(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	targetPath := filepath.Join(project.RootPath, "danger.txt")
	if err := os.WriteFile(targetPath, []byte("keep-me"), 0o644); err != nil {
		t.Fatalf("write danger file: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, execErr := app.executeSystemActionPlan(session.ID, session, "truncate that file", []*SystemAction{{
		Action: "shell",
		Params: map[string]interface{}{"command": "truncate -s 0 danger.txt"},
	}})
	if execErr != nil {
		t.Fatalf("executeSystemActionPlan error: %v", execErr)
	}
	if !strings.Contains(strings.ToLower(message), "destructive action blocked") {
		t.Fatalf("unexpected block message: %q", message)
	}
	requireConfirmationRequired(t, message, payloads, "dangerous")
	beforeInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat before confirm: %v", err)
	}
	if beforeInfo.Size() == 0 {
		t.Fatalf("file should not be modified before confirm")
	}

	confirmedMessage, _, handled := confirmNextAction(t, app, session)
	if !handled {
		t.Fatal("expected confirm to be handled")
	}
	if strings.Contains(strings.ToLower(confirmedMessage), "blocked") {
		t.Fatalf("confirm message still blocked: %q", confirmedMessage)
	}
	afterInfo, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat after confirm: %v", err)
	}
	if afterInfo.Size() != 0 {
		t.Fatalf("file size=%d want 0", afterInfo.Size())
	}
}

func TestDestructivePlanCanBeCanceled(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	targetPath := filepath.Join(project.RootPath, "cancel-danger.txt")
	if err := os.WriteFile(targetPath, []byte("keep-me"), 0o644); err != nil {
		t.Fatalf("write danger file: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	_, _, execErr := app.executeSystemActionPlan(session.ID, session, "truncate that file", []*SystemAction{{
		Action: "shell",
		Params: map[string]interface{}{"command": "truncate -s 0 cancel-danger.txt"},
	}})
	if execErr != nil {
		t.Fatalf("executeSystemActionPlan error: %v", execErr)
	}

	message, _, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "cancel")
	if !handled {
		t.Fatal("expected cancel to be handled")
	}
	if !strings.Contains(strings.ToLower(message), "canceled") {
		t.Fatalf("unexpected cancel message: %q", message)
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat after cancel: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("file should not be modified when canceled")
	}
}

func TestYoloModeBypassesDestructiveConfirmation(t *testing.T) {
	app := newAuthedTestApp(t)
	if err := app.setYoloModeEnabled(true); err != nil {
		t.Fatalf("set yolo mode: %v", err)
	}
	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	targetPath := filepath.Join(project.RootPath, "yolo-danger.txt")
	if err := os.WriteFile(targetPath, []byte("keep-me"), 0o644); err != nil {
		t.Fatalf("write danger file: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, execErr := app.executeSystemActionPlan(session.ID, session, "truncate with yolo", []*SystemAction{{
		Action: "shell",
		Params: map[string]interface{}{"command": "truncate -s 0 yolo-danger.txt"},
	}})
	if execErr != nil {
		t.Fatalf("executeSystemActionPlan error: %v", execErr)
	}
	if strings.Contains(strings.ToLower(message), "blocked") {
		t.Fatalf("unexpected blocked message in yolo mode: %q", message)
	}
	if len(payloads) > 0 && strings.TrimSpace(strFromAny(payloads[0]["type"])) == "confirmation_required" {
		t.Fatalf("unexpected confirmation payload in yolo mode")
	}
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("stat after yolo execute: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("file size=%d want 0", info.Size())
	}
}
