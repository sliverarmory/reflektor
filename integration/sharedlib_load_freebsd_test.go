//go:build freebsd && (amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor"
)

func TestLoadGeneratedCFreeBSDSOAndCallStartW(t *testing.T) {
	requireCommand(t, "zig")

	soPath := buildOneSharedLib(t, t.TempDir(), "freebsd", runtime.GOARCH)
	markerPath := filepath.Join(t.TempDir(), "reflektor_marker.txt")
	t.Setenv("REFLEKTOR_MARKER", markerPath)

	library, err := reflektor.LoadLibraryFile(soPath)
	if err != nil {
		t.Fatalf("LoadLibraryFile(%s): %v", soPath, err)
	}
	t.Cleanup(func() { _ = library.Close() })
	if err := library.CallExport("StartW"); err != nil {
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
