package bofloader

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"strings"
	"testing"
)

func TestParseELFPPC64LEFixtureRelocationsAndLocalEntry(t *testing.T) {
	object, err := parseELF(buildPPC64LEELFObject())
	if err != nil {
		t.Fatalf("parseELF() error = %v", err)
	}
	if object.format != "elf" || object.arch != "ppc64le" {
		t.Fatalf("parsed object = %s/%s, want elf/ppc64le", object.format, object.arch)
	}
	if local := object.symbols[3]; local.name != "local_fn" || local.localEntry != 8 {
		t.Fatalf("parsed local-entry symbol = %#v, want local_fn +8", local)
	}
	wantTypes := []elf.R_PPC64{
		elf.R_PPC64_REL16_HA,
		elf.R_PPC64_REL16_LO,
		elf.R_PPC64_REL24,
		elf.R_PPC64_TOC16_HA,
		elf.R_PPC64_TOC16_LO,
		elf.R_PPC64_TOC16_LO_DS,
		elf.R_PPC64_ADDR64,
		elf.R_PPC64_NONE,
	}
	if len(object.relocations) != len(wantTypes) {
		t.Fatalf("parsed relocation count = %d, want %d", len(object.relocations), len(wantTypes))
	}
	for index, want := range wantTypes {
		got := object.relocations[index]
		if got.section != 0 || got.typeID != uint32(want) || !got.hasAdd {
			t.Errorf("relocation %d = %#v, want type %s RELA in section 0", index, got, want)
		}
	}
	imports := objectImports(object, referencedLinkageSymbols(object))
	if len(imports) != 1 || imports[0].Name != "BeaconOutput" {
		t.Fatalf("imports = %#v, want only BeaconOutput with synthetic .TOC. omitted", imports)
	}
}

func TestParseELFPPC64LERejectsClassAndABIFlags(t *testing.T) {
	t.Run("ELF32", func(t *testing.T) {
		image := make([]byte, 52)
		copy(image[:4], []byte{0x7f, 'E', 'L', 'F'})
		image[elf.EI_CLASS] = byte(elf.ELFCLASS32)
		image[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
		image[elf.EI_VERSION] = byte(elf.EV_CURRENT)
		binary.LittleEndian.PutUint16(image[16:18], uint16(elf.ET_REL))
		binary.LittleEndian.PutUint16(image[18:20], uint16(elf.EM_PPC64))
		binary.LittleEndian.PutUint32(image[20:24], uint32(elf.EV_CURRENT))
		binary.LittleEndian.PutUint16(image[40:42], 52)
		binary.LittleEndian.PutUint16(image[46:48], 40)
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "unsupported ELF machine/class") {
			t.Fatalf("parseELF() error = %v, want class rejection", err)
		}
	})
	t.Run("big endian", func(t *testing.T) {
		image := buildPPC64LEELFObject()
		image[elf.EI_DATA] = byte(elf.ELFDATA2MSB)
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "only little-endian ELF objects are supported") {
			t.Fatalf("parseELF() error = %v, want endianness rejection", err)
		}
	})

	for _, flags := range []uint32{0, 1, 3, 6} {
		t.Run("flags", func(t *testing.T) {
			image := buildPPC64LEELFObject()
			binary.LittleEndian.PutUint32(image[48:52], flags)
			if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "require the ELFv2 ABI flags") {
				t.Fatalf("parseELF(flags=%#x) error = %v, want ELFv2 rejection", flags, err)
			}
		})
	}
}

func TestParseELFPPC64LEAcceptsSupportedLocalEntryEncodings(t *testing.T) {
	for _, test := range []struct {
		encoding byte
		want     uint64
	}{
		{encoding: 0, want: 0},
		{encoding: 2, want: 4},
		{encoding: 3, want: 8},
	} {
		image := buildPPC64LEELFObject()
		image[ppc64TestSymbolOtherOffset(image, 3)] = test.encoding << 5
		object, err := parseELF(image)
		if err != nil {
			t.Errorf("parseELF(local-entry encoding %d) error = %v", test.encoding, err)
			continue
		}
		if got := object.symbols[3].localEntry; got != test.want {
			t.Errorf("local-entry encoding %d decoded to %#x, want %#x", test.encoding, got, test.want)
		}
	}
}

func TestParseELFPPC64LERejectsUnsupportedLocalEntryEncodings(t *testing.T) {
	for _, test := range []struct {
		name     string
		encoding byte
		want     string
	}{
		{name: "NOTOC encoding one", encoding: 1, want: "unsupported local-entry encoding 1"},
		{name: "reserved encoding seven", encoding: 7, want: "reserved local-entry encoding 7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			image := buildPPC64LEELFObject()
			image[ppc64TestSymbolOtherOffset(image, 3)] = test.encoding << 5
			if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parseELF() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestParseELFPPC64LERejectsRELAndRestoreSlotOverlap(t *testing.T) {
	t.Run("REL encoding", func(t *testing.T) {
		image := buildPPC64LEELFObject()
		const relocationSectionType = 64 + 2*64 + 4
		binary.LittleEndian.PutUint32(image[relocationSectionType:], uint32(elf.SHT_REL))
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "must use RELA encoding") {
			t.Fatalf("parseELF() error = %v, want RELA rejection", err)
		}
	})

	for _, relocationIndex := range []int{0, 6} {
		t.Run("overlap", func(t *testing.T) {
			image := buildPPC64LEELFObject()
			binary.LittleEndian.PutUint64(image[ppc64TestRelocationOffset(image, relocationIndex):], 12)
			if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "overlaps PPC64 external-call TOC restore slot") {
				t.Fatalf("parseELF() error = %v, want restore-slot overlap rejection", err)
			}
		})
	}
}

func buildPPC64LEELFObject() []byte {
	const (
		headerSize        = 64
		sectionHeaderSize = 64
		sectionCount      = 6
	)
	words := []uint32{
		0x3c4c0000, // addis r2,r12,0 for REL16_HA .TOC.
		0x38420000, // addi r2,r2,0 for REL16_LO .TOC.
		0x48000001, // bl external for REL24.
		ppc64NOP,   // linker-reserved TOC restore slot.
		0x3c620000, // addis r3,r2,0 for TOC16_HA.
		0x38630000, // addi r3,r3,0 for TOC16_LO.
		0xe8630000, // ld r3,0(r3) for TOC16_LO_DS.
		ppc64NOP,
		0,
		0, // ADDR64 location.
		ppc64NOP,
		ppc64NOP,
		ppc64NOP,
	}
	text := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(text[index*4:], word)
	}
	type fixtureRelocation struct {
		offset uint64
		typeID elf.R_PPC64
		symbol uint32
		addend int64
	}
	relocations := []fixtureRelocation{
		{offset: 0, typeID: elf.R_PPC64_REL16_HA, symbol: 1},
		{offset: 4, typeID: elf.R_PPC64_REL16_LO, symbol: 1, addend: 4},
		{offset: 8, typeID: elf.R_PPC64_REL24, symbol: 2},
		{offset: 16, typeID: elf.R_PPC64_TOC16_HA, symbol: 3},
		{offset: 20, typeID: elf.R_PPC64_TOC16_LO, symbol: 3},
		{offset: 24, typeID: elf.R_PPC64_TOC16_LO_DS, symbol: 3},
		{offset: 32, typeID: elf.R_PPC64_ADDR64, symbol: 2},
		{offset: 40, typeID: elf.R_PPC64_NONE, symbol: 0},
	}
	rela := make([]byte, len(relocations)*24)
	for index, relocation := range relocations {
		offset := index * 24
		binary.LittleEndian.PutUint64(rela[offset:], relocation.offset)
		binary.LittleEndian.PutUint64(rela[offset+8:], uint64(relocation.symbol)<<32|uint64(relocation.typeID))
		binary.LittleEndian.PutUint64(rela[offset+16:], uint64(relocation.addend))
	}
	strtab := []byte("\x00.TOC.\x00BeaconOutput\x00local_fn\x00go\x00")
	shstrtab := []byte("\x00.text\x00.rela.text\x00.symtab\x00.strtab\x00.shstrtab\x00")
	nameOffset := func(table []byte, name string) uint32 { return uint32(bytes.Index(table, []byte(name))) }
	align8 := func(value int) int { return (value + 7) &^ 7 }

	textOffset := align8(headerSize + sectionCount*sectionHeaderSize)
	relocationOffset := align8(textOffset + len(text))
	symbolOffset := align8(relocationOffset + len(rela))
	stringOffset := symbolOffset + 5*24
	sectionStringOffset := stringOffset + len(strtab)
	image := make([]byte, sectionStringOffset+len(shstrtab))

	copy(image[:4], []byte{0x7f, 'E', 'L', 'F'})
	image[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	image[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	image[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(image[16:18], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(image[18:20], uint16(elf.EM_PPC64))
	binary.LittleEndian.PutUint32(image[20:24], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint64(image[40:48], headerSize)
	binary.LittleEndian.PutUint32(image[48:52], ppc64ELFABI2)
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
	writeSection(1, nameOffset(shstrtab, ".text"), elf.SHT_PROGBITS, elf.SHF_ALLOC|elf.SHF_EXECINSTR, textOffset, len(text), 0, 0, 4, 0)
	writeSection(2, nameOffset(shstrtab, ".rela.text"), elf.SHT_RELA, 0, relocationOffset, len(rela), 3, 1, 8, 24)
	writeSection(3, nameOffset(shstrtab, ".symtab"), elf.SHT_SYMTAB, 0, symbolOffset, 5*24, 4, 1, 8, 24)
	writeSection(4, nameOffset(shstrtab, ".strtab"), elf.SHT_STRTAB, 0, stringOffset, len(strtab), 0, 0, 1, 0)
	writeSection(5, nameOffset(shstrtab, ".shstrtab"), elf.SHT_STRTAB, 0, sectionStringOffset, len(shstrtab), 0, 0, 1, 0)

	copy(image[textOffset:], text)
	copy(image[relocationOffset:], rela)
	writeSymbol := func(index int, name string, binding elf.SymBind, symbolType elf.SymType, other byte, section elf.SectionIndex, value, size uint64) {
		start := symbolOffset + index*24
		binary.LittleEndian.PutUint32(image[start:], nameOffset(strtab, name))
		image[start+4] = elf.ST_INFO(binding, symbolType)
		image[start+5] = other
		binary.LittleEndian.PutUint16(image[start+6:], uint16(section))
		binary.LittleEndian.PutUint64(image[start+8:], value)
		binary.LittleEndian.PutUint64(image[start+16:], size)
	}
	writeSymbol(1, ".TOC.", elf.STB_GLOBAL, elf.STT_NOTYPE, 0, elf.SHN_UNDEF, 0, 0)
	writeSymbol(2, "BeaconOutput", elf.STB_GLOBAL, elf.STT_FUNC, 0, elf.SHN_UNDEF, 0, 0)
	writeSymbol(3, "local_fn", elf.STB_GLOBAL, elf.STT_FUNC, 0x60, 1, 40, 12)
	writeSymbol(4, "go", elf.STB_GLOBAL, elf.STT_FUNC, 0, 1, 0, uint64(len(text)))
	copy(image[stringOffset:], strtab)
	copy(image[sectionStringOffset:], shstrtab)
	return image
}

func ppc64TestSymbolOtherOffset(image []byte, symbolIndex int) int {
	sectionTable := int(binary.LittleEndian.Uint64(image[40:48]))
	symbolHeader := sectionTable + 3*64
	symbolTable := int(binary.LittleEndian.Uint64(image[symbolHeader+24 : symbolHeader+32]))
	return symbolTable + symbolIndex*24 + 5
}

func ppc64TestRelocationOffset(image []byte, relocationIndex int) int {
	sectionTable := int(binary.LittleEndian.Uint64(image[40:48]))
	relocationHeader := sectionTable + 2*64
	relocationTable := int(binary.LittleEndian.Uint64(image[relocationHeader+24 : relocationHeader+32]))
	return relocationTable + relocationIndex*24
}
