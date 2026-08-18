//go:build (darwin || linux || windows) && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor"
)

func TestLoadGeneratedCSharedLibraryRecursiveMode(t *testing.T) {
	requireCommand(t, "zig")
	requireRecursiveLoaderPlatform(t)

	outDir := t.TempDir()
	libraryPath := buildOneSharedLib(t, outDir, runtime.GOOS, runtime.GOARCH)
	markerPath := filepath.Join(t.TempDir(), "reflektor_recursive_c_marker.txt")
	t.Setenv("REFLEKTOR_MARKER", markerPath)
	if runtime.GOOS == "windows" {
		_ = os.Remove(windowsRecursiveFallbackMarker)
		t.Cleanup(func() { _ = os.Remove(windowsRecursiveFallbackMarker) })
	}

	library, err := reflektor.LoadLibraryFileRecursive(libraryPath)
	if err != nil {
		t.Fatalf("LoadLibraryFileRecursive(%s): %v", libraryPath, err)
	}
	t.Cleanup(func() { _ = library.Close() })
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW): %v", err)
	}

	got, err := os.ReadFile(markerPath)
	if runtime.GOOS == "windows" && errors.Is(err, os.ErrNotExist) {
		got, err = os.ReadFile(windowsRecursiveFallbackMarker)
	}
	if err != nil {
		t.Fatalf("read recursive C marker: %v", err)
	}
	if !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("unexpected recursive C marker: got=%q want=%q", got, []byte("ok"))
	}
}

func TestLoadGeneratedGoSharedLibraryRecursiveMode(t *testing.T) {
	requireRecursiveLoaderPlatform(t)
	outDir := t.TempDir()
	libraryPath := buildOneGoSharedLib(t, outDir, runtime.GOOS, runtime.GOARCH)
	runGoRuntimeFixtureSubprocessMode(t, libraryPath, true)
}

func TestLoadGeneratedRustSharedLibraryRecursiveMode(t *testing.T) {
	requireRecursiveLoaderPlatform(t)
	outDir := newRustBuildDir(t)
	libraryPath := buildOneRustSharedLib(t, outDir, runtime.GOOS, runtime.GOARCH)
	markerPath := filepath.Join(t.TempDir(), "reflektor_recursive_rust_marker.txt")
	t.Setenv("REFLEKTOR_MARKER", markerPath)

	library, err := reflektor.LoadLibraryFileRecursive(libraryPath)
	if err != nil {
		t.Fatalf("LoadLibraryFileRecursive(%s): %v", libraryPath, err)
	}
	t.Cleanup(func() { _ = library.Close() })
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW): %v", err)
	}
	assertRustHTTPMarker(t, markerPath)
}

const windowsRecursiveFallbackMarker = `C:\Windows\Temp\reflektor_marker.txt`
