//go:build (darwin && (amd64 || arm64)) || (linux && (386 || amd64 || arm64)) || (windows && (386 || amd64 || arm64))

package reflektor_test

import (
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor/native"
)

func TestNativePackageRejectsGoCSharedImage(t *testing.T) {
	libraryPath := buildOneGoSharedLib(t, t.TempDir(), runtime.GOOS, runtime.GOARCH)
	image, err := os.ReadFile(libraryPath)
	if err != nil {
		t.Fatalf("read Go c-shared fixture: %v", err)
	}

	library, err := native.LoadLibrary(image)
	if library != nil {
		_ = library.Close()
		t.Fatal("native.LoadLibrary returned a library for a Go c-shared image")
	}
	if !errors.Is(err, native.ErrGoSharedLibraryUnsupported) {
		t.Fatalf("native.LoadLibrary(Go c-shared) error = %v, want ErrGoSharedLibraryUnsupported", err)
	}
}
