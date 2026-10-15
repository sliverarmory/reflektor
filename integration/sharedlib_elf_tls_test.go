//go:build (freebsd && (amd64 || arm64)) || (linux && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64))

package reflektor_test

import (
	"bytes"
	"debug/elf"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sliverarmory/reflektor"
	"github.com/sliverarmory/reflektor/native"
)

func TestELFGeneralTLSRequiresRegistration(t *testing.T) {
	requireCommand(t, "zig")
	path := buildCSharedLib(t, t.TempDir(), runtime.GOOS, runtime.GOARCH,
		"tls", filepath.Join("..", "testdata", "c", "tls.c"))
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	f, err := elf.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var hasTLS bool
	for _, program := range f.Progs {
		if program.Type == elf.PT_TLS && program.Memsz != 0 {
			hasTLS = true
		}
	}
	if !hasTLS {
		t.Fatal("fixture has no nonempty PT_TLS segment")
	}

	loaders := []struct {
		name string
		load func() error
	}{
		{name: "root", load: func() error {
			lib, err := reflektor.LoadLibrary(image)
			if lib != nil {
				_ = lib.Close()
			}
			return err
		}},
		{name: "native", load: func() error {
			lib, err := native.LoadLibrary(image)
			if lib != nil {
				_ = lib.Close()
			}
			return err
		}},
		{name: "recursive", load: func() error {
			lib, err := reflektor.LoadLibraryRecursive(image)
			if lib != nil {
				_ = lib.Close()
			}
			return err
		}},
	}
	for _, loader := range loaders {
		t.Run(loader.name, func(t *testing.T) {
			if err := loader.load(); err == nil || !strings.Contains(err.Error(), "PT_TLS") {
				t.Fatalf("load error = %v, want explicit PT_TLS rejection", err)
			}
		})
	}
}
