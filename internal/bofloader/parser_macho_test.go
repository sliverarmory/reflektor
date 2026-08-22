package bofloader

import (
	"encoding/binary"
	"strings"
	"testing"
)

type machoTestRelocation struct {
	offset   uint32
	value    uint32
	typeID   uint8
	length   uint8
	pcrel    bool
	external bool
}

type machoTestSection struct {
	name        string
	segment     string
	address     uint64
	size        uint64
	align       uint32
	flags       uint32
	data        []byte
	relocations []machoTestRelocation
}

type machoTestSymbol struct {
	name    string
	typeID  uint8
	section uint8
	desc    uint16
	value   uint64
}

func TestParseMachOAMD64Object(t *testing.T) {
	image := buildMachOTestObject(machoCPUAMD64, []machoTestSection{
		{
			name:    "__text",
			segment: "__TEXT",
			address: 0,
			flags:   machoSectionAttrPureInstructions | machoSectionAttrSomeInstructions,
			data:    make([]byte, 4),
			relocations: []machoTestRelocation{{
				offset: 0, value: 2, typeID: uint8(machoX86RelocSigned), length: 2, pcrel: true,
			}},
		},
		{name: "__cstring", segment: "__TEXT", address: 4, flags: machoSectionCStringLiterals, data: []byte("ok\x00")},
	}, []machoTestSymbol{
		{name: "_go", typeID: machoNSection | 1, section: 1, value: 0},
		{name: "l_message", typeID: machoNSection, section: 2, value: 4},
	})

	object, err := parseMachO(image)
	if err != nil {
		t.Fatal(err)
	}
	if object.format != "macho" || object.arch != "amd64" {
		t.Fatalf("parsed object = %s/%s, want macho/amd64", object.format, object.arch)
	}
	if len(object.sections) != 2 || object.sections[0].protection != protRead|protExec || object.sections[1].protection != protRead {
		t.Fatalf("parsed sections = %#v", object.sections)
	}
	if symbol := object.symbols[0]; symbol.name != "_go" || symbol.section != 0 || symbol.value != 0 {
		t.Fatalf("entry symbol = %#v, want raw Mach-O spelling _go in text", symbol)
	}
	if len(object.relocations) != 1 {
		t.Fatalf("relocation count = %d, want 1", len(object.relocations))
	}
	relocation := object.relocations[0]
	if !relocation.hasAdd || relocation.addend != 0 || relocation.width != 4 {
		t.Fatalf("normalized local relocation = %#v, want explicit zero addend and width 4", relocation)
	}
	sectionSymbol := object.symbols[relocation.symbol]
	if sectionSymbol.section != 1 || sectionSymbol.value != 0 {
		t.Fatalf("synthetic section symbol = %#v, want cstring section base", sectionSymbol)
	}
	if dispatched, err := parseObject(image); err != nil || dispatched.format != "macho" {
		t.Fatalf("parseObject() = %#v, %v; want Mach-O dispatch", dispatched, err)
	}
}

func TestMachOArm64RejectsAppleVariadicCallbacksBeforeMapping(t *testing.T) {
	image := buildMachOTestObject(machoCPUARM64, []machoTestSection{{
		name:    "__text",
		segment: "__TEXT",
		flags:   machoSectionAttrPureInstructions | machoSectionAttrSomeInstructions,
		data:    []byte{0, 0, 0, 0x94},
		relocations: []machoTestRelocation{{
			offset: 0, value: 1, typeID: uint8(machoARM64RelocBranch26), length: 2, pcrel: true, external: true,
		}},
	}}, []machoTestSymbol{
		{name: "_go", typeID: machoNSection | 1, section: 1},
		{name: "_BeaconPrintf", typeID: machoNUndef | 1},
	})
	_, err := parseMachO(image)
	if err == nil || !strings.Contains(err.Error(), "Apple's arm64 variadic ABI") || !strings.Contains(err.Error(), "BeaconOutput") {
		t.Fatalf("parseMachO() error = %v, want Apple variadic-ABI guidance", err)
	}
}

func TestMachOImportsPreserveRawSymbolSpelling(t *testing.T) {
	image := buildMachOTestObject(machoCPUARM64, []machoTestSection{{
		name:    "__text",
		segment: "__TEXT",
		flags:   machoSectionAttrPureInstructions | machoSectionAttrSomeInstructions,
		data:    []byte{0, 0, 0, 0x94},
		relocations: []machoTestRelocation{{
			offset: 0, value: 1, typeID: uint8(machoARM64RelocBranch26), length: 2, pcrel: true, external: true,
		}},
	}}, []machoTestSymbol{
		{name: "_go", typeID: machoNSection | 1, section: 1},
		{name: "_BeaconOutput", typeID: machoNUndef | 1},
	})
	object, err := parseMachO(image)
	if err != nil {
		t.Fatal(err)
	}
	imports := objectImports(object, referencedLinkageSymbols(object))
	if len(imports) != 1 || imports[0].Name != "_BeaconOutput" || !imports[0].Builtin {
		t.Fatalf("Mach-O imports = %#v, want exact _BeaconOutput spelling classified as builtin", imports)
	}
}

func TestMachOPreflightRejectsUnsupportedAndMalformedImages(t *testing.T) {
	minimal := buildMachOTestObject(machoCPUAMD64, []machoTestSection{{
		name: "__text", segment: "__TEXT", flags: machoSectionAttrPureInstructions, data: make([]byte, 4),
	}}, []machoTestSymbol{{name: "_go", typeID: machoNSection | 1, section: 1}})

	tests := []struct {
		name string
		make func() []byte
		want string
	}{
		{
			name: "32-bit",
			make: func() []byte {
				image := append([]byte(nil), minimal...)
				binary.LittleEndian.PutUint32(image[:4], machoMagic32)
				return image
			},
			want: "32-bit Mach-O",
		},
		{
			name: "fat",
			make: func() []byte {
				image := append([]byte(nil), minimal...)
				binary.LittleEndian.PutUint32(image[:4], bitswap32(machoMagicFat))
				return image
			},
			want: "universal Mach-O",
		},
		{
			name: "flagged arm64e subtype",
			make: func() []byte {
				image := append([]byte(nil), minimal...)
				binary.LittleEndian.PutUint32(image[4:8], machoCPUARM64)
				binary.LittleEndian.PutUint32(image[8:12], 0x80000000|machoCPUARM64E)
				return image
			},
			want: "arm64e",
		},
		{
			name: "oversized command count",
			make: func() []byte {
				image := append([]byte(nil), minimal...)
				binary.LittleEndian.PutUint32(image[16:20], maxObjectSections+1)
				return image
			},
			want: "load command count",
		},
		{
			name: "section data out of range",
			make: func() []byte {
				image := append([]byte(nil), minimal...)
				const firstSectionOffsetField = 32 + 72 + 48
				binary.LittleEndian.PutUint32(image[firstSectionOffsetField:firstSectionOffsetField+4], uint32(len(image)))
				return image
			},
			want: "data extends outside",
		},
		{
			name: "invalid symbol string offset",
			make: func() []byte {
				image := append([]byte(nil), minimal...)
				symtabCommand := 32 + 72 + 80
				symoff := binary.LittleEndian.Uint32(image[symtabCommand+8 : symtabCommand+12])
				binary.LittleEndian.PutUint32(image[symoff:symoff+4], ^uint32(0))
				return image
			},
			want: "invalid string offset",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := preflightMachO(test.make()); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("preflightMachO() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMachORejectsTLSAndUnpairedARM64Addend(t *testing.T) {
	t.Run("TLS", func(t *testing.T) {
		image := buildMachOTestObject(machoCPUARM64, []machoTestSection{{
			name: "__thread_data", segment: "__DATA", flags: machoSectionThreadLocalRegular, data: make([]byte, 8),
		}}, nil)
		if _, err := parseMachO(image); err == nil || !strings.Contains(err.Error(), "TLS section") {
			t.Fatalf("parseMachO() error = %v, want TLS rejection", err)
		}
	})

	t.Run("unpaired ADDEND", func(t *testing.T) {
		image := buildMachOTestObject(machoCPUARM64, []machoTestSection{{
			name: "__text", segment: "__TEXT", flags: machoSectionAttrPureInstructions, data: make([]byte, 4),
			relocations: []machoTestRelocation{{
				offset: 0, value: 7, typeID: uint8(machoARM64RelocAddend), length: 2,
			}},
		}}, []machoTestSymbol{{name: "_go", typeID: machoNSection | 1, section: 1}})
		if _, err := parseMachO(image); err == nil || !strings.Contains(err.Error(), "missing its target relocation") {
			t.Fatalf("parseMachO() error = %v, want unpaired ADDEND rejection", err)
		}
	})

	t.Run("ADDEND before unsupported target", func(t *testing.T) {
		image := buildMachOTestObject(machoCPUARM64, []machoTestSection{{
			name: "__text", segment: "__TEXT", flags: machoSectionAttrPureInstructions, data: make([]byte, 8),
			relocations: []machoTestRelocation{
				{offset: 0, value: 7, typeID: uint8(machoARM64RelocAddend), length: 2},
				{offset: 0, value: 0, typeID: uint8(machoARM64RelocUnsigned), length: 3, external: true},
			},
		}}, []machoTestSymbol{{name: "_go", typeID: machoNSection | 1, section: 1}})
		if _, err := parseMachO(image); err == nil || !strings.Contains(err.Error(), "cannot precede") {
			t.Fatalf("parseMachO() error = %v, want invalid ADDEND pair rejection", err)
		}
	})

	t.Run("embedded instruction addend", func(t *testing.T) {
		instruction := make([]byte, 4)
		binary.LittleEndian.PutUint32(instruction, 0x94000001)
		image := buildMachOTestObject(machoCPUARM64, []machoTestSection{{
			name: "__text", segment: "__TEXT", flags: machoSectionAttrPureInstructions, data: instruction,
			relocations: []machoTestRelocation{{
				offset: 0, value: 1, typeID: uint8(machoARM64RelocBranch26), length: 2, pcrel: true, external: true,
			}},
		}}, []machoTestSymbol{
			{name: "_go", typeID: machoNSection | 1, section: 1},
			{name: "_BeaconOutput", typeID: machoNUndef | 1},
		})
		if _, err := parseMachO(image); err == nil || !strings.Contains(err.Error(), "non-zero embedded addend") {
			t.Fatalf("parseMachO() error = %v, want embedded-addend rejection", err)
		}
	})
}

func TestMachOParserBoundsCumulativeZeroFillSections(t *testing.T) {
	const sectionSize = 1 << 20
	sections := make([]machoTestSection, maxImageSize/sectionSize+1)
	for index := range sections {
		sections[index] = machoTestSection{
			name: "__bss", segment: "__DATA", flags: machoSectionZeroFill, size: sectionSize,
		}
	}
	image := buildMachOTestObject(machoCPUAMD64, sections, nil)
	if _, err := parseMachO(image); err == nil || !strings.Contains(err.Error(), "cumulative Mach-O mapped section size") {
		t.Fatalf("parseMachO() error = %v, want cumulative mapped-section limit", err)
	}
}

func buildMachOTestObject(cpu uint32, sections []machoTestSection, symbols []machoTestSymbol) []byte {
	segmentCommandSize := 72 + len(sections)*80
	commandSize := segmentCommandSize + 24
	cursor := 32 + commandSize
	sectionOffsets := make([]uint32, len(sections))
	relocationOffsets := make([]uint32, len(sections))
	for index := range sections {
		if sections[index].size == 0 {
			sections[index].size = uint64(len(sections[index].data))
		}
		sectionType := sections[index].flags & machoSectionTypeMask
		if sectionType != machoSectionZeroFill && sectionType != machoSectionGBZeroFill {
			sectionOffsets[index] = uint32(cursor)
			cursor += int(sections[index].size)
		}
	}
	for index := range sections {
		if len(sections[index].relocations) != 0 {
			relocationOffsets[index] = uint32(cursor)
			cursor += len(sections[index].relocations) * 8
		}
	}
	symbolOffset := cursor
	cursor += len(symbols) * 16
	stringTable := []byte{0}
	nameOffsets := make([]uint32, len(symbols))
	for index, symbol := range symbols {
		nameOffsets[index] = uint32(len(stringTable))
		stringTable = append(stringTable, symbol.name...)
		stringTable = append(stringTable, 0)
	}
	stringOffset := cursor
	cursor += len(stringTable)
	image := make([]byte, cursor)

	binary.LittleEndian.PutUint32(image[0:4], machoMagic64)
	binary.LittleEndian.PutUint32(image[4:8], cpu)
	binary.LittleEndian.PutUint32(image[12:16], machoTypeObject)
	binary.LittleEndian.PutUint32(image[16:20], 2)
	binary.LittleEndian.PutUint32(image[20:24], uint32(commandSize))

	segment := image[32 : 32+segmentCommandSize]
	binary.LittleEndian.PutUint32(segment[0:4], machoLoadSegment64)
	binary.LittleEndian.PutUint32(segment[4:8], uint32(segmentCommandSize))
	binary.LittleEndian.PutUint32(segment[64:68], uint32(len(sections)))
	for index, section := range sections {
		header := segment[72+index*80 : 72+(index+1)*80]
		copy(header[0:16], section.name)
		copy(header[16:32], section.segment)
		binary.LittleEndian.PutUint64(header[32:40], section.address)
		binary.LittleEndian.PutUint64(header[40:48], section.size)
		binary.LittleEndian.PutUint32(header[48:52], sectionOffsets[index])
		binary.LittleEndian.PutUint32(header[52:56], section.align)
		binary.LittleEndian.PutUint32(header[56:60], relocationOffsets[index])
		binary.LittleEndian.PutUint32(header[60:64], uint32(len(section.relocations)))
		binary.LittleEndian.PutUint32(header[64:68], section.flags)
		if sectionOffsets[index] != 0 {
			copy(image[sectionOffsets[index]:uint64(sectionOffsets[index])+section.size], section.data)
		}
		for relocationIndex, relocation := range section.relocations {
			offset := int(relocationOffsets[index]) + relocationIndex*8
			binary.LittleEndian.PutUint32(image[offset:offset+4], relocation.offset)
			info := relocation.value | uint32(relocation.length&3)<<25 | uint32(relocation.typeID&15)<<28
			if relocation.pcrel {
				info |= 1 << 24
			}
			if relocation.external {
				info |= 1 << 27
			}
			binary.LittleEndian.PutUint32(image[offset+4:offset+8], info)
		}
	}

	symtab := image[32+segmentCommandSize : 32+segmentCommandSize+24]
	binary.LittleEndian.PutUint32(symtab[0:4], machoLoadSymtab)
	binary.LittleEndian.PutUint32(symtab[4:8], 24)
	binary.LittleEndian.PutUint32(symtab[8:12], uint32(symbolOffset))
	binary.LittleEndian.PutUint32(symtab[12:16], uint32(len(symbols)))
	binary.LittleEndian.PutUint32(symtab[16:20], uint32(stringOffset))
	binary.LittleEndian.PutUint32(symtab[20:24], uint32(len(stringTable)))
	for index, symbol := range symbols {
		offset := symbolOffset + index*16
		binary.LittleEndian.PutUint32(image[offset:offset+4], nameOffsets[index])
		image[offset+4] = symbol.typeID
		image[offset+5] = symbol.section
		binary.LittleEndian.PutUint16(image[offset+6:offset+8], symbol.desc)
		binary.LittleEndian.PutUint64(image[offset+8:offset+16], symbol.value)
	}
	copy(image[stringOffset:], stringTable)
	return image
}
