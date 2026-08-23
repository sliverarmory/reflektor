//go:build linux

package main

import "os/user"

func exercisePlatformRuntime() {
	// The cgo implementation reaches glibc's NSS path. On RISC-V that path
	// calls STT_GNU_IFUNC symbols such as memcpy, so this also verifies that a
	// manually mapped Go c-shared image invokes the resolved implementation
	// rather than the IFUNC resolver.
	current, err := user.Current()
	if err != nil {
		panic("os/user.Current: " + err.Error())
	}
	if current == nil || current.Uid == "" || current.Username == "" {
		panic("os/user.Current returned an incomplete user")
	}
}
