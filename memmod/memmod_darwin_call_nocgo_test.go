//go:build darwin && !ios && (amd64 || arm64) && !cgo

package memmod

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
)

// TestDarwinCall10PassesEveryArgument checks the native calling convention,
// including the arguments that must be passed on the stack.
func TestDarwinCall10PassesEveryArgument(t *testing.T) {
	const source = `#include <stdint.h>

uintptr_t reflektor_call10_probe(
    uintptr_t a0, uintptr_t a1, uintptr_t a2, uintptr_t a3, uintptr_t a4,
    uintptr_t a5, uintptr_t a6, uintptr_t a7, uintptr_t a8, uintptr_t a9
) {
    const uintptr_t got[] = {a0, a1, a2, a3, a4, a5, a6, a7, a8, a9};
    const uintptr_t want[] = {0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa};
    uintptr_t matched = 0;
    for (unsigned i = 0; i < 10; ++i) {
        if (got[i] == want[i]) {
            matched |= (uintptr_t)1 << i;
        }
    }
#if defined(__x86_64__)
    // A SysV callee enters with RSP % 16 == 8. Its frame pointer, saved by
    // the prologue, must therefore be 16-byte aligned.
    if ((((uintptr_t)__builtin_frame_address(0)) & 15) == 0) {
        matched |= (uintptr_t)1 << 10;
    }
#endif
    return matched;
}
`
	path := filepath.Join(t.TempDir(), "call10.c")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("write call10 probe: %v", err)
	}
	library := filepath.Join(filepath.Dir(path), "libcall10.dylib")
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	if output, err := exec.Command("clang", "-arch", arch, "-dynamiclib", "-O2", "-fno-omit-frame-pointer", "-o", library, path).CombinedOutput(); err != nil {
		t.Fatalf("build call10 probe: %v\n%s", err, output)
	}
	handle, err := purego.Dlopen(library, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		t.Fatalf("dlopen call10 probe: %v", err)
	}
	defer func() {
		if err := purego.Dlclose(handle); err != nil {
			t.Errorf("dlclose call10 probe: %v", err)
		}
	}()
	probe, err := purego.Dlsym(handle, "reflektor_call10_probe")
	if err != nil {
		t.Fatalf("dlsym call10 probe: %v", err)
	}
	allArgumentsMatched := uintptr((1 << 10) - 1)
	if runtime.GOARCH == "amd64" {
		allArgumentsMatched |= 1 << 10
	}
	got := call10(probe, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa)
	if got != allArgumentsMatched {
		t.Fatalf("call10 matched argument bitmap = %#x, want %#x", got, allArgumentsMatched)
	}
}
