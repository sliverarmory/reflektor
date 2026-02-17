//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

const windowsFallbackMarkerPath = `C:\Windows\Temp\reflektor_marker.txt`

func callWindowsExportFromDLL(t *testing.T, dllPath string, exportName string) {
	t.Helper()

	handle, err := windows.LoadLibrary(dllPath)
	if err != nil {
		t.Fatalf("LoadLibrary(%s): %v", dllPath, err)
	}

	addr, err := windows.GetProcAddress(handle, exportName)
	if err != nil {
		t.Fatalf("GetProcAddress(%s, %s): %v", dllPath, exportName, err)
	}

	// The export is a zero-argument function; ignore last-error semantics.
	_, _, _ = syscall.Syscall(addr, 0, 0, 0, 0)

	// Go c-shared DLL unload can destabilize the test process on Windows.
	// Keep the handle pinned for those fixtures and only unload C fixtures.
	if strings.Contains(strings.ToLower(filepath.Base(dllPath)), "basic_go_") {
		return
	}
	t.Cleanup(func() {
		_ = windows.FreeLibrary(handle)
	})
}

func readMarkerWithWindowsFallback(t *testing.T, markerPath string) []byte {
	t.Helper()

	got, err := os.ReadFile(markerPath)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		got, err = os.ReadFile(windowsFallbackMarkerPath)
	}
	if err != nil {
		t.Fatalf("read marker %s (or fallback %s): %v", markerPath, windowsFallbackMarkerPath, err)
	}
	return got
}
