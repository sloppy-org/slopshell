package licensing

import (
	"os/exec"
	"strings"
	"testing"
)

func TestLegacyIntentClassifierArtifactsAreRemovedFromTrackedFiles(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("git", "ls-files")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-files failed: %v\n%s", err, string(out))
	}

	for _, path := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		cleanPath := strings.TrimSpace(path)
		switch {
		case cleanPath == "":
			continue
		case strings.HasPrefix(cleanPath, "services/intent-classifier/"):
			t.Fatalf("legacy intent classifier path is still tracked: %s", cleanPath)
		case strings.HasSuffix(cleanPath, "/tabura-intent.service"):
			t.Fatalf("legacy intent service unit is still tracked: %s", cleanPath)
		}
	}
}

func TestPlaytestScriptUsesLocalIntentRuntimeProbe(t *testing.T) {
	t.Parallel()

	content := readRepoFile(t, "scripts", "playtest.sh")
	if strings.Contains(content, "Intent classifier") {
		t.Fatal("playtest script still mentions the removed intent classifier")
	}
	if strings.Contains(content, "8425") {
		t.Fatal("playtest script still probes the removed classifier port 8425")
	}
	requireContainsAll(t, content,
		"Local intent runtime detected on :8426.",
		"Local intent runtime not detected on :8426; continuing with live runtime defaults.",
	)
}
