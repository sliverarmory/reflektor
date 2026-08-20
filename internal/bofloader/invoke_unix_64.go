//go:build bof && ((darwin && (amd64 || arm64)) || (linux && (amd64 || arm64)))

package bofloader

import "github.com/ebitengine/purego"

//go:uintptrescapes
func invokeEntry(entry, argumentAddress uintptr, argumentLength int32) {
	purego.SyscallN(entry, argumentAddress, uintptr(uint32(argumentLength)))
}
