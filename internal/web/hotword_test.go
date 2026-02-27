package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func decodeJSONBody(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return payload
}

func TestHotwordStatusReportsMissingAssets(t *testing.T) {
	app := newAuthedTestApp(t)
	root := t.TempDir()
	app.localProjectDir = root

	rr := doAuthedJSONRequest(t, app.Router(), "GET", "/api/hotword/status", nil)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	payload := decodeJSONBody(t, rr.Body.String())
	if ready, _ := payload["ready"].(bool); ready {
		t.Fatalf("expected ready=false, got true")
	}
	missingRaw, ok := payload["missing"].([]interface{})
	if !ok || len(missingRaw) == 0 {
		t.Fatalf("expected non-empty missing assets list, got %#v", payload["missing"])
	}
}

func TestHotwordStatusReportsReadyWhenAllAssetsPresent(t *testing.T) {
	app := newAuthedTestApp(t)
	root := t.TempDir()
	app.localProjectDir = root

	vendorDir := filepath.Join(root, "internal", "web", "static", "vendor", "openwakeword")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatalf("mkdir vendor dir: %v", err)
	}
	for _, file := range hotwordRuntimeAssetFiles {
		if err := os.WriteFile(filepath.Join(vendorDir, file), []byte("x"), 0o644); err != nil {
			t.Fatalf("write vendor file %s: %v", file, err)
		}
	}

	rr := doAuthedJSONRequest(t, app.Router(), "GET", "/api/hotword/status", nil)
	if rr.Code != 200 {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	payload := decodeJSONBody(t, rr.Body.String())
	if ready, _ := payload["ready"].(bool); !ready {
		t.Fatalf("expected ready=true when all assets present, got payload=%#v", payload)
	}
	missingRaw, _ := payload["missing"].([]interface{})
	if len(missingRaw) != 0 {
		t.Fatalf("expected empty missing list, got %v", missingRaw)
	}
}
