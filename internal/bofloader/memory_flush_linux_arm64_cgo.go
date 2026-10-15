//go:build linux && !android && arm64 && cgo

package bofloader

/*
#include <stdint.h>

static void reflektor_clear_cache(uintptr_t start, uintptr_t end) {
	__builtin___clear_cache((char *)start, (char *)end);
}
*/
import "C"

func (region *memoryRegion) flushInstructionCache(offset, length int) error {
	if _, err := region.rangeBytes(offset, length); err != nil || length == 0 {
		return err
	}
	start := region.base() + uintptr(offset)
	C.reflektor_clear_cache(C.uintptr_t(start), C.uintptr_t(start+uintptr(length)))
	return nil
}
