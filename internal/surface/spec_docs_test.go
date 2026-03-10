package surface

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRootFromCaller(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func TestGestureTruthTableDocIsIndexedAndStructured(t *testing.T) {
	root := repoRootFromCaller(t)

	truthTablePath := filepath.Join(root, "docs", "gesture-truth-table.md")
	truthTable, err := os.ReadFile(truthTablePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", truthTablePath, err)
	}
	content := string(truthTable)

	requiredSnippets := []string{
		"| Input | Blank surface | Artifact visible | Annotation mode | Dialogue live | Meeting live |",
		"Canonical tap-to-voice rule",
		"`start local capture bound to the current context`",
		"Live session state wins over ordinary prompt/annotation routing.",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(content, snippet) {
			t.Fatalf("%s missing snippet %q", truthTablePath, snippet)
		}
	}

	specIndexPath := filepath.Join(root, "docs", "spec-index.md")
	specIndex, err := os.ReadFile(specIndexPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", specIndexPath, err)
	}
	if !strings.Contains(string(specIndex), "`gesture-truth-table.md`") {
		t.Fatalf("%s does not reference gesture-truth-table.md", specIndexPath)
	}
}
