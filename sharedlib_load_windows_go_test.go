//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadGeneratedGoWindowsDLLAndCallStartW(t *testing.T) {
	outDir, err := os.MkdirTemp("", "reflektor-go-windows-dll-*")
	if err != nil {
		t.Fatalf("create temp build dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(outDir)
	})
	dllPath := buildOneGoSharedLib(t, outDir, "windows", runtime.GOARCH)
	markerPath := filepath.Join(t.TempDir(), "reflektor_go_marker.txt")
	_ = os.Remove(windowsFallbackMarkerPath)
	t.Cleanup(func() {
		_ = os.Remove(windowsFallbackMarkerPath)
	})

	if err := os.Setenv("REFLEKTOR_MARKER", markerPath); err != nil {
		t.Fatalf("set env REFLEKTOR_MARKER: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("REFLEKTOR_MARKER")
	})

	callWindowsExportFromDLL(t, dllPath, "StartW")

	got := readMarkerWithWindowsFallback(t, markerPath)
	if !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("unexpected marker bytes: got=%q want=%q", got, []byte("ok"))
	}
}
