//go:build (linux && !android && (amd64 || arm64)) || (freebsd && (amd64 || arm64))

package linuxmem

import (
	"debug/elf"
	"strings"
	"testing"

	"github.com/ebitengine/purego"
)

func TestResolveDynamicSymbolExactVersionFailsClosed(t *testing.T) {
	const unversionedAddress = uintptr(0xfeed)
	dlsymCalls := 0
	api := &linuxDynAPI{
		dlsym: purego.NewCallback(func(uintptr, uintptr) uintptr {
			dlsymCalls++
			return unversionedAddress
		}),
		dlvsym: purego.NewCallback(func(uintptr, uintptr, uintptr) uintptr {
			return 0
		}),
		defaultHandle: 1,
	}
	resolver := &symbolResolver{
		api:      api,
		resolved: make(map[string]uintptr),
		misses:   make(map[string]error),
		opened:   make(map[string]uintptr),
	}
	symbol := elf.Symbol{Name: "freebsd_exact", Version: "FBSD_MISSING"}

	address, err := resolver.resolveDynamicSymbol(symbol, true)
	if err == nil || !strings.Contains(err.Error(), "freebsd_exact@FBSD_MISSING") {
		t.Fatalf("exact-version resolution error = %v, want version-qualified failure", err)
	}
	if address != 0 {
		t.Fatalf("exact-version resolution address = %#x, want zero", address)
	}
	if dlsymCalls != 0 {
		t.Fatalf("exact-version resolution fell back to unversioned dlsym %d times", dlsymCalls)
	}

	address, err = resolver.resolveDynamicSymbol(symbol, false)
	if err != nil {
		t.Fatalf("non-strict resolution: %v", err)
	}
	if address != unversionedAddress {
		t.Fatalf("non-strict resolution address = %#x, want %#x", address, unversionedAddress)
	}
	if dlsymCalls != 1 {
		t.Fatalf("non-strict resolution dlsym calls = %d, want 1", dlsymCalls)
	}
}
