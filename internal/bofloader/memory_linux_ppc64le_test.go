//go:build linux && !android && ppc64le

package bofloader

import (
	"encoding/binary"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

func TestPPC64LECacheBlockRange(t *testing.T) {
	tests := []struct {
		name               string
		start, end         uintptr
		wantStart, wantEnd uintptr
	}{
		{name: "aligned", start: 0x1020, end: 0x1040, wantStart: 0x1020, wantEnd: 0x1040},
		{name: "one unaligned block", start: 0x1021, end: 0x103f, wantStart: 0x1020, wantEnd: 0x1040},
		{name: "unaligned boundary crossing", start: 0x103f, end: 0x1041, wantStart: 0x1020, wantEnd: 0x1060},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start, end := ppc64CacheBlockRange(test.start, test.end)
			if start != test.wantStart || end != test.wantEnd {
				t.Fatalf("ppc64CacheBlockRange(%#x, %#x) = (%#x, %#x), want (%#x, %#x)", test.start, test.end, start, end, test.wantStart, test.wantEnd)
			}
		})
	}
}

func TestPPC64LEExecutableMemory(t *testing.T) {
	region, err := allocateMemory(memoryPageSize())
	if err != nil {
		t.Fatal(err)
	}
	defer region.close()

	if err := region.protect(0, memoryPageSize(), protRead|protWrite|protExec); err == nil {
		t.Fatal("writable executable mapping was accepted")
	}

	// li r5, 42; std r5, 0(r3); blr
	binary.LittleEndian.PutUint32(region.data[0:4], 0x38a0002a)
	binary.LittleEndian.PutUint32(region.data[4:8], 0xf8a30000)
	binary.LittleEndian.PutUint32(region.data[8:12], 0x4e800020)
	if err := region.protect(0, memoryPageSize(), protRead|protExec); err != nil {
		t.Fatal(err)
	}
	if err := region.flushInstructionCache(0, memoryPageSize()); err != nil {
		t.Fatal(err)
	}
	var result uint64
	invokeEntry(region.base(), uintptr(unsafe.Pointer(&result)), int32(unsafe.Sizeof(result)))
	if result != 42 {
		t.Fatalf("executable mapping stored %d, want 42", result)
	}
}

func TestPPC64LEPureGoBridge(t *testing.T) {
	// Ten arguments cross the ELFv2 ABI's eight integer-argument register
	// boundary and exercise PureGo's separate ppc64le stack-argument frame.
	callback := purego.NewCallback(func(a0, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) uintptr {
		return a0 + a1 + a2 + a3 + a4 + a5 + a6 + a7 + a8 + a9
	})
	result, _, _ := purego.SyscallN(callback, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
	if result != 55 {
		t.Fatalf("callback returned %d, want 55", result)
	}

	getpid, err := resolveSymbol("getpid")
	if err != nil {
		t.Fatal(err)
	}
	pid, _, _ := purego.SyscallN(getpid)
	if pid == 0 {
		t.Fatal("getpid returned zero")
	}
}
