// A minimal form of the foreign calls and callbacks needed by both loaders.
// This is cross-built by check-purego-bridge.sh, not executed by go test.
package main

import "github.com/ebitengine/purego"

func main() {
	_, _ = purego.Dlopen("libc.so", purego.RTLD_NOW)
	_ = purego.NewCallback(func() uintptr { return 42 })
	_, _, _ = purego.SyscallN(0)
}
