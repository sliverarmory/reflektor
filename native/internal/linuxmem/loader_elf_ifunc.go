//go:build (linux && !android && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)) || (freebsd && (amd64 || arm64))

package linuxmem

import (
	"debug/elf"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"unsafe"

	"github.com/sliverarmory/reflektor/internal/elfifunc"
	"golang.org/x/sys/unix"
)

type elfIFUNCDefinition struct {
	file          *elf.File
	mapped        mappedELF
	resolverVAddr uint64
	name          string
}

type deferredELFIFUNCRelocation struct {
	definition elfIFUNCDefinition
	place      uintptr
	kind       uint32
	addend     int64
	section    string
	index      int
}

type elfIFUNCKey struct {
	file     *elf.File
	resolver uint64
}

type elfIFUNCResolutionCache map[elfIFUNCKey]uintptr

func (cache elfIFUNCResolutionCache) resolve(definition elfIFUNCDefinition) (uintptr, error) {
	key := elfIFUNCKey{file: definition.file, resolver: definition.resolverVAddr}
	if implementation, exists := cache[key]; exists {
		if implementation == 0 {
			return 0, fmt.Errorf("IFUNC resolver %q recursively entered", definition.name)
		}
		return implementation, nil
	}
	cache[key] = 0
	implementation, err := resolveELFIFUNCDefinition(definition)
	if err != nil {
		delete(cache, key)
		return 0, err
	}
	cache[key] = implementation
	return implementation, nil
}

// queueELFIFUNCRelocation only identifies work. The caller must finish every
// ordinary relocation before executing the queued resolver.
func queueELFIFUNCRelocation(f *elf.File, mapped mappedELF, symbols []elf.Symbol, resolver *symbolResolver, symbolIndex uint32, kind uint32, offset uint64, addend int64, hasAddend bool, section string, index int) (bool, error) {
	if resolver == nil || !resolver.enableIFUNC || !elfifunc.Supported(f) {
		return false, nil
	}
	var definition elfIFUNCDefinition
	if elfifunc.IsIRelative(f.Machine, kind) {
		if symbolIndex != 0 || !hasAddend || addend < 0 {
			return false, fmt.Errorf("%s[%d]: x86_64 IRELATIVE requires a nonnegative RELA addend and symbol index zero", section, index)
		}
		definition = elfIFUNCDefinition{file: f, mapped: mapped, resolverVAddr: uint64(addend), name: "IRELATIVE"}
	} else if symbolIndex != 0 {
		symbol, ok := dynSymbolByIndex(symbols, symbolIndex)
		if !ok {
			return false, fmt.Errorf("%s[%d]: invalid IFUNC symbol index %d", section, index, symbolIndex)
		}
		if symbol.Section != elf.SHN_UNDEF && elf.ST_TYPE(symbol.Info) == elf.STT_GNU_IFUNC {
			definition = elfIFUNCDefinition{file: f, mapped: mapped, resolverVAddr: symbol.Value, name: symbol.Name}
		} else if symbol.Section == elf.SHN_UNDEF && resolver.resolveIFUNC != nil {
			var found bool
			var err error
			definition, found, err = resolver.resolveIFUNC(symbol)
			if err != nil {
				return false, fmt.Errorf("%s[%d]: resolve IFUNC provider %q: %w", section, index, symbol.Name, err)
			}
			if !found {
				return false, nil
			}
		} else {
			return false, nil
		}
	} else {
		return false, nil
	}
	if !elfifunc.IsIRelative(f.Machine, kind) {
		switch elf.R_X86_64(kind) {
		case elf.R_X86_64_JMP_SLOT, elf.R_X86_64_GLOB_DAT, elf.R_X86_64_64:
		default:
			return false, fmt.Errorf("%s[%d]: unsupported x86_64 relocation type %d for IFUNC %q", section, index, kind, definition.name)
		}
	}
	if !hasAddend {
		return false, fmt.Errorf("%s[%d]: x86_64 IFUNC requires RELA relocation", section, index)
	}
	if offset > math.MaxUint64-8 || uint64(uintptr(offset)) != offset || uintptr(offset) > ^uintptr(0)-mapped.loadBias {
		return false, fmt.Errorf("%s[%d]: IFUNC relocation target %#x overflows", section, index, offset)
	}
	place := mapped.loadBias + uintptr(offset)
	if !mappedAddressInRange(mapped.mapping, place, 8) {
		return false, fmt.Errorf("%s[%d]: IFUNC relocation target %#x is outside mapped image", section, index, offset)
	}
	if err := elfifunc.CheckWritableTarget(f, offset, 8); err != nil {
		return false, fmt.Errorf("%s[%d]: %w", section, index, err)
	}
	if err := elfifunc.CheckExecutable(definition.file, definition.resolverVAddr, "IFUNC resolver"); err != nil {
		return false, fmt.Errorf("%s[%d]: %w", section, index, err)
	}
	resolver.deferredIFUNC = append(resolver.deferredIFUNC, deferredELFIFUNCRelocation{
		definition: definition, place: place, kind: kind, addend: addend, section: section, index: index,
	})
	return true, nil
}

func resolveELFIFUNCDefinition(definition elfIFUNCDefinition) (uintptr, error) {
	if !elfifunc.Supported(definition.file) {
		return 0, errors.New("custom ELF IFUNC resolver ABI is unsupported")
	}
	if err := elfifunc.CheckExecutable(definition.file, definition.resolverVAddr, "IFUNC resolver"); err != nil {
		return 0, err
	}
	if uint64(uintptr(definition.resolverVAddr)) != definition.resolverVAddr || uintptr(definition.resolverVAddr) > ^uintptr(0)-definition.mapped.loadBias {
		return 0, fmt.Errorf("IFUNC resolver %q address overflows", definition.name)
	}
	address := definition.mapped.loadBias + uintptr(definition.resolverVAddr)
	if !mappedAddressInRange(definition.mapped.mapping, address, 1) {
		return 0, fmt.Errorf("IFUNC resolver %q is outside mapped image", definition.name)
	}
	runtime.LockOSThread()
	implementation := callExportFunction(address)
	runtime.UnlockOSThread()
	if implementation == 0 {
		return 0, fmt.Errorf("IFUNC resolver %q returned null implementation", definition.name)
	}
	if implementation < definition.mapped.loadBias || !mappedAddressInRange(definition.mapped.mapping, implementation, 1) {
		return 0, fmt.Errorf("IFUNC resolver %q returned implementation %#x outside provider image", definition.name, implementation)
	}
	if err := elfifunc.CheckExecutable(definition.file, uint64(implementation-definition.mapped.loadBias), "IFUNC implementation"); err != nil {
		return 0, fmt.Errorf("IFUNC resolver %q returned implementation: %w", definition.name, err)
	}
	return implementation, nil
}

func resolveDeferredELFIFUNC(relocations []deferredELFIFUNCRelocation, mapped mappedELF, cache elfIFUNCResolutionCache) error {
	for _, relocation := range relocations {
		implementation, err := cache.resolve(relocation.definition)
		if err != nil {
			return fmt.Errorf("%s[%d]: %w", relocation.section, relocation.index, err)
		}
		if elf.R_X86_64(relocation.kind) == elf.R_X86_64_IRELATIVE {
			writeU64(relocation.place, uint64(implementation))
			continue
		}
		if err := applyX8664Reloc(relocation.kind, relocation.place, mapped.loadBias, implementation, relocation.addend, mapped.tlsOffset, mapped.hasTLS); err != nil {
			return fmt.Errorf("%s[%d]: bind IFUNC implementation: %w", relocation.section, relocation.index, err)
		}
	}
	return nil
}

// protectELFExecutableSegments makes all code callable while ordinary data
// and deferred GOT/PLT targets remain writable. Full PT_LOAD protections are
// applied after resolver writes complete.
func protectELFExecutableSegments(mapped mappedELF) error {
	pageSize := uint64(unix.Getpagesize())
	if pageSize == 0 {
		return errors.New("invalid page size")
	}
	for _, program := range mapped.progs {
		if program.Type != elf.PT_LOAD || program.Memsz == 0 || program.Flags&elf.PF_X == 0 {
			continue
		}
		if program.Flags&elf.PF_W != 0 || program.Vaddr > math.MaxUint64-program.Memsz {
			return fmt.Errorf("IFUNC executable PT_LOAD vaddr=%#x is writable or overflows", program.Vaddr)
		}
		start := alignDown64(program.Vaddr, pageSize)
		end := alignUp64(program.Vaddr+program.Memsz, pageSize)
		if end <= start {
			return fmt.Errorf("IFUNC executable PT_LOAD vaddr=%#x has invalid page range", program.Vaddr)
		}
		length, err := u64ToInt(end - start)
		if err != nil {
			return err
		}
		address := mapped.loadBias + uintptr(start)
		if !mappedAddressInRange(mapped.mapping, address, length) {
			return fmt.Errorf("IFUNC executable PT_LOAD vaddr=%#x is outside mapped image", program.Vaddr)
		}
		pages := unsafe.Slice((*byte)(unsafe.Pointer(address)), length)
		if err := unix.Mprotect(pages, unix.PROT_READ|unix.PROT_EXEC); err != nil {
			return fmt.Errorf("mprotect IFUNC executable PT_LOAD vaddr=%#x: %w", program.Vaddr, err)
		}
	}
	return nil
}

func buildExportedELFIFUNCSymbolTable(f *elf.File, mapped mappedELF, cache elfIFUNCResolutionCache) (map[string]uintptr, error) {
	out := buildExportedSymbolTable(f, mapped.loadBias)
	for _, readSymbols := range []func() ([]elf.Symbol, error){f.DynamicSymbols, f.Symbols} {
		symbols, err := readSymbols()
		if err != nil {
			continue
		}
		for _, symbol := range symbols {
			if symbol.Name == "" || symbol.Section == elf.SHN_UNDEF || elf.ST_TYPE(symbol.Info) != elf.STT_GNU_IFUNC {
				continue
			}
			binding := elf.ST_BIND(symbol.Info)
			if binding != elf.STB_GLOBAL && binding != elf.STB_WEAK {
				continue
			}
			visibility := elf.ST_VISIBILITY(symbol.Other)
			if visibility == elf.STV_HIDDEN || visibility == elf.STV_INTERNAL {
				continue
			}
			if _, exists := out[symbol.Name]; exists {
				continue
			}
			implementation, err := cache.resolve(elfIFUNCDefinition{file: f, mapped: mapped, resolverVAddr: symbol.Value, name: symbol.Name})
			if err != nil {
				return nil, fmt.Errorf("exported IFUNC %q: %w", symbol.Name, err)
			}
			out[symbol.Name] = implementation
			if at := strings.IndexByte(symbol.Name, '@'); at > 0 {
				if _, exists := out[symbol.Name[:at]]; !exists {
					out[symbol.Name[:at]] = implementation
				}
			}
		}
	}
	return out, nil
}
