//go:build linux && !android && loong64

package memmod

import "github.com/sliverarmory/reflektor/internal/loong64cache"

func flushLinuxInstructionCache(start, end uintptr) error {
	if start < end {
		loong64cache.Flush()
	}
	return nil
}
