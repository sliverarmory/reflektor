package elfrelr

import (
	"debug/elf"
	"encoding/binary"
	"strings"
	"testing"
	"unsafe"
)

func TestApplyTable64(t *testing.T) {
	mapping := make([]byte, 1024)
	loadBias := uintptr(unsafe.Pointer(&mapping[0])) - 0x100
	binary.LittleEndian.PutUint64(mapping[0:], 0x20)
	binary.LittleEndian.PutUint64(mapping[8:], 0x30)
	binary.LittleEndian.PutUint64(mapping[24:], 0x40)
	binary.LittleEndian.PutUint64(mapping[512:], 0x50)

	data := make([]byte, 24)
	binary.LittleEndian.PutUint64(data[0:], 0x100)
	binary.LittleEndian.PutUint64(data[8:], 1|(1<<1)|(1<<3))
	binary.LittleEndian.PutUint64(data[16:], 1|(1<<1))
	if err := applyTable(data, elf.ELFCLASS64, mapping, loadBias); err != nil {
		t.Fatalf("apply packed RELR: %v", err)
	}
	for _, test := range []struct {
		index int
		want  uint64
	}{
		{index: 0, want: uint64(loadBias) + 0x20},
		{index: 8, want: uint64(loadBias) + 0x30},
		{index: 24, want: uint64(loadBias) + 0x40},
		{index: 512, want: uint64(loadBias) + 0x50},
	} {
		if got := binary.LittleEndian.Uint64(mapping[test.index:]); got != test.want {
			t.Fatalf("word at %d = %#x, want %#x", test.index, got, test.want)
		}
	}
}

func TestApplyTable32(t *testing.T) {
	mapping := make([]byte, 128)
	loadBias := uintptr(unsafe.Pointer(&mapping[0])) - 0x100
	binary.LittleEndian.PutUint32(mapping[0:], 0x20)
	binary.LittleEndian.PutUint32(mapping[4:], 0x30)
	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:], 0x100)
	binary.LittleEndian.PutUint32(data[4:], 1|(1<<1))
	if err := applyTable(data, elf.ELFCLASS32, mapping, loadBias); err != nil {
		t.Fatalf("apply 32-bit packed RELR: %v", err)
	}
	for index, addend := range map[int]uint32{0: 0x20, 4: 0x30} {
		if got, want := binary.LittleEndian.Uint32(mapping[index:]), uint32(loadBias)+addend; got != want {
			t.Fatalf("word at %d = %#x, want %#x", index, got, want)
		}
	}
}

func TestApplyTableRejectsMalformed(t *testing.T) {
	mapping := make([]byte, 64)
	loadBias := uintptr(unsafe.Pointer(&mapping[0])) - 0x100
	for _, test := range []struct {
		name    string
		entries []uint64
		bytes   []byte
		want    string
	}{
		{name: "misaligned size", bytes: []byte{1}, want: "not a multiple"},
		{name: "bitmap without base", entries: []uint64{3}, want: "no base address"},
		{name: "unaligned target", entries: []uint64{0x102}, want: "unaligned"},
		{name: "target outside mapping", entries: []uint64{0x200}, want: "outside mapped image"},
		{name: "bitmap outside mapping", entries: []uint64{0x100, 1 | (1 << 10)}, want: "outside mapped image"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := test.bytes
			if data == nil {
				data = make([]byte, len(test.entries)*8)
				for index, entry := range test.entries {
					binary.LittleEndian.PutUint64(data[index*8:], entry)
				}
			}
			if err := applyTable(data, elf.ELFCLASS64, mapping, loadBias); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("applyTable error = %v, want %q", err, test.want)
			}
		})
	}
}
