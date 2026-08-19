package reflektor_test

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const rustHTTPMarker = "ok:200"

func buildOneRustSharedLib(t *testing.T, outDir string, goos string, goarch string) string {
	t.Helper()
	requireCommand(t, "cargo")

	target, ok := rustTargetFor(goos, goarch)
	if !ok {
		t.Fatalf("unsupported Rust shared-library target %s/%s", goos, goarch)
	}

	manifestPath, err := filepath.Abs(filepath.Join("..", "testdata", "rust", "Cargo.toml"))
	if err != nil {
		t.Fatalf("resolve Rust fixture manifest: %v", err)
	}
	targetDir := filepath.Join(outDir, "cargo-target")
	cmd := exec.Command(
		"cargo",
		"build",
		"--manifest-path", manifestPath,
		"--locked",
		"--release",
		"--target", target,
		"--target-dir", targetDir,
	)
	cmd.Env = overrideEnv(os.Environ(), map[string]string{
		"CARGO_TERM_COLOR": "never",
	})
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build Rust shared lib target=%s/%s (%s): %v\n%s", goos, goarch, target, err, output)
	}

	artifactName, err := rustSharedLibName(goos)
	if err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(targetDir, target, "release", artifactName)
	info, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatalf("stat Rust shared lib %s: %v\ncargo output:\n%s", artifactPath, err, output)
	}
	if info.Size() == 0 {
		t.Fatalf("empty Rust shared lib: %s", artifactPath)
	}

	assertRustSharedLibIsLoadable(t, artifactPath, goos)
	return artifactPath
}

func rustTargetFor(goos string, goarch string) (string, bool) {
	switch {
	case goos == "darwin" && goarch == "amd64":
		return "x86_64-apple-darwin", true
	case goos == "darwin" && goarch == "arm64":
		return "aarch64-apple-darwin", true
	case goos == "linux" && goarch == "386":
		return "i686-unknown-linux-gnu", true
	case goos == "linux" && goarch == "amd64":
		return "x86_64-unknown-linux-gnu", true
	case goos == "linux" && goarch == "arm64":
		return "aarch64-unknown-linux-gnu", true
	case goos == "windows" && goarch == "386":
		return "i686-pc-windows-msvc", true
	case goos == "windows" && goarch == "amd64":
		return "x86_64-pc-windows-msvc", true
	case goos == "windows" && goarch == "arm64":
		return "aarch64-pc-windows-msvc", true
	default:
		return "", false
	}
}

func rustSharedLibName(goos string) (string, error) {
	switch goos {
	case "darwin":
		return "libreflektor_http_fixture.dylib", nil
	case "linux":
		return "libreflektor_http_fixture.so", nil
	case "windows":
		return "reflektor_http_fixture.dll", nil
	default:
		return "", fmt.Errorf("unsupported Rust shared-library OS: %s", goos)
	}
}

func newRustBuildDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "reflektor-rust-fixture-*")
	if err != nil {
		t.Fatalf("create Rust fixture build dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func assertRustHTTPMarker(t *testing.T, markerPath string) {
	t.Helper()
	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read Rust HTTP marker %s: %v", markerPath, err)
	}
	if !bytes.Equal(got, []byte(rustHTTPMarker)) {
		t.Fatalf("Rust HTTPS GET failed: marker=%q want=%q", got, rustHTTPMarker)
	}
}

func assertRustSharedLibIsLoadable(t *testing.T, path string, goos string) {
	t.Helper()
	switch goos {
	case "darwin":
		file, err := macho.Open(path)
		if err != nil {
			t.Fatalf("open Rust Mach-O fixture: %v", err)
		}
		defer file.Close()
		const (
			machoSectionTypeMask             = 0x000000ff
			machoThreadLocalRegular          = 0x11
			machoThreadLocalInitFunctionPtrs = 0x15
		)
		for _, section := range file.Sections {
			if section.Name == "__la_symbol_ptr" && section.Size != 0 {
				t.Fatalf("Rust Mach-O fixture contains unsupported lazy bindings in %s", section.Name)
			}
			sectionType := section.Flags & machoSectionTypeMask
			if strings.HasPrefix(section.Name, "__thread") ||
				(sectionType >= machoThreadLocalRegular && sectionType <= machoThreadLocalInitFunctionPtrs) {
				t.Fatalf("Rust Mach-O fixture contains unsupported TLS section %s (type=%#x)", section.Name, sectionType)
			}
		}
	case "linux":
		file, err := elf.Open(path)
		if err != nil {
			t.Fatalf("open Rust ELF fixture: %v", err)
		}
		defer file.Close()
		for _, program := range file.Progs {
			if program.Type == elf.PT_TLS && program.Memsz != 0 {
				t.Fatalf("Rust ELF fixture contains unsupported PT_TLS segment (filesz=%d memsz=%d)", program.Filesz, program.Memsz)
			}
		}
	case "windows":
		file, err := pe.Open(path)
		if err != nil {
			t.Fatalf("open Rust PE fixture: %v", err)
		}
		defer file.Close()
		const imageDirectoryEntryTLS = 9
		var virtualAddress, size uint32
		switch optional := file.OptionalHeader.(type) {
		case *pe.OptionalHeader32:
			virtualAddress = optional.DataDirectory[imageDirectoryEntryTLS].VirtualAddress
			size = optional.DataDirectory[imageDirectoryEntryTLS].Size
		case *pe.OptionalHeader64:
			virtualAddress = optional.DataDirectory[imageDirectoryEntryTLS].VirtualAddress
			size = optional.DataDirectory[imageDirectoryEntryTLS].Size
		default:
			t.Fatalf("Rust PE fixture has unsupported optional header %T", file.OptionalHeader)
		}
		if virtualAddress != 0 || size != 0 {
			t.Fatalf("Rust PE fixture contains unsupported TLS directory (rva=%#x size=%d)", virtualAddress, size)
		}
	default:
		t.Fatalf("unsupported Rust fixture OS validation: %s", goos)
	}
}
