package ios

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaburaIOSProjectIncludesExpectedFiles(t *testing.T) {
	projectRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	projectFile := filepath.Join(projectRoot, "TaburaIOS.xcodeproj", "project.pbxproj")
	data, err := os.ReadFile(projectFile)
	if err != nil {
		t.Fatalf("ReadFile(project): %v", err)
	}
	project := string(data)
	expected := []string{
		"Package.swift",
		filepath.Join("Sources", "TaburaFlowContract", "FlowFixture.swift"),
		filepath.Join("Sources", "TaburaFlowContract", "FlowRunner.swift"),
		filepath.Join("Tests", "TaburaFlowContractTests", "TaburaFlowContractTests.swift"),
		filepath.Join("Tests", "TaburaFlowContractTests", "Resources", "flow-fixtures.json"),
		filepath.Join("Tests", "TaburaIOSModelsTests", "TaburaDialogueModeTests.swift"),
		"TaburaIOSApp.swift",
		"ContentView.swift",
		"TaburaAppModel.swift",
		"TaburaModels.swift",
		"TaburaServerDiscovery.swift",
		"TaburaChatTransport.swift",
		"TaburaCanvasTransport.swift",
		"TaburaAudioCapture.swift",
		"TaburaInkCaptureView.swift",
		"TaburaCanvasWebView.swift",
		"Info.plist",
	}
	for _, name := range expected {
		path := filepath.Join(projectRoot, name)
		if strings.HasPrefix(name, "Sources") || strings.HasPrefix(name, "Tests") || name == "Package.swift" {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("missing expected file %q: %v", path, err)
			}
			continue
		}
		path = filepath.Join(projectRoot, "TaburaIOS", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing expected file %q: %v", path, err)
		}
		if !strings.Contains(project, name) {
			t.Fatalf("project.pbxproj missing reference to %q", name)
		}
	}
}

func TestTaburaIOSInfoPlistDeclaresMobileCapabilities(t *testing.T) {
	projectRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	infoPath := filepath.Join(projectRoot, "TaburaIOS", "Info.plist")
	data, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatalf("ReadFile(Info.plist): %v", err)
	}
	info := string(data)
	requiredSnippets := []string{
		"<key>UIBackgroundModes</key>",
		"<string>audio</string>",
		"<key>NSBonjourServices</key>",
		"<string>_tabura._tcp</string>",
		"<key>NSMicrophoneUsageDescription</key>",
		"<key>NSLocalNetworkUsageDescription</key>",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(info, snippet) {
			t.Fatalf("Info.plist missing %q", snippet)
		}
	}
}

func TestTaburaIOSSourcesCoverBlackScreenDialogueMode(t *testing.T) {
	projectRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	checks := []struct {
		relative string
		snippets []string
	}{
		{
			relative: filepath.Join("TaburaIOS", "ContentView.swift"),
			snippets: []string{"blackScreenDialoguePanel", "Exit Dialogue", "isIdleTimerDisabled"},
		},
		{
			relative: filepath.Join("TaburaIOS", "TaburaAppModel.swift"),
			snippets: []string{"toggleDialogueMode()", "companion/config", "live-policy", "toggle_live_dialogue"},
		},
		{
			relative: filepath.Join("TaburaIOS", "TaburaModels.swift"),
			snippets: []string{"TaburaDialogueModePresentation", "usesBlackScreen", "keepScreenAwake", "Tap to stop recording"},
		},
	}
	for _, check := range checks {
		path := filepath.Join(projectRoot, check.relative)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", path, err)
		}
		content := string(data)
		for _, snippet := range check.snippets {
			if !strings.Contains(content, snippet) {
				t.Fatalf("%s missing %q", check.relative, snippet)
			}
		}
	}
}
