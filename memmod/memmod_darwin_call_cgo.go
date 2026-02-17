//go:build darwin && (amd64 || arm64) && cgo

package memmod

/*
#include <stdint.h>

typedef uintptr_t (*reflektor_fn10_t)(
	uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t,
	uintptr_t, uintptr_t, uintptr_t, uintptr_t, uintptr_t
);

static uintptr_t reflektor_call10(
	uintptr_t fn,
	uintptr_t a0, uintptr_t a1, uintptr_t a2, uintptr_t a3, uintptr_t a4,
	uintptr_t a5, uintptr_t a6, uintptr_t a7, uintptr_t a8, uintptr_t a9
) {
	return ((reflektor_fn10_t)fn)(a0, a1, a2, a3, a4, a5, a6, a7, a8, a9);
}
*/
import "C"

func call0(fn uintptr) uintptr {
	return call10(fn, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
}

func call1(fn, a0 uintptr) uintptr {
	return call10(fn, a0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
}

func call2(fn, a0, a1 uintptr) uintptr {
	return call10(fn, a0, a1, 0, 0, 0, 0, 0, 0, 0, 0)
}

func call4(fn, a0, a1, a2, a3 uintptr) uintptr {
	return call10(fn, a0, a1, a2, a3, 0, 0, 0, 0, 0, 0)
}

func call6(fn, a0, a1, a2, a3, a4, a5 uintptr) uintptr {
	return call10(fn, a0, a1, a2, a3, a4, a5, 0, 0, 0, 0)
}

func call10(fn, a0, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) uintptr {
	return uintptr(C.reflektor_call10(
		C.uintptr_t(fn),
		C.uintptr_t(a0),
		C.uintptr_t(a1),
		C.uintptr_t(a2),
		C.uintptr_t(a3),
		C.uintptr_t(a4),
		C.uintptr_t(a5),
		C.uintptr_t(a6),
		C.uintptr_t(a7),
		C.uintptr_t(a8),
		C.uintptr_t(a9),
	))
}
