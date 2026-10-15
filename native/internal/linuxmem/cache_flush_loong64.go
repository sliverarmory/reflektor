//go:build linux && !android && loong64

package linuxmem

import "github.com/sliverarmory/reflektor/internal/loong64cache"

func flushMappedInstructionCache(mapping []byte) error {
	if len(mapping) != 0 {
		loong64cache.Flush()
	}
	return nil
}
