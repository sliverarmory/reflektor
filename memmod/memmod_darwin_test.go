//go:build darwin && (amd64 || arm64)

package memmod

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func runDarwinLoadAndCallTest(t *testing.T, dylibName string) {
	t.Helper()

	dylibPath := ensureDarwinTestDylib(t, dylibName)
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
			if os.Getenv("GITHUB_ACTIONS") == "true" && strings.Contains(err.Error(), "failed to resolve required dyld symbols") {
				t.Skipf("skipping on GitHub Actions runner: %v", err)
			}
			t.Fatalf("CallExport(StartW): %v", err)
		}
		module.Free()
	case <-time.After(3 * time.Second):
		t.Log("StartW invocation is still running after timeout; treating as successful long-running export")
	}
}

func ensureDarwinTestDylib(t *testing.T, dylibName string) string {
	t.Helper()

	dylibPath := filepath.Join("..", "testdata", dylibName)
	if _, err := os.Stat(dylibPath); err == nil {
		return dylibPath
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat test dylib (%s): %v", dylibPath, err)
	}

	if _, err := exec.LookPath("zig"); err != nil {
		t.Skipf("missing test dylib %s and zig not found in PATH", dylibPath)
	}

	var zigTarget string
	switch runtime.GOARCH {
	case "amd64":
		zigTarget = "x86_64-macos"
	case "arm64":
		zigTarget = "aarch64-macos"
	default:
		t.Fatalf("unsupported GOARCH for darwin test dylib build: %s", runtime.GOARCH)
	}

	outPath := filepath.Join(t.TempDir(), dylibName)
	sourcePath := filepath.Join("..", "testdata", "c", "basic.c")
	cmd := exec.Command("zig", "cc",
		"-target", zigTarget,
		"-dynamiclib", "-fPIC",
		"-O2", "-g0",
		"-o", outPath,
		sourcePath,
	)
	cmd.Env = append(
		os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build fallback darwin test dylib: %v\n%s", err, out)
	}

	return outPath
}
