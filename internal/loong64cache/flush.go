//go:build linux && loong64

package loong64cache

// Flush synchronizes subsequently executed instructions with recently written
// code. Linux/LoongArch compiler-rt implements __clear_cache with ibar 0.
func Flush()
