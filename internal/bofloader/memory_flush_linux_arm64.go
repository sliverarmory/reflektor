//go:build linux && arm64

package bofloader

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

var (
	linuxCacheFlushOnce   sync.Once
	linuxCacheFlushAddr   uintptr
	linuxCacheFlushHandle uintptr
	linuxCacheFlushErr    error
)

func (region *memoryRegion) flushInstructionCache(offset, length int) error {
	if _, err := region.rangeBytes(offset, length); err != nil || length == 0 {
		return err
	}
	linuxCacheFlushOnce.Do(func() {
		linuxCacheFlushAddr, linuxCacheFlushErr = purego.Dlsym(purego.RTLD_DEFAULT, "__clear_cache")
		if linuxCacheFlushErr == nil && linuxCacheFlushAddr != 0 {
			return
		}
		linuxCacheFlushHandle, linuxCacheFlushErr = purego.Dlopen("libgcc_s.so.1", purego.RTLD_NOW|purego.RTLD_LOCAL)
		if linuxCacheFlushErr != nil {
			return
		}
		linuxCacheFlushAddr, linuxCacheFlushErr = purego.Dlsym(linuxCacheFlushHandle, "__clear_cache")
	})
	if linuxCacheFlushErr != nil {
		return fmt.Errorf("resolve __clear_cache: %w", linuxCacheFlushErr)
	}
	if linuxCacheFlushAddr == 0 {
		return errors.New("resolve __clear_cache: symbol address is zero")
	}
	start := region.base() + uintptr(offset)
	purego.SyscallN(linuxCacheFlushAddr, start, start+uintptr(length))
	return nil
}
