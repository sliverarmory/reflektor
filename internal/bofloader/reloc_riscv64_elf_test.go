package bofloader

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"strings"
	"testing"
)

func TestELFRISCV64RelocationCapabilities(t *testing.T) {
	widths := map[elf.R_RISCV]int{
		elf.R_RISCV_64:           8,
		elf.R_RISCV_CALL:         8,
		elf.R_RISCV_CALL_PLT:     8,
		elf.R_RISCV_GOT_HI20:     4,
		elf.R_RISCV_PCREL_HI20:   4,
		elf.R_RISCV_PCREL_LO12_I: 4,
		elf.R_RISCV_PCREL_LO12_S: 4,
	}
	for typeID, wantWidth := range widths {
		width, noop, err := elfRelocationWidth("riscv64", uint32(typeID))
		if err != nil || noop || width != wantWidth {
			t.Errorf("elfRelocationWidth(riscv64, %s) = (%d, %v, %v), want (%d, false, nil)", typeID, width, noop, err, wantWidth)
		}
		if got, want := elfRelocationNeedsGOT("riscv64", uint32(typeID)), typeID == elf.R_RISCV_GOT_HI20; got != want {
			t.Errorf("elfRelocationNeedsGOT(riscv64, %s) = %v, want %v", typeID, got, want)
		}
	}
	for _, typeID := range []elf.R_RISCV{elf.R_RISCV_ALIGN, elf.R_RISCV_RELAX} {
		width, noop, err := elfRelocationWidth("riscv64", uint32(typeID))
		if err != nil || !noop || width != 0 {
			t.Errorf("elfRelocationWidth(riscv64, %s) = (%d, %v, %v), want (0, true, nil)", typeID, width, noop, err)
		}
	}
	if _, _, err := elfRelocationWidth("riscv64", uint32(elf.R_RISCV_TLS_GD_HI20)); err == nil {
		t.Fatal("TLS relocation was accepted by the RISC-V BOF backend")
	}
}

func TestELFRISCV64FixtureRelocations(t *testing.T) {
	t.Run("CALL_PLT uses external thunk", func(t *testing.T) {
		location := riscvWords(0x00000097, 0x000080e7)
		linked := linkedSymbol{
			symbol:   objectSymbol{name: "BeaconOutput", section: sectionUndefined},
			address:  0x9000,
			external: &externalSymbol{target: 0x9000, thunk: 0x2800},
		}
		relocation := objectRelocation{typeID: uint32(elf.R_RISCV_CALL_PLT), hasAdd: true}
		if err := applyELFRISCV64Relocation(nil, relocation, location, 0x1000, linked, nil); err != nil {
			t.Fatal(err)
		}
		if got, want := riscvCallDisplacement(location), int64(0x1800); got != want {
			t.Fatalf("CALL_PLT displacement = %#x, want %#x", got, want)
		}
	})

	t.Run("PCREL_HI20 and LO12_I use direct symbol", func(t *testing.T) {
		const target = uint64(0x2804)
		highLocation := riscvWords(0x00000297)
		linked := linkedSymbol{symbol: objectSymbol{name: "data", section: sectionAbsolute}, address: target}
		highRelocation := objectRelocation{typeID: uint32(elf.R_RISCV_PCREL_HI20), hasAdd: true}
		if err := applyELFRISCV64Relocation(nil, highRelocation, highLocation, 0x1000, linked, nil); err != nil {
			t.Fatal(err)
		}
		if got, want := riscvHighImmediate(highLocation), int64(2); got != want {
			t.Fatalf("PCREL_HI20 immediate = %d, want %d", got, want)
		}

		object := riscvLowPairObject(elf.R_RISCV_PCREL_HI20, target, 0x00000297, 4)
		lowLocation := riscvWords(0x00028293)
		lowRelocation := objectRelocation{section: 0, offset: 4, typeID: uint32(elf.R_RISCV_PCREL_LO12_I), hasAdd: true}
		if err := applyELFRISCV64Relocation(object, lowRelocation, lowLocation, 0x1004, linkedSymbol{}, nil); err != nil {
			t.Fatal(err)
		}
		if got, want := riscvIImmediate(lowLocation), int64(-2044); got != want {
			t.Fatalf("PCREL_LO12_I immediate = %d, want %d", got, want)
		}
	})

	t.Run("GOT_HI20 pair uses GOT slot", func(t *testing.T) {
		const gotAddress = uintptr(0x2808)
		object := riscvLowPairObject(elf.R_RISCV_GOT_HI20, 0x4000, 0x00000297, 4)
		externals := map[uint32]externalSymbol{1: {name: "bof_pic_global", got: gotAddress}}
		highLocation := riscvWords(0x00000297)
		linked := linkedSymbol{symbol: object.symbols[1], address: 0x4000, got: gotAddress}
		highRelocation := objectRelocation{typeID: uint32(elf.R_RISCV_GOT_HI20), hasAdd: true}
		if err := applyELFRISCV64Relocation(object, highRelocation, highLocation, 0x1000, linked, externals); err != nil {
			t.Fatal(err)
		}
		lowLocation := riscvWords(0x0002b283)
		lowRelocation := objectRelocation{section: 0, offset: 4, typeID: uint32(elf.R_RISCV_PCREL_LO12_I), hasAdd: true}
		if err := applyELFRISCV64Relocation(object, lowRelocation, lowLocation, 0x1004, linkedSymbol{}, externals); err != nil {
			t.Fatal(err)
		}
		if got, want := (riscvHighImmediate(highLocation)<<12)+riscvIImmediate(lowLocation), int64(gotAddress-0x1000); got != want {
			t.Fatalf("GOT pair displacement = %#x, want %#x", got, want)
		}
	})

	t.Run("PCREL_LO12_S preserves store fields", func(t *testing.T) {
		const target = uint64(0x1804)
		object := riscvLowPairObject(elf.R_RISCV_PCREL_HI20, target, 0x00000297, 4)
		location := riscvWords(0x00a2b023) // SD a0, 0(t0).
		before := binary.LittleEndian.Uint32(location)
		relocation := objectRelocation{section: 0, offset: 4, typeID: uint32(elf.R_RISCV_PCREL_LO12_S), hasAdd: true}
		if err := applyELFRISCV64Relocation(object, relocation, location, 0x1004, linkedSymbol{}, nil); err != nil {
			t.Fatal(err)
		}
		if got, want := riscvSImmediate(location), int64(-2044); got != want {
			t.Fatalf("PCREL_LO12_S immediate = %d, want %d", got, want)
		}
		if got := binary.LittleEndian.Uint32(location) &^ 0xfe000f80; got != before&^0xfe000f80 {
			t.Fatalf("PCREL_LO12_S changed non-immediate fields: %#x -> %#x", before, binary.LittleEndian.Uint32(location))
		}
	})

	t.Run("64 uses direct symbol", func(t *testing.T) {
		location := make([]byte, 8)
		linked := linkedSymbol{symbol: objectSymbol{name: "pointer", section: sectionAbsolute}, address: 0x123456789abcdef0}
		relocation := objectRelocation{typeID: uint32(elf.R_RISCV_64), hasAdd: true, addend: -0x10}
		if err := applyELFRISCV64Relocation(nil, relocation, location, 0, linked, nil); err != nil {
			t.Fatal(err)
		}
		if got, want := binary.LittleEndian.Uint64(location), uint64(0x123456789abcdee0); got != want {
			t.Fatalf("R_RISCV_64 value = %#x, want %#x", got, want)
		}
	})
}

func TestELFRISCV64RejectsMalformedInstructionsAndRanges(t *testing.T) {
	t.Run("external CALL addend", func(t *testing.T) {
		location := riscvWords(0x00000097, 0x000080e7)
		before := append([]byte(nil), location...)
		linked := linkedSymbol{
			symbol:   objectSymbol{name: "BeaconOutput", section: sectionUndefined},
			external: &externalSymbol{target: 0x9000, thunk: 0x1800},
		}
		relocation := objectRelocation{typeID: uint32(elf.R_RISCV_CALL_PLT), hasAdd: true, addend: 4}
		err := applyELFRISCV64Relocation(nil, relocation, location, 0x1000, linked, nil)
		if err == nil || !strings.Contains(err.Error(), "external CALL") || !strings.Contains(err.Error(), "zero addend") {
			t.Fatalf("error = %v, want external CALL addend rejection", err)
		}
		if !bytes.Equal(location, before) {
			t.Fatalf("location changed on rejection: %x -> %x", before, location)
		}
	})

	t.Run("CALL errors preserve instructions", func(t *testing.T) {
		tests := []struct {
			name   string
			words  []uint32
			target uint64
			place  uint64
			want   string
		}{
			{name: "AUIPC class", words: []uint32{0x00000013, 0x000080e7}, target: 0x1800, place: 0x1000, want: "expected AUIPC"},
			{name: "AUIPC x0", words: []uint32{0x00000017, 0x000000e7}, target: 0x1800, place: 0x1000, want: "cannot be x0"},
			{name: "JALR class", words: []uint32{0x00000097, 0x00008013}, target: 0x1800, place: 0x1000, want: "expected JALR"},
			{name: "register mismatch", words: []uint32{0x00000097, 0x000100e7}, target: 0x1800, place: 0x1000, want: "does not match"},
			{name: "odd target", words: []uint32{0x00000097, 0x000080e7}, target: 0x1801, place: 0x1000, want: "not 2-byte aligned"},
			{name: "range", words: []uint32{0x00000097, 0x000080e7}, target: 0x7ffff800, place: 0, want: "exceeds signed AUIPC/LO12 range"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				location := riscvWords(test.words...)
				before := append([]byte(nil), location...)
				err := applyRISCVCall(location, test.target, test.place, 0)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("error = %v, want substring %q", err, test.want)
				}
				if !bytes.Equal(location, before) {
					t.Fatalf("location changed on rejection: %x -> %x", before, location)
				}
			})
		}
	})

	t.Run("LO12 errors preserve instruction", func(t *testing.T) {
		tests := []struct {
			name      string
			object    *objectFile
			word      uint32
			typeID    elf.R_RISCV
			externals map[uint32]externalSymbol
			want      string
		}{
			{name: "missing pair", object: &objectFile{}, word: 0x00028293, typeID: elf.R_RISCV_PCREL_LO12_I, want: "no validated HI20 pair"},
			{name: "bad high instruction", object: riscvLowPairObject(elf.R_RISCV_PCREL_HI20, 0x1800, 0x00000013, 4), word: 0x00028293, typeID: elf.R_RISCV_PCREL_LO12_I, want: "expected AUIPC"},
			{name: "I instruction class", object: riscvLowPairObject(elf.R_RISCV_PCREL_HI20, 0x1800, 0x00000297, 4), word: 0x000282b3, typeID: elf.R_RISCV_PCREL_LO12_I, want: "expected I-type"},
			{name: "I register mismatch", object: riscvLowPairObject(elf.R_RISCV_PCREL_HI20, 0x1800, 0x00000297, 4), word: 0x00030293, typeID: elf.R_RISCV_PCREL_LO12_I, want: "does not match"},
			{name: "S instruction class", object: riscvLowPairObject(elf.R_RISCV_PCREL_HI20, 0x1800, 0x00000297, 4), word: 0x00a282b3, typeID: elf.R_RISCV_PCREL_LO12_S, want: "expected S-type"},
			{name: "missing GOT", object: riscvLowPairObject(elf.R_RISCV_GOT_HI20, 0x1800, 0x00000297, 4), word: 0x0002b283, typeID: elf.R_RISCV_PCREL_LO12_I, want: "none was allocated"},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				location := riscvWords(test.word)
				before := append([]byte(nil), location...)
				relocation := objectRelocation{section: 0, offset: 4, typeID: uint32(test.typeID), hasAdd: true}
				err := applyELFRISCV64Relocation(test.object, relocation, location, 0x1004, linkedSymbol{}, test.externals)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("error = %v, want substring %q", err, test.want)
				}
				if !bytes.Equal(location, before) {
					t.Fatalf("location changed on rejection: %x -> %x", before, location)
				}
			})
		}
	})
}

func TestRISCVPCRelativeRangeBoundaries(t *testing.T) {
	const (
		minimum = -((1 << 19) << 12) - (1 << 11)
		maximum = (((1 << 19) - 1) << 12) + (1<<11 - 1)
	)
	for _, value := range []int64{minimum, -2049, -2048, -1, 0, 2047, 2048, maximum} {
		high, low, err := riscvSplitPCRelative(value)
		if err != nil {
			t.Errorf("riscvSplitPCRelative(%d) error = %v", value, err)
			continue
		}
		if got := (high << 12) + low; got != value {
			t.Errorf("riscvSplitPCRelative(%d) reconstructs %d", value, got)
		}
		if low < -(1<<11) || low > (1<<11)-1 {
			t.Errorf("riscvSplitPCRelative(%d) low = %d", value, low)
		}
	}
	for _, value := range []int64{minimum - 1, maximum + 1} {
		if _, _, err := riscvSplitPCRelative(value); err == nil {
			t.Errorf("riscvSplitPCRelative(%d) accepted out-of-range displacement", value)
		}
	}
}

func TestWriteRISCV64Thunk(t *testing.T) {
	buffer := bytes.Repeat([]byte{0xff}, 16)
	if err := writeRISCV64Thunk(buffer, 0x1000, 0x2808); err != nil {
		t.Fatal(err)
	}
	want := []uint32{0x00002297, 0x8082b283, 0x00028067, 0x00000013}
	for index, word := range want {
		if got := binary.LittleEndian.Uint32(buffer[index*4:]); got != word {
			t.Errorf("RISC-V thunk word %d = %#08x, want %#08x", index, got, word)
		}
	}

	short := make([]byte, 15)
	if err := writeRISCV64Thunk(short, 0x1000, 0x2000); err == nil || !strings.Contains(err.Error(), "16-byte") {
		t.Fatalf("short thunk error = %v", err)
	}
	before := bytes.Repeat([]byte{0xaa}, 16)
	outOfRange := append([]byte(nil), before...)
	if err := writeRISCV64Thunk(outOfRange, 0, 0x7ffff800); err == nil || !strings.Contains(err.Error(), "signed AUIPC/LO12 range") {
		t.Fatalf("out-of-range thunk error = %v", err)
	}
	if !bytes.Equal(outOfRange, before) {
		t.Fatalf("out-of-range thunk mutated destination: %x", outOfRange)
	}
}

func riscvLowPairObject(highType elf.R_RISCV, target uint64, highWord uint32, lowOffset uint64) *objectFile {
	sectionData := make([]byte, lowOffset+4)
	binary.LittleEndian.PutUint32(sectionData, highWord)
	return &objectFile{
		format: "elf",
		arch:   "riscv64",
		sections: []objectSection{{
			name:    ".text",
			data:    sectionData,
			size:    uint64(len(sectionData)),
			address: 0x1000,
			mapped:  true,
		}},
		symbols: map[uint32]objectSymbol{
			1: {index: 1, name: "target", section: sectionAbsolute, value: target},
		},
		riscvPairs: map[riscvRelocationSite]riscvRelocationPair{
			{section: 0, offset: lowOffset}: {typeID: uint32(highType), symbol: 1, offset: 0},
		},
	}
}

func riscvWords(words ...uint32) []byte {
	result := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(result[index*4:], word)
	}
	return result
}

func riscvHighImmediate(location []byte) int64 {
	return signExtend(uint64(binary.LittleEndian.Uint32(location)>>12), 20)
}

func riscvIImmediate(location []byte) int64 {
	return signExtend(uint64(binary.LittleEndian.Uint32(location)>>20), 12)
}

func riscvSImmediate(location []byte) int64 {
	word := binary.LittleEndian.Uint32(location)
	immediate := ((word >> 25) << 5) | ((word >> 7) & 0x1f)
	return signExtend(uint64(immediate), 12)
}

func riscvCallDisplacement(location []byte) int64 {
	return (riscvHighImmediate(location[:4]) << 12) + riscvIImmediate(location[4:8])
}
