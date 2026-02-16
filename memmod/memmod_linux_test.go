//go:build linux && (386 || amd64 || arm64)

package memmod

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadLibraryAndCallExport_Linux(t *testing.T) {
	if _, err := exec.LookPath("zig"); err != nil {
		t.Skip("zig not found in PATH")
	}

	tmp := t.TempDir()
	soPath := filepath.Join(tmp, fmt.Sprintf("basic_linux-%s.so", runtime.GOARCH))
	buildLinuxTestSO(t, soPath)

	payload, err := os.ReadFile(soPath)
	if err != nil {
		t.Fatalf("read built shared library: %v", err)
	}

	module, err := LoadLibrary(payload)
	if err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	t.Cleanup(module.Free)

	addr, err := module.ProcAddressByName("StartW")
	if err != nil {
		t.Fatalf("ProcAddressByName(StartW): %v", err)
	}
	if addr == 0 {
		t.Fatalf("ProcAddressByName(StartW) returned zero address")
	}

	marker := filepath.Join(tmp, "linux_marker.txt")
	if err := os.Setenv("REFLEKTOR_MARKER", marker); err != nil {
		t.Fatalf("set REFLEKTOR_MARKER: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("REFLEKTOR_MARKER")
	})

	if err := module.CallExport("StartW"); err != nil {
		t.Fatalf("CallExport(StartW): %v", err)
	}

	got, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if !bytes.Equal(got, []byte("ok")) {
		t.Fatalf("unexpected marker content: got=%q want=%q", got, []byte("ok"))
	}
}

func buildLinuxTestSO(t *testing.T, output string) {
	t.Helper()

	var zigTarget string
	switch runtime.GOARCH {
	case "386":
		zigTarget = "x86-linux-gnu"
	case "amd64":
		zigTarget = "x86_64-linux-gnu"
	case "arm64":
		zigTarget = "aarch64-linux-gnu"
	default:
		t.Fatalf("unsupported GOARCH for linux test: %s", runtime.GOARCH)
	}

	source := filepath.Join("..", "testdata", "c", "basic.c")
	cmd := exec.Command("zig", "cc",
		"-target", zigTarget,
		"-shared", "-fPIC",
		"-O2", "-g0",
		"-o", output,
		source,
	)
	cmd.Env = append(
		os.Environ(),
		"ZIG_GLOBAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-global-cache"),
		"ZIG_LOCAL_CACHE_DIR="+filepath.Join(os.TempDir(), "reflektor-zig-local-cache"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build linux test shared object: %v\n%s", err, out)
	}
}
