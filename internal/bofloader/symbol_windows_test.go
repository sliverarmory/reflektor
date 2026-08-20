//go:build bof && windows && (386 || amd64 || arm64)

package bofloader

import (
	"reflect"
	"runtime"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
)

func TestWindowsVsnprintfCandidatesPreserveC99Semantics(t *testing.T) {
	want := []string{"vsnprintf"}
	if got := windowsFunctionCandidates("vsnprintf"); !reflect.DeepEqual(got, want) {
		t.Fatalf("windowsFunctionCandidates(vsnprintf) = %q, want %q; _vsnprintf is not C99-compatible", got, want)
	}
}

func TestResolveWindowsMSVCRTVsnprintf(t *testing.T) {
	address, err := resolveSymbol("__imp_MSVCRT$vsnprintf")
	if err != nil {
		t.Fatalf("resolve MSVCRT$vsnprintf: %v", err)
	}
	if address == 0 {
		t.Fatal("resolve MSVCRT$vsnprintf returned a zero address")
	}

	var vsnprintf func(_ purego.CDecl, buffer, count, format, argumentList uintptr) uintptr
	purego.RegisterFunc(&vsnprintf, address)
	format := []byte("reflektor-vsnprintf-ok\x00")
	formatAddress := uintptr(unsafe.Pointer(&format[0]))
	wantLength := len(format) - 1

	length := int32(vsnprintf(purego.CDecl{}, 0, 0, formatAddress, 0))
	if length != int32(wantLength) {
		t.Fatalf("vsnprintf(NULL, 0) = %d, want %d", length, wantLength)
	}

	buffer := make([]byte, len(format))
	bufferAddress := uintptr(unsafe.Pointer(&buffer[0]))
	length = int32(vsnprintf(purego.CDecl{}, bufferAddress, uintptr(len(buffer)), formatAddress, 0))
	runtime.KeepAlive(format)
	runtime.KeepAlive(buffer)
	if length != int32(wantLength) {
		t.Fatalf("vsnprintf(buffer) = %d, want %d", length, wantLength)
	}
	if got := string(buffer[:wantLength]); got != string(format[:wantLength]) {
		t.Fatalf("vsnprintf output = %q, want %q", got, format[:wantLength])
	}
	if buffer[wantLength] != 0 {
		t.Fatalf("vsnprintf output is not NUL-terminated: %#v", buffer[:wantLength+1])
	}

	truncated := make([]byte, 5)
	truncatedAddress := uintptr(unsafe.Pointer(&truncated[0]))
	length = int32(vsnprintf(purego.CDecl{}, truncatedAddress, uintptr(len(truncated)), formatAddress, 0))
	runtime.KeepAlive(format)
	runtime.KeepAlive(truncated)
	if length != int32(wantLength) {
		t.Fatalf("truncated vsnprintf = %d, want required length %d", length, wantLength)
	}
	if got, want := string(truncated), "refl\x00"; got != want {
		t.Fatalf("truncated vsnprintf output = %q, want %q", got, want)
	}
}
