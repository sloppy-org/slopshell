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
		path := filepath.Join(projectRoot, "TaburaIOS", name)
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
