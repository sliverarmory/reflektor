//go:build windows && (386 || amd64 || arm64)

package reflektor_test

import (
	"os"
	"runtime"
	"testing"
)

func TestLoadGeneratedGoWindowsDLLAndCallStartW(t *testing.T) {
	outDir, err := os.MkdirTemp("", "reflektor-go-windows-dll-*")
	if err != nil {
		t.Fatalf("create temp build dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(outDir)
	})
	dllPath := buildOneGoSharedLib(t, outDir, "windows", runtime.GOARCH)
	runGoRuntimeFixtureSubprocess(t, dllPath)
}
