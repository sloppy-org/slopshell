package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

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

func TestRunUnknownCommandReturnsUsageStatus(t *testing.T) {
	status := run([]string{"not-a-command"})
	if status != 2 {
		t.Fatalf("run(unknown) status = %d, want 2", status)
	}
}

func TestCmdSchemaOutputsProtocolJSON(t *testing.T) {
	out := captureStdout(t, func() {
		status := cmdSchema()
		if status != 0 {
			t.Fatalf("cmdSchema() status = %d, want 0", status)
		}
	})
	if !strings.Contains(out, `"title": "TaburaCanvasEvent"`) {
		t.Fatalf("cmdSchema output missing title: %q", out)
	}
	if !strings.Contains(out, `"const": "text_artifact"`) {
		t.Fatalf("cmdSchema output missing text_artifact schema: %q", out)
	}
}

func TestWaitForMCPReadySuccess(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := &http.Server{Handler: mux}
	go func() {
		_ = srv.Serve(ln)
	}()
	t.Cleanup(func() {
		_ = srv.Close()
	})

	port := ln.Addr().(*net.TCPAddr).Port
	errCh := make(chan error, 1)
	if err := waitForMCPReady("127.0.0.1", port, 2*time.Second, errCh); err != nil {
		t.Fatalf("waitForMCPReady() error: %v", err)
	}
}

func TestWaitForMCPReadyReturnsListenerError(t *testing.T) {
	errCh := make(chan error, 1)
	errCh <- errors.New("boom")
	err := waitForMCPReady("127.0.0.1", 65533, 10*time.Millisecond, errCh)
	if err == nil {
		t.Fatalf("expected waitForMCPReady to fail")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("waitForMCPReady error = %q, want boom context", err.Error())
	}
}

func TestCmdUpdateFailureReturnsStatusOne(t *testing.T) {
	prev := runUpdate
	t.Cleanup(func() { runUpdate = prev })
	runUpdate = func(opts updater.Options) (updater.Result, error) {
		return updater.Result{}, errors.New("update failed")
	}

	status := cmdUpdate(nil)
	if status != 1 {
		t.Fatalf("cmdUpdate() status = %d, want 1", status)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("capture stdout copy: %v", err)
	}
	return buf.String()
}
