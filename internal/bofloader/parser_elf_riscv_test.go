package bofloader

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseELFRISCV64FixtureRelocations(t *testing.T) {
	object, err := parseELF(buildRISCV64ELFObject())
	if err != nil {
		t.Fatalf("parseELF() error = %v", err)
	}
	if object.format != "elf" || object.arch != "riscv64" {
		t.Fatalf("parsed object = %s/%s, want elf/riscv64", object.format, object.arch)
	}
	if len(object.relocations) != 6 {
		t.Fatalf("parsed relocation count = %d, want 6", len(object.relocations))
	}
	// The input deliberately lists both LO12 relocations before one or both of
	// their HI20 partners. Pairing must not depend on relocation-table order.
	wantPairs := map[riscvRelocationSite]riscvRelocationPair{
		{section: 0, offset: 4}:  {typeID: uint32(elf.R_RISCV_PCREL_HI20), symbol: 3, offset: 0},
		{section: 0, offset: 12}: {typeID: uint32(elf.R_RISCV_GOT_HI20), symbol: 3, offset: 8},
	}
	if len(object.riscvPairs) != len(wantPairs) {
		t.Fatalf("pair count = %d, want %d", len(object.riscvPairs), len(wantPairs))
	}
	for site, want := range wantPairs {
		if got := object.riscvPairs[site]; got != want {
			t.Errorf("pair at %#x = %#v, want %#v", site.offset, got, want)
		}
	}
}

func TestParseELFRISCV64RejectsUnsupportedClassAndFlags(t *testing.T) {
	t.Run("ELF32 class", func(t *testing.T) {
		image := buildRISCV32ELFObject()
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "unsupported ELF machine/class") {
			t.Fatalf("parseELF() error = %v, want machine/class rejection", err)
		}
	})

	tests := []struct {
		name  string
		flags uint32
		want  string
	}{
		{name: "soft float", flags: riscvELFRVC, want: "require the LP64D double-float ABI"},
		{name: "single float", flags: riscvELFRVC | 0x2, want: "require the LP64D double-float ABI"},
		{name: "quad float", flags: riscvELFRVC | 0x6, want: "require the LP64D double-float ABI"},
		{name: "RVE", flags: riscvELFRVC | riscvELFFloatABIDouble | riscvELFRVE, want: "cannot use the RV32E register ABI"},
		{name: "TSO", flags: riscvELFRVC | riscvELFFloatABIDouble | riscvELFTSO, want: "requiring RVTSO are unsupported"},
		{name: "unknown", flags: riscvELFRVC | riscvELFFloatABIDouble | 0x20, want: "unknown flags"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			image := buildRISCV64ELFObject()
			binary.LittleEndian.PutUint32(image[48:52], test.flags)
			if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseELF() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestParseELFRISCV64AcceptsOptionalKnownFlags(t *testing.T) {
	for _, flags := range []uint32{riscvELFFloatABIDouble, riscvELFRVC | riscvELFFloatABIDouble} {
		image := buildRISCV64ELFObject()
		binary.LittleEndian.PutUint32(image[48:52], flags)
		if _, err := parseELF(image); err != nil {
			t.Errorf("parseELF(flags=%#x) error = %v", flags, err)
		}
	}
}

func TestParseELFRISCV64RejectsRELAndMissingPairs(t *testing.T) {
	t.Run("REL encoding", func(t *testing.T) {
		image := buildRISCV64ELFObject()
		const relocationSectionType = 64 + 2*64 + 4
		binary.LittleEndian.PutUint32(image[relocationSectionType:], uint32(elf.SHT_REL))
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "must use RELA encoding") {
			t.Fatalf("parseELF() error = %v, want RELA rejection", err)
		}
	})

	t.Run("missing HI20", func(t *testing.T) {
		image := buildRISCV64ELFObject()
		const (
			headerAndSections = 64 + 6*64
			textSize          = 32
			firstHighType     = headerAndSections + textSize + 2*24 + 8
		)
		// The third RELA entry is the GOT_HI20 paired with the first entry.
		info := binary.LittleEndian.Uint64(image[firstHighType:])
		binary.LittleEndian.PutUint64(image[firstHighType:], riscvELFInfo64(elf.R_SYM64(info), uint32(elf.R_RISCV_64)))
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "has no HI20 pair") {
			t.Fatalf("parseELF() error = %v, want missing-pair rejection", err)
		}
	})

	t.Run("non-zero LO12 addend", func(t *testing.T) {
		image := buildRISCV64ELFObject()
		const (
			headerAndSections = 64 + 6*64
			textSize          = 32
			firstLOAddend     = headerAndSections + textSize + 16
		)
		binary.LittleEndian.PutUint64(image[firstLOAddend:], 4)
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "must have a zero addend") {
			t.Fatalf("parseELF() error = %v, want non-zero LO12 addend rejection", err)
		}
	})

	t.Run("non-zero GOT_HI20 addend", func(t *testing.T) {
		image := buildRISCV64ELFObject()
		const (
			headerAndSections = 64 + 6*64
			textSize          = 32
			gotHighAddend     = headerAndSections + textSize + 2*24 + 16
		)
		binary.LittleEndian.PutUint64(image[gotHighAddend:], 1)
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "GOT_HI20 relocation") || !strings.Contains(err.Error(), "zero addend") {
			t.Fatalf("parseELF() error = %v, want non-zero GOT_HI20 addend rejection", err)
		}
	})
}

func buildRISCV64ELFObject() []byte {
	const (
		headerSize        = 64
		sectionHeaderSize = 64
		sectionCount      = 6
	)
	words := []uint32{
		0x00000297, // AUIPC t0 for PCREL_HI20.
		0x00028293, // ADDI t0, t0 for PCREL_LO12_I.
		0x00000297, // AUIPC t0 for GOT_HI20.
		0x0002b283, // LD t0, 0(t0) for PCREL_LO12_I.
		0x00000097, // AUIPC ra for CALL_PLT.
		0x000080e7, // JALR ra, 0(ra) for CALL_PLT.
		0,
		0, // R_RISCV_64 location.
	}
	text := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(text[index*4:], word)
	}
	type fixtureRelocation struct {
		offset uint64
		typeID elf.R_RISCV
		symbol uint32
		addend int64
	}
	relocations := []fixtureRelocation{
		{offset: 12, typeID: elf.R_RISCV_PCREL_LO12_I, symbol: 2},
		{offset: 16, typeID: elf.R_RISCV_CALL_PLT, symbol: 3},
		{offset: 8, typeID: elf.R_RISCV_GOT_HI20, symbol: 3},
		{offset: 4, typeID: elf.R_RISCV_PCREL_LO12_I, symbol: 1},
		{offset: 24, typeID: elf.R_RISCV_64, symbol: 3},
		{offset: 0, typeID: elf.R_RISCV_PCREL_HI20, symbol: 3},
	}
	rela := make([]byte, len(relocations)*24)
	for index, relocation := range relocations {
		offset := index * 24
		binary.LittleEndian.PutUint64(rela[offset:], relocation.offset)
		binary.LittleEndian.PutUint64(rela[offset+8:], riscvELFInfo64(relocation.symbol, uint32(relocation.typeID)))
		binary.LittleEndian.PutUint64(rela[offset+16:], uint64(relocation.addend))
	}
	strtab := []byte("\x00.Lpcrel_hi0\x00.Lpcrel_hi1\x00target\x00")
	shstrtab := []byte("\x00.text\x00.rela.text\x00.symtab\x00.strtab\x00.shstrtab\x00")
	nameOffset := func(table []byte, name string) uint32 { return uint32(bytes.Index(table, []byte(name))) }
	align8 := func(value int) int { return (value + 7) &^ 7 }

	textOffset := align8(headerSize + sectionCount*sectionHeaderSize)
	relocationOffset := align8(textOffset + len(text))
	symbolOffset := align8(relocationOffset + len(rela))
	stringOffset := symbolOffset + 4*24
	sectionStringOffset := stringOffset + len(strtab)
	image := make([]byte, sectionStringOffset+len(shstrtab))

	copy(image[:4], []byte{0x7f, 'E', 'L', 'F'})
	image[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	image[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	image[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(image[16:18], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(image[18:20], uint16(elf.EM_RISCV))
	binary.LittleEndian.PutUint32(image[20:24], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint64(image[40:48], headerSize)
	binary.LittleEndian.PutUint32(image[48:52], riscvELFRVC|riscvELFFloatABIDouble)
	binary.LittleEndian.PutUint16(image[52:54], headerSize)
	binary.LittleEndian.PutUint16(image[58:60], sectionHeaderSize)
	binary.LittleEndian.PutUint16(image[60:62], sectionCount)
	binary.LittleEndian.PutUint16(image[62:64], 5)

	writeSection := func(index int, name uint32, sectionType elf.SectionType, flags elf.SectionFlag, offset, size int, link, info uint32, alignment, entrySize uint64) {
		start := headerSize + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(image[start:], name)
		binary.LittleEndian.PutUint32(image[start+4:], uint32(sectionType))
		binary.LittleEndian.PutUint64(image[start+8:], uint64(flags))
		binary.LittleEndian.PutUint64(image[start+24:], uint64(offset))
		binary.LittleEndian.PutUint64(image[start+32:], uint64(size))
		binary.LittleEndian.PutUint32(image[start+40:], link)
		binary.LittleEndian.PutUint32(image[start+44:], info)
		binary.LittleEndian.PutUint64(image[start+48:], alignment)
		binary.LittleEndian.PutUint64(image[start+56:], entrySize)
	}
	writeSection(1, nameOffset(shstrtab, ".text"), elf.SHT_PROGBITS, elf.SHF_ALLOC|elf.SHF_EXECINSTR, textOffset, len(text), 0, 0, 2, 0)
	writeSection(2, nameOffset(shstrtab, ".rela.text"), elf.SHT_RELA, 0, relocationOffset, len(rela), 3, 1, 8, 24)
	writeSection(3, nameOffset(shstrtab, ".symtab"), elf.SHT_SYMTAB, 0, symbolOffset, 4*24, 4, 3, 8, 24)
	writeSection(4, nameOffset(shstrtab, ".strtab"), elf.SHT_STRTAB, 0, stringOffset, len(strtab), 0, 0, 1, 0)
	writeSection(5, nameOffset(shstrtab, ".shstrtab"), elf.SHT_STRTAB, 0, sectionStringOffset, len(shstrtab), 0, 0, 1, 0)

	copy(image[textOffset:], text)
	copy(image[relocationOffset:], rela)
	writeSymbol := func(index int, name string, binding elf.SymBind, section elf.SectionIndex, value uint64) {
		start := symbolOffset + index*24
		binary.LittleEndian.PutUint32(image[start:], nameOffset(strtab, name))
		image[start+4] = elf.ST_INFO(binding, elf.STT_NOTYPE)
		binary.LittleEndian.PutUint16(image[start+6:], uint16(section))
		binary.LittleEndian.PutUint64(image[start+8:], value)
	}
	writeSymbol(1, ".Lpcrel_hi0", elf.STB_LOCAL, 1, 0)
	writeSymbol(2, ".Lpcrel_hi1", elf.STB_LOCAL, 1, 8)
	writeSymbol(3, "target", elf.STB_GLOBAL, elf.SHN_UNDEF, 0)
	copy(image[stringOffset:], strtab)
	copy(image[sectionStringOffset:], shstrtab)
	return image
}

func buildRISCV32ELFObject() []byte {
	const (
		headerSize        = 52
		sectionHeaderSize = 40
		sectionCount      = 4
	)
	table := []byte("\x00.text\x00.symtab\x00.strtab\x00go\x00")
	nameOffset := func(name string) uint32 { return uint32(bytes.Index(table, []byte(name))) }
	align4 := func(value int) int { return (value + 3) &^ 3 }
	textOffset := align4(headerSize + sectionCount*sectionHeaderSize)
	symbolOffset := align4(textOffset + 4)
	stringOffset := symbolOffset + 2*16
	image := make([]byte, stringOffset+len(table))

	copy(image[:4], []byte{0x7f, 'E', 'L', 'F'})
	image[elf.EI_CLASS] = byte(elf.ELFCLASS32)
	image[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	image[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(image[16:18], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(image[18:20], uint16(elf.EM_RISCV))
	binary.LittleEndian.PutUint32(image[20:24], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint32(image[32:36], headerSize)
	binary.LittleEndian.PutUint16(image[40:42], headerSize)
	binary.LittleEndian.PutUint16(image[46:48], sectionHeaderSize)
	binary.LittleEndian.PutUint16(image[48:50], sectionCount)
	binary.LittleEndian.PutUint16(image[50:52], 3)

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
	writeSection(1, nameOffset(".text"), elf.SHT_PROGBITS, elf.SHF_ALLOC|elf.SHF_EXECINSTR, textOffset, 4, 0, 0, 4, 0)
	writeSection(2, nameOffset(".symtab"), elf.SHT_SYMTAB, 0, symbolOffset, 2*16, 3, 1, 4, 16)
	writeSection(3, nameOffset(".strtab"), elf.SHT_STRTAB, 0, stringOffset, len(table), 0, 0, 1, 0)

	copy(image[textOffset:], riscvWords(0x00000013))
	defined := symbolOffset + 16
	binary.LittleEndian.PutUint32(image[defined:], nameOffset("go"))
	image[defined+12] = elf.ST_INFO(elf.STB_GLOBAL, elf.STT_FUNC)
	binary.LittleEndian.PutUint16(image[defined+14:], 1)
	copy(image[stringOffset:], table)
	return image
}

func riscvELFInfo64(symbol, typeID uint32) uint64 {
	return uint64(symbol)<<32 | uint64(typeID)
}
