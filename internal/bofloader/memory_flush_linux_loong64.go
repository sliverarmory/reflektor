//go:build linux && !android && loong64

package bofloader

import "github.com/sliverarmory/reflektor/internal/loong64cache"

func (region *memoryRegion) flushInstructionCache(offset, length int) error {
	if _, err := region.rangeBytes(offset, length); err != nil || length == 0 {
		return err
	}
	loong64cache.Flush()
	return nil
}
