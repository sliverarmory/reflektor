//go:build linux && !android && arm64 && cgo

package linuxmem

/*
#include <stdint.h>

static void reflektor_clear_cache(uintptr_t start, uintptr_t end) {
	__builtin___clear_cache((char *)start, (char *)end);
}
*/
import "C"

import "unsafe"

func flushMappedInstructionCache(mapping []byte) error {
	if len(mapping) != 0 {
		start := uintptr(unsafe.Pointer(unsafe.SliceData(mapping)))
		C.reflektor_clear_cache(C.uintptr_t(start), C.uintptr_t(start+uintptr(len(mapping))))
	}
	return nil
}
