//go:build linux && (386 || amd64 || arm64)

package linuxmem

import (
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/sliverarmory/reflektor/native/internal/rejection"
	"golang.org/x/sys/unix"
)

func TestLoadLibraryDefensivelyRejectsGoImage(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	image, err := os.ReadFile(executable)
	if err != nil {
		t.Fatalf("read test executable: %v", err)
	}

	if _, err := LoadLibrary(image); !errors.Is(err, rejection.ErrGoSharedLibraryUnsupported) {
		t.Fatalf("LoadLibrary(Go image) error = %v, want ErrGoSharedLibraryUnsupported", err)
	}
}

func TestCollectELFFinalizersUsesABIOrder(t *testing.T) {
	mapping, err := unix.Mmap(-1, 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_PRIVATE|unix.MAP_ANON)
	if err != nil {
		t.Fatalf("mmap test image: %v", err)
	}
	defer unix.Munmap(mapping)
	base := uintptr(unsafe.Pointer(unsafe.SliceData(mapping)))
	class := elf.ELFCLASS64
	entrySize := 8
	if unsafe.Sizeof(uintptr(0)) == 4 {
		class = elf.ELFCLASS32
		entrySize = 4
	}

	const arrayOffset = 8
	arrayFunctions := []uintptr{base + 48, base + 64, base + 80}
	for index, function := range arrayFunctions {
		entry := base + arrayOffset + uintptr(index*entrySize)
		if entrySize == 8 {
			writeU64(entry, uint64(function))
		} else {
			writeU32(entry, uint32(function))
		}
	}

	mapped := mappedELF{mapping: mapping, loadBias: base}
	finalizers, err := collectELFFinalizers(mapped, class, dynamicInitInfo{
		fini:        96,
		finiArray:   arrayOffset,
		finiArraySz: uint64(len(arrayFunctions) * entrySize),
	})
	runtime.KeepAlive(mapping)
	if err != nil {
		t.Fatalf("collectELFFinalizers: %v", err)
	}
	want := []uintptr{base + 80, base + 64, base + 48, base + 96}
	if len(finalizers) != len(want) {
		t.Fatalf("finalizer count = %d, want %d", len(finalizers), len(want))
	}
	for index := range want {
		if finalizers[index] != want[index] {
			t.Fatalf("finalizers[%d] = %#x, want %#x", index, finalizers[index], want[index])
		}
	}
}

func TestModuleFreeRunsFinalizersOnce(t *testing.T) {
	var order []string
	first := purego.NewCallback(func() {
		order = append(order, "fini-1")
	})
	second := purego.NewCallback(func() {
		order = append(order, "fini-2")
	})
	module := &Module{
		finalizers:  []uintptr{first, second},
		dynamicAPI:  &linuxDynAPI{dlclose: 1},
		ownedDlopen: []uintptr{10, 20},
		closeDlopenHandle: func(_ *linuxDynAPI, handle uintptr) error {
			order = append(order, fmt.Sprintf("close-%d", handle))
			return nil
		},
	}

	module.Free()
	module.Free()
	want := []string{"fini-1", "fini-2", "close-20", "close-10"}
	if len(order) != len(want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("lifecycle order = %v, want %v", order, want)
		}
	}
}

func TestResolverOwnsNeededReferencesAndDeduplicatesAliases(t *testing.T) {
	var openCalls []string
	var closeCalls []uintptr
	nextHandle := uintptr(100)
	resolver := &symbolResolver{
		api:      &linuxDynAPI{dlopen: 1, dlclose: 1},
		modules:  []runtimeELFModule{{path: "/lib/libvisible.so"}},
		resolved: make(map[string]uintptr),
		misses:   make(map[string]error),
		opened:   make(map[string]uintptr),
		openLibrary: func(_ *linuxDynAPI, name string) (uintptr, error) {
			openCalls = append(openCalls, name)
			handle := nextHandle
			nextHandle++
			return handle, nil
		},
		closeLibrary: func(_ *linuxDynAPI, handle uintptr) error {
			closeCalls = append(closeCalls, handle)
			return nil
		},
	}

	if err := resolver.ensureLibraryLoaded("libvisible.so", false); err != nil {
		t.Fatalf("optional visible dependency: %v", err)
	}
	if len(openCalls) != 0 {
		t.Fatalf("optional visible dependency acquired a handle: %v", openCalls)
	}
	if err := resolver.ensureLibraryLoaded("libvisible.so", true); err != nil {
		t.Fatalf("owned visible dependency: %v", err)
	}
	if err := resolver.ensureLibraryLoaded("/alternate/libvisible.so", true); err != nil {
		t.Fatalf("owned alias: %v", err)
	}
	if err := resolver.ensureLibraryLoaded("libsecond.so", true); err != nil {
		t.Fatalf("second owned dependency: %v", err)
	}
	if len(openCalls) != 2 {
		t.Fatalf("dlopen calls = %v, want two distinct acquisitions", openCalls)
	}

	resolver.closeOwnedLibraries()
	wantClose := []uintptr{101, 100}
	if len(closeCalls) != len(wantClose) || closeCalls[0] != wantClose[0] || closeCalls[1] != wantClose[1] {
		t.Fatalf("dlclose calls = %v, want %v", closeCalls, wantClose)
	}
}

func TestResolverNeededFailureRetainsCleanupOwnership(t *testing.T) {
	var closeCalls []uintptr
	resolver := &symbolResolver{
		api:      &linuxDynAPI{dlopen: 1, dlclose: 1},
		resolved: make(map[string]uintptr),
		misses:   make(map[string]error),
		opened:   make(map[string]uintptr),
		openLibrary: func(_ *linuxDynAPI, name string) (uintptr, error) {
			if name == "libavailable.so" {
				return 77, nil
			}
			return 0, errors.New("not found")
		},
		closeLibrary: func(_ *linuxDynAPI, handle uintptr) error {
			closeCalls = append(closeCalls, handle)
			return nil
		},
	}

	err := resolver.primeNeededLibraries([]string{"libavailable.so", "libmissing.so"})
	if err == nil || !strings.Contains(err.Error(), `load DT_NEEDED "libmissing.so"`) {
		t.Fatalf("primeNeededLibraries error = %v, want missing DT_NEEDED", err)
	}
	resolver.closeOwnedLibraries()
	if len(closeCalls) != 1 || closeCalls[0] != 77 {
		t.Fatalf("failure cleanup dlclose calls = %v, want [77]", closeCalls)
	}
}
