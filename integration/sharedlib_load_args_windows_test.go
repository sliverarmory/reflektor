//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/sliverarmory/reflektor"
)

func TestCallExportWithGoCallbackWindows(t *testing.T) {
	requireCommand(t, "zig")

	outDir := t.TempDir()
	libraryPath := buildArgumentSharedLib(t, outDir, "windows", runtime.GOARCH)
	libraryData, err := os.ReadFile(libraryPath)
	if err != nil {
		t.Fatalf("read argument fixture: %v", err)
	}

	modes := []struct {
		name string
		load func() (*reflektor.Library, error)
	}{
		{name: "legacy", load: func() (*reflektor.Library, error) {
			return reflektor.LoadLibrary(libraryData)
		}},
		{name: "recursive", load: func() (*reflektor.Library, error) {
			return reflektor.LoadLibraryRecursive(libraryData)
		}},
	}

	for _, mode := range modes {
		mode := mode
		t.Run(mode.name, func(t *testing.T) {
			library, err := mode.load()
			if err != nil {
				t.Fatalf("load fixture: %v", err)
			}
			t.Cleanup(func() { _ = library.Close() })

			if err := library.CallExport("ReflektorArgsInit"); err != nil {
				t.Fatalf("CallExport(ReflektorArgsInit): %v", err)
			}

			var callbackSum, callbackState uintptr
			callback := syscall.NewCallback(func(sum, state uintptr) uintptr {
				callbackSum = sum
				callbackState = state
				return 0xbeef
			})
			input := []byte{3, 1, 4, 1, 5}
			result, err := library.CallExportWithArgs(
				"ReflektorArgsRun",
				uintptr(unsafe.Pointer(unsafe.SliceData(input))),
				uintptr(uint32(len(input))),
				callback,
			)
			runtime.KeepAlive(input)
			if err != nil {
				t.Fatalf("CallExportWithArgs(ReflektorArgsRun): %v", err)
			}
			if result != 0xbeef {
				t.Fatalf("callback result: got=%#x want=%#x", result, uintptr(0xbeef))
			}
			if callbackSum != 14 || callbackState != 41 {
				t.Fatalf("callback arguments: got=(%d, %d) want=(14, 41)", callbackSum, callbackState)
			}
		})
	}
}
