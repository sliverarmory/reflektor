package bofloader

import (
	"debug/elf"
	"fmt"
)

func elfRelocationWidth(arch string, typeID uint32) (int, bool, error) {
	switch arch {
	case "amd64":
		switch elf.R_X86_64(typeID) {
		case elf.R_X86_64_64, elf.R_X86_64_PC64, elf.R_X86_64_GOTOFF64,
			elf.R_X86_64_GOTPCREL64, elf.R_X86_64_GOTPC64, elf.R_X86_64_SIZE64:
			return 8, false, nil
		case elf.R_X86_64_PC32, elf.R_X86_64_GOT32, elf.R_X86_64_PLT32,
			elf.R_X86_64_GOTPCREL, elf.R_X86_64_32, elf.R_X86_64_32S,
			elf.R_X86_64_GOTPC32, elf.R_X86_64_SIZE32,
			elf.R_X86_64_GOTPCRELX, elf.R_X86_64_REX_GOTPCRELX:
			return 4, false, nil
		}
	case "386":
		switch elf.R_386(typeID) {
		case elf.R_386_32, elf.R_386_PC32, elf.R_386_GOT32, elf.R_386_PLT32,
			elf.R_386_GOTOFF, elf.R_386_GOTPC, elf.R_386_SIZE32, elf.R_386_GOT32X:
			return 4, false, nil
		}
	case "arm64":
		switch elf.R_AARCH64(typeID) {
		case elf.R_AARCH64_ABS64, elf.R_AARCH64_PREL64:
			return 8, false, nil
		case elf.R_AARCH64_ABS16, elf.R_AARCH64_PREL16:
			return 2, false, nil
		case elf.R_AARCH64_ABS32, elf.R_AARCH64_PREL32,
			elf.R_AARCH64_LD_PREL_LO19, elf.R_AARCH64_ADR_PREL_LO21,
			elf.R_AARCH64_ADR_PREL_PG_HI21, elf.R_AARCH64_ADR_PREL_PG_HI21_NC,
			elf.R_AARCH64_ADD_ABS_LO12_NC, elf.R_AARCH64_LDST8_ABS_LO12_NC,
			elf.R_AARCH64_TSTBR14, elf.R_AARCH64_CONDBR19,
			elf.R_AARCH64_JUMP26, elf.R_AARCH64_CALL26,
			elf.R_AARCH64_LDST16_ABS_LO12_NC, elf.R_AARCH64_LDST32_ABS_LO12_NC,
			elf.R_AARCH64_LDST64_ABS_LO12_NC, elf.R_AARCH64_LDST128_ABS_LO12_NC,
			elf.R_AARCH64_ADR_GOT_PAGE, elf.R_AARCH64_LD64_GOT_LO12_NC:
			return 4, false, nil
		}
	default:
		return 0, false, fmt.Errorf("unsupported ELF architecture %q", arch)
	}
	return 0, false, fmt.Errorf("unsupported ELF/%s relocation", arch)
}

func elfRelocationNeedsGOT(arch string, typeID uint32) bool {
	switch arch {
	case "amd64":
		switch elf.R_X86_64(typeID) {
		case elf.R_X86_64_GOT32, elf.R_X86_64_GOTPCREL, elf.R_X86_64_GOTPCREL64,
			elf.R_X86_64_GOTPCRELX, elf.R_X86_64_REX_GOTPCRELX:
			return true
		}
	case "386":
		switch elf.R_386(typeID) {
		case elf.R_386_GOT32, elf.R_386_GOT32X:
			return true
		}
	case "arm64":
		switch elf.R_AARCH64(typeID) {
		case elf.R_AARCH64_ADR_GOT_PAGE, elf.R_AARCH64_LD64_GOT_LO12_NC:
			return true
		}
	}
	return false
}

func applyELFRelocation(object *objectFile, relocation objectRelocation, location []byte, place uint64, linked linkedSymbol, externals map[uint32]externalSymbol) error {
	switch object.arch {
	case "amd64":
		return applyELFAMD64Relocation(relocation, location, place, linked, externals)
	case "386":
		return applyELFI386Relocation(relocation, location, place, linked, externals)
	case "arm64":
		return applyELFARM64Relocation(relocation, location, place, linked)
	default:
		return fmt.Errorf("unsupported ELF architecture %q", object.arch)
	}
}

func applyELFAMD64Relocation(relocation objectRelocation, location []byte, place uint64, linked linkedSymbol, externals map[uint32]externalSymbol) error {
	addend, err := relocationAddend(location, relocation.hasAdd, relocation.addend)
	if err != nil {
		return err
	}
	switch elf.R_X86_64(relocation.typeID) {
	case elf.R_X86_64_64:
		value, err := addSigned(directLinkedAddress(linked), addend)
		if err != nil {
			return err
		}
		putUint64(location, value)
		return nil
	case elf.R_X86_64_PC32:
		// PC32 is also used for external data. PLT32 is the unambiguous
		// external-call relocation and is handled through a near thunk below.
		value, err := relativeValue(directLinkedAddress(linked), addend, place, 0)
		if err != nil {
			return err
		}
		return putInt32(location, value)
	case elf.R_X86_64_PLT32:
		value, err := relativeValue(thunkLinkedAddress(linked), addend, place, 0)
		if err != nil {
			return err
		}
		return putInt32(location, value)
	case elf.R_X86_64_32:
		value, err := addSigned(directLinkedAddress(linked), addend)
		if err != nil {
			return err
		}
		return putUint32(location, value)
	case elf.R_X86_64_32S:
		value, err := absoluteSignedValue(directLinkedAddress(linked), addend)
		if err != nil {
			return err
		}
		return putInt32(location, value)
	case elf.R_X86_64_PC64:
		value, err := relativeValue(directLinkedAddress(linked), addend, place, 0)
		if err != nil {
			return err
		}
		putInt64(location, value)
		return nil
	case elf.R_X86_64_GOTPCREL, elf.R_X86_64_GOTPCREL64,
		elf.R_X86_64_GOTPCRELX, elf.R_X86_64_REX_GOTPCRELX:
		got, err := gotLinkedAddress(linked)
		if err != nil {
			return err
		}
		value, err := relativeValue(got, addend, place, 0)
		if err != nil {
			return err
		}
		if len(location) == 8 {
			putInt64(location, value)
			return nil
		}
		return putInt32(location, value)
	case elf.R_X86_64_GOT32:
		got, err := gotLinkedAddress(linked)
		if err != nil {
			return err
		}
		base, err := elfGOTBase(linked, externals)
		if err != nil {
			return err
		}
		value, err := relativeValue(got, addend, base, 0)
		if err != nil {
			return err
		}
		return putInt32(location, value)
	case elf.R_X86_64_GOTPC32, elf.R_X86_64_GOTPC64:
		base, err := elfGOTBase(linked, externals)
		if err != nil {
			return err
		}
		value, err := relativeValue(base, addend, place, 0)
		if err != nil {
			return err
		}
		if len(location) == 8 {
			putInt64(location, value)
			return nil
		}
		return putInt32(location, value)
	case elf.R_X86_64_GOTOFF64:
		base, err := elfGOTBase(linked, externals)
		if err != nil {
			return err
		}
		value, err := relativeValue(directLinkedAddress(linked), addend, base, 0)
		if err != nil {
			return err
		}
		putInt64(location, value)
		return nil
	case elf.R_X86_64_SIZE32, elf.R_X86_64_SIZE64:
		value, err := addSigned(linked.symbol.size, addend)
		if err != nil {
			return err
		}
		if len(location) == 8 {
			putUint64(location, value)
			return nil
		}
		return putUint32(location, value)
	default:
		return fmt.Errorf("unsupported ELF/amd64 relocation")
	}
}

func applyELFI386Relocation(relocation objectRelocation, location []byte, place uint64, linked linkedSymbol, externals map[uint32]externalSymbol) error {
	addend, err := relocationAddend(location, relocation.hasAdd, relocation.addend)
	if err != nil {
		return err
	}
	switch elf.R_386(relocation.typeID) {
	case elf.R_386_32:
		value, err := addSigned(directLinkedAddress(linked), addend)
		if err != nil {
			return err
		}
		return putUint32(location, value)
	case elf.R_386_PC32, elf.R_386_PLT32:
		value, err := relativeValue(thunkLinkedAddress(linked), addend, place, 0)
		if err != nil {
			return err
		}
		return putInt32(location, value)
	case elf.R_386_GOT32, elf.R_386_GOT32X:
		got, err := gotLinkedAddress(linked)
		if err != nil {
			return err
		}
		base, err := elfGOTBase(linked, externals)
		if err != nil {
			return err
		}
		value, err := relativeValue(got, addend, base, 0)
		if err != nil {
			return err
		}
		return putInt32(location, value)
	case elf.R_386_GOTOFF:
		base, err := elfGOTBase(linked, externals)
		if err != nil {
			return err
		}
		value, err := relativeValue(directLinkedAddress(linked), addend, base, 0)
		if err != nil {
			return err
		}
		return putInt32(location, value)
	case elf.R_386_GOTPC:
		base, err := elfGOTBase(linked, externals)
		if err != nil {
			return err
		}
		value, err := relativeValue(base, addend, place, 0)
		if err != nil {
			return err
		}
		return putInt32(location, value)
	case elf.R_386_SIZE32:
		value, err := addSigned(linked.symbol.size, addend)
		if err != nil {
			return err
		}
		return putUint32(location, value)
	default:
		return fmt.Errorf("unsupported ELF/386 relocation")
	}
}

func applyELFARM64Relocation(relocation objectRelocation, location []byte, place uint64, linked linkedSymbol) error {
	typeID := elf.R_AARCH64(relocation.typeID)
	switch typeID {
	case elf.R_AARCH64_ABS64, elf.R_AARCH64_ABS32, elf.R_AARCH64_ABS16:
		addend, err := relocationAddend(location, relocation.hasAdd, relocation.addend)
		if err != nil {
			return err
		}
		value, err := addSigned(directLinkedAddress(linked), addend)
		if err != nil {
			return err
		}
		switch typeID {
		case elf.R_AARCH64_ABS64:
			putUint64(location, value)
			return nil
		case elf.R_AARCH64_ABS32:
			return putUint32(location, value)
		default:
			return putUint16(location, value)
		}
	case elf.R_AARCH64_PREL64, elf.R_AARCH64_PREL32, elf.R_AARCH64_PREL16:
		addend, err := relocationAddend(location, relocation.hasAdd, relocation.addend)
		if err != nil {
			return err
		}
		value, err := relativeValue(directLinkedAddress(linked), addend, place, 0)
		if err != nil {
			return err
		}
		switch typeID {
		case elf.R_AARCH64_PREL64:
			putInt64(location, value)
			return nil
		case elf.R_AARCH64_PREL32:
			return putInt32(location, value)
		default:
			return putInt16(location, value)
		}
	case elf.R_AARCH64_LD_PREL_LO19:
		return applyARM64Literal19(location, directLinkedAddress(linked), place, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_ADR_PREL_LO21:
		return applyARM64ADR(location, directLinkedAddress(linked), place, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_ADR_PREL_PG_HI21:
		return applyARM64ADRP(location, directLinkedAddress(linked), place, relocation.hasAdd, relocation.addend, true)
	case elf.R_AARCH64_ADR_PREL_PG_HI21_NC:
		return applyARM64ADRP(location, directLinkedAddress(linked), place, relocation.hasAdd, relocation.addend, false)
	case elf.R_AARCH64_ADD_ABS_LO12_NC:
		return applyARM64AddLO12(location, directLinkedAddress(linked), relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_LDST8_ABS_LO12_NC:
		return applyARM64LoadStoreLO12(location, directLinkedAddress(linked), 0, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_LDST16_ABS_LO12_NC:
		return applyARM64LoadStoreLO12(location, directLinkedAddress(linked), 1, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_LDST32_ABS_LO12_NC:
		return applyARM64LoadStoreLO12(location, directLinkedAddress(linked), 2, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_LDST64_ABS_LO12_NC:
		return applyARM64LoadStoreLO12(location, directLinkedAddress(linked), 3, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_LDST128_ABS_LO12_NC:
		return applyARM64LoadStoreLO12(location, directLinkedAddress(linked), 4, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_TSTBR14:
		return applyARM64Branch14(location, thunkLinkedAddress(linked), place, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_CONDBR19:
		return applyARM64Branch19(location, thunkLinkedAddress(linked), place, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_JUMP26, elf.R_AARCH64_CALL26:
		return applyARM64Branch26(location, thunkLinkedAddress(linked), place, relocation.hasAdd, relocation.addend)
	case elf.R_AARCH64_ADR_GOT_PAGE:
		got, err := gotLinkedAddress(linked)
		if err != nil {
			return err
		}
		return applyARM64ADRP(location, got, place, relocation.hasAdd, relocation.addend, true)
	case elf.R_AARCH64_LD64_GOT_LO12_NC:
		got, err := gotLinkedAddress(linked)
		if err != nil {
			return err
		}
		return applyARM64LoadStoreLO12(location, got, 3, relocation.hasAdd, relocation.addend)
	default:
		return fmt.Errorf("unsupported ELF/arm64 relocation")
	}
}

func elfGOTBase(linked linkedSymbol, externals map[uint32]externalSymbol) (uint64, error) {
	if linked.symbol.name == "_GLOBAL_OFFSET_TABLE_" && linked.address != 0 {
		return linked.address, nil
	}
	for _, external := range externals {
		if external.name == "_GLOBAL_OFFSET_TABLE_" && external.target != 0 {
			return uint64(external.target), nil
		}
	}
	return gotBaseAddress(externals)
}
