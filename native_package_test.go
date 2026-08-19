//go:build (darwin && (amd64 || arm64)) || (linux && (386 || amd64 || arm64)) || (windows && (386 || amd64 || arm64))

package reflektor_test

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"unsafe"

	"github.com/sliverarmory/reflektor/native"
)

func TestNativePackageCallExport(t *testing.T) {
	requireCommand(t, "zig")

	fixturePath := buildArgumentSharedLib(t, t.TempDir(), runtime.GOOS, runtime.GOARCH)
	payload, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read native argument fixture: %v", err)
	}
	library, err := native.LoadLibrary(payload)
	if err != nil {
		t.Fatalf("native.LoadLibrary: %v", err)
	}
	t.Cleanup(func() { _ = library.Close() })

	if err := library.CallExport("ReflektorArgsInit"); err != nil {
		t.Fatalf("CallExport(ReflektorArgsInit): %v", err)
	}
	callbackAddress, err := library.CallExportWithArgs("ReflektorArgsCallbackAddress")
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorArgsCallbackAddress): %v", err)
	}
	if callbackAddress == 0 {
		t.Fatal("ReflektorArgsCallbackAddress returned zero")
	}

	input := []byte{3, 1, 4, 1, 5}
	result, err := library.CallExportWithArgs(
		"ReflektorArgsRun",
		uintptr(unsafe.Pointer(unsafe.SliceData(input))),
		uintptr(len(input)),
		callbackAddress,
	)
	runtime.KeepAlive(input)
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorArgsRun): %v", err)
	}
	if want := uintptr(14<<16 | 41); result != want {
		t.Fatalf("CallExportWithArgs result = %#x, want %#x", result, want)
	}

	state, err := library.CallExportWithArgs("ReflektorArgsState")
	if err != nil {
		t.Fatalf("CallExportWithArgs(ReflektorArgsState): %v", err)
	}
	if state != 41 {
		t.Fatalf("state after argument call = %d, want 41", state)
	}

	if err := library.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := library.CallExportWithArgs("ReflektorArgsState"); !errors.Is(err, native.ErrLibraryClosed) {
		t.Fatalf("call after Close error = %v, want ErrLibraryClosed", err)
	}
}
