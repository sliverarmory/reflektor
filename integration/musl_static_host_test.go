//go:build linux && musltest && cgo && (amd64 || arm64)

package reflektor_test

import (
	"debug/elf"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sliverarmory/reflektor"
	"github.com/sliverarmory/reflektor/bof"
	"github.com/sliverarmory/reflektor/native"
)

// TestAlpineStaticHost runs only from the Alpine harness's externally linked
// static test binary. It keeps system-library delegation outside the supported
// static-host subset and proves that missing dynamic-loader calls fail safely.
func TestAlpineStaticHost(t *testing.T) {
	if _, err := os.Stat("/etc/alpine-release"); err != nil {
		t.Fatalf("static musl test requires Alpine: %v", err)
	}
	host, err := elf.Open("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	for _, program := range host.Progs {
		if program.Type == elf.PT_INTERP {
			t.Fatal("static musl test binary has a PT_INTERP segment")
		}
	}
	requireMuslTool(t, "gcc")

	fixtures := t.TempDir()
	bofPath := filepath.Join(fixtures, "fixture.o")
	buildMusl(t, "gcc", "-c", "-O1", "-g0", "-fPIC", "-fno-stack-protector",
		"-o", bofPath, filepath.Join("..", "testdata", "bof", "fixture.c"))
	t.Run("BOF callbacks", func(t *testing.T) {
		object, err := bof.Load(readMuslFixture(t, bofPath))
		if err != nil {
			t.Fatal(err)
		}
		defer object.Close()
		var args bof.Arguments
		if err := args.AddInt32(0x12345678); err != nil {
			t.Fatal(err)
		}
		if err := args.AddInt16(0x1234); err != nil {
			t.Fatal(err)
		}
		if err := args.AddString("bof"); err != nil {
			t.Fatal(err)
		}
		outputs, err := object.Execute(args.Bytes())
		if err != nil || len(outputs) != 3 || string(outputs[0].Data) != "bof-e2e-ok" {
			t.Fatalf("static BOF outputs = %#v, error = %v", outputs, err)
		}
	})

	argsPath := filepath.Join(fixtures, "args.so")
	buildMusl(t, "gcc", "-nostdlib", "-nodefaultlibs", "-shared", "-fPIC", "-O2",
		"-fno-stack-protector", "-Wl,-z,now", "-o", argsPath,
		filepath.Join("..", "testdata", "c", "args.c"))
	image := readMuslFixture(t, argsPath)
	for _, loader := range []struct {
		name string
		load func([]byte) (muslLibrary, error)
	}{
		{name: "root", load: func(data []byte) (muslLibrary, error) { return reflektor.LoadLibrary(data) }},
		{name: "native", load: func(data []byte) (muslLibrary, error) { return native.LoadLibrary(data) }},
		{name: "recursive", load: func(data []byte) (muslLibrary, error) { return reflektor.LoadLibraryRecursive(data) }},
	} {
		t.Run(loader.name+" dependency-free C", func(t *testing.T) {
			library, err := loader.load(image)
			if err != nil {
				t.Fatal(err)
			}
			defer library.Close()
			assertMuslCArguments(t, library)
		})
	}

	libcPath := filepath.Join(fixtures, "basic.so")
	buildMusl(t, "gcc", "-shared", "-fPIC", "-O2", "-Wl,-z,now", "-o", libcPath,
		filepath.Join("..", "testdata", "c", "basic.c"))
	libcImage := readMuslFixture(t, libcPath)
	for _, loader := range []struct {
		name string
		load func([]byte) (muslLibrary, error)
	}{
		{name: "root", load: func(data []byte) (muslLibrary, error) { return reflektor.LoadLibrary(data) }},
		{name: "native", load: func(data []byte) (muslLibrary, error) { return native.LoadLibrary(data) }},
		{name: "recursive", load: func(data []byte) (muslLibrary, error) { return reflektor.LoadLibraryRecursive(data) }},
	} {
		t.Run(loader.name+" system dependency rejection", func(t *testing.T) {
			library, err := loader.load(libcImage)
			if err == nil {
				if library != nil {
					_ = library.Close()
				}
				t.Fatal("system dependency loaded in a static host")
			}
			if !strings.Contains(err.Error(), "statically linked Linux host") {
				t.Fatalf("static system dependency error = %v", err)
			}
		})
	}
}
