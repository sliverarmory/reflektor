//go:build netbsd && amd64

package reflektor_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sliverarmory/reflektor"
	"github.com/sliverarmory/reflektor/native"
)

func TestLoadGeneratedCNetBSDSOAndCallStartW(t *testing.T) {
	requireCommand(t, "zig")
	path := buildOneSharedLib(t, t.TempDir(), "netbsd", "amd64")
	marker := filepath.Join(t.TempDir(), "c-marker")
	t.Setenv("REFLEKTOR_MARKER", marker)

	library, err := reflektor.LoadLibraryFile(path)
	if err != nil {
		t.Fatalf("LoadLibraryFile: %v", err)
	}
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport: %v", err)
	}
	if got, err := os.ReadFile(marker); err != nil || !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("C marker = %q, error = %v", got, err)
	}
	if err := library.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := library.CallExport("StartW"); !errors.Is(err, reflektor.ErrLibraryClosed) {
		t.Fatalf("call after Close = %v, want ErrLibraryClosed", err)
	}
}

func TestLoadGeneratedRustNetBSDSharedLibrary(t *testing.T) {
	path := buildOneRustSharedLib(t, newRustBuildDir(t), "netbsd", "amd64")
	marker := filepath.Join(t.TempDir(), "rust-marker")
	t.Setenv("REFLEKTOR_MARKER", marker)

	library, err := reflektor.LoadLibraryFile(path)
	if err != nil {
		t.Fatalf("LoadLibraryFile: %v", err)
	}
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport: %v", err)
	}
	assertRustHTTPMarker(t, marker)
	if err := library.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNativeNetBSDCImageAndClose(t *testing.T) {
	path := buildOneSharedLib(t, t.TempDir(), "netbsd", "amd64")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	library, err := native.LoadLibrary(data)
	if err != nil {
		t.Fatalf("native C image load: %v", err)
	}
	if err := library.Close(); err != nil {
		t.Fatalf("native Close: %v", err)
	}
}
