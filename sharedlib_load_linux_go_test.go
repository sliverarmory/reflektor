//go:build linux && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor"
)

func TestLoadGeneratedGoLinuxSOAndCallStartW(t *testing.T) {
	outDir := t.TempDir()
	soPath := buildOneGoSharedLib(t, outDir, "linux", runtime.GOARCH)
	markerPath := filepath.Join(t.TempDir(), "reflektor_go_marker.txt")

	if err := os.Setenv("REFLEKTOR_MARKER", markerPath); err != nil {
		t.Fatalf("set env REFLEKTOR_MARKER: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("REFLEKTOR_MARKER")
	})

	lib, err := reflektor.LoadLibraryFile(soPath)
	if err != nil {
		t.Fatalf("LoadLibraryFile(%s): %v", soPath, err)
	}
	t.Cleanup(func() {
		_ = lib.Close()
	})

	if err := lib.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW): %v", err)
	}

	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker %s: %v", markerPath, err)
	}
	if !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("unexpected marker bytes: got=%q want=%q", got, []byte("ok"))
	}
}
