//go:build bof

package bofloader

import (
	"bytes"
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"
)

const (
	maxFuzzObjectSize  = 1 << 20
	maxFuzzFormatSize  = 4 << 10
	maxFuzzParserBytes = 64 << 10
	maxFuzzParserOps   = 256
)

func FuzzELFObjectParser(f *testing.F) {
	f.Add([]byte{0x7f, 'E', 'L', 'F'})
	f.Add(overlappingELF64Sections(1, 16))
	f.Add(repeatedELFSectionNames(3, 32))
	addOptionalObjectFuzzSeeds(f, "elf")

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzObjectSize {
			return
		}
		if err := preflightELFHeader(data); err != nil {
			return
		}
		object, err := parseELF(data)
		if err != nil {
			return
		}
		if object == nil || object.format != "elf" {
			t.Fatalf("parseELF returned invalid object: %#v", object)
		}
		if len(object.sections) > maxObjectSections || len(object.symbols) > maxObjectSymbols || len(object.relocations) > maxObjectRelocations {
			t.Fatalf("parseELF exceeded parser record limits: %d sections, %d symbols, %d relocations", len(object.sections), len(object.symbols), len(object.relocations))
		}
	})
}

func FuzzCOFFObjectParser(f *testing.F) {
	f.Add([]byte{0x64, 0x86, 0, 0})
	f.Add(overlappingCOFFSections(1, 16))
	f.Add(repeatedCOFFSectionNames(3, 32))
	addOptionalObjectFuzzSeeds(f, "coff")

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzObjectSize {
			return
		}
		if err := preflightCOFF(data); err != nil {
			return
		}
		object, err := parseCOFF(data)
		if err != nil {
			return
		}
		if object == nil || object.format != "coff" {
			t.Fatalf("parseCOFF returned invalid object: %#v", object)
		}
		if len(object.sections) > maxObjectSections || len(object.symbols) > maxObjectSymbols || len(object.relocations) > maxObjectRelocations {
			t.Fatalf("parseCOFF exceeded parser record limits: %d sections, %d symbols, %d relocations", len(object.sections), len(object.symbols), len(object.relocations))
		}
	})
}

func FuzzMachOObjectParser(f *testing.F) {
	f.Add([]byte{0xcf, 0xfa, 0xed, 0xfe})
	addOptionalObjectFuzzSeeds(f, "macho")

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxFuzzObjectSize {
			return
		}
		if _, err := preflightMachO(data); err != nil {
			return
		}
		object, err := parseMachO(data)
		if err != nil {
			return
		}
		if object == nil || object.format != "macho" {
			t.Fatalf("parseMachO returned invalid object: %#v", object)
		}
		if len(object.sections) > maxObjectSections || len(object.symbols) > maxObjectSymbols || len(object.relocations) > maxObjectRelocations {
			t.Fatalf("parseMachO exceeded parser record limits: %d sections, %d symbols, %d relocations", len(object.sections), len(object.symbols), len(object.relocations))
		}
	})
}

func FuzzARM64RelocationEncoders(f *testing.F) {
	f.Add(uint8(0), uint32(0x94000000), uint64(0x1100), uint64(0x1000), int64(0), true)
	f.Add(uint8(2), uint32(0x58000000), uint64(0x1000), uint64(0x1000), int64(0), false)
	f.Add(uint8(11), uint32(0xf9400000), uint64(0x12345328), uint64(0), int64(0), true)

	f.Fuzz(func(t *testing.T, kind uint8, word uint32, target, place uint64, addend int64, explicit bool) {
		location := bytes.Repeat([]byte{0xa5}, 12)
		binary.LittleEndian.PutUint32(location[4:8], word)
		before := append([]byte(nil), location...)
		instruction := location[4:8]

		var err error
		switch kind % 13 {
		case 0:
			err = applyARM64Branch26(instruction, target, place, explicit, addend)
		case 1:
			err = applyARM64Branch19(instruction, target, place, explicit, addend)
		case 2:
			err = applyARM64Literal19(instruction, target, place, explicit, addend)
		case 3:
			err = applyARM64Branch14(instruction, target, place, explicit, addend)
		case 4:
			err = applyARM64ADR(instruction, target, place, explicit, addend)
		case 5:
			err = applyARM64ADRP(instruction, target, place, explicit, addend, true)
		case 6:
			err = applyARM64AddLO12(instruction, target, explicit, addend)
		case 7:
			err = applyARM64AddHigh12(instruction, target, explicit, addend)
		default:
			err = applyARM64LoadStoreLO12(instruction, target, uint(kind%13-8), explicit, addend)
		}

		if !bytes.Equal(location[:4], before[:4]) || !bytes.Equal(location[8:], before[8:]) {
			t.Fatalf("relocation encoder modified red-zone bytes: %x -> %x", before, location)
		}
		if err != nil && !bytes.Equal(location[4:8], before[4:8]) {
			t.Fatalf("relocation encoder changed instruction on rejection: %x -> %x (%v)", before[4:8], location[4:8], err)
		}
	})
}

func FuzzBeaconPrintfFormatter(f *testing.F) {
	f.Add([]byte("name=%s value=%08x"))
	f.Add([]byte("%I64u %S %%"))
	f.Add([]byte("%999999999999999999999999s"))

	f.Fuzz(func(t *testing.T, format []byte) {
		if len(format) > maxFuzzFormatSize {
			return
		}
		terminated := make([]byte, len(format)+1)
		copy(terminated, format)
		zeroString := make([]byte, 8)
		address := byteSliceAddress(zeroString)
		var arguments [maxPrintfArgument]uintptr
		for index := range arguments {
			arguments[index] = address
		}

		formatted, _ := formatPrintf(byteSliceAddress(terminated), arguments)
		if len(formatted) > maxFormattedData {
			t.Fatalf("formatted output exceeds %d bytes: %d", maxFormattedData, len(formatted))
		}
		runtime.KeepAlive(terminated)
		runtime.KeepAlive(zeroString)
	})
}

func FuzzBeaconDataParserState(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 2, 0, 0, 0, 'o', 'k'}, []byte{0, 1, 2, 3})
	f.Add([]byte{}, []byte{2, 4, 0, 5, 1})

	f.Fuzz(func(t *testing.T, payload, operations []byte) {
		if len(payload) > maxFuzzParserBytes || len(operations) > maxFuzzParserOps {
			return
		}
		argument := make([]byte, 4+len(payload))
		binary.LittleEndian.PutUint32(argument[:4], uint32(len(payload)))
		copy(argument[4:], payload)
		var parser beaconDataParser
		parserAddress := uintptr(unsafe.Pointer(&parser))
		beaconDataParse(parserAddress, byteSliceAddress(argument), uintptr(len(argument)))

		for index := 0; index < len(operations); index++ {
			switch operations[index] % 7 {
			case 0:
				beaconDataInt(parserAddress)
			case 1:
				beaconDataShort(parserAddress)
			case 2:
				var size int32
				beaconDataExtract(parserAddress, uintptr(unsafe.Pointer(&size)))
			case 3:
				beaconDataLength(parserAddress)
			case 4:
				// Exercise invalid state rejection without manufacturing an
				// unsafe cursor that could point outside the owned argument.
				parser.length = int32(int8(operations[index]))
			case 5:
				// Keep the manufactured pointer inside owned memory while making
				// it independent of the logical consumed-byte count.
				cursor := int(operations[index]) % (len(payload) + 1)
				parser.buffer = parser.original + 4 + uintptr(cursor)
			case 6:
				if parser.size >= 0 && int(parser.size) <= len(payload) {
					remaining := int32(operations[index]) % (parser.size + 1)
					parser.length = remaining
					parser.buffer = parser.original + 4 + uintptr(parser.size-remaining)
				}
			}
		}
		runtime.KeepAlive(argument)
		runtime.KeepAlive(&parser)
	})
}

func addOptionalObjectFuzzSeeds(f *testing.F, format string) {
	seen := make(map[[sha256.Size]byte]struct{})
	addPath := func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read fuzz seed %q: %w", path, err)
		}
		seedFormat := fuzzObjectFormat(data)
		if seedFormat == "" {
			return fmt.Errorf("fuzz seed %q has an unrecognized object format", path)
		}
		if seedFormat != format {
			return nil
		}
		if len(data) > maxFuzzObjectSize {
			return fmt.Errorf("fuzz seed %q is %d bytes; limit is %d", path, len(data), maxFuzzObjectSize)
		}
		digest := sha256.Sum256(data)
		if _, ok := seen[digest]; ok {
			return nil
		}
		seen[digest] = struct{}{}
		f.Add(data)
		return nil
	}
	if fixture := os.Getenv("REFLEKTOR_BOF_FIXTURE_FILE"); fixture != "" {
		if err := addPath(fixture); err != nil {
			f.Fatalf("register %s fixture fuzz seed: %v", format, err)
		}
	}
	for _, root := range []string{
		os.Getenv("REFLEKTOR_BOF_FIXTURE_DIR"),
		os.Getenv("REFLEKTOR_BOF_CORPUS_DIR"),
	} {
		if root == "" {
			continue
		}
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == ".git" {
					return fs.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".o", ".obj":
				return addPath(path)
			}
			return nil
		}); err != nil {
			f.Fatalf("register %s fuzz seeds from %q: %v", format, root, err)
		}
	}
	f.Logf("registered %d optional %s object fuzz seeds", len(seen), format)
}

func fuzzObjectFormat(data []byte) string {
	if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x7f, 'E', 'L', 'F'}) {
		return "elf"
	}
	if isMachOMagic(data) {
		return "macho"
	}
	if len(data) < 2 {
		return ""
	}
	switch binary.LittleEndian.Uint16(data[:2]) {
	case pe.IMAGE_FILE_MACHINE_I386, pe.IMAGE_FILE_MACHINE_AMD64, pe.IMAGE_FILE_MACHINE_ARM64:
		return "coff"
	default:
		return ""
	}
}
