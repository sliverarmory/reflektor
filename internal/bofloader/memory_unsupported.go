//go:build (darwin && !(amd64 || arm64)) || (linux && !(386 || amd64 || arm64)) || (windows && !(386 || amd64 || arm64)) || (!darwin && !linux && !windows)

package bofloader

import "errors"

type memoryRegion struct {
	data []byte
}

func allocateMemory(size int) (*memoryRegion, error) {
	return nil, errors.New("BOF executable memory is unsupported on this platform")
}

func (region *memoryRegion) base() uintptr {
	return 0
}

func (region *memoryRegion) protect(offset, length int, requested protection) error {
	return errors.New("BOF executable memory is unsupported on this platform")
}

func (region *memoryRegion) flushInstructionCache(offset, length int) error {
	return errors.New("BOF executable memory is unsupported on this platform")
}

func (region *memoryRegion) close() error {
	return nil
}

func memoryPageSize() int {
	return 4096
}
