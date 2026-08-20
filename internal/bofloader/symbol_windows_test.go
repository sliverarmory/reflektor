//go:build bof && windows && (amd64 || arm64)

package bofloader

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestResolveWindowsMSVCRTVsnprintf(t *testing.T) {
	address, err := resolveSymbol("__imp_MSVCRT$vsnprintf")
	if err != nil {
		t.Fatalf("resolve MSVCRT$vsnprintf: %v", err)
	}
	if address == 0 {
		t.Fatal("resolve MSVCRT$vsnprintf returned a zero address")
	}

	format := []byte("reflektor-vsnprintf-ok\x00")
	formatAddress := uintptr(unsafe.Pointer(&format[0]))
	wantLength := len(format) - 1

	result, _, _ := syscall.SyscallN(address, 0, 0, formatAddress, 0)
	length := int32(result)
	if length != int32(wantLength) {
		t.Fatalf("vsnprintf(NULL, 0) = %d, want %d", length, wantLength)
	}

	buffer := make([]byte, len(format))
	bufferAddress := uintptr(unsafe.Pointer(&buffer[0]))
	result, _, _ = syscall.SyscallN(address, bufferAddress, uintptr(len(buffer)), formatAddress, 0)
	length = int32(result)
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

	// The existing amd64 MSVCRT export retains its legacy -1-on-truncation
	// behavior. The ARM64 compatibility shim deliberately provides the
	// documented behavior of UCRT's public vsnprintf wrapper.
	if runtime.GOARCH != "arm64" {
		return
	}
	truncated := make([]byte, 5)
	truncatedAddress := uintptr(unsafe.Pointer(&truncated[0]))
	result, _, _ = syscall.SyscallN(address, truncatedAddress, uintptr(len(truncated)), formatAddress, 0)
	length = int32(result)
	runtime.KeepAlive(format)
	runtime.KeepAlive(truncated)
	if length != int32(wantLength) {
		t.Fatalf("truncated vsnprintf = %d, want required length %d", length, wantLength)
	}
	if got, want := string(truncated), "refl\x00"; got != want {
		t.Fatalf("truncated vsnprintf output = %q, want %q", got, want)
	}
}
