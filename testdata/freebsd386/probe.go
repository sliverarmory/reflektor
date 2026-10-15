//go:build freebsd && 386

// A small foreign-call probe for a real FreeBSD/i386 guest. This remains
// separate from the library's support claim until the entire path runs.
package main

import (
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

func mustSymbol(handle uintptr, name string) uintptr {
	address, err := purego.Dlsym(handle, name)
	if err != nil || address == 0 {
		panic(fmt.Sprintf("Dlsym(%q): address=%#x err=%v", name, address, err))
	}
	return address
}

func main() {
	if runtime.GOOS != "freebsd" || runtime.GOARCH != "386" {
		panic("probe requires a FreeBSD/i386 guest")
	}
	fmt.Printf("target=%s/%s go=%s cgo-free probe\n", runtime.GOOS, runtime.GOARCH, runtime.Version())

	libc, err := purego.Dlopen("libc.so.7", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		panic(err)
	}
	defer purego.Dlclose(libc)
	getpid := mustSymbol(libc, "getpid")
	pid, _, _ := purego.SyscallN(getpid)
	if int(pid) != os.Getpid() {
		panic(fmt.Sprintf("getpid=%d, Go pid=%d", pid, os.Getpid()))
	}
	fmt.Printf("foreign-call=getpid:%d\n", pid)

	callback := purego.NewCallback(func(value uintptr) uintptr { return value + 1 })
	result, _, _ := purego.SyscallN(callback, 41)
	if result != 42 {
		panic(fmt.Sprintf("callback returned %d, want 42", result))
	}
	fmt.Printf("same-thread-callback=%d\n", result)

	libthr, err := purego.Dlopen("libthr.so.3", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		panic(err)
	}
	defer purego.Dlclose(libthr)
	create := mustSymbol(libthr, "pthread_create")
	join := mustSymbol(libthr, "pthread_join")
	var thread, returned uintptr
	code, _, _ := purego.SyscallN(create, uintptr(unsafe.Pointer(&thread)), 0, callback, 41)
	if code != 0 {
		panic(fmt.Sprintf("pthread_create returned %d", code))
	}
	code, _, _ = purego.SyscallN(join, thread, uintptr(unsafe.Pointer(&returned)))
	if code != 0 || returned != 42 {
		panic(fmt.Sprintf("pthread_join returned code=%d value=%d", code, returned))
	}
	runtime.KeepAlive(&thread)
	runtime.KeepAlive(&returned)
	fmt.Printf("native-thread-callback=%d\n", returned)
}
