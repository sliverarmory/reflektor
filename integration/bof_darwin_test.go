//go:build darwin && (amd64 || arm64)

package reflektor_test

import (
	"runtime"
	"testing"
)

// TestLoadAndExecuteLegacyDarwinELFBOF keeps execution coverage for the
// pre-Mach-O Darwin interchange format while the primary Darwin fixture and
// external corpus exercise native Mach-O MH_OBJECT files.
func TestLoadAndExecuteLegacyDarwinELFBOF(t *testing.T) {
	requireCommand(t, "zig")
	zigTarget := map[string]string{
		"amd64": "x86_64-linux-none",
		"arm64": "aarch64-linux-none",
	}[runtime.GOARCH]
	testLoadAndExecuteGeneratedBOF(t, bofTarget{
		goos:      "darwin",
		goarch:    runtime.GOARCH,
		zigTarget: zigTarget,
		format:    "elf",
	})
}
