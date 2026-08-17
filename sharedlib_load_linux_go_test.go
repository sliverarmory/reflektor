//go:build linux && (386 || amd64 || arm64)

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
