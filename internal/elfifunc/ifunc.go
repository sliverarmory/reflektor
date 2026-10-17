// Package elfifunc validates custom ELF indirect-function inputs before an
// image is mapped or any resolver supplied by that image is executed.
package elfifunc

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"runtime"
)

// Features describes resolver work required by a custom image. An imported
// system IFUNC is resolved by the operating system and does not count here.
type Features struct {
	DefinedIFUNC bool
	IRelative    bool
}

func (features Features) Present() bool { return features.DefinedIFUNC || features.IRelative }

// Supported deliberately names only the ABI for which resolver execution is
// implemented and covered by native runtime fixtures.
func Supported(file *elf.File) bool {
	return file != nil && runtime.GOOS == "linux" && runtime.GOARCH == "amd64" &&
		file.Class == elf.ELFCLASS64 && file.Machine == elf.EM_X86_64 && file.Data == elf.ELFDATA2LSB
}

// Inspect rejects resolver features on unsupported ABIs and checks resolver
// addresses and IRELATIVE records before an image can be mapped.
func Inspect(file *elf.File) (Features, error) {
	var features Features
	if file == nil {
		return features, fmt.Errorf("nil ELF image")
	}
	for _, readSymbols := range []func() ([]elf.Symbol, error){file.DynamicSymbols, file.Symbols} {
		symbols, err := readSymbols()
		if err != nil {
			continue // A stripped image may omit the static symbol table.
		}
		for _, symbol := range symbols {
			if symbol.Section == elf.SHN_UNDEF || elf.ST_TYPE(symbol.Info) != elf.STT_GNU_IFUNC {
				continue
			}
			features.DefinedIFUNC = true
			if !Supported(file) {
				return features, fmt.Errorf("custom ELF image does not support GNU IFUNC resolver %q on %s/%s", symbol.Name, runtime.GOOS, runtime.GOARCH)
			}
			if err := CheckExecutable(file, symbol.Value, "GNU IFUNC resolver"); err != nil {
				return features, fmt.Errorf("GNU IFUNC symbol %q: %w", symbol.Name, err)
			}
		}
	}
	for _, name := range []string{".rela.dyn", ".rela.plt", ".rela.plt.sec", ".rel.dyn", ".rel.plt", ".rel.plt.sec"} {
		section := file.Section(name)
		if section == nil {
			continue
		}
		data, err := section.Data()
		if err != nil {
			return features, fmt.Errorf("read %s: %w", name, err)
		}
		entrySize := 0
		infoOffset := 0
		infoSize := 0
		switch {
		case section.Type == elf.SHT_RELA && file.Class == elf.ELFCLASS64:
			entrySize, infoOffset, infoSize = 24, 8, 8
		case section.Type == elf.SHT_RELA && file.Class == elf.ELFCLASS32:
			entrySize, infoOffset, infoSize = 12, 4, 4
		case section.Type == elf.SHT_REL && file.Class == elf.ELFCLASS64:
			entrySize, infoOffset, infoSize = 16, 8, 8
		case section.Type == elf.SHT_REL && file.Class == elf.ELFCLASS32:
			entrySize, infoOffset, infoSize = 8, 4, 4
		default:
			return features, fmt.Errorf("unsupported ELF relocation section %s", name)
		}
		if len(data)%entrySize != 0 {
			return features, fmt.Errorf("malformed %s: size %d is not a multiple of %d", name, len(data), entrySize)
		}
		for offset := 0; offset < len(data); offset += entrySize {
			var symbolIndex, kind uint32
			if infoSize == 8 {
				info := binary.LittleEndian.Uint64(data[offset+infoOffset:])
				symbolIndex, kind = uint32(elf.R_SYM64(info)), uint32(elf.R_TYPE64(info))
			} else {
				info := binary.LittleEndian.Uint32(data[offset+infoOffset:])
				symbolIndex, kind = elf.R_SYM32(info), elf.R_TYPE32(info)
			}
			if !IsIRelative(file.Machine, kind) {
				continue
			}
			features.IRelative = true
			if !Supported(file) {
				return features, fmt.Errorf("custom ELF image does not support IRELATIVE/IFUNC resolver relocations on %s/%s", runtime.GOOS, runtime.GOARCH)
			}
			if section.Type != elf.SHT_RELA || symbolIndex != 0 {
				return features, fmt.Errorf("%s[%d]: x86_64 IRELATIVE requires RELA and symbol index zero", name, offset/entrySize)
			}
			resolver := int64(binary.LittleEndian.Uint64(data[offset+16:]))
			if resolver < 0 {
				return features, fmt.Errorf("%s[%d]: negative IRELATIVE resolver address", name, offset/entrySize)
			}
			if err := CheckExecutable(file, uint64(resolver), "IRELATIVE resolver"); err != nil {
				return features, fmt.Errorf("%s[%d]: %w", name, offset/entrySize, err)
			}
		}
	}
	return features, nil
}

func IsIRelative(machine elf.Machine, relocationType uint32) bool {
	switch machine {
	case elf.EM_X86_64:
		return elf.R_X86_64(relocationType) == elf.R_X86_64_IRELATIVE
	case elf.EM_386:
		return elf.R_386(relocationType) == elf.R_386_IRELATIVE
	case elf.EM_AARCH64:
		return elf.R_AARCH64(relocationType) == elf.R_AARCH64_IRELATIVE
	case elf.EM_ARM:
		return elf.R_ARM(relocationType) == elf.R_ARM_IRELATIVE
	case elf.EM_RISCV:
		return relocationType == 58 // R_RISCV_IRELATIVE
	case elf.EM_PPC64:
		return elf.R_PPC64(relocationType) == elf.R_PPC64_IRELATIVE
	default:
		return false
	}
}

// CheckExecutable requires the function entry point to lie in a nonwritable
// executable PT_LOAD. It is used for both resolvers and their returned values.
func CheckExecutable(file *elf.File, vaddr uint64, label string) error {
	for _, program := range file.Progs {
		if program.Type != elf.PT_LOAD || program.Flags&elf.PF_X == 0 || program.Flags&elf.PF_W != 0 {
			continue
		}
		if vaddr >= program.Vaddr && vaddr-program.Vaddr < program.Memsz {
			return nil
		}
	}
	return fmt.Errorf("%s %#x is outside a nonwritable executable PT_LOAD", label, vaddr)
}

// CheckWritableTarget rejects text relocations and target pages that overlap
// executable mappings. Deferred writes happen after executable pages are RX.
func CheckWritableTarget(file *elf.File, vaddr uint64, width uint64) error {
	if width == 0 || vaddr > math.MaxUint64-width {
		return fmt.Errorf("IFUNC relocation target %#x overflows", vaddr)
	}
	end := vaddr + width
	writable := false
	for _, program := range file.Progs {
		if program.Type != elf.PT_LOAD || program.Memsz == 0 || program.Vaddr > math.MaxUint64-program.Memsz {
			continue
		}
		if vaddr >= program.Vaddr && end <= program.Vaddr+program.Memsz && program.Flags&elf.PF_W != 0 && program.Flags&elf.PF_X == 0 {
			writable = true
		}
	}
	if !writable {
		return fmt.Errorf("IFUNC relocation target %#x is outside a writable nonexecutable PT_LOAD", vaddr)
	}
	pageSize := uint64(os.Getpagesize())
	startPage := vaddr / pageSize
	endPage := (end - 1) / pageSize
	for _, program := range file.Progs {
		if program.Type != elf.PT_LOAD || program.Memsz == 0 || program.Flags&elf.PF_X == 0 || program.Vaddr > math.MaxUint64-program.Memsz {
			continue
		}
		programStart := program.Vaddr / pageSize
		programEnd := (program.Vaddr + program.Memsz - 1) / pageSize
		if startPage <= programEnd && endPage >= programStart {
			return fmt.Errorf("IFUNC relocation target %#x shares an executable page", vaddr)
		}
	}
	return nil
}
