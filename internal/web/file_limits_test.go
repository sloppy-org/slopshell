package web

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWebSplitFileLineLimits(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(filename)
	cases := []struct {
		path string
		max  int
	}{
		{path: "projects.go", max: 1000},
		{path: "projects_model.go", max: 1000},
		{path: "projects_import.go", max: 1000},
		{path: "projects_hub.go", max: 1000},
		{path: "chat_intent.go", max: 1000},
		{path: "chat_intent_canvas.go", max: 1000},
		{path: "chat_intent_execution.go", max: 1000},
	}
	for _, tc := range cases {
		data, err := os.ReadFile(filepath.Join(dir, tc.path))
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		lines := strings.Count(string(data), "\n")
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lines++
		}
		if lines > tc.max {
			t.Fatalf("%s has %d lines, want <= %d", tc.path, lines, tc.max)
		}
	}
}
