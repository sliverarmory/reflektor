//go:build bof && windows && (386 || amd64 || arm64)

package bofloader

import (
	"testing"
	"unsafe"
)

func TestToWideCharWindows(t *testing.T) {
	source := append([]byte("test"), 0)
	destination := make([]uint16, 5)
	if got := toWideChar(
		byteSliceAddress(source),
		uintptr(unsafe.Pointer(unsafe.SliceData(destination))),
		uintptr(len(destination)*2),
	); got != 1 {
		t.Fatalf("toWideChar() = %d", got)
	}
	want := []uint16{'t', 'e', 's', 't', 0}
	for index := range want {
		if destination[index] != want[index] {
			t.Fatalf("destination[%d] = %#x, want %#x", index, destination[index], want[index])
		}
	}
	tooSmall := make([]uint16, 1)
	if got := toWideChar(
		byteSliceAddress(source),
		uintptr(unsafe.Pointer(unsafe.SliceData(tooSmall))),
		uintptr(len(tooSmall)*2),
	); got != 0 {
		t.Fatalf("short toWideChar() = %d", got)
	}
}
