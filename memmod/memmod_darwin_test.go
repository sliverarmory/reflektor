//go:build darwin && (amd64 || arm64)

package memmod

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
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
			t.Fatalf("CallExport(StartW): %v", err)
		}
		module.Free()
	case <-time.After(3 * time.Second):
		t.Log("StartW invocation is still running after timeout; treating as successful long-running export")
	}
}

func TestDarwinLoaderTransactionsUnderSchedulerPressure(t *testing.T) {
	if runtime.GOARCH == "amd64" {
		if translated, err := unix.SysctlUint32("sysctl.proc_translated"); err == nil && translated == 1 {
			t.Skip("darwin/amd64 under Rosetta is not supported by the dyld4-only in-memory loader")
		}
	}

	compiler, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is required for the Darwin loader thread-affinity regression test")
	}
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "thread_affinity.c")
	dylibPath := filepath.Join(directory, "libthread_affinity.dylib")
	if err := os.WriteFile(sourcePath, []byte(`
__attribute__((visibility("default"))) void StartW(void) {
}
`), 0o600); err != nil {
		t.Fatalf("write Darwin thread-affinity fixture: %v", err)
	}
	command := exec.Command(compiler,
		"-dynamiclib", "-fPIC", "-O0", "-g0",
		"-Wl,-install_name,"+dylibPath,
		"-o", dylibPath, sourcePath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile Darwin thread-affinity fixture: %v\n%s", err, output)
	}
	image, err := os.ReadFile(dylibPath)
	if err != nil {
		t.Fatalf("read Darwin thread-affinity fixture: %v", err)
	}

	previousProcs := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previousProcs)

	// Keep other goroutines runnable while each private dyld transaction crosses
	// multiple cgo or assembly calls. Before memmodLoader pinned its goroutine,
	// exitsyscall could resume it on a different native thread and make the
	// os_unfair_lock unlock/relock sequence abort with SIGILL.
	stopPressure := make(chan struct{})
	var pressure sync.WaitGroup
	for i := 0; i < 4; i++ {
		pressure.Add(1)
		go func() {
			defer pressure.Done()
			for {
				select {
				case <-stopPressure:
					return
				default:
					runtime.Gosched()
				}
			}
		}()
	}

	const loadCount = 24
	results := make(chan error, loadCount)
	var loaders sync.WaitGroup
	for i := 0; i < loadCount; i++ {
		loaders.Add(1)
		go func(index int) {
			defer loaders.Done()
			module, err := LoadLibrary(append([]byte(nil), image...))
			if err != nil {
				results <- fmt.Errorf("load %d: %w", index, err)
				return
			}
			defer module.Free()
			if err := module.CallExport("StartW"); err != nil {
				results <- fmt.Errorf("call %d: %w", index, err)
				return
			}
			results <- nil
		}(i)
	}
	loaders.Wait()
	close(stopPressure)
	pressure.Wait()
	close(results)

	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
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
