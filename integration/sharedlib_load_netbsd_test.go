//go:build netbsd && amd64

package reflektor_test

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sliverarmory/reflektor"
	"github.com/sliverarmory/reflektor/native"
)

func TestNetBSDGeneratedBOF(t *testing.T) {
	requireCommand(t, "zig")
	testLoadAndExecuteGeneratedBOF(t, bofTarget{
		goos: "netbsd", goarch: "amd64", zigTarget: "x86_64-netbsd-none", format: "elf",
	})
}

func TestNetBSDSharedLoadersRejectMalformedAndWrongArchitecture(t *testing.T) {
	path := buildOneSharedLib(t, t.TempDir(), "netbsd", "amd64")
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wrongMachine := bytes.Clone(image)
	binary.LittleEndian.PutUint16(wrongMachine[18:20], uint16(elf.EM_AARCH64))

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

func TestNativeNetBSDRustImageAndClose(t *testing.T) {
	path := buildOneRustSharedLib(t, newRustBuildDir(t), "netbsd", "amd64")
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
