package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sessionEntry struct {
	WorkspacePath string    `json:"workspace_path"`
	SessionID     string    `json:"session_id"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type sessionStore struct {
	Entries []sessionEntry `json:"entries"`
}

func sessionStorePath() string {
	if override := strings.TrimSpace(os.Getenv("SLOPSHELL_CLI_STATE_FILE")); override != "" {
		return override
	}
	if state := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); state != "" {
		return filepath.Join(state, "slopshell", "slsh-sessions.json")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".local", "state", "slopshell", "slsh-sessions.json")
	}
	return ""
}

func historyFilePath() string {
	if override := strings.TrimSpace(os.Getenv("SLOPSHELL_CLI_HISTORY_FILE")); override != "" {
		return override
	}
	if state := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); state != "" {
		return filepath.Join(state, "slopshell", "slsh-history")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".local", "state", "slopshell", "slsh-history")
	}
	return ""
}

func loadSessionStore() (*sessionStore, string, error) {
	path := sessionStorePath()
	if path == "" {
		return nil, "", errors.New("cannot resolve slsh state path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &sessionStore{}, path, nil
		}
		return nil, path, err
	}
	var s sessionStore
	if err := json.Unmarshal(data, &s); err != nil {
		return &sessionStore{}, path, nil
	}
	return &s, path, nil
}

func persistSessionForWorkspace(workspacePath, sessionID string) error {
	workspacePath = strings.TrimSpace(workspacePath)
	sessionID = strings.TrimSpace(sessionID)
	if workspacePath == "" || sessionID == "" {
		return nil
	}
	store, path, err := loadSessionStore()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	updated := false
	for i := range store.Entries {
		if store.Entries[i].WorkspacePath == workspacePath {
			store.Entries[i].SessionID = sessionID
			store.Entries[i].UpdatedAt = now
			updated = true
			break
		}
	}
	if !updated {
		store.Entries = append(store.Entries, sessionEntry{
			WorkspacePath: workspacePath,
			SessionID:     sessionID,
			UpdatedAt:     now,
		})
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

func listKnownSessions() ([]sessionEntry, error) {
	store, _, err := loadSessionStore()
	if err != nil {
		return nil, err
	}
	entries := append([]sessionEntry(nil), store.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
	return entries, nil
}
