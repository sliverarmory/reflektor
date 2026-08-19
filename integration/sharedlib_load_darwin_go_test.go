//go:build darwin && (amd64 || arm64)

package reflektor_test

import (
	"runtime"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLoadGeneratedGoDylibAndCallStartW(t *testing.T) {
	if runtime.GOARCH == "amd64" {
		if translated, err := unix.SysctlUint32("sysctl.proc_translated"); err == nil && translated == 1 {
			t.Skip("darwin/amd64 under Rosetta is not supported by the dyld4-only in-memory loader")
		}
	}

	outDir := t.TempDir()
	dylibPath := buildOneGoSharedLib(t, outDir, "darwin", runtime.GOARCH)
	runGoRuntimeFixtureSubprocess(t, dylibPath)
}
