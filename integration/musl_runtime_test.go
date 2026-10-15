//go:build linux && musltest && (amd64 || arm64)

package reflektor_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/sliverarmory/reflektor"
	"github.com/sliverarmory/reflektor/bof"
	"github.com/sliverarmory/reflektor/native"
)

type muslLibrary interface {
	CallExport(string) error
	CallExportWithArgs(string, ...uintptr) (uintptr, error)
	Close() error
}

// TestAlpineMuslRuntime is run only by the Alpine Docker harness. All native
// fixtures are built by Alpine's GCC or Rust compiler, so the test cannot pass
// by loading the usual GNU-targeted fixtures through a compatibility layer.
func TestAlpineMuslRuntime(t *testing.T) {
	if _, err := os.Stat("/etc/alpine-release"); err != nil {
		t.Fatalf("musltest requires Alpine: %v", err)
	}
	for _, name := range []string{"gcc", "rustc"} {
		requireMuslTool(t, name)
	}

	fixtures := t.TempDir()
	bofPath := filepath.Join(fixtures, "fixture.o")
	buildMusl(t, "gcc", "-c", "-O1", "-g0", "-fPIC", "-fno-stack-protector",
		"-fno-asynchronous-unwind-tables", "-fno-unwind-tables", "-fno-ident",
		"-o", bofPath, filepath.Join("..", "testdata", "bof", "fixture.c"))
	argsPath := filepath.Join(fixtures, "libreflektor_args.so")
	buildMusl(t, "gcc", "-shared", "-fPIC", "-O2", "-Wl,-z,now", "-o", argsPath,
		filepath.Join("..", "testdata", "c", "args.c"))
	rustPath := filepath.Join(fixtures, "libreflektor_rust_args.so")
	buildMusl(t, "rustc", "--crate-name", "reflektor_native_args", "--crate-type", "cdylib",
		"--edition", "2021", "-C", "panic=abort", "-C", "opt-level=2",
		"-C", "link-arg=-Wl,-z,now", "-o", rustPath,
		filepath.Join("..", "testdata", "rust", "native_args.rs"))

	t.Run("BOF callback and relocations", func(t *testing.T) {
		image := readMuslFixture(t, bofPath)
		object, err := bof.Load(image)
		if err != nil {
			t.Fatalf("bof.Load: %v", err)
		}
		defer object.Close()

		var arguments bof.Arguments
		if err := arguments.AddInt32(0x12345678); err != nil {
			t.Fatal(err)
		}
		if err := arguments.AddInt16(0x1234); err != nil {
			t.Fatal(err)
		}
		if err := arguments.AddString("bof"); err != nil {
			t.Fatal(err)
		}
		outputs, err := object.Execute(arguments.Bytes())
		if err != nil {
			t.Fatalf("BOF Execute: %v", err)
		}
		want := []string{"bof-e2e-ok", "bof-printf=7:callback-ok", "bof-pic-defined-global"}
		if len(outputs) != len(want) {
			t.Fatalf("BOF output count = %d, want %d: %#v", len(outputs), len(want), outputs)
		}
		for index, expected := range want {
			if outputs[index].Type != bof.OutputDefault || string(outputs[index].Data) != expected {
				t.Fatalf("BOF output %d = %#v, want %q", index, outputs[index], expected)
			}
		}
	})

	for _, loader := range []struct {
		name string
		load func([]byte) (muslLibrary, error)
	}{
		{name: "root", load: func(data []byte) (muslLibrary, error) { return reflektor.LoadLibrary(data) }},
		{name: "native", load: func(data []byte) (muslLibrary, error) { return native.LoadLibrary(data) }},
	} {
		t.Run(loader.name+" C arguments and C-to-Go callback", func(t *testing.T) {
			library, err := loader.load(readMuslFixture(t, argsPath))
			if err != nil {
				t.Fatalf("load native-musl C image: %v", err)
			}
			defer library.Close()
			assertMuslCArguments(t, library)
		})
		t.Run(loader.name+" Rust export", func(t *testing.T) {
			library, err := loader.load(readMuslFixture(t, rustPath))
			if err != nil {
				t.Fatalf("load native-musl Rust image: %v", err)
			}
			defer library.Close()
			result, err := library.CallExportWithArgs("ReflektorRustArgs3", 2, 3, 5)
			if err != nil || result != 47 {
				t.Fatalf("Rust export result = %d, error = %v; want 47", result, err)
			}
		})
	}

	t.Run("root recursive C dependencies", func(t *testing.T) {
		graphDir := filepath.Join(fixtures, "graph")
		if err := os.Mkdir(graphDir, 0o700); err != nil {
			t.Fatal(err)
		}
		leaf := filepath.Join(graphDir, "libreflektor_leaf.so")
		middle := filepath.Join(graphDir, "libreflektor_middle.so")
		root := filepath.Join(graphDir, "reflektor_recursive_root.so")
		buildMusl(t, "gcc", "-shared", "-fPIC", "-Wl,-z,now", "-Wl,-soname,libreflektor_leaf.so",
			"-o", leaf, filepath.Join("..", "testdata", "c", "recursive_leaf.c"))
		buildMusl(t, "gcc", "-shared", "-fPIC", "-Wl,-z,now", "-Wl,-soname,libreflektor_middle.so",
			"-Wl,-rpath,$ORIGIN", "-o", middle,
			filepath.Join("..", "testdata", "c", "recursive_middle.c"), "-L"+graphDir,
			"-Wl,--no-as-needed", "-lreflektor_leaf")
		buildMusl(t, "gcc", "-shared", "-fPIC", "-Wl,-z,now", "-Wl,-soname,reflektor_recursive_root.so",
			"-Wl,-rpath,$ORIGIN", "-o", root,
			filepath.Join("..", "testdata", "c", "recursive_root.c"), "-L"+graphDir,
			"-Wl,--no-as-needed", "-lreflektor_middle")

		marker := filepath.Join(fixtures, "recursive-marker")
		t.Setenv("REFLEKTOR_MARKER", marker)
		library, err := reflektor.LoadLibraryFileRecursive(root)
		if err != nil {
			t.Fatalf("load recursive native-musl graph: %v", err)
		}
		defer library.Close()
		if err := os.Rename(graphDir, graphDir+"-moved"); err != nil {
			t.Fatalf("rename recursive fixture graph after loading: %v", err)
		}
		if err := library.CallExport("StartW"); err != nil {
			t.Fatalf("call recursive StartW after source rename: %v", err)
		}
		if got := readMuslFixture(t, marker); string(got) != "ok" {
			t.Fatalf("recursive marker = %q, want ok", got)
		}
		maps := readMuslFixture(t, "/proc/self/maps")
		for _, name := range []string{"libreflektor_leaf.so", "libreflektor_middle.so"} {
			if bytes.Contains(maps, []byte(name)) {
				t.Fatalf("custom recursive dependency %s was mapped by the OS loader", name)
			}
		}
	})
}

func assertMuslCArguments(t *testing.T, library muslLibrary) {
	t.Helper()
	if err := library.CallExport("ReflektorArgsInit"); err != nil {
		t.Fatal(err)
	}
	callbackAddress, err := library.CallExportWithArgs("ReflektorArgsCallbackAddress")
	if err != nil || callbackAddress == 0 {
		t.Fatalf("C callback address = %#x, error = %v", callbackAddress, err)
	}
	input := []byte{3, 1, 4, 1, 5}
	result, err := library.CallExportWithArgs("ReflektorArgsRun",
		uintptr(unsafe.Pointer(unsafe.SliceData(input))), uintptr(len(input)), callbackAddress)
	runtime.KeepAlive(input)
	if err != nil || result != (14<<16)|41 {
		t.Fatalf("C argument result = %#x, error = %v; want %#x", result, err, uintptr((14<<16)|41))
	}

	called := false
	goCallback := purego.NewCallback(func(pointer uintptr, length int32) int32 {
		called = true
		if pointer == 0 || length != int32(len(input)) || !bytes.Equal(unsafe.Slice((*byte)(unsafe.Pointer(pointer)), len(input)), input) {
			return -1
		}
		return 42
	})
	result, err = library.CallExportWithArgs("ReflektorArgsEcho",
		uintptr(unsafe.Pointer(unsafe.SliceData(input))), uintptr(len(input)), goCallback)
	runtime.KeepAlive(input)
	if err != nil || result != 42 || !called {
		t.Fatalf("C-to-Go callback result = %d, called = %t, error = %v", result, called, err)
	}
}

func requireMuslTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("Alpine fixture compiler %q is required: %v", name, err)
	}
}

func buildMusl(t *testing.T, program string, args ...string) {
	t.Helper()
	command := exec.Command(program, args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build Alpine fixture with %s %s: %v\n%s", program, strings.Join(args, " "), err, output)
	}
}

func readMuslFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read Alpine fixture %s: %v", path, err)
	}
	return data
}
