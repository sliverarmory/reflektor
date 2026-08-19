//go:build (darwin || linux || windows) && (386 || amd64 || arm64)

package reflektor_test

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"github.com/sliverarmory/reflektor"
)

func TestCallExportWithArgs(t *testing.T) {
	requireCommand(t, "zig")
	requireRecursiveLoaderPlatform(t)

	outDir := t.TempDir()
	libraryPath := buildArgumentSharedLib(t, outDir, runtime.GOOS, runtime.GOARCH)
	libraryData, err := os.ReadFile(libraryPath)
	if err != nil {
		t.Fatalf("read argument fixture: %v", err)
	}
	modes := []struct {
		name string
		load func() (*reflektor.Library, error)
	}{
		{name: "legacy-bytes", load: func() (*reflektor.Library, error) {
			return reflektor.LoadLibrary(libraryData)
		}},
	}
	modes = append(modes,
		struct {
			name string
			load func() (*reflektor.Library, error)
		}{name: "recursive-bytes", load: func() (*reflektor.Library, error) {
			return reflektor.LoadLibraryRecursive(libraryData)
		}},
		struct {
			name string
			load func() (*reflektor.Library, error)
		}{name: "recursive-file", load: func() (*reflektor.Library, error) {
			return reflektor.LoadLibraryFileRecursive(libraryPath)
		}},
	)

	for _, mode := range modes {
		mode := mode
		t.Run(mode.name, func(t *testing.T) {
			library, err := mode.load()
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			t.Cleanup(func() { _ = library.Close() })

			// This intentionally uses the original zero-argument API. The later
			// calls must observe state initialized in this exact mapped image.
			if err := library.CallExport("ReflektorArgsInit"); err != nil {
				t.Fatalf("CallExport(ReflektorArgsInit): %v", err)
			}
			callback, err := library.CallExportWithArgs("ReflektorArgsCallbackAddress")
			if err != nil {
				t.Fatalf("CallExportWithArgs(ReflektorArgsCallbackAddress): %v", err)
			}
			if callback == 0 {
				t.Fatal("argument fixture returned a nil callback address")
			}

			first := []byte{3, 1, 4, 1, 5}
			got, err := callArgumentFixture(library, first, callback)
			if err != nil {
				t.Fatalf("first argument call: %v", err)
			}
			if want := argumentFixtureResult(first, 41); got != want {
				t.Fatalf("first argument result: got=%#x want=%#x", got, want)
			}

			second := []byte{2, 7}
			got, err = callArgumentFixture(library, second, callback)
			if err != nil {
				t.Fatalf("second argument call: %v", err)
			}
			if want := argumentFixtureResult(second, 42); got != want {
				t.Fatalf("second argument result: got=%#x want=%#x", got, want)
			}

			state, err := library.CallExportWithArgs("ReflektorArgsState")
			if err != nil {
				t.Fatalf("CallExportWithArgs(ReflektorArgsState): %v", err)
			}
			if state != 42 {
				t.Fatalf("state after repeated calls: got=%d want=42", state)
			}

			if _, err := library.CallExportWithArgs("ReflektorMissingExport", 1); err == nil {
				t.Fatal("missing argument export unexpectedly resolved")
			}
			if _, err := library.CallExportWithArgs("ReflektorArgsRun", 1, 2, 3, 4); err == nil {
				t.Fatal("four-argument export call unexpectedly succeeded")
			}

			if err := library.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if _, err := library.CallExportWithArgs("ReflektorArgsState"); !errors.Is(err, reflektor.ErrLibraryClosed) {
				t.Fatalf("call after Close: got=%v want ErrLibraryClosed", err)
			}
		})
	}
}

func callArgumentFixture(library *reflektor.Library, input []byte, callback uintptr) (uintptr, error) {
	result, err := library.CallExportWithArgs(
		"ReflektorArgsRun",
		uintptr(unsafe.Pointer(unsafe.SliceData(input))),
		uintptr(uint32(len(input))),
		callback,
	)
	runtime.KeepAlive(input)
	return result, err
}

func argumentFixtureResult(input []byte, state uintptr) uintptr {
	var sum uintptr
	for _, value := range input {
		sum += uintptr(value)
	}
	return (sum << 16) | (state & 0xffff)
}
