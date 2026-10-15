//go:build (linux && (386 || amd64 || arm64)) || (freebsd && (amd64 || arm64))

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

func TestPackedRELRSharedLibraries(t *testing.T) {
	requireCommand(t, "cc")
	outDir := t.TempDir()
	leafPath := filepath.Join(outDir, "libreflektor_relr_leaf.so")
	rootPath := filepath.Join(outDir, "reflektor_relr_root.so")
	buildPackedRELRFixture(t, []string{
		"-Wl,-soname,libreflektor_relr_leaf.so", "-o", leafPath,
		filepath.Join("..", "testdata", "c", "relr_leaf.c"),
	})
	buildPackedRELRFixture(t, []string{
		"-Wl,-soname,reflektor_relr_root.so", "-Wl,-rpath,$ORIGIN",
		"-o", rootPath, filepath.Join("..", "testdata", "c", "relr_root.c"),
		"-L" + outDir, "-Wl,--no-as-needed", "-lreflektor_relr_leaf",
	})
	assertPackedRELRMetadata(t, leafPath)
	assertPackedRELRMetadata(t, rootPath)

	leafBytes, err := os.ReadFile(leafPath)
	if err != nil {
		t.Fatalf("read packed RELR leaf: %v", err)
	}
	t.Run("root-single-image", func(t *testing.T) {
		library, err := reflektor.LoadLibrary(leafBytes)
		if err != nil {
			t.Fatalf("load packed RELR leaf: %v", err)
		}
		defer func() {
			if err := library.Close(); err != nil {
				t.Errorf("close packed RELR root library: %v", err)
			}
		}()
		result, err := library.CallExportWithArgs("ReflektorRELRValue")
		if err != nil || result != 41 {
			t.Fatalf("packed RELR root call = %#x, %v; want 41", result, err)
		}
	})
	t.Run("native-single-image", func(t *testing.T) {
		library, err := native.LoadLibrary(leafBytes)
		if err != nil {
			t.Fatalf("load packed RELR leaf in native package: %v", err)
		}
		defer func() {
			if err := library.Close(); err != nil {
				t.Errorf("close packed RELR native library: %v", err)
			}
		}()
		result, err := library.CallExportWithArgs("ReflektorRELRValue")
		if err != nil || result != 41 {
			t.Fatalf("packed RELR native call = %#x, %v; want 41", result, err)
		}
	})
	t.Run("root-recursive", func(t *testing.T) {
		library, err := reflektor.LoadLibraryFileRecursive(rootPath)
		if err != nil {
			t.Fatalf("load packed RELR graph: %v", err)
		}
		defer func() {
			if err := library.Close(); err != nil {
				t.Errorf("close packed RELR recursive graph: %v", err)
			}
		}()
		result, err := library.CallExportWithArgs("ReflektorRELRRootValue")
		if err != nil || result != 42 {
			t.Fatalf("packed RELR recursive call = %#x, %v; want 42", result, err)
		}
	})

	// The DT_RELRENT contract must be checked before the mapped words are used.
	badLeaf := append([]byte(nil), leafBytes...)
	file, err := elf.NewFile(bytes.NewReader(badLeaf))
	if err != nil {
		t.Fatalf("parse packed RELR leaf: %v", err)
	}
	defer file.Close()
	dynamic := file.SectionByType(elf.SHT_DYNAMIC)
	if dynamic == nil {
		t.Fatal("packed RELR leaf has no dynamic section")
	}
	mutated := false
	entrySize := uint64(16)
	if file.Class == elf.ELFCLASS32 {
		entrySize = 8
	}
	for offset := dynamic.Offset; offset+entrySize <= dynamic.Offset+dynamic.Size; offset += entrySize {
		var tag uint64
		if file.Class == elf.ELFCLASS32 {
			tag = uint64(binary.LittleEndian.Uint32(badLeaf[offset:]))
		} else {
			tag = binary.LittleEndian.Uint64(badLeaf[offset:])
		}
		if tag == 37 {
			if file.Class == elf.ELFCLASS32 {
				binary.LittleEndian.PutUint32(badLeaf[offset+4:], 2)
			} else {
				binary.LittleEndian.PutUint64(badLeaf[offset+8:], 4)
			}
			mutated = true
			break
		}
	}
	if !mutated {
		t.Fatal("packed RELR leaf has no DT_RELRENT")
	}
	for _, loader := range []struct {
		name string
		load func([]byte) error
	}{
		{name: "root", load: func(data []byte) error {
			library, err := reflektor.LoadLibrary(data)
			if err == nil {
				_ = library.Close()
			}
			return err
		}},
		{name: "native", load: func(data []byte) error {
			library, err := native.LoadLibrary(data)
			if err == nil {
				_ = library.Close()
			}
			return err
		}},
	} {
		t.Run("reject-malformed-"+loader.name, func(t *testing.T) {
			if err := loader.load(badLeaf); err == nil || !strings.Contains(err.Error(), "packed RELR entry size") {
				t.Fatalf("malformed DT_RELRENT error = %v", err)
			}
		})
	}
}

func buildPackedRELRFixture(t *testing.T, args []string) {
	t.Helper()
	common := []string{"-O2", "-g0", "-shared", "-fPIC", "-Wl,-z,now", "-Wl,-z,defs"}
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		// GNU ld currently ignores packed RELR for AArch64; use LLVM lld.
		common = append(common, "-fuse-ld=lld", "-Wl,--pack-dyn-relocs=relr")
	} else if runtime.GOOS == "freebsd" {
		common = append(common, "-Wl,--pack-dyn-relocs=relr")
	} else {
		common = append(common, "-Wl,-z,pack-relative-relocs")
	}
	cmd := exec.Command("cc", append(common, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build packed RELR fixture: %v\n%s", err, output)
	}
}

func assertPackedRELRMetadata(t *testing.T, path string) {
	t.Helper()
	file, err := elf.Open(path)
	if err != nil {
		t.Fatalf("open packed RELR fixture: %v", err)
	}
	defer file.Close()
	section := file.Section(".relr.dyn")
	if section == nil || section.Size == 0 || section.Type != elf.SectionType(19) {
		t.Fatalf("%s has no nonempty SHT_RELR section", path)
	}
	for _, tag := range []elf.DynTag{35, 36, 37} {
		values, err := file.DynValue(tag)
		if err != nil || len(values) != 1 {
			t.Fatalf("%s dynamic tag %d = %v, %v; want one value", path, tag, values, err)
		}
	}
}
