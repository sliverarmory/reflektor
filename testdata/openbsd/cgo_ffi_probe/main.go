// A minimal CGo foreign-call and callback proof for the OpenBSD guest.
// It does not implement the Reflektor loader bridge.
package main

/*
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

extern uintptr_t goCallback(uintptr_t);

static uintptr_t call0(void *fn) {
	return ((uintptr_t (*)(void))fn)();
}

static uintptr_t call1(void *fn, uintptr_t arg) {
	return ((uintptr_t (*)(uintptr_t))fn)(arg);
}

static uintptr_t cCallback(uintptr_t arg) {
	return goCallback(arg);
}

static void *callbackAddress(void) {
	return (void *)cCallback;
}
*/
import "C"

import (
	"fmt"
	"os"
	"unsafe"
)

//export goCallback
func goCallback(arg C.uintptr_t) C.uintptr_t {
	return arg + 1
}

func main() {
	handle := C.dlopen(nil, C.RTLD_NOW)
	if handle == nil {
		panic("dlopen(NULL) failed: " + C.GoString(C.dlerror()))
	}
	defer C.dlclose(handle)
	name := C.CString("getpid")
	defer C.free(unsafe.Pointer(name))
	getpid := C.dlsym(handle, name)
	if getpid == nil {
		panic("dlsym(getpid) failed: " + C.GoString(C.dlerror()))
	}
	gotPID := C.call0(getpid)
	if int(gotPID) != os.Getpid() {
		panic(fmt.Sprintf("foreign getpid = %d, want %d", gotPID, os.Getpid()))
	}
	gotCallback := C.call1(C.callbackAddress(), 41)
	if gotCallback != 42 {
		panic(fmt.Sprintf("callback = %d, want 42", gotCallback))
	}
	fmt.Println("OpenBSD/amd64 CGo dlsym, foreign call, and Go callback passed")
}
