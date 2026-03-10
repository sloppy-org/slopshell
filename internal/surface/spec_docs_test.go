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

func TestInteractionGrammarDocIsIndexedAndLinked(t *testing.T) {
	root := repoRootFromCaller(t)

	grammarPath := filepath.Join(root, "docs", "interaction-grammar.md")
	grammarDoc, err := os.ReadFile(grammarPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", grammarPath, err)
	}
	content := string(grammarDoc)

	requiredSnippets := []string{
		"## Authoritative Ontology",
		"## Authoritative Live Model",
		"## Canonical Action Semantics",
		"## Allowed Tool Modalities",
		"## Rules for Auxiliary Surfaces",
		"## Rules for New Artifact Kinds",
		"Project is not a product concept.",
		"- Dialogue",
		"- Meeting",
		"- Workspace",
		"- Artifact",
		"- Item",
		"- Actor",
		"- Label",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(content, snippet) {
			t.Fatalf("%s missing snippet %q", grammarPath, snippet)
		}
	}

	specIndexPath := filepath.Join(root, "docs", "spec-index.md")
	specIndex, err := os.ReadFile(specIndexPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", specIndexPath, err)
	}
	if !strings.Contains(string(specIndex), "`interaction-grammar.md`") {
		t.Fatalf("%s does not reference interaction-grammar.md", specIndexPath)
	}

	claudePath := filepath.Join(root, "CLAUDE.md")
	claudeDoc, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", claudePath, err)
	}
	if !strings.Contains(string(claudeDoc), "`docs/interaction-grammar.md`") {
		t.Fatalf("%s does not reference docs/interaction-grammar.md", claudePath)
	}
}
