//go:build (darwin && !(amd64 || arm64)) || (linux && !(386 || amd64 || arm64)) || (windows && !(386 || amd64 || arm64)) || (!darwin && !linux && !windows)

package bofloader

func invokeEntry(entry, argumentAddress uintptr, argumentLength int32) {
	panic("bofloader: BOF entry invocation is unsupported on this platform")
}
