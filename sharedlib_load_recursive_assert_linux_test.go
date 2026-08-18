//go:build linux && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func assertRecursiveDependenciesNotNativeLoaded(t *testing.T, graphDir string) {
	t.Helper()
	maps, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		t.Fatalf("read /proc/self/maps: %v", err)
	}
	absDir, err := filepath.Abs(graphDir)
	if err != nil {
		t.Fatalf("resolve recursive graph directory: %v", err)
	}
	if evaluated, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = evaluated
	}
	if bytes.Contains(maps, []byte(absDir)) {
		t.Fatalf("recursive dependency graph appears in /proc/self/maps as an OS file mapping: %s", absDir)
	}
}
