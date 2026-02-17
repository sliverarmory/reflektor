//go:build linux && cgo && (386 || amd64 || arm64)

package memmod

/*
#include <stdint.h>

typedef uintptr_t (*reflektor_fn0)(void);
typedef uintptr_t (*reflektor_fn1)(uintptr_t);
typedef uintptr_t (*reflektor_fn2)(uintptr_t, uintptr_t);

static uintptr_t reflektor_call0(uintptr_t fn) {
	return ((reflektor_fn0)fn)();
}

static uintptr_t reflektor_call1(uintptr_t fn, uintptr_t a0) {
	return ((reflektor_fn1)fn)(a0);
}

static uintptr_t reflektor_call2(uintptr_t fn, uintptr_t a0, uintptr_t a1) {
	return ((reflektor_fn2)fn)(a0, a1);
}
*/
import "C"

func cCall0(fn uintptr) uintptr {
	return uintptr(C.reflektor_call0(C.uintptr_t(fn)))
}

func cCall1(fn, a0 uintptr) uintptr {
	return uintptr(C.reflektor_call1(C.uintptr_t(fn), C.uintptr_t(a0)))
}

func cCall2(fn, a0, a1 uintptr) uintptr {
	return uintptr(C.reflektor_call2(C.uintptr_t(fn), C.uintptr_t(a0), C.uintptr_t(a1)))
}
