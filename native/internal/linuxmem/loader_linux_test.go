//go:build linux && !android && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)

package linuxmem

import (
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/sliverarmory/reflektor/native/internal/rejection"
	"golang.org/x/sys/unix"
)

func TestLoadLibraryDefensivelyRejectsGoImage(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	image, err := os.ReadFile(executable)
	if err != nil {
		t.Fatalf("read test executable: %v", err)
	}

	if _, err := LoadLibrary(image); !errors.Is(err, rejection.ErrGoSharedLibraryUnsupported) {
		t.Fatalf("LoadLibrary(Go image) error = %v, want ErrGoSharedLibraryUnsupported", err)
	}
}

func TestCollectELFFinalizersUsesABIOrder(t *testing.T) {
	mapping, err := unix.Mmap(-1, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatalf("mmap test image: %v", err)
	}
	defer unix.Munmap(mapping)
	base := uintptr(unsafe.Pointer(unsafe.SliceData(mapping)))
	class := elf.ELFCLASS64
	entrySize := 8
	if unsafe.Sizeof(uintptr(0)) == 4 {
		class = elf.ELFCLASS32
		entrySize = 4
	}

	const arrayOffset = 8
	arrayFunctions := []uintptr{base + 48, base + 64, base + 80}
	for index, function := range arrayFunctions {
		entry := base + arrayOffset + uintptr(index*entrySize)
		if entrySize == 8 {
			writeU64(entry, uint64(function))
		} else {
			writeU32(entry, uint32(function))
		}
	}

	mapped := mappedELF{mapping: mapping, loadBias: base}
	finalizers, err := collectELFFinalizers(mapped, class, dynamicInitInfo{
		fini:        96,
		finiArray:   arrayOffset,
		finiArraySz: uint64(len(arrayFunctions) * entrySize),
	})
	runtime.KeepAlive(mapping)
	if err != nil {
		t.Fatalf("collectELFFinalizers: %v", err)
	}
	want := []uintptr{base + 80, base + 64, base + 48, base + 96}
	if len(finalizers) != len(want) {
		t.Fatalf("finalizer count = %d, want %d", len(finalizers), len(want))
	}
	for index := range want {
		if finalizers[index] != want[index] {
			t.Fatalf("finalizers[%d] = %#x, want %#x", index, finalizers[index], want[index])
		}
	}
}

func TestModuleFreeRunsFinalizersOnce(t *testing.T) {
	var order []string
	first := purego.NewCallback(func() {
		order = append(order, "fini-1")
	})
	second := purego.NewCallback(func() {
		order = append(order, "fini-2")
	})
	module := &Module{
		finalizers:  []uintptr{first, second},
		dynamicAPI:  &linuxDynAPI{dlclose: 1},
		ownedDlopen: []uintptr{10, 20},
		closeDlopenHandle: func(_ *linuxDynAPI, handle uintptr) error {
			order = append(order, fmt.Sprintf("close-%d", handle))
			return nil
		},
	}

	module.Free()
	module.Free()
	want := []string{"fini-1", "fini-2", "close-20", "close-10"}
	if len(order) != len(want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("lifecycle order = %v, want %v", order, want)
		}
	}
}

func TestResolverOwnsNeededReferencesAndDeduplicatesAliases(t *testing.T) {
	var openCalls []string
	var closeCalls []uintptr
	nextHandle := uintptr(100)
	resolver := &symbolResolver{
		api:      &linuxDynAPI{dlopen: 1, dlclose: 1},
		modules:  []runtimeELFModule{{path: "/lib/libvisible.so"}},
		resolved: make(map[string]uintptr),
		misses:   make(map[string]error),
		opened:   make(map[string]uintptr),
		openLibrary: func(_ *linuxDynAPI, name string) (uintptr, error) {
			openCalls = append(openCalls, name)
			handle := nextHandle
			nextHandle++
			return handle, nil
		},
		closeLibrary: func(_ *linuxDynAPI, handle uintptr) error {
			closeCalls = append(closeCalls, handle)
			return nil
		},
	}

	if err := resolver.ensureLibraryLoaded("libvisible.so", false); err != nil {
		t.Fatalf("optional visible dependency: %v", err)
	}
	if len(openCalls) != 0 {
		t.Fatalf("optional visible dependency acquired a handle: %v", openCalls)
	}
	if err := resolver.ensureLibraryLoaded("libvisible.so", true); err != nil {
		t.Fatalf("owned visible dependency: %v", err)
	}
	if err := resolver.ensureLibraryLoaded("/alternate/libvisible.so", true); err != nil {
		t.Fatalf("owned alias: %v", err)
	}
	if err := resolver.ensureLibraryLoaded("libsecond.so", true); err != nil {
		t.Fatalf("second owned dependency: %v", err)
	}
	if len(openCalls) != 2 {
		t.Fatalf("dlopen calls = %v, want two distinct acquisitions", openCalls)
	}

	resolver.closeOwnedLibraries()
	wantClose := []uintptr{101, 100}
	if len(closeCalls) != len(wantClose) || closeCalls[0] != wantClose[0] || closeCalls[1] != wantClose[1] {
		t.Fatalf("dlclose calls = %v, want %v", closeCalls, wantClose)
	}
}

func TestResolverNeededFailureRetainsCleanupOwnership(t *testing.T) {
	var closeCalls []uintptr
	resolver := &symbolResolver{
		api:      &linuxDynAPI{dlopen: 1, dlclose: 1},
		resolved: make(map[string]uintptr),
		misses:   make(map[string]error),
		opened:   make(map[string]uintptr),
		openLibrary: func(_ *linuxDynAPI, name string) (uintptr, error) {
			if name == "libavailable.so" {
				return 77, nil
			}
			return 0, errors.New("not found")
		},
		closeLibrary: func(_ *linuxDynAPI, handle uintptr) error {
			closeCalls = append(closeCalls, handle)
			return nil
		},
	}

	err := resolver.primeNeededLibraries([]string{"libavailable.so", "libmissing.so"})
	if err == nil || !strings.Contains(err.Error(), `load DT_NEEDED "libmissing.so"`) {
		t.Fatalf("primeNeededLibraries error = %v, want missing DT_NEEDED", err)
	}
	resolver.closeOwnedLibraries()
	if len(closeCalls) != 1 || closeCalls[0] != 77 {
		t.Fatalf("failure cleanup dlclose calls = %v, want [77]", closeCalls)
	}
}

func TestNewLinuxArchitectureRelocations(t *testing.T) {
	t.Run("arm", func(t *testing.T) {
		var word uint32
		place := uintptr(unsafe.Pointer(&word))
		if err := applyARMReloc(uint32(elf.R_ARM_RELATIVE), place, 0x1000, 0, 0x234, 0, false); err != nil {
			t.Fatal(err)
		}
		if word != 0x1234 {
			t.Fatalf("R_ARM_RELATIVE = %#x, want %#x", word, uint32(0x1234))
		}
		if err := applyARMReloc(uint32(elf.R_ARM_GLOB_DAT), place, 0, 0x5678, 0x99, 0, false); err != nil {
			t.Fatal(err)
		}
		if word != 0x5678 {
			t.Fatalf("R_ARM_GLOB_DAT = %#x, want %#x", word, uint32(0x5678))
		}
		if err := applyARMReloc(uint32(elf.R_ARM_TLS_TPOFF32), place, 0, 0, 0, 0, false); err == nil {
			t.Fatal("R_ARM_TLS_TPOFF32 without a TLS slot succeeded")
		}
	})

	t.Run("riscv64", func(t *testing.T) {
		var word uint64
		place := uintptr(unsafe.Pointer(&word))
		if err := applyRISCV64Reloc(uint32(elf.R_RISCV_RELATIVE), place, 0x1000, 0, 0x234, 0, false); err != nil {
			t.Fatal(err)
		}
		if word != 0x1234 {
			t.Fatalf("R_RISCV_RELATIVE = %#x, want %#x", word, uint64(0x1234))
		}
		if err := applyRISCV64Reloc(uint32(elf.R_RISCV_64), place, 0, 0x5000, 0x678, 0, false); err != nil {
			t.Fatal(err)
		}
		if word != 0x5678 {
			t.Fatalf("R_RISCV_64 = %#x, want %#x", word, uint64(0x5678))
		}
		if err := applyRISCV64Reloc(uint32(elf.R_RISCV_TLS_TPREL64), place, 0, 0, 0, 0, false); err == nil {
			t.Fatal("R_RISCV_TLS_TPREL64 without a TLS slot succeeded")
		}
	})

	t.Run("ppc64le", func(t *testing.T) {
		var word uint64
		place := uintptr(unsafe.Pointer(&word))
		if err := applyPPC64LEReloc(uint32(elf.R_PPC64_RELATIVE), place, 0x1000, 0, 0x234, 0, false); err != nil {
			t.Fatal(err)
		}
		if word != 0x1234 {
			t.Fatalf("R_PPC64_RELATIVE = %#x, want %#x", word, uint64(0x1234))
		}
		if err := applyPPC64LEReloc(uint32(elf.R_PPC64_ADDR64), place, 0, 0x5000, 0x678, 0, false); err != nil {
			t.Fatal(err)
		}
		if word != 0x5678 {
			t.Fatalf("R_PPC64_ADDR64 = %#x, want %#x", word, uint64(0x5678))
		}
		if err := applyPPC64LEReloc(uint32(elf.R_PPC64_TPREL64), place, 0, 0, 0, 0, false); err == nil {
			t.Fatal("R_PPC64_TPREL64 without a TLS slot succeeded")
		}
	})
}

func TestNewLinuxArchitectureABIFlags(t *testing.T) {
	tests := []struct {
		name    string
		machine elf.Machine
		flags   uint32
		wantErr bool
	}{
		{name: "arm EABI5 hard-float", machine: elf.EM_ARM, flags: armELFEABI5 | armELFFloatHard},
		{name: "arm EABI5 soft-float", machine: elf.EM_ARM, flags: armELFEABI5 | armELFFloatSoft, wantErr: true},
		{name: "riscv64 LP64D", machine: elf.EM_RISCV, flags: riscvELFFloatABIDouble},
		{name: "riscv64 LP64D RVC", machine: elf.EM_RISCV, flags: riscvELFFloatABIDouble | riscvELFRVC},
		{name: "riscv64 soft-float", machine: elf.EM_RISCV, flags: riscvELFRVC, wantErr: true},
		{name: "riscv64 RVE", machine: elf.EM_RISCV, flags: riscvELFFloatABIDouble | riscvELFRVE, wantErr: true},
		{name: "riscv64 TSO", machine: elf.EM_RISCV, flags: riscvELFFloatABIDouble | riscvELFTSO, wantErr: true},
		{name: "riscv64 unknown", machine: elf.EM_RISCV, flags: riscvELFFloatABIDouble | 0x20, wantErr: true},
		{name: "ppc64le ELFv2", machine: elf.EM_PPC64, flags: ppc64ELFABI2},
		{name: "ppc64 ELFv1", machine: elf.EM_PPC64, flags: 1, wantErr: true},
		{name: "ppc64 unspecified ABI", machine: elf.EM_PPC64, flags: 0, wantErr: true},
		{name: "ppc64 unknown", machine: elf.EM_PPC64, flags: ppc64ELFABI2 | 0x4, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateELFArchitectureFlags(test.machine, test.flags)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateELFArchitectureFlags(%s, %#x) error = %v, wantErr=%v", test.machine, test.flags, err, test.wantErr)
			}
		})
	}
}

func TestApplySegmentProtectionsRejectsWritableExecutableSegment(t *testing.T) {
	mapping, err := unix.Mmap(-1, 0, unix.Getpagesize(), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Munmap(mapping)

	mapped := mappedELF{
		mapping:  mapping,
		loadBias: uintptr(unsafe.Pointer(unsafe.SliceData(mapping))),
		progs: []*elf.Prog{{ProgHeader: elf.ProgHeader{
			Type:  elf.PT_LOAD,
			Flags: elf.PF_R | elf.PF_W | elf.PF_X,
			Memsz: uint64(len(mapping)),
		}}},
	}
	if err := applySegmentProtections(mapped); err == nil || !strings.Contains(err.Error(), "writable and executable") {
		t.Fatalf("applySegmentProtections error = %v, want W+X rejection", err)
	}
}

func TestMatchSymbolOffsetSkipsGNUIFUNCResolvers(t *testing.T) {
	ifuncInfo := byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_GNU_IFUNC)
	funcInfo := byte(elf.STB_GLOBAL)<<4 | byte(elf.STT_FUNC)
	symbols := []elf.Symbol{
		{Name: "memcpy", Info: ifuncInfo, Value: 0x1000},
		{Name: "memcpy", Info: funcInfo, Value: 0x2000},
	}

	if got, ok := matchSymbolOffset(symbols, "memcpy"); !ok || got != 0x2000 {
		t.Fatalf("matchSymbolOffset(memcpy) = (%#x, %v), want (%#x, true)", got, ok, uintptr(0x2000))
	}
	if got, ok := matchSymbolOffset(symbols[:1], "memcpy"); ok || got != 0 {
		t.Fatalf("matchSymbolOffset(IFUNC-only memcpy) = (%#x, %v), want (0, false)", got, ok)
	}
}
