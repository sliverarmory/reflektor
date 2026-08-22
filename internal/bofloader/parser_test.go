package bofloader

import (
	"debug/elf"
	"debug/pe"
	"encoding/binary"
	"runtime"
	"strings"
	"testing"
)

func TestLoadRejectsMalformedObjects(t *testing.T) {
	tests := []struct {
		name  string
		image []byte
	}{
		{name: "truncated ELF", image: []byte{0x7f, 'E', 'L', 'F'}},
		{name: "truncated COFF", image: []byte{0x64, 0x86, 0, 0}},
		{name: "random", image: []byte("not-an-object")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if loaded, err := Load(test.image); err == nil {
				_ = loaded.Close()
				t.Fatal("Load accepted malformed object")
			}
		})
	}
}

func TestParsersRejectOverlappingSectionAllocationAmplification(t *testing.T) {
	const sectionSize = 1 << 20
	// One payload range is deliberately reused by enough mapped sections to
	// exceed maxImageSize. The parser must reject from declared metadata before
	// materializing a copy for every section.
	sectionCount := maxImageSize/sectionSize + 1

	t.Run("ELF", func(t *testing.T) {
		image := overlappingELF64Sections(sectionCount, sectionSize)
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "cumulative ELF mapped section size") {
			t.Fatalf("parseELF() error = %v, want cumulative mapped-section limit", err)
		}
	})

	t.Run("COFF", func(t *testing.T) {
		image := overlappingCOFFSections(sectionCount, sectionSize)
		if _, err := parseCOFF(image); err == nil || !strings.Contains(err.Error(), "cumulative COFF mapped section size") {
			t.Fatalf("parseCOFF() error = %v, want cumulative mapped-section limit", err)
		}
	})
}

func TestCOFFPreflightRejectsExtendedRelocationCounts(t *testing.T) {
	image := overlappingCOFFSections(1, 1)
	const firstSectionHeader = 20
	binary.LittleEndian.PutUint16(image[firstSectionHeader+32:firstSectionHeader+34], ^uint16(0))
	characteristics := binary.LittleEndian.Uint32(image[firstSectionHeader+36 : firstSectionHeader+40])
	binary.LittleEndian.PutUint32(
		image[firstSectionHeader+36:firstSectionHeader+40],
		characteristics|coffSectionRelocationOverflow,
	)
	if err := preflightCOFF(image); err == nil || !strings.Contains(err.Error(), "extended relocation count") {
		t.Fatalf("preflightCOFF() error = %v, want extended-relocation rejection", err)
	}
}

func TestELFPreflightRejectsCompressedSectionsBeforeParsing(t *testing.T) {
	image := overlappingELF64Sections(1, 1)
	const firstSectionFlags = 64 + 64 + 8
	binary.LittleEndian.PutUint64(image[firstSectionFlags:firstSectionFlags+8], uint64(elf.SHF_ALLOC|elf.SHF_COMPRESSED))
	if err := preflightELFHeader(image); err == nil || !strings.Contains(err.Error(), "compressed ELF sections") {
		t.Fatalf("preflightELFHeader() error = %v, want compressed-section rejection", err)
	}
}

func TestELFPreflightValidatesSectionZeroPayloadRange(t *testing.T) {
	image := overlappingELF64Sections(1, 1)
	binary.LittleEndian.PutUint32(image[64+4:64+8], uint32(elf.SHT_PROGBITS))
	binary.LittleEndian.PutUint64(image[64+24:64+32], uint64(len(image)))
	binary.LittleEndian.PutUint64(image[64+32:64+40], 1)
	if err := preflightELFHeader(image); err == nil || !strings.Contains(err.Error(), "section 0 data is outside") {
		t.Fatalf("preflightELFHeader() error = %v, want section-zero range rejection", err)
	}
}

func TestParsersRejectRepeatedLongNameAmplification(t *testing.T) {
	const (
		sectionNameSize = 8 << 10
		symbolNameSize  = maxObjectNameSize - 1
	)

	t.Run("ELF section names", func(t *testing.T) {
		count := maxObjectNameBytes/sectionNameSize + 2
		image := repeatedELFSectionNames(count, sectionNameSize)
		if err := preflightELFHeader(image); err == nil || !strings.Contains(err.Error(), "cumulative ELF section-name bytes") {
			t.Fatalf("preflightELFHeader() error = %v, want cumulative section-name limit", err)
		}
	})

	t.Run("ELF symbol names", func(t *testing.T) {
		count := maxObjectNameBytes/symbolNameSize + 1
		image := repeatedELFSymbolNames(count, symbolNameSize)
		if _, err := parseELF(image); err == nil || !strings.Contains(err.Error(), "cumulative ELF symbol-name bytes") {
			t.Fatalf("parseELF() error = %v, want cumulative symbol-name limit", err)
		}
	})

	t.Run("COFF section names", func(t *testing.T) {
		count := maxObjectNameBytes/sectionNameSize + 1
		image := repeatedCOFFSectionNames(count, sectionNameSize)
		if err := preflightCOFF(image); err == nil || !strings.Contains(err.Error(), "cumulative COFF section-name bytes") {
			t.Fatalf("preflightCOFF() error = %v, want cumulative section-name limit", err)
		}
	})

	t.Run("COFF symbol names", func(t *testing.T) {
		count := maxObjectNameBytes/symbolNameSize + 1
		image := repeatedCOFFSymbolNames(count, symbolNameSize)
		if err := preflightCOFF(image); err == nil || !strings.Contains(err.Error(), "cumulative COFF symbol-name bytes") {
			t.Fatalf("preflightCOFF() error = %v, want cumulative symbol-name limit", err)
		}
	})
}

func repeatedELFSectionNames(sectionCount, nameSize int) []byte {
	const (
		headerSize        = 64
		sectionHeaderSize = 64
	)
	stringTable := make([]byte, nameSize+2)
	for index := 1; index <= nameSize; index++ {
		stringTable[index] = 'n'
	}
	stringOffset := headerSize + sectionCount*sectionHeaderSize
	image := make([]byte, stringOffset+len(stringTable))
	initializeELF64Header(image, sectionCount, 1)
	for index := 1; index < sectionCount; index++ {
		offset := headerSize + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(image[offset:offset+4], 1)
		if index == 1 {
			binary.LittleEndian.PutUint32(image[offset+4:offset+8], uint32(elf.SHT_STRTAB))
			binary.LittleEndian.PutUint64(image[offset+24:offset+32], uint64(stringOffset))
			binary.LittleEndian.PutUint64(image[offset+32:offset+40], uint64(len(stringTable)))
		}
	}
	copy(image[stringOffset:], stringTable)
	return image
}

func repeatedELFSymbolNames(symbolCount, nameSize int) []byte {
	const (
		headerSize        = 64
		sectionHeaderSize = 64
		sectionCount      = 3
		symbolSize        = 24
	)
	symbolOffset := headerSize + sectionCount*sectionHeaderSize
	stringOffset := symbolOffset + symbolCount*symbolSize
	stringTable := make([]byte, nameSize+2)
	for index := 1; index <= nameSize; index++ {
		stringTable[index] = 's'
	}
	image := make([]byte, stringOffset+len(stringTable))
	initializeELF64Header(image, sectionCount, 0)
	symbolHeader := headerSize + sectionHeaderSize
	binary.LittleEndian.PutUint32(image[symbolHeader+4:symbolHeader+8], uint32(elf.SHT_SYMTAB))
	binary.LittleEndian.PutUint64(image[symbolHeader+24:symbolHeader+32], uint64(symbolOffset))
	binary.LittleEndian.PutUint64(image[symbolHeader+32:symbolHeader+40], uint64(symbolCount*symbolSize))
	binary.LittleEndian.PutUint32(image[symbolHeader+40:symbolHeader+44], 2)
	binary.LittleEndian.PutUint64(image[symbolHeader+56:symbolHeader+64], symbolSize)
	stringHeader := headerSize + 2*sectionHeaderSize
	binary.LittleEndian.PutUint32(image[stringHeader+4:stringHeader+8], uint32(elf.SHT_STRTAB))
	binary.LittleEndian.PutUint64(image[stringHeader+24:stringHeader+32], uint64(stringOffset))
	binary.LittleEndian.PutUint64(image[stringHeader+32:stringHeader+40], uint64(len(stringTable)))
	for index := 0; index < symbolCount; index++ {
		binary.LittleEndian.PutUint32(image[symbolOffset+index*symbolSize:], 1)
	}
	copy(image[stringOffset:], stringTable)
	return image
}

func initializeELF64Header(image []byte, sectionCount, sectionNameIndex int) {
	copy(image[:4], []byte{0x7f, 'E', 'L', 'F'})
	image[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	image[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	image[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(image[16:18], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(image[18:20], uint16(elf.EM_X86_64))
	binary.LittleEndian.PutUint32(image[20:24], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint64(image[40:48], 64)
	binary.LittleEndian.PutUint16(image[52:54], 64)
	binary.LittleEndian.PutUint16(image[58:60], 64)
	binary.LittleEndian.PutUint16(image[60:62], uint16(sectionCount))
	binary.LittleEndian.PutUint16(image[62:64], uint16(sectionNameIndex))
}

func repeatedCOFFSectionNames(sectionCount, nameSize int) []byte {
	const (
		headerSize        = 20
		sectionHeaderSize = 40
		symbolSize        = 18
	)
	symbolOffset := headerSize + sectionCount*sectionHeaderSize
	stringOffset := symbolOffset + symbolSize
	stringSize := 4 + nameSize + 1
	image := make([]byte, stringOffset+stringSize)
	binary.LittleEndian.PutUint16(image[0:2], pe.IMAGE_FILE_MACHINE_AMD64)
	binary.LittleEndian.PutUint16(image[2:4], uint16(sectionCount))
	binary.LittleEndian.PutUint32(image[8:12], uint32(symbolOffset))
	binary.LittleEndian.PutUint32(image[12:16], 1)
	for index := 0; index < sectionCount; index++ {
		offset := headerSize + index*sectionHeaderSize
		copy(image[offset:offset+8], []byte("/4"))
	}
	copy(image[symbolOffset:symbolOffset+8], []byte("go"))
	binary.LittleEndian.PutUint32(image[stringOffset:stringOffset+4], uint32(stringSize))
	for index := stringOffset + 4; index < stringOffset+4+nameSize; index++ {
		image[index] = 'n'
	}
	return image
}

func repeatedCOFFSymbolNames(symbolCount, nameSize int) []byte {
	const (
		headerSize = 20
		symbolSize = 18
	)
	symbolOffset := headerSize
	stringOffset := symbolOffset + symbolCount*symbolSize
	stringSize := 4 + nameSize + 1
	image := make([]byte, stringOffset+stringSize)
	binary.LittleEndian.PutUint16(image[0:2], pe.IMAGE_FILE_MACHINE_AMD64)
	binary.LittleEndian.PutUint32(image[8:12], uint32(symbolOffset))
	binary.LittleEndian.PutUint32(image[12:16], uint32(symbolCount))
	for index := 0; index < symbolCount; index++ {
		offset := symbolOffset + index*symbolSize
		binary.LittleEndian.PutUint32(image[offset+4:offset+8], 4)
	}
	binary.LittleEndian.PutUint32(image[stringOffset:stringOffset+4], uint32(stringSize))
	for index := stringOffset + 4; index < stringOffset+4+nameSize; index++ {
		image[index] = 's'
	}
	return image
}

func overlappingELF64Sections(mappedCount, sectionSize int) []byte {
	const (
		headerSize        = 64
		sectionHeaderSize = 64
	)
	sectionCount := mappedCount + 1 // Include the required null section.
	payloadOffset := headerSize + sectionCount*sectionHeaderSize
	image := make([]byte, payloadOffset+sectionSize)
	copy(image[:4], []byte{0x7f, 'E', 'L', 'F'})
	image[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	image[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	image[elf.EI_VERSION] = byte(elf.EV_CURRENT)
	binary.LittleEndian.PutUint16(image[16:18], uint16(elf.ET_REL))
	binary.LittleEndian.PutUint16(image[18:20], uint16(elf.EM_X86_64))
	binary.LittleEndian.PutUint32(image[20:24], uint32(elf.EV_CURRENT))
	binary.LittleEndian.PutUint64(image[40:48], headerSize)
	binary.LittleEndian.PutUint16(image[52:54], headerSize)
	binary.LittleEndian.PutUint16(image[58:60], sectionHeaderSize)
	binary.LittleEndian.PutUint16(image[60:62], uint16(sectionCount))
	for index := 1; index < sectionCount; index++ {
		offset := headerSize + index*sectionHeaderSize
		binary.LittleEndian.PutUint32(image[offset+4:offset+8], uint32(elf.SHT_PROGBITS))
		binary.LittleEndian.PutUint64(image[offset+8:offset+16], uint64(elf.SHF_ALLOC))
		binary.LittleEndian.PutUint64(image[offset+24:offset+32], uint64(payloadOffset))
		binary.LittleEndian.PutUint64(image[offset+32:offset+40], uint64(sectionSize))
		binary.LittleEndian.PutUint64(image[offset+48:offset+56], 1)
	}
	return image
}

func overlappingCOFFSections(sectionCount, sectionSize int) []byte {
	const (
		headerSize        = 20
		sectionHeaderSize = 40
	)
	payloadOffset := headerSize + sectionCount*sectionHeaderSize
	image := make([]byte, payloadOffset+sectionSize)
	binary.LittleEndian.PutUint16(image[0:2], pe.IMAGE_FILE_MACHINE_AMD64)
	binary.LittleEndian.PutUint16(image[2:4], uint16(sectionCount))
	for index := 0; index < sectionCount; index++ {
		offset := headerSize + index*sectionHeaderSize
		copy(image[offset:offset+8], []byte(".data"))
		binary.LittleEndian.PutUint32(image[offset+16:offset+20], uint32(sectionSize))
		binary.LittleEndian.PutUint32(image[offset+20:offset+24], uint32(payloadOffset))
		binary.LittleEndian.PutUint32(image[offset+36:offset+40], pe.IMAGE_SCN_CNT_INITIALIZED_DATA|pe.IMAGE_SCN_MEM_READ)
	}
	return image
}

func TestValidateHostRejectsWrongFormatAndArchitecture(t *testing.T) {
	wrongArch := "amd64"
	if runtime.GOARCH == wrongArch {
		wrongArch = "arm64"
	}
	if err := validateHost(&objectFile{format: "elf", arch: wrongArch}); err == nil || !strings.Contains(err.Error(), "does not match host") {
		t.Fatalf("wrong architecture error = %v", err)
	}

	format := "coff"
	want := "require a Windows host"
	if runtime.GOOS == "windows" {
		format = "elf"
		want = "unsupported on windows"
	}
	if err := validateHost(&objectFile{format: format, arch: runtime.GOARCH}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("wrong format error = %v, want substring %q", err, want)
	}
}

func TestFindDefinedSymbolRequiresUniqueExecutableDefinition(t *testing.T) {
	executable := objectSection{name: ".text", size: 16, protection: protRead | protExec, mapped: true, address: 0x1000}
	writable := objectSection{name: ".data", size: 16, protection: protRead | protWrite, mapped: true, address: 0x2000}

	t.Run("valid executable definition", func(t *testing.T) {
		object := &objectFile{
			sections: []objectSection{executable},
			symbols: map[uint32]objectSymbol{
				1: {index: 1, name: "go", section: sectionUndefined},
				7: {index: 7, name: "go", section: 0, value: 4},
			},
		}
		address, found, err := findDefinedSymbol(object, "go")
		if err != nil || !found || address != 0x1004 {
			t.Fatalf("findDefinedSymbol() = (%#x, %v, %v), want (%#x, true, nil)", address, found, err, uintptr(0x1004))
		}
	})

	t.Run("writable data definition", func(t *testing.T) {
		object := &objectFile{
			sections: []objectSection{writable},
			symbols:  map[uint32]objectSymbol{1: {index: 1, name: "go", section: 0}},
		}
		if _, _, err := findDefinedSymbol(object, "go"); err == nil || !strings.Contains(err.Error(), "mapped executable section") {
			t.Fatalf("findDefinedSymbol() error = %v, want executable-section error", err)
		}
	})

	t.Run("duplicate definitions report sorted indices", func(t *testing.T) {
		object := &objectFile{
			sections: []objectSection{executable},
			symbols: map[uint32]objectSymbol{
				9: {index: 9, name: "go", section: 0},
				2: {index: 2, name: "go", section: 0},
			},
		}
		if _, _, err := findDefinedSymbol(object, "go"); err == nil || !strings.Contains(err.Error(), "[2 9]") {
			t.Fatalf("findDefinedSymbol() error = %v, want sorted duplicate indices", err)
		}
	})
}

func TestMappedSectionAlignmentRejectsUnsupportedRequirements(t *testing.T) {
	const pageSize = uint64(4096)
	for _, alignment := range []uint64{0, 1, 16, pageSize} {
		got, err := mappedSectionAlignment(objectSection{name: ".data", align: alignment}, pageSize)
		if err != nil || got != pageSize {
			t.Errorf("mappedSectionAlignment(%d) = (%d, %v), want (%d, nil)", alignment, got, err, pageSize)
		}
	}
	if _, err := mappedSectionAlignment(objectSection{name: ".data", align: 3}, pageSize); err == nil || !strings.Contains(err.Error(), "invalid alignment") {
		t.Fatalf("non-power-of-two alignment error = %v", err)
	}
	if _, err := mappedSectionAlignment(objectSection{name: ".common", align: pageSize * 2}, pageSize); err == nil || !strings.Contains(err.Error(), "exceeds guaranteed allocation alignment") {
		t.Fatalf("over-aligned common section error = %v", err)
	}
}
