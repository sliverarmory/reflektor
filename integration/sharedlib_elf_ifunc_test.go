//go:build linux && !android && (amd64 || arm64)

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

func TestELFCustomIFUNCRejected(t *testing.T) {
	requireCommand(t, "cc")
	outputPath := filepath.Join(t.TempDir(), "ifunc.so")
	cmd := exec.Command("cc", "-O0", "-g0", "-nostdlib", "-shared", "-fPIC",
		"-o", outputPath, filepath.Join("..", "testdata", "c", "ifunc.c"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build IFUNC fixture: %v\n%s", err, output)
	}
	image, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := elf.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer parsed.Close()
	if !hasDefinedIFUNCSymbol(parsed) {
		t.Fatal("fixture has no defined GNU IFUNC symbol")
	}
	assertCustomIFUNCRejected(t, image, "IFUNC")

	if runtime.GOARCH == "amd64" {
		withoutIFUNCSymbols := append([]byte(nil), image...)
		if !replaceIFUNCSymbolTypes(parsed, withoutIFUNCSymbols) {
			t.Fatal("fixture has no IRELATIVE relocation")
		}
		assertCustomIFUNCRejected(t, withoutIFUNCSymbols, "IRELATIVE")
	}
}

func assertCustomIFUNCRejected(t *testing.T, image []byte, want string) {
	t.Helper()
	loaders := []struct {
		name string
		load func() error
	}{
		{name: "root", load: func() error {
			library, err := reflektor.LoadLibrary(image)
			if library != nil {
				_ = library.Close()
			}
			return err
		}},
		{name: "native", load: func() error {
			library, err := native.LoadLibrary(image)
			if library != nil {
				_ = library.Close()
			}
			return err
		}},
		{name: "recursive", load: func() error {
			library, err := reflektor.LoadLibraryRecursive(image)
			if library != nil {
				_ = library.Close()
			}
			return err
		}},
	}
	for _, loader := range loaders {
		t.Run(want+"-"+loader.name, func(t *testing.T) {
			if err := loader.load(); err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("load error = %v, want explicit %s rejection", err, want)
			}
		})
	}
}

func hasDefinedIFUNCSymbol(f *elf.File) bool {
	for _, readSymbols := range []func() ([]elf.Symbol, error){f.DynamicSymbols, f.Symbols} {
		symbols, err := readSymbols()
		if err != nil {
			continue
		}
		for _, symbol := range symbols {
			if symbol.Section != elf.SHN_UNDEF && elf.ST_TYPE(symbol.Info) == elf.STT_GNU_IFUNC {
				return true
			}
		}
	}
	return false
}

func replaceIFUNCSymbolTypes(f *elf.File, image []byte) bool {
	replaced := false
	for _, name := range []string{".dynsym", ".symtab"} {
		section := f.Section(name)
		if section == nil || section.Entsize < 5 || section.Offset+section.Size > uint64(len(image)) {
			continue
		}
		for off := section.Offset; off+section.Entsize <= section.Offset+section.Size; off += section.Entsize {
			info := &image[off+4]
			if elf.SymType(*info&0x0f) == elf.STT_GNU_IFUNC {
				*info = *info&0xf0 | byte(elf.STT_FUNC)
				replaced = true
			}
		}
	}
	if !replaced {
		return false
	}
	for _, name := range []string{".rela.dyn", ".rela.plt", ".rela.plt.sec"} {
		section := f.Section(name)
		if section == nil {
			continue
		}
		data, err := section.Data()
		if err != nil {
			continue
		}
		for off := 0; off+24 <= len(data); off += 24 {
			if elf.R_X86_64(elf.R_TYPE64(binary.LittleEndian.Uint64(data[off+8:off+16]))) == elf.R_X86_64_IRELATIVE {
				return true
			}
		}
	}
	return false
}
