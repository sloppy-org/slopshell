package main

import (
	"fmt"
	"testing"

	updater "github.com/krystophny/tabura/internal/update"
)

func TestParseServerConfigDefaultsToLoopbackWebHost(t *testing.T) {
	cfg, status := parseServerConfig([]string{})
	if status != 0 {
		t.Fatalf("parseServerConfig() status = %d, want 0", status)
	}
	if cfg.webHost != "127.0.0.1" {
		t.Fatalf("default webHost = %q, want 127.0.0.1", cfg.webHost)
	}
}

func TestParseServerConfigRejectsPublicMCPWithoutUnsafeFlag(t *testing.T) {
	_, status := parseServerConfig([]string{"--mcp-host", "0.0.0.0"})
	if status != 2 {
		t.Fatalf("parseServerConfig(public mcp) status = %d, want 2", status)
	}
}

func TestParseServerConfigRejectsIncompleteTLSConfig(t *testing.T) {
	_, status := parseServerConfig([]string{"--web-cert-file", "/tmp/cert.pem"})
	if status != 2 {
		t.Fatalf("parseServerConfig(incomplete tls) status = %d, want 2", status)
	}
}

func TestParseServerConfigAcceptsTLSConfigPair(t *testing.T) {
	cfg, status := parseServerConfig([]string{"--web-cert-file", "/tmp/cert.pem", "--web-key-file", "/tmp/key.pem"})
	if status != 0 {
		t.Fatalf("parseServerConfig(tls pair) status = %d, want 0", status)
	}
	if cfg.webCertFile != "/tmp/cert.pem" {
		t.Fatalf("webCertFile = %q, want /tmp/cert.pem", cfg.webCertFile)
	}
	if cfg.webKeyFile != "/tmp/key.pem" {
		t.Fatalf("webKeyFile = %q, want /tmp/key.pem", cfg.webKeyFile)
	}
}

func TestFormatVersionLinePrefixesVersion(t *testing.T) {
	got := formatVersionLine("0.1.4", "abc1234", "linux", "amd64")
	want := "tabura v0.1.4 (abc1234) linux/amd64"
	if got != want {
		t.Fatalf("formatVersionLine() = %q, want %q", got, want)
	}
}

func TestFormatVersionLineKeepsPrefixedVersionAndHandlesMissingCommit(t *testing.T) {
	got := formatVersionLine("v1.2.3", "", "windows", "arm64")
	want := "tabura v1.2.3 (unknown) windows/arm64"
	if got != want {
		t.Fatalf("formatVersionLine() = %q, want %q", got, want)
	}
}

func TestRunDispatchesUpdateCommand(t *testing.T) {
	prev := runUpdate
	t.Cleanup(func() { runUpdate = prev })
	called := false
	runUpdate = func(opts updater.Options) (updater.Result, error) {
		called = true
		if opts.CurrentVersion != defaultBinaryVersion {
			return updater.Result{}, fmt.Errorf("unexpected version %q", opts.CurrentVersion)
		}
		return updater.Result{CurrentVersion: "v0.1.4", LatestVersion: "v0.1.4", Updated: false}, nil
	}

	status := run([]string{"update"})
	if status != 0 {
		t.Fatalf("run(update) status = %d, want 0", status)
	}
	if !called {
		t.Fatalf("expected updater to be called")
	}
}
