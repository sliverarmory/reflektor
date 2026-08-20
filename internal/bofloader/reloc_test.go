//go:build bof

package bofloader

import (
	"debug/elf"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestCOFFAMD64ExternalLinkage(t *testing.T) {
	tests := []struct {
		name       string
		symbolName string
		wantTarget uint64
	}{
		{name: "direct call uses thunk", symbolName: "memcpy", wantTarget: 0x1800},
		{name: "dllimport call uses GOT slot", symbolName: "__imp_KERNEL32$GetLastError", wantTarget: 0x1c00},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location := make([]byte, 4)
			linked := linkedSymbol{
				symbol: objectSymbol{name: test.symbolName, section: sectionUndefined},
				external: &externalSymbol{
					target: 0x9000,
					thunk:  uintptr(0x1800),
					got:    uintptr(0x1c00),
				},
			}
			err := applyCOFFAMD64Relocation(
				&objectFile{imageBase: 0x1000},
				objectRelocation{typeID: coffAMD64Rel32},
				location,
				0x2000,
				linked,
			)
			if err != nil {
				t.Fatal(err)
			}
			want := int32(int64(test.wantTarget) - 0x2000 - 4)
			if got := int32(binary.LittleEndian.Uint32(location)); got != want {
				t.Fatalf("relocation = %d, want %d", got, want)
			}
		})
	}
}

func TestELFI386PICRelocations(t *testing.T) {
	externals := map[uint32]externalSymbol{
		1: {name: "_GLOBAL_OFFSET_TABLE_", target: 0x3000, got: 0x3000},
		2: {name: "BeaconOutput", target: 0x9000, thunk: 0x1800, got: 0x3004},
	}

	t.Run("GOTPC", func(t *testing.T) {
		location := make([]byte, 4)
		binary.LittleEndian.PutUint32(location, 3)
		linked := linkedSymbol{
			symbol:   objectSymbol{index: 1, name: "_GLOBAL_OFFSET_TABLE_", section: sectionUndefined},
			address:  0x3000,
			external: externalPointer(externals[1]),
		}
		if err := applyELFI386Relocation(objectRelocation{typeID: uint32(elf.R_386_GOTPC)}, location, 0x100f, linked, externals); err != nil {
			t.Fatal(err)
		}
		if got, want := int32(binary.LittleEndian.Uint32(location)), int32(0x3000+3-0x100f); got != want {
			t.Fatalf("GOTPC = %d, want %d", got, want)
		}
	})

	t.Run("GOTOFF", func(t *testing.T) {
		location := make([]byte, 4)
		linked := linkedSymbol{symbol: objectSymbol{name: "local", section: 0}, address: 0x3400}
		if err := applyELFI386Relocation(objectRelocation{typeID: uint32(elf.R_386_GOTOFF)}, location, 0x1000, linked, externals); err != nil {
			t.Fatal(err)
		}
		if got, want := int32(binary.LittleEndian.Uint32(location)), int32(0x400); got != want {
			t.Fatalf("GOTOFF = %d, want %d", got, want)
		}
	})

	t.Run("PLT32", func(t *testing.T) {
		location := make([]byte, 4)
		binary.LittleEndian.PutUint32(location, math.MaxUint32-3) // ELF call addend -4.
		linked := linkedSymbol{
			symbol:   objectSymbol{index: 2, name: "BeaconOutput", section: sectionUndefined},
			address:  0x9000,
			external: externalPointer(externals[2]),
		}
		if err := applyELFI386Relocation(objectRelocation{typeID: uint32(elf.R_386_PLT32)}, location, 0x2000, linked, externals); err != nil {
			t.Fatal(err)
		}
		if got, want := int32(binary.LittleEndian.Uint32(location)), int32(0x1800-4-0x2000); got != want {
			t.Fatalf("PLT32 = %d, want %d", got, want)
		}
	})
}

func TestELFAMD64DistinguishesDataAndCallRelocations(t *testing.T) {
	linked := linkedSymbol{
		symbol:  objectSymbol{name: "external", section: sectionUndefined},
		address: 0x2800,
		external: &externalSymbol{
			target: 0x2800,
			thunk:  0x1800,
		},
	}
	for _, test := range []struct {
		name   string
		typeID elf.R_X86_64
		want   int32
	}{
		{name: "PC32 data uses symbol", typeID: elf.R_X86_64_PC32, want: 0x2800 - 4 - 0x2000},
		{name: "PLT32 call uses thunk", typeID: elf.R_X86_64_PLT32, want: 0x1800 - 4 - 0x2000},
	} {
		t.Run(test.name, func(t *testing.T) {
			location := make([]byte, 4)
			binary.LittleEndian.PutUint32(location, math.MaxUint32-3)
			if err := applyELFAMD64Relocation(objectRelocation{typeID: uint32(test.typeID)}, location, 0x2000, linked, nil); err != nil {
				t.Fatal(err)
			}
			if got := int32(binary.LittleEndian.Uint32(location)); got != test.want {
				t.Fatalf("relocation = %d, want %d", got, test.want)
			}
		})
	}
}

func TestDefinedELFSymbolGetsGOTLinkageWithoutBecomingExternal(t *testing.T) {
	object := &objectFile{
		format: "elf",
		arch:   "amd64",
		sections: []objectSection{
			{name: ".text", size: 4, mapped: true, address: 0x2000},
			{name: ".data", size: 4, mapped: true, address: 0x3000},
		},
		symbols: map[uint32]objectSymbol{
			1: {index: 1, name: "defined_global", section: 1},
		},
		relocations: []objectRelocation{{
			section: 0,
			offset:  0,
			typeID:  uint32(elf.R_X86_64_REX_GOTPCRELX),
			symbol:  1,
		}},
	}
	referenced := referencedLinkageSymbols(object)
	if len(referenced) != 1 || referenced[0] != 1 {
		t.Fatalf("referenced linkage symbols = %v, want [1]", referenced)
	}

	linked, err := linkRelocationSymbol(object, object.relocations[0], map[uint32]externalSymbol{
		1: {target: 0x3000, got: 0x4000, thunk: 0x5000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if linked.external != nil {
		t.Fatal("defined symbol was classified as an external import")
	}
	if got, err := gotLinkedAddress(linked); err != nil || got != 0x4000 {
		t.Fatalf("defined-symbol GOT address = %#x, %v; want %#x", got, err, uint64(0x4000))
	}
	if got := thunkLinkedAddress(linked); got != 0x3000 {
		t.Fatalf("defined-symbol branch target = %#x, want direct address %#x", got, uint64(0x3000))
	}
}

func TestARM64RelocationEncoders(t *testing.T) {
	t.Run("branch26", func(t *testing.T) {
		location := make([]byte, 4)
		binary.LittleEndian.PutUint32(location, 0x94000000) // BL
		if err := applyARM64Branch26(location, 0x1100, 0x1000, true, 0); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(location), uint32(0x94000040); got != want {
			t.Fatalf("BL encoding = %#08x, want %#08x", got, want)
		}
	})

	t.Run("branch overflow", func(t *testing.T) {
		location := make([]byte, 4)
		binary.LittleEndian.PutUint32(location, 0x14000000) // B
		err := applyARM64Branch26(location, 0x10000000, 0, true, 0)
		if err == nil || !strings.Contains(err.Error(), "exceeds signed 28-bit range") {
			t.Fatalf("error = %v, want range error", err)
		}
	})

	t.Run("ADRP", func(t *testing.T) {
		location := make([]byte, 4)
		binary.LittleEndian.PutUint32(location, 0x90000000)
		if err := applyARM64ADRP(location, 0x12345000, 0x12300000, true, 0, true); err != nil {
			t.Fatal(err)
		}
		if got, want := decodeARM64ADRImmediate(binary.LittleEndian.Uint32(location)), int64(0x45); got != want {
			t.Fatalf("ADRP pages = %#x, want %#x", got, want)
		}
	})

	t.Run("LDR64 low12", func(t *testing.T) {
		location := make([]byte, 4)
		binary.LittleEndian.PutUint32(location, 0xf9400000)
		if err := applyARM64LoadStoreLO12(location, 0x12345328, 3, true, 0); err != nil {
			t.Fatal(err)
		}
		got := (binary.LittleEndian.Uint32(location) >> 10) & 0xfff
		if want := uint32(0x328 >> 3); got != want {
			t.Fatalf("LDR immediate = %#x, want %#x", got, want)
		}
	})
}

func TestCOFFARM64SectionSymbolUsesByteAddend(t *testing.T) {
	pageBase := make([]byte, 4)
	// COFF encodes a section-relative byte addend in the ADRP immediate.
	// This is the shape emitted for a reference to .rdata+0x269.
	binary.LittleEndian.PutUint32(pageBase, encodeARM64ADRImmediate(0x90000000, 0x269))
	linked := linkedSymbol{
		symbol:  objectSymbol{name: ".rdata", section: 1},
		address: 0x12345000,
	}
	if err := applyCOFFARM64Relocation(
		&objectFile{imageBase: 0x12300000},
		objectRelocation{typeID: coffARM64PageBaseRel21},
		pageBase,
		0x12300000,
		linked,
	); err != nil {
		t.Fatal(err)
	}
	if got, want := decodeARM64ADRImmediate(binary.LittleEndian.Uint32(pageBase)), int64(0x45); got != want {
		t.Fatalf("ADRP pages = %#x, want %#x", got, want)
	}

	pageOffset := make([]byte, 4)
	binary.LittleEndian.PutUint32(pageOffset, 0x91000000|(0x269<<10)) // ADD x0, x0, #0x269
	if err := applyCOFFARM64Relocation(
		&objectFile{imageBase: 0x12300000},
		objectRelocation{typeID: coffARM64PageOffset12A},
		pageOffset,
		0x12300004,
		linked,
	); err != nil {
		t.Fatal(err)
	}
	page := uint64(0x12300000) + uint64(decodeARM64ADRImmediate(binary.LittleEndian.Uint32(pageBase))<<12)
	offset := uint64((binary.LittleEndian.Uint32(pageOffset) >> 10) & 0xfff)
	if got, want := page+offset, linked.address+0x269; got != want {
		t.Fatalf("ADRP+ADD address = %#x, want %#x", got, want)
	}
}

func TestApplyRelocationsRejectsBoundsAndDiscardedSymbols(t *testing.T) {
	baseObject := objectFile{
		format: "elf",
		arch:   "amd64",
		sections: []objectSection{{
			name:    ".text",
			size:    4,
			mapped:  true,
			offset:  0,
			address: 0x1000,
		}},
		symbols: map[uint32]objectSymbol{
			1: {index: 1, name: "target", section: sectionAbsolute, value: 0x2000},
		},
	}

	t.Run("out of bounds", func(t *testing.T) {
		object := baseObject
		object.relocations = []objectRelocation{{section: 0, offset: 2, typeID: uint32(elf.R_X86_64_PC32), symbol: 1, hasAdd: true}}
		err := applyRelocations(&object, &memoryRegion{data: make([]byte, 4)}, nil)
		if err == nil || !strings.Contains(err.Error(), "exceeds target section size") {
			t.Fatalf("error = %v, want section bounds error", err)
		}
	})

	t.Run("discarded target", func(t *testing.T) {
		object := baseObject
		object.symbols = map[uint32]objectSymbol{1: {index: 1, name: ".debug_info", section: sectionDiscarded}}
		object.relocations = []objectRelocation{{section: 0, offset: 0, typeID: uint32(elf.R_X86_64_PC32), symbol: 1, hasAdd: true}}
		err := applyRelocations(&object, &memoryRegion{data: make([]byte, 4)}, nil)
		if err == nil || !strings.Contains(err.Error(), "discarded section") {
			t.Fatalf("error = %v, want discarded-section error", err)
		}
	})
}

func externalPointer(value externalSymbol) *externalSymbol {
	return &value
}
