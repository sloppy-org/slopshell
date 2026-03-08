package web

import (
	"fmt"
	"strings"
	"testing"
)

func TestStyleCSSImportsSplitStylesheets(t *testing.T) {
	t.Parallel()

	imports := []string{
		`@import url("./base.css");`,
		`@import url("./canvas.css");`,
		`@import url("./edge-panels.css");`,
		`@import url("./chat.css");`,
		`@import url("./ink.css");`,
		`@import url("./overlay.css");`,
		`@import url("./companion.css");`,
		`@import url("./items.css");`,
		`@import url("./mobile.css");`,
	}

	data, err := staticFiles.ReadFile("static/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	content := string(data)
	lastIndex := -1
	for _, importLine := range imports {
		idx := strings.Index(content, importLine)
		if idx == -1 {
			t.Fatalf("style.css missing import %q", importLine)
		}
		if idx <= lastIndex {
			t.Fatalf("style.css import %q out of order", importLine)
		}
		lastIndex = idx
	}
	if got, want := strings.Count(content, "@import"), len(imports); got != want {
		t.Fatalf("style.css import count = %d, want %d", got, want)
	}
}

func TestSplitStylesheetsExistAndStayBounded(t *testing.T) {
	t.Parallel()

	files := []string{
		"base.css",
		"canvas.css",
		"edge-panels.css",
		"chat.css",
		"ink.css",
		"overlay.css",
		"companion.css",
		"items.css",
		"mobile.css",
	}

	for _, name := range files {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			data, err := staticFiles.ReadFile("static/" + name)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			lines := strings.Count(string(data), "\n") + 1
			if lines > 500 {
				t.Fatalf("%s has %d lines, want <= 500", name, lines)
			}
			if strings.Contains(string(data), "@import") {
				t.Fatalf("%s should not contain nested @import directives", name)
			}
			if len(strings.TrimSpace(string(data))) == 0 {
				t.Fatalf("%s is empty", name)
			}
		})
	}
}

func TestStyleEntryPointStaysSmall(t *testing.T) {
	t.Parallel()

	data, err := staticFiles.ReadFile("static/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	lines := strings.Count(string(data), "\n") + 1
	if lines > 16 {
		t.Fatalf("style.css has %d lines, want <= 16", lines)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(data)), `@import url("./mobile.css");`) {
		t.Fatalf("style.css should end with the mobile stylesheet import")
	}
	if strings.Contains(string(data), "{") {
		t.Fatalf("style.css should only contain import directives, got %q", fmt.Sprintf("%.40s", strings.TrimSpace(string(data))))
	}
}
