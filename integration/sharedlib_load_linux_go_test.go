//go:build linux && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)

package reflektor_test

import (
	"runtime"
	"testing"
)

func TestLoadGeneratedGoLinuxSOAndCallStartW(t *testing.T) {
	outDir := t.TempDir()
	soPath := buildOneGoSharedLib(t, outDir, "linux", runtime.GOARCH)
	runGoRuntimeFixtureSubprocess(t, soPath)
}
