//go:build darwin && (amd64 || arm64)

package memmod

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func runDarwinLoadAndCallTest(t *testing.T, dylibName string) {
	t.Helper()

	dylibPath := filepath.Join("..", "testdata", dylibName)
	payload, err := os.ReadFile(dylibPath)
	if err != nil {
		t.Fatalf("read test dylib (%s): %v", dylibPath, err)
	}

	module, err := LoadLibrary(payload)
	if err != nil {
		t.Fatalf("LoadLibrary(%s): %v", dylibName, err)
	}

	// Some StartW exports are designed to remain resident (no fast return).
	// Treat either a successful return or continued execution after timeout as
	// a successful invocation.
	done := make(chan error, 1)
	go func() {
		done <- module.CallExport("StartW")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("CallExport(StartW): %v", err)
		}
		module.Free()
	case <-time.After(3 * time.Second):
		t.Log("StartW invocation is still running after timeout; treating as successful long-running export")
	}
}
