//go:build netbsd && (amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sliverarmory/reflektor"
	"github.com/sliverarmory/reflektor/native"
)

func TestNetBSDSharedLoadersRejectMalformedAndWrongArchitecture(t *testing.T) {
	path := buildOneSharedLib(t, t.TempDir(), "netbsd", runtime.GOARCH)
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wrongMachine := bytes.Clone(image)
	wrongArchitecture := elf.EM_X86_64
	if runtime.GOARCH == "amd64" {
		wrongArchitecture = elf.EM_AARCH64
	}
	binary.LittleEndian.PutUint16(wrongMachine[18:20], uint16(wrongArchitecture))

	for _, input := range []struct {
		name string
		data []byte
	}{
		{name: "malformed", data: []byte("not an ELF image")},
		{name: "wrong architecture", data: wrongMachine},
	} {
		t.Run(input.name, func(t *testing.T) {
			if library, err := reflektor.LoadLibrary(input.data); err == nil {
				if library != nil {
					_ = library.Close()
				}
				t.Fatal("root loader accepted invalid image")
			}
			if library, err := reflektor.LoadLibraryRecursive(input.data); err == nil {
				if library != nil {
					_ = library.Close()
				}
				t.Fatal("recursive loader accepted invalid image")
			}
			if library, err := native.LoadLibrary(input.data); err == nil {
				if library != nil {
					_ = library.Close()
				}
				t.Fatal("native loader accepted invalid image")
			}
		})
	}
}

func TestLoadGeneratedCNetBSDSOAndCallStartW(t *testing.T) {
	requireCommand(t, "zig")
	path := buildOneSharedLib(t, t.TempDir(), "netbsd", runtime.GOARCH)
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
	path := buildOneRustSharedLib(t, newRustBuildDir(t), "netbsd", runtime.GOARCH)
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
	path := buildOneSharedLib(t, t.TempDir(), "netbsd", runtime.GOARCH)
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

func TestNativeNetBSDRustImageAndClose(t *testing.T) {
	path := buildOneRustSharedLib(t, newRustBuildDir(t), "netbsd", runtime.GOARCH)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "native-rust-marker")
	t.Setenv("REFLEKTOR_MARKER", marker)
	library, err := native.LoadLibrary(data)
	if err != nil {
		t.Fatalf("native Rust image load: %v", err)
	}
	if err := library.CallExport("StartW"); err != nil {
		t.Fatalf("native Rust CallExport: %v", err)
	}
	assertRustHTTPMarker(t, marker)
	if err := library.Close(); err != nil {
		t.Fatalf("native Close: %v", err)
	}
}
