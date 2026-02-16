//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"errors"
	"os"
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
	t.Cleanup(func() {
		_ = windows.FreeLibrary(handle)
	})

	addr, err := windows.GetProcAddress(handle, exportName)
	if err != nil {
		t.Fatalf("GetProcAddress(%s, %s): %v", dllPath, exportName, err)
	}

	// The export is a zero-argument function; ignore last-error semantics.
	_, _, _ = syscall.SyscallN(addr)
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
