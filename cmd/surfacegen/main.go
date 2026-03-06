package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/krystophny/tabura/internal/surface"
)

func main() {
	var checkOnly bool
	flag.BoolVar(&checkOnly, "check", false, "verify generated files are up to date")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fatalf("resolve working directory: %v", err)
	}

	interfacesPath := filepath.Join(root, "docs", "interfaces.md")

	changed := false

	interfacesChanged, err := syncFullFile(interfacesPath, []byte(surface.InterfacesMarkdown()), checkOnly)
	if err != nil {
		fatalf("sync %s: %v", interfacesPath, err)
	}
	if interfacesChanged {
		changed = true
	}

	if checkOnly && changed {
		os.Exit(1)
	}
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func syncFullFile(path string, next []byte, checkOnly bool) (bool, error) {
	current, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return writeIfChanged(path, current, ensureTrailingNewline(next), checkOnly)
}

func writeIfChanged(path string, current, next []byte, checkOnly bool) (bool, error) {
	if bytes.Equal(current, next) {
		return false, nil
	}
	if checkOnly {
		fmt.Printf("out of sync: %s\n", path)
		return true, nil
	}
	if err := os.WriteFile(path, next, 0o644); err != nil {
		return false, err
	}
	fmt.Printf("updated: %s\n", path)
	return true, nil
}

func ensureTrailingNewline(in []byte) []byte {
	if len(in) == 0 || in[len(in)-1] == '\n' {
		return in
	}
	return append(in, '\n')
}
