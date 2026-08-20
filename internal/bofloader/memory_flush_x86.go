//go:build bof && ((darwin && amd64) || (linux && (386 || amd64)))

package bofloader

func (region *memoryRegion) flushInstructionCache(offset, length int) error {
	_, err := region.rangeBytes(offset, length)
	return err
}
