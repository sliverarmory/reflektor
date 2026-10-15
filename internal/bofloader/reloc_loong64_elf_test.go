package bofloader

import (
	"debug/elf"
	"encoding/binary"
	"strings"
	"testing"
)

func TestELFLoong64RelocationCapabilities(t *testing.T) {
	for _, typeID := range []elf.R_LARCH{
		elf.R_LARCH_CALL36, elf.R_LARCH_PCALA_HI20, elf.R_LARCH_PCALA_LO12,
		elf.R_LARCH_GOT_PC_HI20, elf.R_LARCH_GOT_PC_LO12,
	} {
		width, noop, err := elfRelocationWidth("loong64", uint32(typeID))
		wantWidth := 4
		if typeID == elf.R_LARCH_CALL36 {
			wantWidth = 8
		}
		if err != nil || noop || width != wantWidth {
			t.Fatalf("loong64 relocation %s: width=%d noop=%v err=%v", typeID, width, noop, err)
		}
	}
	for _, typeID := range []elf.R_LARCH{elf.R_LARCH_GOT_PC_HI20, elf.R_LARCH_GOT_PC_LO12} {
		if !elfRelocationNeedsGOT("loong64", uint32(typeID)) {
			t.Fatalf("loong64 relocation %s must allocate a GOT slot", typeID)
		}
	}
	if _, _, err := elfRelocationWidth("loong64", uint32(elf.R_LARCH_TLS_GD_PC_HI20)); err == nil {
		t.Fatal("LoongArch TLS relocation was accepted without TLS support")
	}
}

func TestLoong64Call36AndBranch26(t *testing.T) {
	for _, delta := range []int64{0, 4, 0x12345678, -0x8765434} {
		call := make([]byte, 8)
		binary.LittleEndian.PutUint32(call[:4], 0x1e000001)
		binary.LittleEndian.PutUint32(call[4:], 0x4c000021)
		place := uint64(0x400000000)
		if err := patchLoongCall36(call, uint64(int64(place)+delta), place, 0); err != nil {
			t.Fatalf("CALL36 delta %d: %v", delta, err)
		}
		hi := signExtend(uint64(binary.LittleEndian.Uint32(call[:4])>>5)&0xfffff, 20) << 18
		lo := signExtend(uint64(binary.LittleEndian.Uint32(call[4:])>>10)&0xffff, 16) << 2
		if got := hi + lo; got != delta {
			t.Fatalf("CALL36 encoded displacement = %d, want %d", got, delta)
		}
	}
	bad := make([]byte, 8)
	binary.LittleEndian.PutUint32(bad[:4], 0x1e000001)
	binary.LittleEndian.PutUint32(bad[4:], 0x4c000041)
	if err := patchLoongCall36(bad, 0x1000, 0x1000, 0); err == nil || !strings.Contains(err.Error(), "matching jirl") {
		t.Fatalf("mismatched CALL36 pair error = %v", err)
	}
	branch := make([]byte, 4)
	binary.LittleEndian.PutUint32(branch, 0x54000000)
	if err := patchLoongBranch26(branch, 0x1200, 0x1000, 0); err != nil {
		t.Fatal(err)
	}
	word := binary.LittleEndian.Uint32(branch)
	units := (uint64(word&0x3ff) << 16) | uint64((word>>10)&0xffff)
	if got := signExtend(units, 26) << 2; got != 0x200 {
		t.Fatalf("B26 encoded displacement = %#x, want 0x200", got)
	}
}

func TestLoong64PageRelocationAndThunk(t *testing.T) {
	page := make([]byte, 4)
	low := make([]byte, 4)
	binary.LittleEndian.PutUint32(page, 0x1a00000c)
	binary.LittleEndian.PutUint32(low, 0x28c0018c)
	place, target := uint64(0x40000ffc), uint64(0x40002980)
	if err := patchLoongPageHigh20(page, target, place, 0); err != nil {
		t.Fatal(err)
	}
	if err := patchLoongLow12(low, target, 0); err != nil {
		t.Fatal(err)
	}
	high := signExtend(uint64(binary.LittleEndian.Uint32(page)>>5)&0xfffff, 20) << 12
	lo := signExtend(uint64(binary.LittleEndian.Uint32(low)>>10)&0xfff, 12)
	if got := int64(place&^0xfff) + high + lo; got != int64(target) {
		t.Fatalf("PCALA pair resolves to %#x, want %#x", got, target)
	}

	thunk := make([]byte, 16)
	if err := writeLoong64Thunk(thunk, uintptr(place), uintptr(target)); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(thunk[8:12]); got != 0x4c000180 {
		t.Fatalf("import thunk tail call = %#08x, want jirl zero,t0,0", got)
	}
	if err := writeLoong64Thunk(thunk[:15], uintptr(place), uintptr(target)); err == nil {
		t.Fatal("short import thunk destination accepted")
	}
}
