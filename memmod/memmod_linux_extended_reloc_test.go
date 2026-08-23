//go:build linux && !android && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)

package memmod

import (
	"debug/elf"
	"strings"
	"testing"
	"unsafe"
)

func TestLinuxExtendedArchitectureDynamicRelocations(t *testing.T) {
	t.Run("arm", func(t *testing.T) {
		word := make([]byte, 4)
		place := uintptr(unsafe.Pointer(&word[0]))
		if err := applyARMReloc(uint32(elf.R_ARM_RELATIVE), place, 0x1000, 0, 0x28, 0, false); err != nil {
			t.Fatal(err)
		}
		if got, want := readU32(place), uint32(0x1028); got != want {
			t.Fatalf("R_ARM_RELATIVE = %#x, want %#x", got, want)
		}
		if err := applyARMReloc(uint32(elf.R_ARM_ABS32), place, 0, 0x2200, -8, 0, false); err != nil {
			t.Fatal(err)
		}
		if got, want := readU32(place), uint32(0x21f8); got != want {
			t.Fatalf("R_ARM_ABS32 = %#x, want %#x", got, want)
		}
		if err := applyARMReloc(uint32(elf.R_ARM_TLS_TPOFF32), place, 0, 0, 4, -0x200, true); err != nil {
			t.Fatal(err)
		}
		if got, want := readU32(place), ^uint32(0)-0x1fb; got != want {
			t.Fatalf("R_ARM_TLS_TPOFF32 = %#x, want %#x", got, want)
		}
	})

	t.Run("riscv64", func(t *testing.T) {
		word := make([]byte, 8)
		place := uintptr(unsafe.Pointer(&word[0]))
		if err := applyRISCV64Reloc(uint32(elf.R_RISCV_RELATIVE), place, 0x1000, 0, 0x28, 0, false); err != nil {
			t.Fatal(err)
		}
		if got, want := readU64(place), uint64(0x1028); got != want {
			t.Fatalf("R_RISCV_RELATIVE = %#x, want %#x", got, want)
		}
		if err := applyRISCV64Reloc(uint32(elf.R_RISCV_64), place, 0, 0x2200, -8, 0, false); err != nil {
			t.Fatal(err)
		}
		if got, want := readU64(place), uint64(0x21f8); got != want {
			t.Fatalf("R_RISCV_64 = %#x, want %#x", got, want)
		}
		if err := applyRISCV64Reloc(uint32(elf.R_RISCV_TLS_TPREL64), place, 0, 0, 8, -0x400, true); err != nil {
			t.Fatal(err)
		}
		if got, want := readU64(place), ^uint64(0)-0x3f7; got != want {
			t.Fatalf("R_RISCV_TLS_TPREL64 = %#x, want %#x", got, want)
		}
	})

	t.Run("ppc64le", func(t *testing.T) {
		word := make([]byte, 8)
		place := uintptr(unsafe.Pointer(&word[0]))
		if err := applyPPC64LEReloc(uint32(elf.R_PPC64_RELATIVE), place, 0x1000, 0, 0x28, 0, false); err != nil {
			t.Fatal(err)
		}
		if got, want := readU64(place), uint64(0x1028); got != want {
			t.Fatalf("R_PPC64_RELATIVE = %#x, want %#x", got, want)
		}
		if err := applyPPC64LEReloc(uint32(elf.R_PPC64_ADDR64), place, 0, 0x2200, -8, 0, false); err != nil {
			t.Fatal(err)
		}
		if got, want := readU64(place), uint64(0x21f8); got != want {
			t.Fatalf("R_PPC64_ADDR64 = %#x, want %#x", got, want)
		}
		if err := applyPPC64LEReloc(uint32(elf.R_PPC64_TPREL64), place, 0, 0, 8, -0x400, true); err != nil {
			t.Fatal(err)
		}
		if got, want := readU64(place), ^uint64(0)-0x3f7; got != want {
			t.Fatalf("R_PPC64_TPREL64 = %#x, want %#x", got, want)
		}
	})
}

func TestLinuxExtendedArchitectureTLSRelocationsRequireReservedSlot(t *testing.T) {
	word := make([]byte, 8)
	place := uintptr(unsafe.Pointer(&word[0]))
	tests := []struct {
		name string
		call func() error
	}{
		{name: "arm", call: func() error { return applyARMReloc(uint32(elf.R_ARM_TLS_TPOFF32), place, 0, 0, 0, 0, false) }},
		{name: "riscv64", call: func() error { return applyRISCV64Reloc(uint32(elf.R_RISCV_TLS_TPREL64), place, 0, 0, 0, 0, false) }},
		{name: "ppc64le", call: func() error { return applyPPC64LEReloc(uint32(elf.R_PPC64_TPREL64), place, 0, 0, 0, 0, false) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); err == nil || !strings.Contains(err.Error(), "no reserved host TLS slot") {
				t.Fatalf("error = %v, want reserved TLS slot error", err)
			}
		})
	}
}

func TestValidateLinuxELFABI(t *testing.T) {
	tests := []struct {
		name    string
		machine elf.Machine
		class   elf.Class
		flags   uint32
		wantErr string
	}{
		{name: "armv7 hard float", machine: elf.EM_ARM, class: elf.ELFCLASS32, flags: 0x05000400},
		{name: "arm wrong class", machine: elf.EM_ARM, class: elf.ELFCLASS64, flags: 0x05000400, wantErr: "expected ELFCLASS32"},
		{name: "arm soft float", machine: elf.EM_ARM, class: elf.ELFCLASS32, flags: 0x05000200, wantErr: "hard-float"},
		{name: "arm old EABI", machine: elf.EM_ARM, class: elf.ELFCLASS32, flags: 0x04000400, wantErr: "EABI5"},
		{name: "riscv LP64D", machine: elf.EM_RISCV, class: elf.ELFCLASS64, flags: 0x5},
		{name: "riscv LP64", machine: elf.EM_RISCV, class: elf.ELFCLASS64, flags: 0x1, wantErr: "LP64D"},
		{name: "riscv RVE", machine: elf.EM_RISCV, class: elf.ELFCLASS64, flags: 0xd, wantErr: "RV32E"},
		{name: "riscv TSO", machine: elf.EM_RISCV, class: elf.ELFCLASS64, flags: 0x15, wantErr: "RVTSO"},
		{name: "riscv unknown", machine: elf.EM_RISCV, class: elf.ELFCLASS64, flags: 0x25, wantErr: "unknown flags"},
		{name: "ppc64 ELFv2", machine: elf.EM_PPC64, class: elf.ELFCLASS64, flags: 2},
		{name: "ppc64 ELFv1", machine: elf.EM_PPC64, class: elf.ELFCLASS64, flags: 1, wantErr: "ELFv2"},
		{name: "ppc64 unknown", machine: elf.EM_PPC64, class: elf.ELFCLASS64, flags: 6, wantErr: "ELFv2"},
		{name: "amd64", machine: elf.EM_X86_64, class: elf.ELFCLASS64},
		{name: "amd64 wrong class", machine: elf.EM_X86_64, class: elf.ELFCLASS32, wantErr: "expected ELFCLASS64"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateLinuxELFABI(test.machine, test.class, test.flags)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("validateLinuxELFABI: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestFlushELFInstructionCacheRejectsOutOfRangeSegment(t *testing.T) {
	mapping := make([]byte, 64)
	base := uintptr(unsafe.Pointer(&mapping[0]))
	mapped := mappedELF{
		mapping:  mapping,
		loadBias: base,
		progs: []*elf.Prog{{ProgHeader: elf.ProgHeader{
			Type:  elf.PT_LOAD,
			Flags: elf.PF_R | elf.PF_X,
			Vaddr: 32,
			Memsz: 64,
		}}},
	}
	if err := flushELFInstructionCache(mapped); err == nil || !strings.Contains(err.Error(), "out of mapped image") {
		t.Fatalf("error = %v, want executable segment bounds error", err)
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
