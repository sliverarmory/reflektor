// Package elfrelr applies ELF packed relative relocations to mapped images.
package elfrelr

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"
	"unsafe"
)

const (
	// These values are part of the ELF generic ABI but are not named by the
	// pinned Go toolchain's debug/elf package.
	dtRELR    = elf.DynTag(36)
	dtRELRSZ  = elf.DynTag(35)
	dtRELRENT = elf.DynTag(37)
	shtRELR   = elf.SectionType(19)
)

// Apply processes DT_RELR before the ordinary REL and RELA tables. A packed
// relocation adds the image load bias to the machine word already at its
// target address. The caller must invoke Apply before making segments read-only.
func Apply(file *elf.File, mapping []byte, loadBias uintptr) error {
	var wordSize int
	switch file.Class {
	case elf.ELFCLASS32:
		wordSize = 4
	case elf.ELFCLASS64:
		wordSize = 8
	default:
		return fmt.Errorf("unsupported ELF class for RELR: %s", file.Class)
	}
	if file.Data != elf.ELFDATA2LSB {
		return fmt.Errorf("unsupported ELF byte order for RELR: %s", file.Data)
	}

	address, err := file.DynValue(dtRELR)
	if err != nil {
		return fmt.Errorf("read DT_RELR: %w", err)
	}
	size, err := file.DynValue(dtRELRSZ)
	if err != nil {
		return fmt.Errorf("read DT_RELRSZ: %w", err)
	}
	entrySize, err := file.DynValue(dtRELRENT)
	if err != nil {
		return fmt.Errorf("read DT_RELRENT: %w", err)
	}

	var section *elf.Section
	for _, candidate := range file.Sections {
		if candidate.Type != shtRELR {
			continue
		}
		if section != nil {
			return fmt.Errorf("multiple SHT_RELR sections are unsupported")
		}
		section = candidate
	}
	if len(address) == 0 && len(size) == 0 && len(entrySize) == 0 && section == nil {
		return nil
	}
	if len(address) != 1 || len(size) != 1 || len(entrySize) != 1 || section == nil {
		return fmt.Errorf("packed RELR requires one DT_RELR, DT_RELRSZ, DT_RELRENT, and SHT_RELR section")
	}
	if !runtimeSupported() {
		return fmt.Errorf("packed RELR is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if address[0] != section.Addr || size[0] != section.Size {
		return fmt.Errorf("DT_RELR address/size (%#x/%#x) disagree with %s (%#x/%#x)", address[0], size[0], section.Name, section.Addr, section.Size)
	}
	if entrySize[0] != uint64(wordSize) || (section.Entsize != 0 && section.Entsize != uint64(wordSize)) {
		return fmt.Errorf("packed RELR entry size must be %d bytes", wordSize)
	}
	data, err := section.Data()
	if err != nil {
		return fmt.Errorf("read %s: %w", section.Name, err)
	}
	return applyTable(data, file.Class, mapping, loadBias)
}

func runtimeSupported() bool {
	switch runtime.GOOS {
	case "linux":
		return runtime.GOARCH == "386" || runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	case "freebsd":
		return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	default:
		return false
	}
}

func applyTable(data []byte, class elf.Class, mapping []byte, loadBias uintptr) error {
	wordSize := 8
	wordBits := 64
	if class == elf.ELFCLASS32 {
		wordSize = 4
		wordBits = 32
	}
	if len(data)%wordSize != 0 {
		return fmt.Errorf("malformed packed RELR: size %d is not a multiple of %d", len(data), wordSize)
	}
	if len(data) == 0 {
		return nil
	}
	if len(mapping) < wordSize {
		return fmt.Errorf("packed RELR target is outside mapped image")
	}

	baseAddress := uintptr(unsafe.Pointer(&mapping[0]))
	var nextOffset uint64
	baseSet := false
	for index := 0; index < len(data)/wordSize; index++ {
		var entry uint64
		if wordSize == 8 {
			entry = binary.LittleEndian.Uint64(data[index*wordSize:])
		} else {
			entry = uint64(binary.LittleEndian.Uint32(data[index*wordSize:]))
		}
		if entry&1 == 0 {
			if err := applyWord(mapping, baseAddress, loadBias, entry, wordSize); err != nil {
				return fmt.Errorf("packed RELR[%d]: %w", index, err)
			}
			if entry > math.MaxUint64-uint64(wordSize) {
				return fmt.Errorf("packed RELR[%d]: offset overflows", index)
			}
			nextOffset = entry + uint64(wordSize)
			baseSet = true
			continue
		}
		if !baseSet {
			return fmt.Errorf("packed RELR[%d]: bitmap has no base address", index)
		}
		for bit := 0; bit < wordBits-1; bit++ {
			if entry&(uint64(1)<<(bit+1)) == 0 {
				continue
			}
			delta := uint64(bit * wordSize)
			if nextOffset > math.MaxUint64-delta {
				return fmt.Errorf("packed RELR[%d]: offset overflows", index)
			}
			if err := applyWord(mapping, baseAddress, loadBias, nextOffset+delta, wordSize); err != nil {
				return fmt.Errorf("packed RELR[%d] bit %d: %w", index, bit+1, err)
			}
		}
		advance := uint64((wordBits - 1) * wordSize)
		if nextOffset > math.MaxUint64-advance {
			return fmt.Errorf("packed RELR[%d]: bitmap advance overflows", index)
		}
		nextOffset += advance
	}
	return nil
}

func applyWord(mapping []byte, baseAddress uintptr, loadBias uintptr, offset uint64, wordSize int) error {
	if offset%uint64(wordSize) != 0 || uint64(uintptr(offset)) != offset {
		return fmt.Errorf("unaligned or overflowing target %#x", offset)
	}
	if uintptr(offset) > ^uintptr(0)-loadBias {
		return fmt.Errorf("target %#x overflows address space", offset)
	}
	target := loadBias + uintptr(offset)
	if target < baseAddress || target-baseAddress > uintptr(len(mapping)-wordSize) {
		return fmt.Errorf("target %#x is outside mapped image", offset)
	}
	index := int(target - baseAddress)
	if wordSize == 8 {
		value := binary.LittleEndian.Uint64(mapping[index:])
		binary.LittleEndian.PutUint64(mapping[index:], value+uint64(loadBias))
	} else {
		value := binary.LittleEndian.Uint32(mapping[index:])
		binary.LittleEndian.PutUint32(mapping[index:], value+uint32(loadBias))
	}
	return nil
}
