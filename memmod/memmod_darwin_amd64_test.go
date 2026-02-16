//go:build darwin && amd64

package memmod

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestLoadLibraryAndCallExport_DarwinAMD64(t *testing.T) {
	if translated, err := unix.SysctlUint32("sysctl.proc_translated"); err == nil && translated == 1 {
		t.Skip("darwin/amd64 under Rosetta is not supported by the dyld4-only in-memory loader")
	}
	runDarwinLoadAndCallTest(t, "test1_darwin-amd64.dylib")
}
