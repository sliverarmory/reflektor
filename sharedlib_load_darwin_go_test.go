//go:build darwin && (amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor"
	"golang.org/x/sys/unix"
)

func TestLoadGeneratedGoDylibAndCallStartW(t *testing.T) {
	if runtime.GOARCH == "amd64" {
		if translated, err := unix.SysctlUint32("sysctl.proc_translated"); err == nil && translated == 1 {
			t.Skip("darwin/amd64 under Rosetta is not supported by the dyld4-only in-memory loader")
		}
	}

	outDir := t.TempDir()
	dylibPath := buildOneGoSharedLib(t, outDir, "darwin", runtime.GOARCH)
	markerPath := filepath.Join(t.TempDir(), "reflektor_go_marker.txt")
	fallbackMarkerPath := "/tmp/reflektor_marker.txt"
	_ = os.Remove(fallbackMarkerPath)
	t.Cleanup(func() {
		_ = os.Remove(fallbackMarkerPath)
	})

	if err := os.Setenv("REFLEKTOR_MARKER", markerPath); err != nil {
		t.Fatalf("set env REFLEKTOR_MARKER: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("REFLEKTOR_MARKER")
	})

	lib, err := reflektor.LoadLibraryFile(dylibPath)
	if err != nil {
		t.Fatalf("LoadLibraryFile(%s): %v", dylibPath, err)
	}
	t.Cleanup(func() {
		_ = lib.Close()
	})

	if err := lib.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW): %v", err)
	}

	got, err := os.ReadFile(markerPath)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		got, err = os.ReadFile(fallbackMarkerPath)
	}
	if err != nil {
		t.Fatalf("read marker %s (or fallback %s): %v", markerPath, fallbackMarkerPath, err)
	}
	if !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("unexpected marker bytes: got=%q want=%q", got, []byte("ok"))
	}
}
