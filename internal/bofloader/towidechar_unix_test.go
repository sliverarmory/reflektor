//go:build bof && ((darwin && (amd64 || arm64)) || (linux && (386 || amd64 || arm64)))

package bofloader

import (
	"encoding/binary"
	"testing"
)

func TestToWideCharUnix(t *testing.T) {
	source := append([]byte("Aé🙂"), 0)
	destination := make([]byte, 2*5)
	if got := toWideChar(byteSliceAddress(source), byteSliceAddress(destination), uintptr(len(destination))); got != 1 {
		t.Fatalf("toWideChar() = %d", got)
	}
	want := []uint16{'A', 'é', 0xd83d, 0xde42, 0}
	for index, value := range want {
		if got := binary.LittleEndian.Uint16(destination[index*2:]); got != value {
			t.Fatalf("destination[%d] = %#x, want %#x", index, got, value)
		}
	}
	tooSmall := make([]byte, 2)
	if got := toWideChar(byteSliceAddress(source), byteSliceAddress(tooSmall), uintptr(len(tooSmall))); got != 0 {
		t.Fatalf("short toWideChar() = %d", got)
	}
}
