package bofloader

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseELFARM32FixtureRelocations(t *testing.T) {
	types := []elf.R_ARM{
		elf.R_ARM_CALL,
		elf.R_ARM_JUMP24,
		elf.R_ARM_REL32,
		elf.R_ARM_GOT_PREL,
		elf.R_ARM_PREL31,
	}
	words := []uint32{0xebfffffe, 0xeafffffe, 0x60, 0x10, 0}
	relocations := make([]armELFRelocationFixture, len(types))
	for index, typeID := range types {
		relocations[index] = armELFRelocationFixture{
			offset: uint32(index * 4),
			typeID: typeID,
			symbol: 1,
		}
	}

	object, err := parseELF(buildARMELF32Object(words, relocations))
	if err != nil {
		t.Fatalf("parseELF() error = %v", err)
	}
	if object.format != "elf" || object.arch != "arm" {
		t.Fatalf("parsed object = %s/%s, want elf/arm", object.format, object.arch)
	}
	if len(object.sections) != 1 || object.sections[0].name != ".text" {
		t.Fatalf("parsed sections = %#v, want one .text section", object.sections)
	}
	if symbol := object.symbols[1]; symbol.name != "target" || symbol.section != sectionUndefined {
		t.Fatalf("parsed symbol = %#v, want undefined target", symbol)
	}
	if len(object.relocations) != len(types) {
		t.Fatalf("parsed relocation count = %d, want %d", len(object.relocations), len(types))
	}
	for index, wantType := range types {
		got := object.relocations[index]
		if got.section != 0 || got.offset != uint64(index*4) || got.typeID != uint32(wantType) || got.symbol != 1 || got.hasAdd {
			t.Errorf("relocation %d = %#v, want section 0 offset %#x type %s symbol 1 REL encoding", index, got, index*4, wantType)
		}
	}
}

func TestParseELFARM32RejectsInvalidRelocationSymbol(t *testing.T) {
	image := buildARMELF32Object(
		[]uint32{0xebfffffe},
		[]armELFRelocationFixture{{typeID: elf.R_ARM_CALL, symbol: 2}},
	)
	if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "invalid symbol 2") {
		t.Fatalf("parseELF() error = %v, want invalid-symbol error", err)
	}
}

func TestParseELFARMRejectsELF64Class(t *testing.T) {
	image := overlappingELF64Sections(0, 0)
	binary.LittleEndian.PutUint16(image[18:20], uint16(elf.EM_ARM))
	if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "unsupported ELF machine/class") {
		t.Fatalf("parseELF() error = %v, want machine/class rejection", err)
	}
}

func TestParseELFARMRejectsUnsupportedABIFlags(t *testing.T) {
	tests := []struct {
		name  string
		flags uint32
	}{
		{name: "EABI4 hard-float", flags: 0x04000400},
		{name: "EABI5 unspecified float ABI", flags: 0x05000000},
		{name: "EABI5 soft-float", flags: 0x05000200},
		{name: "EABI5 conflicting float ABI", flags: 0x05000600},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			image := buildARMELF32Object([]uint32{0xe1a00000}, nil)
			binary.LittleEndian.PutUint32(image[36:40], test.flags)
			if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "require EABI5 hard-float flags") {
				t.Fatalf("parseELF() error = %v, want ARM ABI flag rejection", err)
			}
		})
	}
}

type armELFRelocationFixture struct {
	offset uint32
	typeID elf.R_ARM
	symbol uint32
}

func buildARMELF32Object(words []uint32, relocations []armELFRelocationFixture) []byte {
	const (
		headerSize        = 52
		sectionHeaderSize = 40
		sectionCount      = 6
	)
	text := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(text[index*4:], word)
	}
	rel := make([]byte, len(relocations)*8)
	for index, relocation := range relocations {
		offset := index * 8
		binary.LittleEndian.PutUint32(rel[offset:], relocation.offset)
		binary.LittleEndian.PutUint32(rel[offset+4:], elf.R_INFO32(relocation.symbol, uint32(relocation.typeID)))
	}
	strtab := []byte("\x00target\x00")
	shstrtab := []byte("\x00.text\x00.rel.text\x00.symtab\x00.strtab\x00.shstrtab\x00")
	nameOffset := func(name string) uint32 {
		return uint32(bytes.Index(shstrtab, []byte(name)))
	}
	align4 := func(value int) int { return (value + 3) &^ 3 }

	textOffset := align4(headerSize + sectionCount*sectionHeaderSize)
	relocationOffset := align4(textOffset + len(text))
	symbolOffset := align4(relocationOffset + len(rel))
	stringOffset := symbolOffset + 2*16
	sectionStringOffset := stringOffset + len(strtab)
	image := make([]byte, sectionStringOffset+len(shstrtab))

	copy(image[:4], []byte{0x7f, 'E', 'L', 'F'})
	image[elf.EI_CLASS] = byte(elf.ELFCLASS32)
	image[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	image[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(image[16:18], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(image[18:20], uint16(elf.EM_ARM))
	binary.LittleEndian.PutUint32(image[20:24], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint32(image[32:36], headerSize)
	binary.LittleEndian.PutUint32(image[36:40], armELFEABI5|armELFFloatHard)
	binary.LittleEndian.PutUint16(image[40:42], headerSize)
	binary.LittleEndian.PutUint16(image[46:48], sectionHeaderSize)
	binary.LittleEndian.PutUint16(image[48:50], sectionCount)
	binary.LittleEndian.PutUint16(image[50:52], 5)

	writeSection := func(index int, name uint32, sectionType elf.SectionType, flags elf.SectionFlag, offset, size int, link, info, alignment, entrySize uint32) {
		start := headerSize + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(image[start:], name)
		binary.LittleEndian.PutUint32(image[start+4:], uint32(sectionType))
		binary.LittleEndian.PutUint32(image[start+8:], uint32(flags))
		binary.LittleEndian.PutUint32(image[start+16:], uint32(offset))
		binary.LittleEndian.PutUint32(image[start+20:], uint32(size))
		binary.LittleEndian.PutUint32(image[start+24:], link)
		binary.LittleEndian.PutUint32(image[start+28:], info)
		binary.LittleEndian.PutUint32(image[start+32:], alignment)
		binary.LittleEndian.PutUint32(image[start+36:], entrySize)
	}
	writeSection(1, nameOffset(".text"), elf.SHT_PROGBITS, elf.SHF_ALLOC|elf.SHF_EXECINSTR, textOffset, len(text), 0, 0, 4, 0)
	writeSection(2, nameOffset(".rel.text"), elf.SHT_REL, 0, relocationOffset, len(rel), 3, 1, 4, 8)
	writeSection(3, nameOffset(".symtab"), elf.SHT_SYMTAB, 0, symbolOffset, 2*16, 4, 1, 4, 16)
	writeSection(4, nameOffset(".strtab"), elf.SHT_STRTAB, 0, stringOffset, len(strtab), 0, 0, 1, 0)
	writeSection(5, nameOffset(".shstrtab"), elf.SHT_STRTAB, 0, sectionStringOffset, len(shstrtab), 0, 0, 1, 0)

	copy(image[textOffset:], text)
	copy(image[relocationOffset:], rel)
	defined := symbolOffset + 16
	binary.LittleEndian.PutUint32(image[defined:], 1)
	image[defined+12] = elf.ST_INFO(elf.STB_GLOBAL, elf.STT_NOTYPE)
	binary.LittleEndian.PutUint16(image[defined+14:], uint16(elf.SHN_UNDEF))
	copy(image[stringOffset:], strtab)
	copy(image[sectionStringOffset:], shstrtab)
	return image
}
