package elftls

import (
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
)

// Template describes the deliberately narrow ELF TLS model supported by the
// custom Linux/amd64 loader. The C resolver owns a copy after Register.
type Template struct {
	initial []byte
	size    uint64
	align   uint64
}

func (template *Template) Size() uint64 {
	if template == nil {
		return 0
	}
	return template.size
}

const maxTemplateSize = 64 << 10

// Inspect accepts local, general-dynamic TLS only. Static TLS, TLSDESC,
// external TLS symbols, and other OS/architecture combinations still fail
// before the image is mapped or any initializer runs.
func Inspect(f *elf.File, raw []byte) (*Template, error) {
	var segment *elf.Prog
	for _, program := range f.Progs {
		if program.Type != elf.PT_TLS {
			continue
		}
		if segment != nil {
			return nil, errors.New("general PT_TLS has multiple segments")
		}
		segment = program
	}
	if segment == nil || (segment.Memsz == 0 && segment.Filesz == 0) {
		for _, readSymbols := range []func() ([]elf.Symbol, error){f.DynamicSymbols, f.Symbols} {
			symbols, err := readSymbols()
			if err != nil {
				continue
			}
			for _, symbol := range symbols {
				if elf.ST_TYPE(symbol.Info) == elf.STT_TLS {
					return nil, fmt.Errorf("TLS symbol %q has no PT_TLS segment", symbol.Name)
				}
			}
		}
		return nil, nil
	}
	if !available() || f.Machine != elf.EM_X86_64 || f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB {
		return nil, errors.New("general PT_TLS requires Linux/amd64 with cgo and local general-dynamic TLS")
	}
	if segment.Memsz == 0 || segment.Filesz > segment.Memsz || segment.Memsz > maxTemplateSize ||
		segment.Off > uint64(len(raw)) || segment.Filesz > uint64(len(raw))-segment.Off {
		return nil, fmt.Errorf("general PT_TLS has invalid template size (filesz=%#x memsz=%#x)", segment.Filesz, segment.Memsz)
	}
	if segment.Align == 0 || segment.Align&(segment.Align-1) != 0 || segment.Align > 4096 {
		return nil, fmt.Errorf("general PT_TLS has unsupported alignment %#x", segment.Align)
	}

	symbols, err := f.DynamicSymbols()
	if err != nil {
		return nil, fmt.Errorf("read PT_TLS dynamic symbols: %w", err)
	}
	for _, symbol := range symbols {
		if elf.ST_TYPE(symbol.Info) != elf.STT_TLS {
			continue
		}
		if symbol.Section == elf.SHN_UNDEF {
			return nil, fmt.Errorf("general PT_TLS imports external TLS symbol %q", symbol.Name)
		}
		if symbol.Value >= segment.Memsz {
			return nil, fmt.Errorf("general PT_TLS symbol %q lies outside its template", symbol.Name)
		}
	}

	var moduleRelocations, resolverRelocations int
	for _, name := range []string{".rela.dyn", ".rela.plt", ".rela.plt.sec", ".rel.dyn", ".rel.plt", ".rel.plt.sec"} {
		section := f.Section(name)
		if section == nil || section.Size == 0 {
			continue
		}
		if section.Type != elf.SHT_RELA {
			return nil, fmt.Errorf("general PT_TLS requires RELA relocations; %s has type %s", name, section.Type)
		}
		data, err := section.Data()
		if err != nil {
			return nil, fmt.Errorf("read PT_TLS relocations in %s: %w", name, err)
		}
		if len(data)%24 != 0 {
			return nil, fmt.Errorf("malformed PT_TLS relocation section %s", name)
		}
		for offset := 0; offset < len(data); offset += 24 {
			info := binary.LittleEndian.Uint64(data[offset+8 : offset+16])
			symbolIndex := uint32(elf.R_SYM64(info))
			relocation := elf.R_X86_64(elf.R_TYPE64(info))
			switch relocation {
			case elf.R_X86_64_DTPMOD64:
				if symbolIndex != 0 {
					return nil, errors.New("general PT_TLS requires local DTPMOD64 relocations")
				}
				moduleRelocations++
			case elf.R_X86_64_DTPOFF64:
				if symbolIndex != 0 {
					return nil, errors.New("general PT_TLS requires local DTPOFF64 relocations")
				}
			case elf.R_X86_64_JMP_SLOT, elf.R_X86_64_GLOB_DAT:
				if symbolIndex == 0 || int(symbolIndex) > len(symbols) {
					continue
				}
				if symbols[symbolIndex-1].Name == "__tls_get_addr" {
					resolverRelocations++
				}
			default:
				if relocation >= 16 && relocation <= 23 || relocation >= 34 && relocation <= 36 {
					return nil, fmt.Errorf("general PT_TLS relocation %s is unsupported", relocation)
				}
			}
		}
	}
	if moduleRelocations == 0 || resolverRelocations == 0 {
		return nil, errors.New("general PT_TLS requires local DTPMOD64 and __tls_get_addr relocations")
	}
	return &Template{
		initial: raw[segment.Off : segment.Off+segment.Filesz],
		size:    segment.Memsz,
		align:   segment.Align,
	}, nil
}

func (template *Template) Register() (uint64, error) {
	if template == nil {
		return 0, nil
	}
	return register(template.initial, template.size, template.align)
}

// Relocate handles only the three relocation forms needed for local
// general-dynamic TLS. The caller applies all other relocations normally.
func Relocate(moduleID uint64, size uint64, symbols []elf.Symbol, symbolIndex uint32, relocation uint32, addend int64) (uint64, bool, error) {
	if moduleID == 0 {
		return 0, false, nil
	}
	switch elf.R_X86_64(relocation) {
	case elf.R_X86_64_DTPMOD64:
		if symbolIndex != 0 {
			return 0, true, errors.New("general PT_TLS DTPMOD64 has an external symbol")
		}
		return moduleID, true, nil
	case elf.R_X86_64_DTPOFF64:
		if symbolIndex != 0 || addend < 0 || uint64(addend) >= size {
			return 0, true, fmt.Errorf("general PT_TLS DTPOFF64 offset %d is outside local template", addend)
		}
		return uint64(addend), true, nil
	case elf.R_X86_64_JMP_SLOT, elf.R_X86_64_GLOB_DAT:
		if symbolIndex != 0 && int(symbolIndex) <= len(symbols) && symbols[symbolIndex-1].Name == "__tls_get_addr" {
			return resolverAddress() + uint64(addend), true, nil
		}
	}
	if relocation >= 16 && relocation <= 23 || relocation >= 34 && relocation <= 36 {
		return 0, true, fmt.Errorf("general PT_TLS relocation %d is unsupported", relocation)
	}
	return 0, false, nil
}

func Release(moduleID uint64) {
	if moduleID != 0 {
		release(moduleID)
	}
}
