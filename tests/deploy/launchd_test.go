package deploy_test

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var repoRoot = filepath.Join("..", "..")

var expectedPlists = []struct {
	file   string
	label  string
	tokens []string
}{
	{
		file:   "io.tabura.codex-app-server.plist",
		label:  "io.tabura.codex-app-server",
		tokens: []string{"@@CODEX_PATH@@"},
	},
	{
		file:   "io.tabura.piper-tts.plist",
		label:  "io.tabura.piper-tts",
		tokens: []string{"@@VENV_DIR@@", "@@SCRIPT_DIR@@", "@@PIPER_MODEL_DIR@@"},
	},
	{
		file:   "io.tabura.llm.plist",
		label:  "io.tabura.llm",
		tokens: []string{"@@LLM_SETUP_SCRIPT@@", "@@LLM_MODEL_DIR@@", "@@LLAMA_SERVER_BIN@@"},
	},
	{
		file:   "io.tabura.codex-llm.plist",
		label:  "io.tabura.codex-llm",
		tokens: []string{"@@LLM_SETUP_SCRIPT@@", "@@LLAMA_SERVER_BIN@@"},
	},
	{
		file:   "io.tabura.stt.plist",
		label:  "io.tabura.stt",
		tokens: []string{"@@STT_SETUP_SCRIPT@@"},
	},
	{
		file:   "io.tabura.web.plist",
		label:  "io.tabura.web",
		tokens: []string{"@@BIN_PATH@@", "@@PROJECT_DIR@@", "@@WEB_DATA_DIR@@", "@@TABURA_INTENT_LLM_URL@@"},
	},
}

var shellVarPattern = regexp.MustCompile(`\$\{[A-Z_]+\}`)

func TestLaunchdTemplatesExist(t *testing.T) {
	dir := filepath.Join(repoRoot, "deploy", "launchd")
	for _, tc := range expectedPlists {
		path := filepath.Join(dir, tc.file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("missing template: %s", path)
		}
	}
}

func TestLaunchdTemplatesValidXML(t *testing.T) {
	dir := filepath.Join(repoRoot, "deploy", "launchd")
	for _, tc := range expectedPlists {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if err := xml.Unmarshal(data, new(interface{})); err != nil {
				t.Fatalf("invalid XML: %v", err)
			}
		})
	}
}

func TestLaunchdTemplatesContainLabel(t *testing.T) {
	dir := filepath.Join(repoRoot, "deploy", "launchd")
	for _, tc := range expectedPlists {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			content := string(data)
			if !strings.Contains(content, tc.label) {
				t.Errorf("template missing Label %q", tc.label)
			}
		})
	}
}

func TestLaunchdTemplatesContainTokens(t *testing.T) {
	dir := filepath.Join(repoRoot, "deploy", "launchd")
	for _, tc := range expectedPlists {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			content := string(data)
			for _, tok := range tc.tokens {
				if !strings.Contains(content, tok) {
					t.Errorf("template missing token %s", tok)
				}
			}
		})
	}
}

func TestLaunchdTemplatesNoShellVars(t *testing.T) {
	dir := filepath.Join(repoRoot, "deploy", "launchd")
	for _, tc := range expectedPlists {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if m := shellVarPattern.FindString(string(data)); m != "" {
				t.Errorf("template contains shell variable %s; use @@TOKEN@@ placeholders", m)
			}
		})
	}
}

func TestLaunchdTemplatesHaveRequiredKeys(t *testing.T) {
	dir := filepath.Join(repoRoot, "deploy", "launchd")
	requiredKeys := []string{"Label", "ProgramArguments", "RunAtLoad", "KeepAlive", "StandardOutPath", "StandardErrorPath"}
	for _, tc := range expectedPlists {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			content := string(data)
			for _, key := range requiredKeys {
				needle := "<key>" + key + "</key>"
				if !strings.Contains(content, needle) {
					t.Errorf("template missing required key %s", key)
				}
			}
		})
	}
}

func TestLaunchdTemplateTokenSubstitution(t *testing.T) {
	dir := filepath.Join(repoRoot, "deploy", "launchd")
	tokenValues := map[string]string{
		"@@BIN_PATH@@":         "/usr/local/bin/tabura",
		"@@CODEX_PATH@@":       "/usr/local/bin/codex",
		"@@PROJECT_DIR@@":      "/tmp/project",
		"@@WEB_DATA_DIR@@":     "/tmp/web-data",
		"@@VENV_DIR@@":         "/tmp/venv",
		"@@SCRIPT_DIR@@":       "/tmp/scripts",
		"@@PIPER_MODEL_DIR@@":  "/tmp/models",
		"@@LLM_SETUP_SCRIPT@@":      "/tmp/setup-llm.sh",
		"@@LLM_MODEL_DIR@@":         "/tmp/llm-models",
		"@@LLAMA_SERVER_BIN@@":      "/tmp/llama-server",
		"@@STT_SETUP_SCRIPT@@":      "/tmp/setup-stt.sh",
		"@@TABURA_WEB_HOST@@":       "127.0.0.1",
		"@@TABURA_INTENT_LLM_URL@@": "http://127.0.0.1:8081",
	}

	for _, tc := range expectedPlists {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			content := string(data)
			for tok, val := range tokenValues {
				content = strings.ReplaceAll(content, tok, val)
			}
			if strings.Contains(content, "@@") {
				t.Error("substituted plist still contains unresolved @@-tokens")
			}
			if err := xml.Unmarshal([]byte(content), new(interface{})); err != nil {
				t.Errorf("substituted plist is invalid XML: %v", err)
			}
		})
	}
}

func TestUserUnitsScriptCoversAllLaunchdTokens(t *testing.T) {
	dir := filepath.Join(repoRoot, "deploy", "launchd")
	script, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "install-tabura-user-units.sh"))
	if err != nil {
		t.Fatalf("read user-units script: %v", err)
	}
	scriptContent := string(script)

	tokenPattern := regexp.MustCompile(`@@[A-Z_]+@@`)
	seen := map[string]bool{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read launchd dir: %v", err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".plist") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, tok := range tokenPattern.FindAllString(string(data), -1) {
			seen[tok] = true
		}
	}

	for tok := range seen {
		if !strings.Contains(scriptContent, tok) {
			t.Errorf("launchd token %s not handled in install-tabura-user-units.sh", tok)
		}
	}
}

func TestSystemdAndLaunchdServiceParity(t *testing.T) {
	launchdDir := filepath.Join(repoRoot, "deploy", "launchd")
	systemdDir := filepath.Join(repoRoot, "deploy", "systemd", "user")

	launchdServices := map[string]bool{
		"codex-app-server": false,
		"codex-llm":        false,
		"piper-tts":        false,
		"llm":              false,
		"stt":              false,
		"web":              false,
	}

	entries, err := os.ReadDir(launchdDir)
	if err != nil {
		t.Fatalf("read launchd dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".plist") {
			continue
		}
		svc := strings.TrimPrefix(name, "io.tabura.")
		svc = strings.TrimSuffix(svc, ".plist")
		launchdServices[svc] = true
	}

	for svc, found := range launchdServices {
		if !found {
			t.Errorf("missing launchd template for service %q", svc)
		}
		systemdFile := filepath.Join(systemdDir, "tabura-"+svc+".service")
		if _, err := os.Stat(systemdFile); os.IsNotExist(err) {
			t.Errorf("launchd template for %q exists but systemd unit %s is missing", svc, systemdFile)
		}
	}
}
