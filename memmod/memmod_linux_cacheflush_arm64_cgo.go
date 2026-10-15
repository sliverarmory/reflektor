//go:build linux && !android && arm64 && cgo

package memmod

/*
#include <stdint.h>

static void reflektor_clear_cache(uintptr_t start, uintptr_t end) {
	__builtin___clear_cache((char *)start, (char *)end);
}
*/
import "C"

func flushLinuxInstructionCache(start, end uintptr) error {
	if start < end {
		C.reflektor_clear_cache(C.uintptr_t(start), C.uintptr_t(end))
	}
	return nil
}
