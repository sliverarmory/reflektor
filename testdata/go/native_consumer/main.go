package main

/*
#include <stdint.h>
*/
import "C"

import (
	"runtime"
	"unsafe"

	"github.com/sliverarmory/reflektor/native"
)

// ReflektorNativeConsumer deliberately references the complete minimal native
// API so the c-shared link cannot discard the package during the TLS audit.
//
//export ReflektorNativeConsumer
func ReflektorNativeConsumer(data unsafe.Pointer, size C.uintptr_t) C.uintptr_t {
	if data == nil || size == 0 {
		return 0
	}
	payload := unsafe.Slice((*byte)(data), int(size))
	library, err := native.LoadLibrary(payload)
	if err != nil {
		return 0
	}
	defer library.Close()

	if err := library.CallExport("ReflektorArgsInit"); err != nil {
		return 0
	}
	result, err := library.CallExportWithArgs("ReflektorArgsRun", 0, 0, 0)
	runtime.KeepAlive(payload)
	if err != nil {
		return 0
	}
	return C.uintptr_t(result)
}

func main() {}
