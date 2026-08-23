package bofloader

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestELFPPC64LERelocationCapabilities(t *testing.T) {
	wantWidths := map[elf.R_PPC64]int{
		elf.R_PPC64_ADDR64:      8,
		elf.R_PPC64_REL24:       4,
		elf.R_PPC64_REL16_HA:    4,
		elf.R_PPC64_REL16_LO:    4,
		elf.R_PPC64_TOC16_HA:    4,
		elf.R_PPC64_TOC16_LO:    4,
		elf.R_PPC64_TOC16_LO_DS: 4,
	}
	for typeID, want := range wantWidths {
		width, noop, err := elfRelocationWidth("ppc64le", uint32(typeID))
		if err != nil || noop || width != want {
			t.Errorf("elfRelocationWidth(ppc64le, %s) = (%d, %v, %v), want (%d, false, nil)", typeID, width, noop, err, want)
		}
		if elfRelocationNeedsGOT("ppc64le", uint32(typeID)) {
			t.Errorf("elfRelocationNeedsGOT(ppc64le, %s) = true, want false", typeID)
		}
	}
	for _, typeID := range []elf.R_PPC64{elf.R_PPC64_REL24_NOTOC, elf.R_PPC64_REL24_P9NOTOC, elf.R_PPC64_ENTRY} {
		if _, _, err := elfRelocationWidth("ppc64le", uint32(typeID)); err == nil {
			t.Errorf("unsupported PPC64 relocation %s was accepted", typeID)
		}
	}
}

func TestELFPPC64LEEmittedRelocationFormulas(t *testing.T) {
	t.Run("ADDR64", func(t *testing.T) {
		location := make([]byte, 8)
		relocation := objectRelocation{typeID: uint32(elf.R_PPC64_ADDR64), hasAdd: true, addend: -0x10}
		linked := linkedSymbol{address: 0x123456789abcdef0}
		if err := applyELFPPC64LERelocation(nil, relocation, location, 0, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint64(location), uint64(0x123456789abcdee0); got != want {
			t.Fatalf("ADDR64 = %#x, want %#x", got, want)
		}
	})

	t.Run("REL16 HA LO", func(t *testing.T) {
		linked := linkedSymbol{address: 0x10018004}
		high := ppc64Words(0x3c4c0000)
		if err := applyELFPPC64LERelocation(nil, objectRelocation{typeID: uint32(elf.R_PPC64_REL16_HA), hasAdd: true}, high, 0x10000000, linked); err != nil {
			t.Fatal(err)
		}
		low := ppc64Words(0x38420000)
		if err := applyELFPPC64LERelocation(nil, objectRelocation{typeID: uint32(elf.R_PPC64_REL16_LO), hasAdd: true, addend: 4}, low, 0x10000004, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(high), uint32(0x3c4c0002); got != want {
			t.Fatalf("REL16_HA instruction = %#08x, want %#08x", got, want)
		}
		if got, want := binary.LittleEndian.Uint32(low), uint32(0x38428004); got != want {
			t.Fatalf("REL16_LO instruction = %#08x, want %#08x", got, want)
		}
	})

	t.Run("TOC16 HA LO and LO_DS", func(t *testing.T) {
		object := &objectFile{ppc64TOC: 0x20008000}
		linked := linkedSymbol{address: 0x20020004}
		high := ppc64Words(0x3c620000)
		if err := applyELFPPC64LERelocation(object, objectRelocation{typeID: uint32(elf.R_PPC64_TOC16_HA), hasAdd: true}, high, 0, linked); err != nil {
			t.Fatal(err)
		}
		low := ppc64Words(0x38630000)
		if err := applyELFPPC64LERelocation(object, objectRelocation{typeID: uint32(elf.R_PPC64_TOC16_LO), hasAdd: true}, low, 0, linked); err != nil {
			t.Fatal(err)
		}
		ds := ppc64Words(0xe8630000)
		if err := applyELFPPC64LERelocation(object, objectRelocation{typeID: uint32(elf.R_PPC64_TOC16_LO_DS), hasAdd: true, addend: 4}, ds, 0, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(high), uint32(0x3c620002); got != want {
			t.Fatalf("TOC16_HA instruction = %#08x, want %#08x", got, want)
		}
		if got, want := binary.LittleEndian.Uint32(low), uint32(0x38638004); got != want {
			t.Fatalf("TOC16_LO instruction = %#08x, want %#08x", got, want)
		}
		if got, want := binary.LittleEndian.Uint32(ds), uint32(0xe8638008); got != want {
			t.Fatalf("TOC16_LO_DS instruction = %#08x, want %#08x", got, want)
		}
	})
}

func TestELFPPC64LEREL24ExternalRestoresTOCAndDefinedUsesLocalEntry(t *testing.T) {
	t.Run("external call", func(t *testing.T) {
		location := ppc64Words(0x48000001, ppc64NOP)
		linked := linkedSymbol{
			symbol:   objectSymbol{name: "BeaconOutput", section: sectionUndefined},
			external: &externalSymbol{target: 0x90000000, thunk: 0x1800},
		}
		relocation := objectRelocation{typeID: uint32(elf.R_PPC64_REL24), hasAdd: true}
		if err := applyELFPPC64LERelocation(nil, relocation, location, 0x1000, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(location), uint32(0x48000801); got != want {
			t.Fatalf("external REL24 branch = %#08x, want %#08x", got, want)
		}
		if got := binary.LittleEndian.Uint32(location[4:]); got != ppc64RestoreTOC {
			t.Fatalf("external REL24 restore = %#08x, want %#08x", got, ppc64RestoreTOC)
		}
	})

	t.Run("defined local entry", func(t *testing.T) {
		location := ppc64Words(0x48000001)
		linked := linkedSymbol{symbol: objectSymbol{name: "local_fn", section: 0, localEntry: 8}, address: 0x1800}
		relocation := objectRelocation{typeID: uint32(elf.R_PPC64_REL24), hasAdd: true}
		if err := applyELFPPC64LERelocation(nil, relocation, location, 0x1000, linked); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint32(location), uint32(0x48000809); got != want {
			t.Fatalf("defined REL24 branch = %#08x, want %#08x", got, want)
		}
	})
}

func TestELFPPC64LERejectsMalformedBranchesWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		words  []uint32
		linked linkedSymbol
		addend int64
		target uint64
		want   string
	}{
		{name: "instruction", words: []uint32{ppc64NOP}, linked: linkedSymbol{address: 0x1800}, want: "expected relative branch"},
		{name: "absolute branch", words: []uint32{0x48000003}, linked: linkedSymbol{address: 0x1800}, want: "absolute branch"},
		{name: "unaligned", words: []uint32{0x48000001}, linked: linkedSymbol{address: 0x1801}, want: "not 4-byte aligned"},
		{name: "range", words: []uint32{0x48000001}, linked: linkedSymbol{address: 1 << 25}, want: "exceeds signed REL24 range"},
		{name: "local entry overflow", words: []uint32{0x48000001}, linked: linkedSymbol{symbol: objectSymbol{localEntry: 4}, address: math.MaxUint64}, want: "local entry address overflows"},
		{name: "external addend", words: []uint32{0x48000001, ppc64NOP}, linked: ppc64ExternalLinked(), addend: 4, want: "zero addend"},
		{name: "external tail branch", words: []uint32{0x48000000}, linked: ppc64ExternalLinked(), want: "tail branches are unsupported"},
		{name: "missing restore", words: []uint32{0x48000001}, linked: ppc64ExternalLinked(), want: "missing its 4-byte TOC restore slot"},
		{name: "non-NOP restore", words: []uint32{0x48000001, 0x7c0004ac}, linked: ppc64ExternalLinked(), want: "must be followed by linker NOP"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location := ppc64Words(test.words...)
			before := append([]byte(nil), location...)
			relocation := objectRelocation{typeID: uint32(elf.R_PPC64_REL24), hasAdd: true, addend: test.addend}
			err := applyELFPPC64LERelocation(nil, relocation, location, 0, test.linked)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if !bytes.Equal(location, before) {
				t.Fatalf("location changed on rejection: %x -> %x", before, location)
			}
		})
	}
}

func TestELFPPC64LERejectsMalformedHalfwordRelocations(t *testing.T) {
	tests := []struct {
		name   string
		object *objectFile
		typeID elf.R_PPC64
		offset uint64
		word   uint32
		linked linkedSymbol
		want   string
	}{
		{name: "REL16_HA class", typeID: elf.R_PPC64_REL16_HA, word: 0x38630000, linked: linkedSymbol{address: 0x1000}, want: "expected addis"},
		{name: "REL16_LO class", typeID: elf.R_PPC64_REL16_LO, word: 0x3c620000, linked: linkedSymbol{address: 0x1000}, want: "expected addi"},
		{name: "REL16 range", typeID: elf.R_PPC64_REL16_HA, word: 0x3c620000, linked: linkedSymbol{address: 1 << 40}, want: "signed 32-bit"},
		{name: "missing TOC", typeID: elf.R_PPC64_TOC16_HA, word: 0x3c620000, linked: linkedSymbol{address: 0x1000}, want: "no ELFv2 TOC base"},
		{name: "TOC16_HA class", object: &objectFile{ppc64TOC: 0x1000}, typeID: elf.R_PPC64_TOC16_HA, word: 0x38630000, linked: linkedSymbol{address: 0x1800}, want: "expected addis"},
		{name: "TOC16_LO class", object: &objectFile{ppc64TOC: 0x1000}, typeID: elf.R_PPC64_TOC16_LO, word: 0x3c620000, linked: linkedSymbol{address: 0x1800}, want: "expected addi"},
		{name: "TOC DS class", object: &objectFile{ppc64TOC: 0x1000}, typeID: elf.R_PPC64_TOC16_LO_DS, word: 0x80630000, linked: linkedSymbol{address: 0x1800}, want: "expected DS-form"},
		{name: "TOC DS alignment", object: &objectFile{ppc64TOC: 0x1000}, typeID: elf.R_PPC64_TOC16_LO_DS, word: 0xe8630000, linked: linkedSymbol{address: 0x1801}, want: "not 4-byte aligned"},
		{name: "relocation site alignment", typeID: elf.R_PPC64_REL16_HA, offset: 2, word: 0x3c620000, linked: linkedSymbol{address: 0x1000}, want: "relocation offset 0x2 is not 4-byte aligned"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			location := ppc64Words(test.word)
			before := append([]byte(nil), location...)
			relocation := objectRelocation{offset: test.offset, typeID: uint32(test.typeID), hasAdd: true}
			err := applyELFPPC64LERelocation(test.object, relocation, location, 0, test.linked)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if !bytes.Equal(location, before) {
				t.Fatalf("location changed on rejection: %x -> %x", before, location)
			}
		})
	}
}

func TestPPC64SplitAddressBoundaries(t *testing.T) {
	for _, value := range []int64{math.MinInt32, -32769, -32768, -1, 0, 32767, 32768, 0x7fff7fff} {
		high, low, err := ppc64SplitAddress(value, false)
		if err != nil {
			t.Errorf("ppc64SplitAddress(%d) error = %v", value, err)
			continue
		}
		if got := int64(high)<<16 + int64(low); got != value {
			t.Errorf("ppc64SplitAddress(%d) reconstructs %d", value, got)
		}
	}
	for _, value := range []int64{int64(math.MinInt32) - 1, 0x7fff8000, int64(math.MaxInt32) + 1} {
		if _, _, err := ppc64SplitAddress(value, false); err == nil {
			t.Errorf("ppc64SplitAddress(%d) accepted an unrepresentable value", value)
		}
	}
	if _, _, err := ppc64SplitAddress(2, true); err == nil || !strings.Contains(err.Error(), "4-byte aligned") {
		t.Fatalf("DS alignment error = %v", err)
	}
}

func TestWritePPC64LEThunk(t *testing.T) {
	buffer := bytes.Repeat([]byte{0xff}, 32)
	if err := writePPC64LEThunk(buffer, 0x20020008, 0x20008000); err != nil {
		t.Fatal(err)
	}
	want := []uint32{0xf8410018, 0x3d820002, 0xe98c8008, 0x7d8903a6, 0x4e800420}
	for index, word := range want {
		if got := binary.LittleEndian.Uint32(buffer[index*4:]); got != word {
			t.Errorf("PPC64 thunk word %d = %#08x, want %#08x", index, got, word)
		}
	}
	if !bytes.Equal(buffer[20:], make([]byte, 12)) {
		t.Fatalf("PPC64 thunk padding = %x, want zeroes", buffer[20:])
	}

	for _, test := range []struct {
		name string
		buf  []byte
		got  uintptr
		toc  uintptr
		want string
	}{
		{name: "short", buf: make([]byte, 31), got: 0x2000, toc: 0x1000, want: "32-byte"},
		{name: "unaligned", buf: bytes.Repeat([]byte{0xaa}, 32), got: 0x2001, toc: 0x1000, want: "4-byte aligned"},
		{name: "range", buf: bytes.Repeat([]byte{0xaa}, 32), got: 0x7fff8000, toc: 0, want: "high-adjusted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := append([]byte(nil), test.buf...)
			if err := writePPC64LEThunk(test.buf, test.got, test.toc); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
			if !bytes.Equal(test.buf, before) {
				t.Fatalf("thunk changed on rejection: %x -> %x", before, test.buf)
			}
		})
	}
}

func TestValidateELFPPC64LERestoreSlotsScalesWithRelocationCount(t *testing.T) {
	const callCount = 4096
	data := make([]byte, callCount*8)
	relocations := make([]objectRelocation, callCount)
	for index := range callCount {
		offset := index * 8
		binary.LittleEndian.PutUint32(data[offset:], 0x48000001)
		binary.LittleEndian.PutUint32(data[offset+4:], ppc64NOP)
		relocations[index] = objectRelocation{
			section: 0,
			offset:  uint64(offset),
			typeID:  uint32(elf.R_PPC64_REL24),
			symbol:  1,
			hasAdd:  true,
		}
	}
	object := &objectFile{
		format:      "elf",
		arch:        "ppc64le",
		sections:    []objectSection{{name: ".text", data: data, size: uint64(len(data)), mapped: true}},
		symbols:     map[uint32]objectSymbol{1: {index: 1, name: "BeaconOutput", section: sectionUndefined}},
		relocations: relocations,
	}
	if err := validateELFPPC64LERelocations(object); err != nil {
		t.Fatalf("validateELFPPC64LERelocations(%d calls) error = %v", callCount, err)
	}
}

func ppc64ExternalLinked() linkedSymbol {
	return linkedSymbol{
		symbol:   objectSymbol{name: "BeaconOutput", section: sectionUndefined},
		external: &externalSymbol{target: 0x9000, thunk: 0x1800},
	}
}

func ppc64Words(words ...uint32) []byte {
	result := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(result[index*4:], word)
	}
	return result
}
