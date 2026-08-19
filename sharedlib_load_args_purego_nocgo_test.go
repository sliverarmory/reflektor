//go:build !cgo && (darwin || linux) && (386 || amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/sliverarmory/reflektor"
)

func TestCallExportWithPuregoCallback(t *testing.T) {
	requireCommand(t, "zig")

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
		{name: "recursive-bytes", load: func() (*reflektor.Library, error) {
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

			const callbackReturn int32 = 0x5a17
			var callbackOutput []byte
			callback := purego.NewCallback(func(data uintptr, size int32) int32 {
				if data == 0 || size < 0 {
					return -1
				}
				callbackOutput = append(callbackOutput[:0], unsafe.Slice((*byte)(unsafe.Pointer(data)), int(size))...)
				return callbackReturn
			})

			input := []byte("sliver-native-extension")
			for call := 0; call < 20; call++ {
				callbackOutput = callbackOutput[:0]
				result, err := library.CallExportWithArgs(
					"ReflektorArgsEcho",
					uintptr(unsafe.Pointer(unsafe.SliceData(input))),
					uintptr(uint32(len(input))),
					callback,
				)
				runtime.KeepAlive(input)
				if err != nil {
					t.Fatalf("CallExportWithArgs(ReflektorArgsEcho) call %d: %v", call, err)
				}
				if result != uintptr(callbackReturn) {
					t.Fatalf("callback return on call %d: got=%#x want=%#x", call, result, uintptr(callbackReturn))
				}
				if !bytes.Equal(callbackOutput, input) {
					t.Fatalf("callback output on call %d: got=%q want=%q", call, callbackOutput, input)
				}
			}

			if err := library.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}
			runtime.Gosched()
		})
	}
}
