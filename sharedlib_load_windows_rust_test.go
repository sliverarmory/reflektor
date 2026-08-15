//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor"
)

func TestLoadGeneratedRustHTTPSharedLibrary(t *testing.T) {
	outDir := newRustBuildDir(t)
	dllPath := buildOneRustSharedLib(t, outDir, "windows", runtime.GOARCH)
	markerPath := filepath.Join(t.TempDir(), "reflektor_rust_http_marker.txt")
	t.Setenv("REFLEKTOR_MARKER", markerPath)

	library, err := reflektor.LoadLibraryFile(dllPath)
	if err != nil {
		t.Fatalf("LoadLibraryFile(%s): %v", dllPath, err)
	}
	t.Cleanup(func() {
		_ = library.Close()
	})
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW): %v", err)
	}

	assertRustHTTPMarker(t, markerPath)
}
