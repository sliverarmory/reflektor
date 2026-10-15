//go:build linux && amd64 && cgo

package reflektor_test

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sliverarmory/reflektor"
	"github.com/sliverarmory/reflektor/native"
)

func TestELFLocalDynamicTLSRootAndNative(t *testing.T) {
	requireCommand(t, "zig")
	path := buildCSharedLib(t, t.TempDir(), "linux", "amd64", "tls_runtime",
		filepath.Join("..", "testdata", "c", "tls_runtime.c"))
	image := readTLSFixture(t, path)
	requireTLSRelocation(t, image, elf.R_X86_64_DTPMOD64)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	root, err := reflektor.LoadLibrary(image)
	if err != nil {
		t.Fatalf("load root TLS fixture: %v", err)
	}
	defer root.Close()
	standalone, err := native.LoadLibrary(image)
	if err != nil {
		t.Fatalf("load native TLS fixture: %v", err)
	}
	defer standalone.Close()
	assertTLSValue(t, "root constructor", root.CallExportWithArgs, "ReflektorTLSConstructorValue", 11)
	assertTLSValue(t, "native constructor", standalone.CallExportWithArgs, "ReflektorTLSConstructorValue", 11)

	assertTLSValue(t, "root first", root.CallExportWithArgs, "ReflektorTLSNext", 801)
	assertTLSValue(t, "root second", root.CallExportWithArgs, "ReflektorTLSNext", 902)
	assertTLSValue(t, "native independent first", standalone.CallExportWithArgs, "ReflektorTLSNext", 801)
	assertTLSValue(t, "root third", root.CallExportWithArgs, "ReflektorTLSNext", 1003)
	assertTLSValue(t, "native second", standalone.CallExportWithArgs, "ReflektorTLSNext", 902)
	assertTLSValue(t, "root pthread isolation", root.CallExportWithArgs, "ReflektorTLSThreadIsolation", 1)
	assertTLSValue(t, "native pthread isolation", standalone.CallExportWithArgs, "ReflektorTLSThreadIsolation", 1)

	if err := root.Close(); err != nil {
		t.Fatalf("close root TLS fixture: %v", err)
	}
	reloaded, err := reflektor.LoadLibrary(image)
	if err != nil {
		t.Fatalf("reload root TLS fixture: %v", err)
	}
	defer reloaded.Close()
	assertTLSValue(t, "reloaded root constructor", reloaded.CallExportWithArgs, "ReflektorTLSConstructorValue", 11)
	assertTLSValue(t, "reloaded root starts from template", reloaded.CallExportWithArgs, "ReflektorTLSNext", 801)
}

func TestELFLocalDynamicTLSRecursiveDependency(t *testing.T) {
	requireCommand(t, "zig")
	dir := t.TempDir()
	leaf := filepath.Join(dir, "libtlsleaf.so")
	root := filepath.Join(dir, "libtlsroot.so")
	buildTLSFixture(t, leaf, filepath.Join("..", "testdata", "c", "tls_recursive_leaf.c"),
		"-Wl,-soname,libtlsleaf.so")
	buildTLSFixture(t, root, filepath.Join("..", "testdata", "c", "tls_recursive_root.c"),
		"-L"+dir, "-ltlsleaf", "-Wl,-rpath,$ORIGIN")
	requireTLSRelocation(t, readTLSFixture(t, leaf), elf.R_X86_64_DTPMOD64)
	requireTLSRelocation(t, readTLSFixture(t, root), elf.R_X86_64_DTPMOD64)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	library, err := reflektor.LoadLibraryFileRecursive(root)
	if err != nil {
		t.Fatalf("load recursive TLS graph: %v", err)
	}
	defer library.Close()
	assertTLSValue(t, "recursive first", library.CallExportWithArgs, "ReflektorTLSRootNext", 408)
	assertTLSValue(t, "recursive second", library.CallExportWithArgs, "ReflektorTLSRootNext", 509)
	assertTLSValue(t, "recursive pthread isolation", library.CallExportWithArgs, "ReflektorTLSRecursiveThreadIsolation", 1)
	if err := library.Close(); err != nil {
		t.Fatalf("close recursive TLS graph: %v", err)
	}
	reloaded, err := reflektor.LoadLibraryFileRecursive(root)
	if err != nil {
		t.Fatalf("reload recursive TLS graph: %v", err)
	}
	defer reloaded.Close()
	assertTLSValue(t, "reloaded graph starts from templates", reloaded.CallExportWithArgs, "ReflektorTLSRootNext", 408)
}

func TestELFInitialExecTLSStillRejected(t *testing.T) {
	requireCommand(t, "zig")
	path := filepath.Join(t.TempDir(), "libtls-initial-exec.so")
	buildTLSFixture(t, path, filepath.Join("..", "testdata", "c", "tls_runtime.c"),
		"-ftls-model=initial-exec")
	image := readTLSFixture(t, path)
	requireTLSRelocation(t, image, elf.R_X86_64_TPOFF64)
	loaders := []struct {
		name string
		load func() error
	}{
		{"root", func() error { _, err := reflektor.LoadLibrary(image); return err }},
		{"native", func() error { _, err := native.LoadLibrary(image); return err }},
		{"recursive", func() error { _, err := reflektor.LoadLibraryRecursive(image); return err }},
	}
	for _, loader := range loaders {
		t.Run(loader.name, func(t *testing.T) {
			if err := loader.load(); err == nil || !strings.Contains(err.Error(), "PT_TLS") {
				t.Fatalf("initial-exec TLS load error = %v, want explicit PT_TLS rejection", err)
			}
		})
	}
}

func TestELFExternalTLSStillRejected(t *testing.T) {
	requireCommand(t, "zig")
	path := filepath.Join(t.TempDir(), "libtls-external.so")
	buildTLSFixture(t, path, filepath.Join("..", "testdata", "c", "tls_external.c"))
	image := readTLSFixture(t, path)
	loaders := []struct {
		name string
		load func() error
	}{
		{"root", func() error { _, err := reflektor.LoadLibrary(image); return err }},
		{"native", func() error { _, err := native.LoadLibrary(image); return err }},
		{"recursive", func() error { _, err := reflektor.LoadLibraryRecursive(image); return err }},
	}
	for _, loader := range loaders {
		t.Run(loader.name, func(t *testing.T) {
			if err := loader.load(); err == nil || !strings.Contains(err.Error(), "external TLS symbol") {
				t.Fatalf("external TLS load error = %v, want explicit rejection", err)
			}
		})
	}
}

func buildTLSFixture(t *testing.T, output, source string, extra ...string) {
	t.Helper()
	args := []string{"cc", "-target", "x86_64-linux-gnu", "-O2", "-g0", "-shared", "-fPIC", "-o", output, source}
	args = append(args, extra...)
	command := exec.Command("zig", args...)
	command.Env = append(os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build TLS fixture: %v\n%s", err, output)
	}
}

func readTLSFixture(t *testing.T, path string) []byte {
	t.Helper()
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := elf.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, segment := range f.Progs {
		if segment.Type == elf.PT_TLS && segment.Filesz > 0 && segment.Memsz >= segment.Filesz {
			return image
		}
	}
	t.Fatalf("fixture %s has no initialized PT_TLS segment", path)
	return nil
}

func assertTLSValue(t *testing.T, label string, call func(string, ...uintptr) (uintptr, error), name string, want uintptr) {
	t.Helper()
	got, err := call(name)
	if err != nil || got != want {
		t.Fatalf("%s: %s returned %d, %v; want %d", label, name, got, err, want)
	}
}

func requireTLSRelocation(t *testing.T, image []byte, want elf.R_X86_64) {
	t.Helper()
	f, err := elf.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, name := range []string{".rela.dyn", ".rela.plt"} {
		section := f.Section(name)
		if section == nil {
			continue
		}
		data, err := section.Data()
		if err != nil {
			t.Fatal(err)
		}
		for offset := 0; offset+24 <= len(data); offset += 24 {
			info := binary.LittleEndian.Uint64(data[offset+8 : offset+16])
			if elf.R_X86_64(elf.R_TYPE64(info)) == want {
				return
			}
		}
	}
	t.Fatalf("ELF fixture has no %s relocation", want)
}
