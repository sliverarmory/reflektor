//go:build bof

package reflektor_test

import (
	"bytes"
	"debug/elf"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"

	reflektor "github.com/sliverarmory/reflektor"
)

type bofTarget struct {
	goos      string
	goarch    string
	zigTarget string
	format    string
}

var bofTargets = []bofTarget{
	{goos: "darwin", goarch: "amd64", zigTarget: "x86_64-linux-none", format: "elf"},
	{goos: "darwin", goarch: "arm64", zigTarget: "aarch64-linux-none", format: "elf"},
	{goos: "linux", goarch: "386", zigTarget: "x86-linux-none", format: "elf"},
	{goos: "linux", goarch: "amd64", zigTarget: "x86_64-linux-none", format: "elf"},
	{goos: "linux", goarch: "arm64", zigTarget: "aarch64-linux-none", format: "elf"},
	{goos: "windows", goarch: "386", zigTarget: "x86-windows-gnu", format: "coff"},
	{goos: "windows", goarch: "amd64", zigTarget: "x86_64-windows-gnu", format: "coff"},
	{goos: "windows", goarch: "arm64", zigTarget: "aarch64-windows-gnu", format: "coff"},
}

func TestBuildBOFMatrix(t *testing.T) {
	requireCommand(t, "zig")
	requireCommand(t, "file")
	outputDirectory := t.TempDir()
	for _, target := range bofTargets {
		target := target
		t.Run(target.goos+"-"+target.goarch, func(t *testing.T) {
			path := buildBOFFixture(t, outputDirectory, target)
			validateBOFObject(t, path, target)
			if target.goos == "darwin" && target.goarch == "arm64" {
				validateDarwinARM64ReservedRegister(t, path)
			}
		})
	}
}

func validateDarwinARM64ReservedRegister(t *testing.T, path string) {
	t.Helper()
	requireCommand(t, "objdump")
	disassembly := runCmd(t, "objdump", "-d", path)
	reservedRegister := regexp.MustCompile(`(?i)(^|[^[:alnum:]_])(x18|w18)([^[:alnum:]_]|$)`)
	if location := reservedRegister.FindStringIndex(disassembly); location != nil {
		start := location[0] - 80
		if start < 0 {
			start = 0
		}
		end := location[1] + 80
		if end > len(disassembly) {
			end = len(disassembly)
		}
		t.Fatalf("Darwin/arm64 fixture uses Apple's reserved x18/w18 register:\n%s", disassembly[start:end])
	}
}

func TestLoadAndExecuteGeneratedBOF(t *testing.T) {
	if !reflektor.BOFEnabled {
		t.Fatal("BOFEnabled = false in a bof-tagged test")
	}
	requireCommand(t, "zig")
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	path := buildBOFFixture(t, t.TempDir(), target)
	if target.goos == "windows" && target.goarch != "386" {
		validateWindowsUnwindFixture(t, path, target.goarch)
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reflektor.LoadBOF(image)
	if err != nil {
		t.Fatalf("LoadBOF() error = %v", err)
	}
	defer loaded.Close()

	var arguments reflektor.BOFArguments
	if err := arguments.AddInt32(0x12345678); err != nil {
		t.Fatal(err)
	}
	if err := arguments.AddInt16(0x1234); err != nil {
		t.Fatal(err)
	}
	if err := arguments.AddString("bof"); err != nil {
		t.Fatal(err)
	}

	assertRun := func() {
		outputs, executeErr := loaded.Execute(arguments.Bytes())
		if executeErr != nil {
			t.Errorf("Execute() error = %v", executeErr)
			return
		}
		if len(outputs) != 3 ||
			outputs[0].Type != reflektor.BOFOutputDefault || string(outputs[0].Data) != "bof-e2e-ok" ||
			outputs[1].Type != reflektor.BOFOutputDefault || string(outputs[1].Data) != "bof-printf=7:callback-ok" ||
			outputs[2].Type != reflektor.BOFOutputDefault || string(outputs[2].Data) != "bof-pic-defined-global" {
			t.Errorf("Execute() outputs = %#v", outputs)
		}
	}
	assertRun()
	assertRun()

	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			assertRun()
		}()
	}
	wait.Wait()

	if err := loaded.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := loaded.Execute(arguments.Bytes()); err != reflektor.ErrBOFClosed {
		t.Fatalf("Execute() after Close error = %v, want ErrBOFClosed", err)
	}
}

func nativeBOFTarget() (bofTarget, bool) {
	for _, target := range bofTargets {
		if target.goos == runtime.GOOS && target.goarch == runtime.GOARCH {
			return target, true
		}
	}
	return bofTarget{}, false
}

func buildBOFFixture(t *testing.T, outputDirectory string, target bofTarget) string {
	t.Helper()
	outputPath := filepath.Join(outputDirectory, fmt.Sprintf("fixture_%s_%s.o", target.goos, target.goarch))
	arguments := []string{
		"cc", "-target", target.zigTarget, "-c", "-O1", "-g0",
		"-fno-stack-protector",
	}
	// The loader may map ELF objects above the low 4 GiB address range. Keep
	// Unix fixtures position-independent so their data references never rely on
	// an R_X86_64_32 absolute relocation that would depend on a lucky mmap.
	if target.format == "elf" {
		arguments = append(arguments, "-fPIC")
	}
	// Zig uses an ELF target as the Darwin interchange container, so reserve
	// Apple's platform register explicitly when generating arm64 machine code.
	if target.goos == "darwin" && target.goarch == "arm64" {
		arguments = append(arguments, "-mcpu=baseline+reserve_x18")
	}
	// Windows/amd64 and Windows/arm64 fixtures deliberately retain their
	// native unwind metadata so the execution test covers RtlAddFunctionTable
	// and the Close path covers RtlDeleteFunctionTable. The 386 and Unix
	// fixtures preserve the minimal no-unwind object shape.
	if target.goos != "windows" || target.goarch == "386" {
		arguments = append(arguments, "-fno-asynchronous-unwind-tables", "-fno-unwind-tables")
	}
	arguments = append(arguments,
		"-fno-exceptions", "-fno-ident", "-o", outputPath,
		filepath.Join("..", "testdata", "bof", "fixture.c"),
	)
	command := exec.Command("zig", arguments...)
	command.Env = append(os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build BOF fixture for %s/%s: %v\n%s", target.goos, target.goarch, err, output)
	}
	return outputPath
}

func validateBOFObject(t *testing.T, path string, target bofTarget) {
	t.Helper()
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	switch target.format {
	case "elf":
		file, parseErr := elf.NewFile(bytes.NewReader(image))
		if parseErr != nil {
			t.Fatalf("parse ELF BOF: %v", parseErr)
		}
		defer file.Close()
		if file.Type != elf.ET_REL {
			t.Fatalf("ELF type = %s, want ET_REL", file.Type)
		}
		wantMachine := map[string]elf.Machine{"386": elf.EM_386, "amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[target.goarch]
		if file.Machine != wantMachine {
			t.Fatalf("ELF machine = %s, want %s", file.Machine, wantMachine)
		}
	case "coff":
		file, parseErr := pe.NewFile(bytes.NewReader(image))
		if parseErr != nil {
			t.Fatalf("parse COFF BOF: %v", parseErr)
		}
		defer file.Close()
		wantMachine := map[string]uint16{"386": pe.IMAGE_FILE_MACHINE_I386, "amd64": pe.IMAGE_FILE_MACHINE_AMD64, "arm64": pe.IMAGE_FILE_MACHINE_ARM64}[target.goarch]
		if file.Machine != wantMachine {
			t.Fatalf("COFF machine = 0x%x, want 0x%x", file.Machine, wantMachine)
		}
		if file.SizeOfOptionalHeader != 0 {
			t.Fatalf("COFF optional header size = %d, want 0", file.SizeOfOptionalHeader)
		}
		if target.goarch != "386" {
			validateParsedWindowsUnwindFixture(t, file, target.goarch)
		}
		if target.goarch == "arm64" {
			validateWindowsARM64ImplicitADRP(t, file)
		}
	default:
		t.Fatalf("unknown BOF fixture format %q", target.format)
	}

	if strings.TrimSpace(runCmd(t, "file", path)) == "" {
		t.Fatal("file produced no object description")
	}
}

func validateWindowsARM64ImplicitADRP(t *testing.T, file *pe.File) {
	t.Helper()
	for _, section := range file.Sections {
		data, err := section.Data()
		if err != nil {
			t.Fatalf("read ARM64 COFF section %q: %v", section.Name, err)
		}
		for _, relocation := range section.Relocs {
			if relocation.Type != 0x0004 || uint64(relocation.SymbolTableIndex) >= uint64(len(file.COFFSymbols)) {
				continue
			}
			symbol := &file.COFFSymbols[relocation.SymbolTableIndex]
			name, err := symbol.FullName(file.StringTable)
			if err != nil {
				t.Fatalf("read ARM64 COFF relocation symbol: %v", err)
			}
			if name != ".rdata" || int(relocation.VirtualAddress)+4 > len(data) {
				continue
			}
			word := binary.LittleEndian.Uint32(data[relocation.VirtualAddress:])
			value := uint64(((word >> 29) & 0x3) | ((word >> 3) & 0x1ffffc))
			addend := int64(value<<43) >> 43
			if addend != 0 {
				return
			}
		}
	}
	t.Fatal("ARM64 COFF fixture has no PAGEBASE_REL21 section-symbol relocation with a non-zero byte addend")
}

func validateWindowsUnwindFixture(t *testing.T, path, goarch string) {
	t.Helper()
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := pe.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatalf("parse Windows unwind fixture: %v", err)
	}
	defer file.Close()
	validateParsedWindowsUnwindFixture(t, file, goarch)
}

func validateParsedWindowsUnwindFixture(t *testing.T, file *pe.File, goarch string) {
	t.Helper()
	entrySize := uint32(12)
	if goarch == "arm64" {
		entrySize = 8
	}
	for _, section := range file.Sections {
		if section.Name != ".pdata" {
			continue
		}
		if section.Size == 0 || section.Size%entrySize != 0 {
			t.Fatalf("Windows/%s .pdata size = %d, want a non-zero multiple of %d", goarch, section.Size, entrySize)
		}
		if len(section.Relocs) == 0 {
			t.Fatalf("Windows/%s .pdata has no relocations", goarch)
		}
		return
	}
	t.Fatalf("Windows/%s fixture has no .pdata section", goarch)
}
