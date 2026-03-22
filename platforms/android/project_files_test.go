package android

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaburaAndroidProjectIncludesExpectedFiles(t *testing.T) {
	projectRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	files := []string{
		"build.gradle.kts",
		"settings.gradle.kts",
		"gradle.properties",
		filepath.Join("app", "build.gradle.kts"),
		filepath.Join("app", "src", "main", "AndroidManifest.xml"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "MainActivity.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaAppModel.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaAudioCaptureService.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaBooxDevice.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaBooxInkSurfaceView.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaCanvasTransport.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaCanvasWebView.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaChatTransport.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaInkSurfaceView.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaModels.kt"),
		filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaServerDiscovery.kt"),
		filepath.Join("flow-contracts", "build.gradle.kts"),
		filepath.Join("flow-contracts", "settings.gradle.kts"),
		filepath.Join("flow-contracts", "src", "test", "kotlin", "com", "tabura", "android", "flow", "FlowFixture.kt"),
		filepath.Join("flow-contracts", "src", "test", "kotlin", "com", "tabura", "android", "flow", "FlowRunner.kt"),
		filepath.Join("flow-contracts", "src", "test", "kotlin", "com", "tabura", "android", "flow", "FlowContractTest.kt"),
		filepath.Join("flow-contracts", "src", "test", "resources", "flow-fixtures.json"),
		filepath.Join("app", "src", "test", "kotlin", "com", "tabura", "android", "TaburaDialogueModeTest.kt"),
		filepath.Join("app", "src", "main", "res", "values", "strings.xml"),
		filepath.Join("app", "src", "main", "res", "values", "themes.xml"),
	}
	for _, relative := range files {
		path := filepath.Join(projectRoot, relative)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing expected file %q: %v", path, err)
		}
	}
}

func TestTaburaAndroidManifestDeclaresMobileCapabilities(t *testing.T) {
	projectRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(projectRoot, "app", "src", "main", "AndroidManifest.xml"))
	if err != nil {
		t.Fatalf("ReadFile(AndroidManifest.xml): %v", err)
	}
	manifest := string(data)
	required := []string{
		"android.permission.FOREGROUND_SERVICE",
		"android.permission.FOREGROUND_SERVICE_MICROPHONE",
		"android.permission.RECORD_AUDIO",
		"android.permission.WAKE_LOCK",
		"android:usesCleartextTraffic=\"true\"",
		"android:name=\".TaburaAudioCaptureService\"",
		"android:foregroundServiceType=\"microphone\"",
	}
	for _, snippet := range required {
		if !strings.Contains(manifest, snippet) {
			t.Fatalf("AndroidManifest.xml missing %q", snippet)
		}
	}
}

func TestTaburaAndroidBuildIncludesRealtimeInkStack(t *testing.T) {
	projectRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(projectRoot, "app", "build.gradle.kts"))
	if err != nil {
		t.Fatalf("ReadFile(app/build.gradle.kts): %v", err)
	}
	buildFile := string(data)
	required := []string{
		"androidx.ink:ink-authoring",
		"androidx.ink:ink-rendering",
		"androidx.ink:ink-brush",
		"androidx.ink:ink-strokes",
		"androidx.graphics:graphics-core",
		"androidx.input:input-motionprediction",
		"androidx.webkit:webkit",
		"com.onyx.android.sdk:onyxsdk-device:1.1.11",
		"com.onyx.android.sdk:onyxsdk-pen:1.2.1",
		"com.squareup.okhttp3:okhttp",
	}
	for _, snippet := range required {
		if !strings.Contains(buildFile, snippet) {
			t.Fatalf("app/build.gradle.kts missing %q", snippet)
		}
	}
}

func TestTaburaAndroidBuildIncludesBooxRepository(t *testing.T) {
	projectRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(projectRoot, "settings.gradle.kts"))
	if err != nil {
		t.Fatalf("ReadFile(settings.gradle.kts): %v", err)
	}
	settings := string(data)
	if !strings.Contains(settings, "https://repo.boox.com/repository/maven-public/") {
		t.Fatalf("settings.gradle.kts missing Boox Maven repository")
	}
}

func TestTaburaAndroidSourcesCoverThinClientResponsibilities(t *testing.T) {
	projectRoot, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	checks := []struct {
		relative string
		snippets []string
	}{
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "MainActivity.kt"),
			snippets: []string{"BlackScreenDialogueSurface", "FLAG_KEEP_SCREEN_ON", "Start Dialogue", "Exit Dialogue"},
		},
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaServerDiscovery.kt"),
			snippets: []string{"NsdManager", "_tabura._tcp."},
		},
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaChatTransport.kt"),
			snippets: []string{"WebSocket", "chat/$sessionId"},
		},
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaCanvasTransport.kt"),
			snippets: []string{"canvas/$sessionId", "snapshot"},
		},
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaAudioCaptureService.kt"),
			snippets: []string{"AudioRecord", "startForeground", "VOICE_RECOGNITION"},
		},
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaInkSurfaceView.kt"),
			snippets: []string{"InProgressStrokesView", "MotionEventPredictor", "TaburaInkStroke"},
		},
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaModels.kt"),
			snippets: []string{"ink_stroke", "audio_pcm", "TaburaDialogueModePresentation", "Tap to stop recording"},
		},
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaCanvasWebView.kt"),
			snippets: []string{"WebView", "loadDataWithBaseURL", "body.eink-display", "scroll-behavior: auto !important"},
		},
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaBooxDevice.kt"),
			snippets: []string{
				"Build.MANUFACTURER.lowercase() == \"onyx\"",
				"setViewDefaultUpdateMode",
				"applyGCOnce",
				"setWebViewContrastOptimize",
			},
		},
		{
			relative: filepath.Join("app", "src", "main", "kotlin", "com", "tabura", "android", "TaburaBooxInkSurfaceView.kt"),
			snippets: []string{
				"TouchHelper.create",
				"setRawInputReaderEnable(true)",
				"openRawDrawing",
				"setRawDrawingEnabled(true)",
				"closeRawDrawing",
				"onRawDrawingTouchPointListReceived",
				"TaburaInkStroke",
			},
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
