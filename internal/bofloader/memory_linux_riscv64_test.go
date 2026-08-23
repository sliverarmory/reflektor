//go:build linux && !android && riscv64

package bofloader

import (
	"encoding/binary"
	"testing"

	"github.com/ebitengine/purego"
)

func TestRISCV64ExecutableMemory(t *testing.T) {
	region, err := allocateMemory(memoryPageSize())
	if err != nil {
		t.Fatal(err)
	}
	defer region.close()

	if err := region.protect(0, memoryPageSize(), protRead|protWrite|protExec); err == nil {
		t.Fatal("writable executable mapping was accepted")
	}

	// addi a0, zero, 42; ret
	binary.LittleEndian.PutUint32(region.data[0:4], 0x02a00513)
	binary.LittleEndian.PutUint32(region.data[4:8], 0x00008067)
	if err := region.protect(0, memoryPageSize(), protRead|protExec); err != nil {
		t.Fatal(err)
	}
	if err := region.flushInstructionCache(0, memoryPageSize()); err != nil {
		t.Fatal(err)
	}
	result, _, _ := purego.SyscallN(region.base())
	if result != 42 {
		t.Fatalf("executable mapping returned %d, want 42", result)
	}
}

func TestRISCV64PureGoBridge(t *testing.T) {
	callback := purego.NewCallback(func(value uintptr) uintptr {
		return value + 1
	})
	result, _, _ := purego.SyscallN(callback, 41)
	if result != 42 {
		t.Fatalf("callback returned %d, want 42", result)
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
