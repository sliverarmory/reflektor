//go:build darwin && (amd64 || arm64)

package memmod

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

func TestCallExportWithArgsRetainsDarwinRootState(t *testing.T) {
	module := loadDarwinArgumentFixture(t)
	defer module.Free()

	// CallExport and CallExportWithArgs must resolve against the exact same
	// retained root mapping; otherwise Accumulate3 would observe zero state.
	if err := module.CallExport("Init"); err != nil {
		t.Fatalf("CallExport(Init): %v", err)
	}
	result, err := module.CallExportWithArgs("Accumulate3", 1, 2, 3)
	if err != nil {
		t.Fatalf("CallExportWithArgs(Accumulate3): %v", err)
	}
	if result != 106 {
		t.Fatalf("first Accumulate3 result = %d, want 106", result)
	}
	result, err = module.CallExportWithArgs("Accumulate3", 4, 5, 6)
	if err != nil {
		t.Fatalf("repeat CallExportWithArgs(Accumulate3): %v", err)
	}
	if result != 121 {
		t.Fatalf("second Accumulate3 result = %d, want 121", result)
	}
	result, err = module.CallExportWithArgs("Current")
	if err != nil {
		t.Fatalf("CallExportWithArgs(Current): %v", err)
	}
	if result != 121 {
		t.Fatalf("Current result = %d, want 121", result)
	}
}

func TestDarwinExportInvocationReleasesModuleLocks(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previousProcs)

	module := loadDarwinArgumentFixture(t)
	defer module.Free()

	state, err := unix.Mmap(-1, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatalf("allocate native synchronization state: %v", err)
	}
	defer func() {
		if err := unix.Munmap(state); err != nil {
			t.Errorf("release native synchronization state: %v", err)
		}
	}()
	entered := (*uint32)(unsafe.Pointer(&state[0]))
	release := (*uint32)(unsafe.Pointer(&state[4]))

	type callResult struct {
		value uintptr
		err   error
	}
	blockedCall := make(chan callResult, 1)
	go func() {
		value, callErr := module.CallExportWithArgs(
			"WaitForRelease",
			uintptr(unsafe.Pointer(entered)),
			uintptr(unsafe.Pointer(release)),
			0,
		)
		blockedCall <- callResult{value: value, err: callErr}
	}()

	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for atomic.LoadUint32(entered) == 0 {
		select {
		case result := <-blockedCall:
			t.Fatalf("WaitForRelease returned before release: value=%#x err=%v", result.value, result.err)
		case <-deadline.C:
			atomic.StoreUint32(release, 1)
			t.Fatal("WaitForRelease did not start")
		default:
			runtime.Gosched()
		}
	}

	// A second call proves recursiveCallMu is not retained by the blocked export.
	reentrantCall := make(chan callResult, 1)
	go func() {
		value, callErr := module.CallExportWithArgs("Current")
		reentrantCall <- callResult{value: value, err: callErr}
	}()
	select {
	case result := <-reentrantCall:
		if result.err != nil || result.value != 0 {
			atomic.StoreUint32(release, 1)
			<-blockedCall
			t.Fatalf("concurrent export call: value=%#x err=%v", result.value, result.err)
		}
	case <-time.After(3 * time.Second):
		atomic.StoreUint32(release, 1)
		<-blockedCall
		<-reentrantCall
		t.Fatal("concurrent export call blocked behind an executing export")
	}

	// Free proves module.mu is also released. The mapped address remains valid
	// process-wide, so the first call can safely return after Free completes.
	freeDone := make(chan struct{})
	go func() {
		module.Free()
		close(freeDone)
	}()
	select {
	case <-freeDone:
	case <-time.After(3 * time.Second):
		atomic.StoreUint32(release, 1)
		<-blockedCall
		<-freeDone
		t.Fatal("Free blocked behind an executing export")
	}

	atomic.StoreUint32(release, 1)
	result := <-blockedCall
	if result.err != nil {
		t.Fatalf("WaitForRelease: %v", result.err)
	}
	if result.value != 0x5a17 {
		t.Fatalf("WaitForRelease result = %#x, want 0x5a17", result.value)
	}
}

func TestDarwinCallExportWithArgsCallsPuregoCallback(t *testing.T) {
	module := loadDarwinArgumentFixture(t)
	defer module.Free()

	input := []byte("reflektor callback payload")
	nativeInput, err := unix.Mmap(-1, 0, len(input), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatalf("allocate native callback input: %v", err)
	}
	copy(nativeInput, input)
	defer func() {
		if err := unix.Munmap(nativeInput); err != nil {
			t.Errorf("release native callback input: %v", err)
		}
	}()

	var callbackInput []byte
	callback := purego.NewCallback(func(data uintptr, size int32) int32 {
		if data != 0 && size > 0 {
			callbackInput = append(callbackInput, unsafe.Slice((*byte)(unsafe.Pointer(data)), int(size))...)
		}
		return 0x4a17
	})

	result, err := module.CallExportWithArgs(
		"Echo",
		uintptr(unsafe.Pointer(&nativeInput[0])),
		uintptr(uint32(len(input))),
		callback,
	)
	runtime.KeepAlive(nativeInput)
	if err != nil {
		t.Fatalf("CallExportWithArgs(Echo): %v", err)
	}
	if result != 0x4a17 {
		t.Fatalf("Echo result = %#x, want 0x4a17", result)
	}
	if !bytes.Equal(callbackInput, input) {
		t.Fatalf("callback input = %q, want %q", callbackInput, input)
	}
}

func loadDarwinArgumentFixture(t *testing.T) *Module {
	t.Helper()
	if runtime.GOARCH == "amd64" {
		if translated, err := unix.SysctlUint32("sysctl.proc_translated"); err == nil && translated == 1 {
			t.Skip("darwin/amd64 under Rosetta is not supported by the dyld4-only in-memory loader")
		}
	}
	compiler, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang is required for the Darwin argument-call integration test")
	}

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "args.c")
	dylibPath := filepath.Join(directory, "libreflektor_args.dylib")
	source := []byte(`
#include <stdint.h>

static uintptr_t reflektor_state;

typedef int32_t (*reflektor_echo_callback_t)(uintptr_t, int32_t);

__attribute__((visibility("default"))) void Init(void) {
    reflektor_state = 100;
}

__attribute__((visibility("default"))) uintptr_t Accumulate3(
    uintptr_t first, uintptr_t second, uintptr_t third) {
    reflektor_state += first + second + third;
    return reflektor_state;
}

__attribute__((visibility("default"))) uintptr_t Current(void) {
    return reflektor_state;
}

__attribute__((visibility("default"))) uintptr_t Echo(
    uintptr_t data, uintptr_t size, uintptr_t callback) {
    reflektor_echo_callback_t echo = (reflektor_echo_callback_t)callback;
    return (uintptr_t)(uint32_t)echo(data, (int32_t)(uint32_t)size);
}

__attribute__((visibility("default"))) uintptr_t WaitForRelease(
    uintptr_t entered_address, uintptr_t release_address, uintptr_t unused) {
    (void)unused;
    uint32_t *entered = (uint32_t *)entered_address;
    uint32_t *release = (uint32_t *)release_address;
    __atomic_store_n(entered, 1, __ATOMIC_RELEASE);
    while (__atomic_load_n(release, __ATOMIC_ACQUIRE) == 0) {
    }
    return (uintptr_t)0x5a17;
}
`)
	if err := os.WriteFile(sourcePath, source, 0o600); err != nil {
		t.Fatalf("write Darwin argument fixture: %v", err)
	}
	command := exec.Command(compiler,
		"-dynamiclib", "-fPIC", "-O0", "-g0",
		"-Wl,-install_name,"+dylibPath,
		"-o", dylibPath, sourcePath,
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("compile Darwin argument fixture: %v\n%s", err, output)
	}

	image, err := os.ReadFile(dylibPath)
	if err != nil {
		t.Fatalf("read Darwin argument fixture: %v", err)
	}
	module, err := LoadLibrary(image)
	if err != nil {
		t.Fatalf("LoadLibrary: %v", err)
	}
	return module
}

func TestCallExportWithArgsRejectsDarwinGoSharedImages(t *testing.T) {
	for _, args := range [][]uintptr{nil, {1, 2, 3}} {
		module := &Module{image: []byte{1}, goRuntime: true}
		if _, err := module.CallExportWithArgs("GoExport", args...); !errors.Is(err, ErrGoExportArgumentsUnsupported) {
			t.Fatalf("CallExportWithArgs Go rejection with %d args = %v", len(args), err)
		}
	}
}
