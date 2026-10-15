package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/ebitengine/purego"
)

func main() {
	if runtime.GOOS != "android" || runtime.GOARCH != "arm64" {
		panic("android/arm64 guest required")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	libc, err := purego.Dlopen("libc.so", purego.RTLD_NOW)
	if err != nil {
		panic(fmt.Errorf("dlopen Bionic libc: %w", err))
	}
	defer purego.Dlclose(libc)
	getpid, err := purego.Dlsym(libc, "getpid")
	if err != nil {
		panic(fmt.Errorf("dlsym getpid: %w", err))
	}
	pid, _, _ := purego.SyscallN(getpid)
	if int(pid) != os.Getpid() {
		panic(fmt.Sprintf("foreign getpid = %d, want %d", pid, os.Getpid()))
	}
	callback := purego.NewCallback(func(value uintptr) uintptr { return value + 1 })
	value, _, _ := purego.SyscallN(callback, 41)
	if value != 42 {
		panic(fmt.Sprintf("foreign callback = %d, want 42", value))
	}
	fmt.Println("android/arm64: PureGo Bionic call and callback passed")
}
