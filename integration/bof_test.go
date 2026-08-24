package reflektor_test

import (
	"bytes"
	"debug/elf"
	"debug/macho"
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

	"github.com/sliverarmory/reflektor/bof"
)

type bofTarget struct {
	goos      string
	goarch    string
	zigTarget string
	format    string
}

var bofTargets = []bofTarget{
	{goos: "darwin", goarch: "amd64", zigTarget: "x86_64-macos-none", format: "macho"},
	{goos: "darwin", goarch: "arm64", zigTarget: "aarch64-macos-none", format: "macho"},
	{goos: "freebsd", goarch: "amd64", zigTarget: "x86_64-freebsd-none", format: "elf"},
	{goos: "freebsd", goarch: "arm64", zigTarget: "aarch64-freebsd-none", format: "elf"},
	{goos: "linux", goarch: "386", zigTarget: "x86-linux-none", format: "elf"},
	{goos: "linux", goarch: "amd64", zigTarget: "x86_64-linux-none", format: "elf"},
	{goos: "linux", goarch: "arm", zigTarget: "arm-linux-none", format: "elf"},
	{goos: "linux", goarch: "arm64", zigTarget: "aarch64-linux-none", format: "elf"},
	{goos: "linux", goarch: "ppc64le", zigTarget: "powerpc64le-linux-none", format: "elf"},
	{goos: "linux", goarch: "riscv64", zigTarget: "riscv64-linux-none", format: "elf"},
	{goos: "windows", goarch: "386", zigTarget: "x86-windows-gnu", format: "coff"},
	{goos: "windows", goarch: "amd64", zigTarget: "x86_64-windows-gnu", format: "coff"},
	{goos: "windows", goarch: "arm64", zigTarget: "aarch64-windows-gnu", format: "coff"},
}

func TestBuildBOFMatrix(t *testing.T) {
	requireCommand(t, "zig")
	requireCommand(t, "file")
	outputDirectory := t.TempDir()
	if configured := os.Getenv("REFLEKTOR_BOF_FIXTURE_DIR"); configured != "" {
		outputDirectory = configured
		if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
			t.Fatalf("create BOF fixture seed directory: %v", err)
		}
	}
	for _, target := range bofTargets {
		target := target
		t.Run(target.goos+"-"+target.goarch, func(t *testing.T) {
			path := buildBOFFixture(t, outputDirectory, target)
			validateBOFObject(t, path, target)
			// The emulated Linux and FreeBSD runtimes deliberately have no native
			// Zig dependency, so seed and inspect their options fixture alongside
			// the primary fixture. Other targets continue to build it in their
			// native LoadWithOptions execution test.
			if target.goos == "freebsd" ||
				(target.goos == "linux" && (target.goarch == "arm" || target.goarch == "ppc64le" || target.goarch == "riscv64")) {
				optionsPath := buildBOFSource(t, outputDirectory, target, "options_fixture", "options_fixture.c")
				validateBOFObject(t, optionsPath, target)
			}
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
	target, ok := nativeBOFTarget()
	if !ok {
		t.Fatalf("missing BOF fixture target for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	testLoadAndExecuteGeneratedBOF(t, target)
}

func testLoadAndExecuteGeneratedBOF(t *testing.T, target bofTarget) {
	t.Helper()
	path := buildBOFFixture(t, t.TempDir(), target)
	if target.goos == "windows" && target.goarch != "386" {
		validateWindowsUnwindFixture(t, path, target.goarch)
	}
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := bof.Load(image)
	if err != nil {
		t.Fatalf("bof.Load() error = %v", err)
	}
	defer loaded.Close()

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

	assertRun := func() {
		outputs, executeErr := loaded.Execute(arguments.Bytes())
		if executeErr != nil {
			t.Errorf("Execute() error = %v", executeErr)
			return
		}
		want := []string{"bof-e2e-ok", "bof-printf=7:callback-ok"}
		if target.goos == "linux" && target.goarch == "arm" {
			want = append(want, "bof-arm-align=7:1122334455667788")
		}
		want = append(want, "bof-pic-defined-global")
		if len(outputs) != len(want) {
			t.Errorf("Execute() outputs = %#v", outputs)
			return
		}
		for index, output := range outputs {
			if output.Type != bof.OutputDefault || string(output.Data) != want[index] {
				t.Errorf("Execute() outputs = %#v", outputs)
				return
			}
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
	if _, err := loaded.Execute(arguments.Bytes()); err != bof.ErrClosed {
		t.Fatalf("Execute() after Close error = %v, want bof.ErrClosed", err)
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
	extraArguments := []string(nil)
	if target.format == "macho" && target.goarch == "arm64" {
		// Native Darwin/arm64 uses Apple's distinct variadic ABI. The Mach-O
		// fixture covers the same output bytes through BeaconOutput while the
		// separate legacy ELF fixture retains BeaconPrintf coverage.
		extraArguments = append(extraArguments, "-DBOF_DARWIN_ARM64_MACHO")
	}
	return buildBOFSource(t, outputDirectory, target, "fixture", "fixture.c", extraArguments...)
}

func buildBOFSource(t *testing.T, outputDirectory string, target bofTarget, name, source string, extraArguments ...string) string {
	t.Helper()
	outputPath := filepath.Join(outputDirectory, fmt.Sprintf("%s_%s_%s.o", name, target.goos, target.goarch))
	if seededPath := seededBOFSource(t, outputDirectory, target, name); seededPath != "" {
		return seededPath
	}
	requireCommand(t, "zig")
	arguments := []string{
		"cc", "-target", target.zigTarget, "-c", "-O1", "-g0",
		"-fno-stack-protector",
	}
	// The loader may map ELF objects above the low 4 GiB address range. Keep ELF
	// fixtures position-independent so their data references never rely on
	// an R_X86_64_32 absolute relocation that would depend on a lucky mmap.
	if target.format == "elf" {
		arguments = append(arguments, "-fPIC")
	}
	// The native macOS target already reserves Apple's x18 platform register.
	// The backwards-compatible Linux-targeted ELF container must opt in.
	if target.goos == "darwin" && target.goarch == "arm64" && target.format == "elf" {
		arguments = append(arguments, "-mcpu=baseline+reserve_x18")
	}
	// Windows/amd64 and Windows/arm64 fixtures deliberately retain their
	// native unwind metadata so the execution test covers RtlAddFunctionTable
	// and the Close path covers RtlDeleteFunctionTable. The 386 and Unix
	// fixtures preserve the minimal no-unwind object shape.
	if target.goos != "windows" || target.goarch == "386" {
		arguments = append(arguments, "-fno-asynchronous-unwind-tables", "-fno-unwind-tables")
	}
	arguments = append(arguments, extraArguments...)
	arguments = append(arguments,
		"-fno-exceptions", "-fno-ident", "-o", outputPath,
		filepath.Join("..", "testdata", "bof", source),
	)
	command := exec.Command("zig", arguments...)
	command.Env = append(os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build BOF fixture %q for %s/%s: %v\n%s", name, target.goos, target.goarch, err, output)
	}
	return outputPath
}

func seededBOFSource(t *testing.T, outputDirectory string, target bofTarget, name string) string {
	t.Helper()
	seedDirectory := os.Getenv("REFLEKTOR_BOF_FIXTURE_DIR")
	if seedDirectory == "" {
		return ""
	}
	seedDirectoryPath, err := filepath.Abs(seedDirectory)
	if err != nil {
		t.Fatalf("resolve BOF fixture seed directory: %v", err)
	}
	outputDirectoryPath, err := filepath.Abs(outputDirectory)
	if err != nil {
		t.Fatalf("resolve BOF fixture output directory: %v", err)
	}
	// TestBuildBOFMatrix points its output at the configured seed directory to
	// produce fresh, inspected objects. Runtime tests use a different temporary
	// output directory and consume those objects without requiring a native Zig
	// binary on an emulated runner.
	if filepath.Clean(seedDirectoryPath) == filepath.Clean(outputDirectoryPath) {
		return ""
	}
	seedPath := filepath.Join(seedDirectoryPath, fmt.Sprintf("%s_%s_%s.o", name, target.goos, target.goarch))
	info, err := os.Stat(seedPath)
	if err != nil {
		t.Fatalf("stat prebuilt BOF fixture %s: %v", seedPath, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("prebuilt BOF fixture %s must be a non-empty regular file", seedPath)
	}
	return seedPath
}

func validateBOFObject(t *testing.T, path string, target bofTarget) {
	t.Helper()
	image, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	switch target.format {
	case "macho":
		file, parseErr := macho.NewFile(bytes.NewReader(image))
		if parseErr != nil {
			t.Fatalf("parse Mach-O BOF: %v", parseErr)
		}
		defer file.Close()
		if file.Magic != macho.Magic64 {
			t.Fatalf("Mach-O magic = 0x%x, want MH_MAGIC_64 (0x%x)", file.Magic, macho.Magic64)
		}
		if file.Type != macho.TypeObj {
			t.Fatalf("Mach-O type = %s, want MH_OBJECT", file.Type)
		}
		wantCPU := map[string]macho.Cpu{"amd64": macho.CpuAmd64, "arm64": macho.CpuArm64}[target.goarch]
		if file.Cpu != wantCPU {
			t.Fatalf("Mach-O CPU = %s, want %s", file.Cpu, wantCPU)
		}
	case "elf":
		file, parseErr := elf.NewFile(bytes.NewReader(image))
		if parseErr != nil {
			t.Fatalf("parse ELF BOF: %v", parseErr)
		}
		defer file.Close()
		if file.Type != elf.ET_REL {
			t.Fatalf("ELF type = %s, want ET_REL", file.Type)
		}
		if file.Data != elf.ELFDATA2LSB {
			t.Fatalf("ELF data encoding = %s, want ELFDATA2LSB", file.Data)
		}
		wantClass := map[string]elf.Class{"386": elf.ELFCLASS32, "amd64": elf.ELFCLASS64, "arm": elf.ELFCLASS32, "arm64": elf.ELFCLASS64, "ppc64le": elf.ELFCLASS64, "riscv64": elf.ELFCLASS64}[target.goarch]
		if file.Class != wantClass {
			t.Fatalf("ELF class = %s, want %s", file.Class, wantClass)
		}
		wantMachine := map[string]elf.Machine{"386": elf.EM_386, "amd64": elf.EM_X86_64, "arm": elf.EM_ARM, "arm64": elf.EM_AARCH64, "ppc64le": elf.EM_PPC64, "riscv64": elf.EM_RISCV}[target.goarch]
		if file.Machine != wantMachine {
			t.Fatalf("ELF machine = %s, want %s", file.Machine, wantMachine)
		}
		if target.goarch == "arm" {
			flags := binary.LittleEndian.Uint32(image[36:40])
			if flags&0xff000000 != 0x05000000 || flags&0x00000600 != 0x00000400 {
				t.Fatalf("ELF/arm flags = %#08x, want EABI5 hard-float", flags)
			}
		}
		if target.goarch == "ppc64le" {
			const efPPC64ABIV2 = 2
			flags := binary.LittleEndian.Uint32(image[48:52])
			if flags != efPPC64ABIV2 {
				t.Fatalf("ELF/ppc64le flags = %#08x, want ELFv2 ABI (%#x)", flags, efPPC64ABIV2)
			}
		}
		if target.goarch == "riscv64" {
			const (
				efRISCVRVC            = 0x1
				efRISCVFloatABIMask   = 0x6
				efRISCVFloatABIDouble = 0x4
				efRISCVRVE            = 0x8
				efRISCVTSO            = 0x10
				efRISCVKnownFlags     = efRISCVRVC | efRISCVFloatABIMask | efRISCVRVE | efRISCVTSO
			)
			flags := binary.LittleEndian.Uint32(image[48:52])
			if flags&efRISCVFloatABIMask != efRISCVFloatABIDouble ||
				flags&(efRISCVRVE|efRISCVTSO) != 0 || flags&^efRISCVKnownFlags != 0 {
				t.Fatalf("ELF/riscv64 flags = %#08x, want LP64D with optional RVC and no RVE, RVTSO, or unknown flags", flags)
			}
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
