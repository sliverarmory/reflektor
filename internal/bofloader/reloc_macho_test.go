//go:build bof

package bofloader

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestMachOX86Relocations(t *testing.T) {
	t.Run("external branch uses thunk", func(t *testing.T) {
		location := make([]byte, 4)
		linked := linkedSymbol{
			symbol:   objectSymbol{name: "_BeaconOutput", section: sectionUndefined},
			address:  0x9000,
			external: &externalSymbol{target: 0x9000, thunk: 0x1800, got: 0x3000},
		}
		err := applyMachOX86Relocation(objectRelocation{typeID: machoX86RelocBranch, width: 4}, location, 0x2000, linked)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := int32(binary.LittleEndian.Uint32(location)), int32(0x1800-0x2000-4); got != want {
			t.Fatalf("branch displacement = %d, want %d", got, want)
		}
	})

	t.Run("defined GOT symbol uses allocated slot", func(t *testing.T) {
		location := make([]byte, 4)
		linked := linkedSymbol{symbol: objectSymbol{name: "_global", section: 1}, address: 0x2800, got: 0x3000}
		err := applyMachOX86Relocation(objectRelocation{typeID: machoX86RelocGOTLoad, width: 4}, location, 0x2000, linked)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := int32(binary.LittleEndian.Uint32(location)), int32(0x3000-0x2000-4); got != want {
			t.Fatalf("GOT displacement = %d, want %d", got, want)
		}
	})

	t.Run("local explicit section offset", func(t *testing.T) {
		location := make([]byte, 4)
		linked := linkedSymbol{symbol: objectSymbol{name: "__TEXT,__cstring", section: 1}, address: 0x4000}
		relocation := objectRelocation{typeID: machoX86RelocSigned, width: 4, hasAdd: true, addend: 0x15}
		if err := applyMachOX86Relocation(relocation, location, 0x2000, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := int32(binary.LittleEndian.Uint32(location)), int32(0x4000+0x15-0x2000-4); got != want {
			t.Fatalf("local displacement = %d, want %d", got, want)
		}
	})

	for _, test := range []struct {
		name   string
		typeID uint32
		suffix int32
	}{
		{name: "signed-1", typeID: machoX86RelocSigned1, suffix: 1},
		{name: "signed-2", typeID: machoX86RelocSigned2, suffix: 2},
		{name: "signed-4", typeID: machoX86RelocSigned4, suffix: 4},
	} {
		t.Run("external "+test.name+" normalizes suffix addend", func(t *testing.T) {
			location := make([]byte, 4)
			binary.LittleEndian.PutUint32(location, uint32(0x13))
			linked := linkedSymbol{
				symbol:   objectSymbol{name: "_external", section: sectionUndefined},
				address:  0x4000,
				external: &externalSymbol{target: 0x4000},
			}
			if err := applyMachOX86Relocation(objectRelocation{typeID: test.typeID, width: 4}, location, 0x2000, linked); err != nil {
				t.Fatal(err)
			}
			if got, want := int32(binary.LittleEndian.Uint32(location)), int32(0x4000+0x13-0x2000-4); got != want {
				t.Fatalf("displacement = %d, want %d (suffix %d)", got, want, test.suffix)
			}
		})
	}
}

func TestMachOARM64Relocations(t *testing.T) {
	t.Run("external branch uses thunk", func(t *testing.T) {
		location := make([]byte, 4)
		binary.LittleEndian.PutUint32(location, 0x94000000)
		linked := linkedSymbol{
			symbol:   objectSymbol{name: "_BeaconOutput", section: sectionUndefined},
			address:  0x9000,
			external: &externalSymbol{target: 0x9000, thunk: 0x1800},
		}
		if err := applyMachOARM64Relocation(objectRelocation{typeID: machoARM64RelocBranch26, width: 4}, location, 0x1000, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(location), uint32(0x94000200); got != want {
			t.Fatalf("branch encoding = %#08x, want %#08x", got, want)
		}
	})

	t.Run("page and low12", func(t *testing.T) {
		linked := linkedSymbol{symbol: objectSymbol{name: "_data", section: 1}, address: 0x12345328}
		page := make([]byte, 4)
		binary.LittleEndian.PutUint32(page, 0x90000000)
		if err := applyMachOARM64Relocation(objectRelocation{typeID: machoARM64RelocPage21, width: 4}, page, 0x12300000, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := decodeARM64ADRImmediate(binary.LittleEndian.Uint32(page)), int64(0x45); got != want {
			t.Fatalf("ADRP page delta = %#x, want %#x", got, want)
		}
		low := make([]byte, 4)
		binary.LittleEndian.PutUint32(low, 0x91000000)
		if err := applyMachOARM64Relocation(objectRelocation{typeID: machoARM64RelocPageOff12, width: 4}, low, 0, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := (binary.LittleEndian.Uint32(low)>>10)&0xfff, uint32(0x328); got != want {
			t.Fatalf("ADD low12 = %#x, want %#x", got, want)
		}
	})

	t.Run("GOT load", func(t *testing.T) {
		linked := linkedSymbol{symbol: objectSymbol{name: "_data", section: 1}, address: 0x5000, got: 0x12345678}
		location := make([]byte, 4)
		binary.LittleEndian.PutUint32(location, 0xf9400000)
		if err := applyMachOARM64Relocation(objectRelocation{typeID: machoARM64RelocGOTLoadPageOff12, width: 4}, location, 0, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := (binary.LittleEndian.Uint32(location)>>10)&0xfff, uint32(0x678>>3); got != want {
			t.Fatalf("GOT LDR low12 = %#x, want %#x", got, want)
		}
	})
}

func TestMachOARM64RejectsEmbeddedInstructionAddends(t *testing.T) {
	for _, test := range []struct {
		name   string
		typeID uint32
		word   uint32
	}{
		{name: "branch26", typeID: machoARM64RelocBranch26, word: 0x94000001},
		{name: "page21", typeID: machoARM64RelocPage21, word: 0x90000020},
		{name: "pageoff12", typeID: machoARM64RelocPageOff12, word: 0x91000400},
		{name: "GOT page21", typeID: machoARM64RelocGOTLoadPage21, word: 0x90000020},
		{name: "GOT pageoff12", typeID: machoARM64RelocGOTLoadPageOff12, word: 0xf9400400},
		{name: "pointer to GOT", typeID: machoARM64RelocPointerToGOT, word: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			location := make([]byte, 4)
			binary.LittleEndian.PutUint32(location, test.word)
			if err := validateMachOARM64EmbeddedAddend(test.typeID, location); err == nil || !strings.Contains(err.Error(), "non-zero embedded addend") {
				t.Fatalf("validation error = %v, want embedded-addend rejection", err)
			}
		})
	}
}

func TestMachORelocationValidation(t *testing.T) {
	if width, noop, err := relocationWidth("macho", "amd64", objectRelocation{typeID: machoX86RelocUnsigned, width: 8}); err != nil || noop || width != 8 {
		t.Fatalf("Mach-O UNSIGNED width = %d, noop=%t, err=%v; want width 8 and active", width, noop, err)
	}
	for _, test := range []struct {
		name   string
		arch   string
		typeID uint8
		length uint8
		pcrel  bool
		want   string
	}{
		{name: "x86 subtractor", arch: "amd64", typeID: uint8(machoX86RelocSubtractor), length: 3, want: "SUBTRACTOR"},
		{name: "arm64 TLS", arch: "arm64", typeID: uint8(machoARM64RelocTLVPLoadPage21), length: 2, pcrel: true, want: "thread-local"},
		{name: "arm64 bad branch width", arch: "arm64", typeID: uint8(machoARM64RelocBranch26), length: 3, pcrel: true, want: "4-byte"},
		{name: "arm64 local branch", arch: "arm64", typeID: uint8(machoARM64RelocBranch26), length: 2, pcrel: true, want: "external symbol"},
		{name: "arm64 absolute pointer to GOT", arch: "arm64", typeID: uint8(machoARM64RelocPointerToGOT), length: 3, want: "ambiguous 8-byte"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateMachORelocationShape(test.arch, test.typeID, test.length, test.pcrel, false)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validation error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestDefinedMachOSymbolGetsGOTLinkage(t *testing.T) {
	object := &objectFile{
		format: "macho",
		arch:   "arm64",
		sections: []objectSection{
			{name: "__TEXT,__text", size: 4, mapped: true},
			{name: "__DATA,__data", size: 8, mapped: true},
		},
		symbols: map[uint32]objectSymbol{1: {index: 1, name: "_global", section: 1}},
		relocations: []objectRelocation{{
			section: 0, typeID: machoARM64RelocGOTLoadPage21, symbol: 1, width: 4,
		}},
	}
	if got := referencedLinkageSymbols(object); len(got) != 1 || got[0] != 1 {
		t.Fatalf("referenced linkage symbols = %v, want [1]", got)
	}
}
