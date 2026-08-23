package bofloader

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestELFARMFixtureRelocations(t *testing.T) {
	t.Run("CALL uses external thunk", func(t *testing.T) {
		location := armWord(0xebfffffe) // BL with the required Arm-state PC bias of -8.
		linked := linkedSymbol{
			symbol:   objectSymbol{name: "BeaconOutput", section: sectionUndefined},
			address:  0x9000,
			external: &externalSymbol{target: 0x9000, thunk: 0x1800},
		}
		relocation := objectRelocation{typeID: uint32(elf.R_ARM_CALL)}
		if err := applyELFARMRelocation(relocation, location, 0x2000, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := armBranchDisplacement(location), int64(0x1800-8-0x2000); got != want {
			t.Fatalf("CALL displacement = %d, want thunk displacement %d", got, want)
		}
	})

	t.Run("JUMP24 uses external thunk", func(t *testing.T) {
		location := armWord(0xeafffffe) // B with an implicit addend of -8.
		linked := linkedSymbol{
			symbol:   objectSymbol{name: "BeaconOutput", section: sectionUndefined},
			address:  0x9000,
			external: &externalSymbol{target: 0x9000, thunk: 0x1c00},
		}
		relocation := objectRelocation{typeID: uint32(elf.R_ARM_JUMP24)}
		if err := applyELFARMRelocation(relocation, location, 0x2000, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := armBranchDisplacement(location), int64(0x1c00-8-0x2000); got != want {
			t.Fatalf("JUMP24 displacement = %d, want thunk displacement %d", got, want)
		}
	})

	t.Run("REL32 uses direct symbol", func(t *testing.T) {
		location := armWord(0x60)
		linked := linkedSymbol{symbol: objectSymbol{name: "local", section: 1}, address: 0x3000}
		relocation := objectRelocation{typeID: uint32(elf.R_ARM_REL32)}
		if err := applyELFARMRelocation(relocation, location, 0x10fc, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(location), uint32(0x3000+0x60-0x10fc); got != want {
			t.Fatalf("REL32 value = %#x, want %#x", got, want)
		}
	})

	t.Run("ABS32 uses direct symbol", func(t *testing.T) {
		location := armWord(4)
		linked := linkedSymbol{symbol: objectSymbol{name: "local", section: 1}, address: 0x3000}
		relocation := objectRelocation{typeID: uint32(elf.R_ARM_ABS32)}
		if err := applyELFARMRelocation(relocation, location, 0, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(location), uint32(0x3004); got != want {
			t.Fatalf("ABS32 value = %#x, want %#x", got, want)
		}
	})

	t.Run("GOT_PREL uses GOT slot", func(t *testing.T) {
		location := armWord(0x10)
		linked := linkedSymbol{
			symbol:  objectSymbol{name: "bof_pic_global", section: 1},
			address: 0x3800,
			got:     0x3000,
		}
		relocation := objectRelocation{typeID: uint32(elf.R_ARM_GOT_PREL)}
		if err := applyELFARMRelocation(relocation, location, 0x110c, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(location), uint32(0x3000+0x10-0x110c); got != want {
			t.Fatalf("GOT_PREL value = %#x, want %#x", got, want)
		}
	})

	t.Run("PREL31 preserves metadata bit", func(t *testing.T) {
		location := armWord(0x80000004)
		linked := linkedSymbol{symbol: objectSymbol{name: ".text", section: 0}, address: 0x1800}
		relocation := objectRelocation{typeID: uint32(elf.R_ARM_PREL31)}
		if err := applyELFARMRelocation(relocation, location, 0x1000, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(location), uint32(0x80000804); got != want {
			t.Fatalf("PREL31 value = %#x, want %#x", got, want)
		}
	})
}

func TestELFARMRelocationCapabilities(t *testing.T) {
	supported := []elf.R_ARM{
		elf.R_ARM_PC24,
		elf.R_ARM_ABS32,
		elf.R_ARM_CALL,
		elf.R_ARM_JUMP24,
		elf.R_ARM_PLT32,
		elf.R_ARM_REL32,
		elf.R_ARM_GOT_PREL,
		elf.R_ARM_PREL31,
	}
	for _, typeID := range supported {
		width, noop, err := elfRelocationWidth("arm", uint32(typeID))
		if err != nil || noop || width != 4 {
			t.Errorf("elfRelocationWidth(arm, %s) = (%d, %v, %v), want (4, false, nil)", typeID, width, noop, err)
		}
	}
	for _, typeID := range supported {
		if got, want := elfRelocationNeedsGOT("arm", uint32(typeID)), typeID == elf.R_ARM_GOT_PREL; got != want {
			t.Errorf("elfRelocationNeedsGOT(arm, %s) = %v, want %v", typeID, got, want)
		}
	}
	if _, _, err := elfRelocationWidth("arm", uint32(elf.R_ARM_THM_PC22)); err == nil {
		t.Fatal("Thumb relocation was accepted by the ARM-state backend")
	}
}

func TestELFARMRelocationsRejectMalformedEncodingsAndRanges(t *testing.T) {
	linked := linkedSymbol{symbol: objectSymbol{name: "target", section: 0}, address: 0x1800}

	tests := []struct {
		name     string
		typeID   elf.R_ARM
		word     uint32
		place    uint64
		linked   linkedSymbol
		want     string
		explicit bool
		addend   int64
	}{
		{name: "CALL instruction class", typeID: elf.R_ARM_CALL, word: 0xeafffffe, place: 0x1000, linked: linked, want: "unconditional BL"},
		{name: "JUMP24 instruction class", typeID: elf.R_ARM_JUMP24, word: 0xe1a00000, place: 0x1000, linked: linked, want: "expected B/BL"},
		{name: "branch range", typeID: elf.R_ARM_CALL, word: 0xebfffffe, place: 0, linked: linkedSymbol{address: (1 << 25) + 8}, want: "signed 26-bit range"},
		{name: "branch alignment", typeID: elf.R_ARM_CALL, word: 0xeb000000, place: 0x1000, linked: linked, explicit: true, addend: 2, want: "not 4-byte aligned"},
		{name: "Thumb target", typeID: elf.R_ARM_CALL, word: 0xebfffffe, place: 0x1000, linked: linkedSymbol{address: 0x1801}, want: "enters Thumb state"},
		{name: "PREL31 range", typeID: elf.R_ARM_PREL31, word: 0, place: 0, linked: linkedSymbol{address: 1 << 30}, want: "signed 31-bit range"},
		{name: "REL32 address width", typeID: elf.R_ARM_REL32, word: 0, place: 0, linked: linkedSymbol{address: uint64(math.MaxUint32) + 1}, want: "32-bit address space"},
		{name: "GOT alignment", typeID: elf.R_ARM_GOT_PREL, word: 0, place: 0x1000, linked: linkedSymbol{got: 0x2002}, want: "not 4-byte aligned"},
		{name: "missing GOT", typeID: elf.R_ARM_GOT_PREL, word: 0, place: 0x1000, linked: linkedSymbol{symbol: objectSymbol{name: "global"}}, want: "none was allocated"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location := armWord(test.word)
			before := append([]byte(nil), location...)
			relocation := objectRelocation{typeID: uint32(test.typeID), hasAdd: test.explicit, addend: test.addend}
			err := applyELFARMRelocation(relocation, location, test.place, test.linked)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if !bytes.Equal(location, before) {
				t.Fatalf("location changed on rejection: %x -> %x", before, location)
			}
		})
	}
}

func armWord(word uint32) []byte {
	result := make([]byte, 4)
	binary.LittleEndian.PutUint32(result, word)
	return result
}

func armBranchDisplacement(location []byte) int64 {
	return signExtend(uint64(binary.LittleEndian.Uint32(location)&0x00ffffff), 24) << 2
}
