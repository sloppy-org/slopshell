package licensing

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestLegacyIntentClassifierArtifactsAreRemovedFromTrackedFiles(t *testing.T) {
	t.Parallel()

	cmd := exec.Command(
		"git",
		"grep",
		"-n",
		"-E",
		"intent-classifier|tabura-intent\\.service|8425",
		"--",
		".",
		":(exclude)internal/licensing/intent_runtime_test.go",
		":(exclude)internal/web/static/vendor/pdf.worker.mjs",
	)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if !(errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.TrimSpace(string(out)) == "") {
			t.Fatalf("git grep failed: %v\n%s", err, string(out))
		}
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("legacy intent classifier reference still tracked:\n%s", string(out))
	}

	cmd = exec.Command("git", "ls-files")
	cmd.Dir = repoRoot(t)
	out, err = cmd.CombinedOutput()
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
		case cleanPath == "deploy/systemd/user/tabura-intent.service":
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
	if strings.Contains(content, "Intent LLM fallback") {
		t.Fatal("playtest script still uses the deprecated intent LLM fallback wording")
	}
}

func TestGoReleaserArchiveOmitsRemovedIntentClassifierFiles(t *testing.T) {
	t.Parallel()

	content := readRepoFile(t, ".goreleaser.yaml")
	for _, forbidden := range []string{
		"services/intent-classifier/main.py",
		"services/intent-classifier/intents.json",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("goreleaser still packages removed classifier artifact %q", forbidden)
		}
	}
	if !strings.Contains(content, "scripts/setup-local-llm.sh") {
		t.Fatalf("goreleaser no longer packages the local runtime setup script:\n%s", content)
	}
	if !strings.Contains(content, "scripts/lib/llama.sh") {
		t.Fatalf("goreleaser no longer packages the llama runtime helper:\n%s", content)
	}
}

func TestGitignoreOmitsRemovedIntentClassifierArtifacts(t *testing.T) {
	t.Parallel()

	content := readRepoFile(t, ".gitignore")
	for _, forbidden := range []string{
		"services/intent-classifier/model/",
		"services/intent-classifier/checkpoints/",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("gitignore still carries removed classifier artifact %q", forbidden)
		}
	}
	required := []string{"playwright-report/", "test-results/", "tools/"}
	for _, marker := range required {
		if !strings.Contains(content, marker) {
			t.Fatalf("gitignore lost expected marker %q", marker)
		}
	}
}
