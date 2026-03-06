package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func boolFromAny(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return parseBoolString(t, false)
	default:
		return false
	}
}

func TestRuntimeIncludesSafetyPreferences(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if got := boolFromAny(payload["safety_yolo_mode"]); got {
		t.Fatalf("safety_yolo_mode = %v, want false", got)
	}
	if got := boolFromAny(payload["disclaimer_ack_required"]); !got {
		t.Fatalf("disclaimer_ack_required = %v, want true", got)
	}
	if got := boolFromAny(payload["silent_mode"]); got {
		t.Fatalf("silent_mode = %v, want false", got)
	}
	if got := strFromAny(payload["input_mode"]); got != "pen" {
		t.Fatalf("input_mode = %q, want %q", got, "pen")
	}
	if got := strFromAny(payload["startup_behavior"]); got != "hub_first" {
		t.Fatalf("startup_behavior = %q, want %q", got, "hub_first")
	}
	if got := strFromAny(payload["disclaimer_version"]); got != disclaimerVersionCurrent {
		t.Fatalf("disclaimer_version = %q, want %q", got, disclaimerVersionCurrent)
	}
}

func TestRuntimeYoloModeUpdatePersists(t *testing.T) {
	app := newAuthedTestApp(t)
	setRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/runtime/yolo", map[string]any{"enabled": true})
	if setRR.Code != http.StatusOK {
		t.Fatalf("set yolo status=%d body=%s", setRR.Code, setRR.Body.String())
	}
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if got := boolFromAny(payload["safety_yolo_mode"]); !got {
		t.Fatalf("safety_yolo_mode = %v, want true", got)
	}
}

func TestRuntimeDisclaimerAckClearsRequiredFlag(t *testing.T) {
	app := newAuthedTestApp(t)
	ackRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/runtime/disclaimer-ack", map[string]any{"version": disclaimerVersionCurrent})
	if ackRR.Code != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", ackRR.Code, ackRR.Body.String())
	}
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if got := boolFromAny(payload["disclaimer_ack_required"]); got {
		t.Fatalf("disclaimer_ack_required = %v, want false", got)
	}
	if got := strFromAny(payload["disclaimer_ack_version"]); got != disclaimerVersionCurrent {
		t.Fatalf("disclaimer_ack_version = %q, want %q", got, disclaimerVersionCurrent)
	}
}

func TestRuntimePreferenceUpdatePersists(t *testing.T) {
	app := newAuthedTestApp(t)
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPatch, "/api/runtime/preferences", map[string]any{
		"silent_mode":      true,
		"input_mode":       "keyboard",
		"startup_behavior": "hub_first",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("preference update status=%d body=%s", rr.Code, rr.Body.String())
	}

	runtimeRR := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime", nil)
	if runtimeRR.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", runtimeRR.Code, runtimeRR.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(runtimeRR.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if got := boolFromAny(payload["silent_mode"]); !got {
		t.Fatalf("silent_mode = %v, want true", got)
	}
	if got := strFromAny(payload["input_mode"]); got != "keyboard" {
		t.Fatalf("input_mode = %q, want %q", got, "keyboard")
	}
	if got := strFromAny(payload["startup_behavior"]); got != "hub_first" {
		t.Fatalf("startup_behavior = %q, want %q", got, "hub_first")
	}
}

func TestRuntimeMigratesLegacyImplicitVoiceModeToPen(t *testing.T) {
	app := newAuthedTestApp(t)
	if err := app.store.SetAppState(appStateInputModeKey, "voice"); err != nil {
		t.Fatalf("seed legacy voice input mode: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if got := strFromAny(payload["input_mode"]); got != "pen" {
		t.Fatalf("input_mode = %q, want %q", got, "pen")
	}
	if got, err := app.store.AppState(appStateInputModeKey); err != nil {
		t.Fatalf("read migrated input mode: %v", err)
	} else if got != "pen" {
		t.Fatalf("stored input_mode = %q, want %q", got, "pen")
	}
}

func TestRuntimeKeepsExplicitVoiceMode(t *testing.T) {
	app := newAuthedTestApp(t)
	if err := app.store.SetAppState(appStateInputModeKey, "voice"); err != nil {
		t.Fatalf("seed explicit voice input mode: %v", err)
	}
	if err := app.store.SetAppState(appStateInputModeExplicitKey, "true"); err != nil {
		t.Fatalf("seed explicit input mode flag: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime response: %v", err)
	}
	if got := strFromAny(payload["input_mode"]); got != "voice" {
		t.Fatalf("input_mode = %q, want %q", got, "voice")
	}
}
