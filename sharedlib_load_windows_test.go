//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestLoadGeneratedCWindowsDLLAndCallStartW(t *testing.T) {
	requireCommand(t, "zig")

	outDir := t.TempDir()
	dllPath := buildOneSharedLib(t, outDir, "windows", runtime.GOARCH)
	markerPath := filepath.Join(t.TempDir(), "reflektor_marker.txt")

	if err := os.Setenv("REFLEKTOR_MARKER", markerPath); err != nil {
		t.Fatalf("set env REFLEKTOR_MARKER: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("REFLEKTOR_MARKER")
	})

	dll := syscall.NewLazyDLL(dllPath)
	startWProc := dll.NewProc("StartW")
	if err := startWProc.Find(); err != nil {
		t.Fatalf("find StartW in %s: %v", dllPath, err)
	}
	_, _, callErr := startWProc.Call()
	if callErr != windows.ERROR_SUCCESS {
		t.Fatalf("call StartW in %s failed: %v", dllPath, callErr)
	}

	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker %s: %v", markerPath, err)
	}
	if !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("unexpected marker bytes: got=%q want=%q", got, []byte("ok"))
	}
}
