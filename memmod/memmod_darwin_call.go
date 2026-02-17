//go:build darwin && (amd64 || arm64) && !cgo

package memmod

import _ "unsafe"

//go:noescape
func cCall10(fn, a0, a1, a2, a3, a4, a5, a6, a7, a8, a9 uintptr) uintptr

//go:linkname runtimeSystemstack runtime.systemstack
func runtimeSystemstack(fn func())

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
	var ret uintptr
	runtimeSystemstack(func() {
		ret = cCall10(fn, a0, a1, a2, a3, a4, a5, a6, a7, a8, a9)
	})
	return ret
}
