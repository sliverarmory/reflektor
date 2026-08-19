package native

import (
	"errors"
	"os"
	"testing"
)

func TestLoadLibraryRejectsGoImageBeforePlatformLoad(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	image, err := os.ReadFile(executable)
	if err != nil {
		t.Fatalf("read test executable: %v", err)
	}

	if _, err := LoadLibrary(image); !errors.Is(err, ErrGoSharedLibraryUnsupported) {
		t.Fatalf("LoadLibrary(Go image) error = %v, want ErrGoSharedLibraryUnsupported", err)
	}
}

func TestLibraryCloseAndArgumentLimit(t *testing.T) {
	backend := &testModule{}
	library := &Library{module: backend}

	if _, err := library.CallExportWithArgs("too_many", 1, 2, 3, 4); err == nil {
		t.Fatal("CallExportWithArgs accepted more than MaxExportArguments")
	}
	if backend.calls != 0 {
		t.Fatalf("backend calls after rejected argument list = %d, want 0", backend.calls)
	}
	if err := library.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := library.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if backend.frees != 1 {
		t.Fatalf("backend frees = %d, want 1", backend.frees)
	}
	if err := library.CallExport("closed"); !errors.Is(err, ErrLibraryClosed) {
		t.Fatalf("CallExport after Close error = %v, want ErrLibraryClosed", err)
	}
}

type testModule struct {
	calls int
	frees int
}

func (module *testModule) CallExport(string) error {
	module.calls++
	return nil
}

func (module *testModule) CallExportWithArgs(string, ...uintptr) (uintptr, error) {
	module.calls++
	return 0, nil
}

func (module *testModule) Free() {
	module.frees++
}
