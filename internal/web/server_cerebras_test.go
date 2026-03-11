package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/krystophny/tabura/internal/cerebras"
)

func TestNewLoadsCerebrasAPIKeyFromSecretFile(t *testing.T) {
	home := t.TempDir()
	secretDir := filepath.Join(home, ".config", "tabura", "secrets")
	if err := os.MkdirAll(secretDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(secretDir): %v", err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, DefaultCerebrasSecretFile), []byte("secret-from-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(secret): %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("TABURA_CEREBRAS_URL", "https://api.cerebras.example")
	t.Setenv("TABURA_CEREBRAS_API_KEY", "")
	t.Setenv("TABURA_CEREBRAS_MODEL", "gpt-oss-120b")
	t.Setenv("TABURA_CEREBRAS_REASONING_EFFORT", "medium")

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	if app.cerebrasClient == nil {
		t.Fatal("cerebrasClient = nil, want configured client")
	}
	if got := app.cerebrasClient.APIKey; got != "secret-from-file" {
		t.Fatalf("APIKey = %q, want secret-from-file", got)
	}
	if got := app.cerebrasClient.Model; got != cerebras.DefaultModel {
		t.Fatalf("Model = %q, want %q", got, cerebras.DefaultModel)
	}
}

func TestNewLoadsCerebrasAPIKeyFromEnv(t *testing.T) {
	t.Setenv("TABURA_CEREBRAS_URL", "https://api.cerebras.example")
	t.Setenv("TABURA_CEREBRAS_API_KEY", "secret-from-env")
	t.Setenv("TABURA_CEREBRAS_MODEL", "gpt-oss-120b")
	t.Setenv("TABURA_CEREBRAS_REASONING_EFFORT", "medium")

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	if app.cerebrasClient == nil {
		t.Fatal("cerebrasClient = nil, want configured client")
	}
	if got := app.cerebrasClient.APIKey; got != "secret-from-env" {
		t.Fatalf("APIKey = %q, want secret-from-env", got)
	}
}

func TestNewDisablesCerebrasWhenURLIsOff(t *testing.T) {
	t.Setenv("TABURA_CEREBRAS_URL", "off")
	t.Setenv("TABURA_CEREBRAS_API_KEY", "ignored")

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	if app.cerebrasClient != nil {
		t.Fatal("cerebrasClient != nil, want nil when TABURA_CEREBRAS_URL=off")
	}
}
