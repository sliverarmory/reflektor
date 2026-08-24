//go:build (darwin && (amd64 || arm64)) || (freebsd && amd64) || (linux && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)) || (windows && (386 || amd64 || arm64))

package reflektor_test

import (
	"runtime"
	"testing"
)

func TestLoadGeneratedGoSharedLibraryRecursiveMode(t *testing.T) {
	requireRecursiveLoaderPlatform(t)
	outDir := t.TempDir()
	libraryPath := buildOneGoSharedLib(t, outDir, runtime.GOOS, runtime.GOARCH)
	runGoRuntimeFixtureSubprocessMode(t, libraryPath, true)
}
