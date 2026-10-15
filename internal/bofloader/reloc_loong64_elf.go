package bofloader

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
)

// LoongArch instruction relocations below follow the LP64D ELF psABI. The
// loader owns one contiguous image, so PCALA and GOT targets remain within
// the signed 32-bit page span even when host callbacks are far away.
func applyELFLoong64Relocation(relocation objectRelocation, location []byte, place uint64, linked linkedSymbol) error {
	if !relocation.hasAdd {
		return fmt.Errorf("LoongArch relocations require RELA encoding")
	}
	target := directLinkedAddress(linked)
	switch elf.R_LARCH(relocation.typeID) {
	case elf.R_LARCH_64:
		value, err := addSigned(target, relocation.addend)
		if err != nil {
			return err
		}
		putUint64(location, value)
		return nil
	case elf.R_LARCH_32:
		value, err := addSigned(target, relocation.addend)
		if err != nil {
			return err
		}
		return putUint32(location, value)
	case elf.R_LARCH_64_PCREL:
		delta, err := relativeValue(target, relocation.addend, place, 0)
		if err != nil {
			return err
		}
		putUint64(location, uint64(delta))
		return nil
	case elf.R_LARCH_32_PCREL:
		delta, err := relativeValue(target, relocation.addend, place, 0)
		if err != nil {
			return err
		}
		return putInt32(location, delta)
	case elf.R_LARCH_CALL36:
		if linked.external != nil && relocation.addend != 0 {
			return fmt.Errorf("external LoongArch CALL36 relocation must have a zero addend")
		}
		return patchLoongCall36(location, thunkLinkedAddress(linked), place, relocation.addend)
	case elf.R_LARCH_B26:
		return patchLoongBranch26(location, thunkLinkedAddress(linked), place, relocation.addend)
	case elf.R_LARCH_PCALA_HI20:
		return patchLoongPageHigh20(location, target, place, relocation.addend)
	case elf.R_LARCH_PCALA_LO12:
		return patchLoongLow12(location, target, relocation.addend)
	case elf.R_LARCH_GOT_PC_HI20, elf.R_LARCH_GOT_PC_LO12:
		got, err := gotLinkedAddress(linked)
		if err != nil {
			return err
		}
		if relocation.addend != 0 {
			return fmt.Errorf("LoongArch GOT relocation must have a zero addend")
		}
		if elf.R_LARCH(relocation.typeID) == elf.R_LARCH_GOT_PC_HI20 {
			return patchLoongPageHigh20(location, got, place, 0)
		}
		return patchLoongLow12(location, got, 0)
	default:
		return fmt.Errorf("unsupported ELF/loong64 relocation")
	}
}

func patchLoongCall36(location []byte, target, place uint64, addend int64) error {
	if len(location) != 8 {
		return fmt.Errorf("LoongArch CALL36 requires an 8-byte instruction pair")
	}
	highWord := binary.LittleEndian.Uint32(location[:4])
	lowWord := binary.LittleEndian.Uint32(location[4:])
	if highWord&0xfe000000 != 0x1e000000 || lowWord&0xfc000000 != 0x4c000000 ||
		(lowWord>>5)&0x1f != highWord&0x1f {
		return fmt.Errorf("LoongArch CALL36 requires pcaddu18i and matching jirl, got %#08x/%#08x", highWord, lowWord)
	}
	delta, err := relativeValue(target, addend, place, 0)
	if err != nil {
		return err
	}
	if delta&3 != 0 || delta < -(1<<37) || delta > (1<<37)-4 {
		return fmt.Errorf("LoongArch CALL36 displacement %d exceeds aligned signed 38-bit range", delta)
	}
	// The two instructions encode offset bits [37:18] and [17:2].
	// The high part is rounded because JIRL sign-extends its low 16 bits.
	units := delta >> 2
	high := (units + (1 << 15)) >> 16
	highWord = (highWord &^ (0xfffff << 5)) | ((uint32(high) & 0xfffff) << 5)
	lowWord = (lowWord &^ (0xffff << 10)) | ((uint32(units) & 0xffff) << 10)
	binary.LittleEndian.PutUint32(location[:4], highWord)
	binary.LittleEndian.PutUint32(location[4:], lowWord)
	return nil
}

func patchLoongBranch26(location []byte, target, place uint64, addend int64) error {
	word := binary.LittleEndian.Uint32(location)
	if word&0xfc000000 != 0x50000000 && word&0xfc000000 != 0x54000000 {
		return fmt.Errorf("LoongArch B26 requires b or bl, got %#08x", word)
	}
	delta, err := relativeValue(target, addend, place, 0)
	if err != nil {
		return err
	}
	if delta&3 != 0 || delta < -(1<<27) || delta > (1<<27)-4 {
		return fmt.Errorf("LoongArch B26 displacement %d exceeds aligned signed 28-bit range", delta)
	}
	units := uint32(delta >> 2)
	word = (word & 0xfc000000) | ((units & 0xffff) << 10) | ((units >> 16) & 0x3ff)
	binary.LittleEndian.PutUint32(location, word)
	return nil
}

func patchLoongPageHigh20(location []byte, target, place uint64, addend int64) error {
	word := binary.LittleEndian.Uint32(location)
	if word&0xfe000000 != 0x1a000000 {
		return fmt.Errorf("LoongArch page relocation requires pcalau12i, got %#08x", word)
	}
	value, err := addSigned(target, addend)
	if err != nil {
		return err
	}
	adjusted, err := addSigned(value, 0x800)
	if err != nil {
		return err
	}
	delta, err := signedDifference(adjusted&^uint64(0xfff), place&^uint64(0xfff))
	if err != nil {
		return err
	}
	pages := delta >> 12
	if pages < -(1<<19) || pages > (1<<19)-1 {
		return fmt.Errorf("LoongArch page displacement %d exceeds signed 20-bit range", pages)
	}
	word = (word &^ (0xfffff << 5)) | ((uint32(pages) & 0xfffff) << 5)
	binary.LittleEndian.PutUint32(location, word)
	return nil
}

func patchLoongLow12(location []byte, target uint64, addend int64) error {
	word := binary.LittleEndian.Uint32(location)
	// The BOF fixtures use addi.d for addresses and ld.d for GOT slots.
	// Corpus hash routines load constant vectors with LSX vld.
	if word&0xffc00000 != 0x02c00000 && word&0xffc00000 != 0x28c00000 &&
		word&0xffc00000 != 0x2c000000 {
		return fmt.Errorf("LoongArch low relocation requires addi.d, ld.d, or vld, got %#08x", word)
	}
	value, err := addSigned(target, addend)
	if err != nil {
		return err
	}
	word = (word &^ (0xfff << 10)) | (uint32(value&0xfff) << 10)
	binary.LittleEndian.PutUint32(location, word)
	return nil
}
