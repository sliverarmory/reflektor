//go:build darwin && arm64

package memmod

import "testing"

func TestLoadLibraryAndCallExport_DarwinArm64(t *testing.T) {
	runDarwinLoadAndCallTest(t, "test1_darwin-arm64.dylib")
}
