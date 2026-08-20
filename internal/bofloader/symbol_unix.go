//go:build bof && ((darwin && (amd64 || arm64)) || (linux && (386 || amd64 || arm64)))

package bofloader

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
)

var unixLibraryHandles sync.Map

func resolveSymbol(symbol string) (uintptr, error) {
	if address, ok, err := resolveBeaconCallback(symbol); err != nil {
		return 0, err
	} else if ok {
		return address, nil
	}

	name := normalizeImportedSymbol(symbol)
	if separator := strings.IndexByte(name, '$'); separator > 0 && separator < len(name)-1 {
		library := name[:separator]
		name = trimStdcallSuffix(name[separator+1:])
		handle, err := openUnixSystemLibrary(library)
		if err != nil {
			return 0, err
		}
		return purego.Dlsym(handle, name)
	}

	candidates := []string{name}
	if strings.HasPrefix(name, "_") {
		candidates = append(candidates, name[1:])
	}
	var resolutionErrors []error
	for _, candidate := range candidates {
		address, err := purego.Dlsym(purego.RTLD_DEFAULT, candidate)
		if err == nil && address != 0 {
			return address, nil
		}
		if err != nil {
			resolutionErrors = append(resolutionErrors, err)
		}
	}
	return 0, fmt.Errorf("system symbol %q was not found: %w", name, errors.Join(resolutionErrors...))
}

func openUnixSystemLibrary(name string) (uintptr, error) {
	if strings.ContainsAny(name, `/\\:`) || name == "." || name == ".." {
		return 0, fmt.Errorf("invalid system library name %q", name)
	}
	if existing, ok := unixLibraryHandles.Load(name); ok {
		return existing.(uintptr), nil
	}
	candidates := unixSystemLibraryCandidates(runtime.GOOS, name)
	var openErrors []error
	for _, candidate := range candidates {
		handle, err := purego.Dlopen(candidate, purego.RTLD_NOW|purego.RTLD_LOCAL)
		if err == nil && handle != 0 {
			actual, loaded := unixLibraryHandles.LoadOrStore(name, handle)
			if loaded {
				_ = purego.Dlclose(handle)
			}
			return actual.(uintptr), nil
		}
		if err != nil {
			openErrors = append(openErrors, err)
		}
	}
	return 0, fmt.Errorf("load system library %q: %w", name, errors.Join(openErrors...))
}

func unixSystemLibraryCandidates(goos, name string) []string {
	candidates := []string{name}
	switch goos {
	case "linux":
		if strings.Contains(name, ".so") {
			return candidates
		}
		stem := name
		if !strings.HasPrefix(stem, "lib") {
			stem = "lib" + stem
		}
		return append(candidates, stem+".so.6", stem+".so")
	case "darwin":
		if strings.HasSuffix(name, ".dylib") {
			return candidates
		}
		if name == "c" || name == "libc" || name == "System" || name == "libSystem" {
			return append(candidates, "/usr/lib/libSystem.B.dylib")
		}
		stem := name
		if !strings.HasPrefix(stem, "lib") {
			stem = "lib" + stem
		}
		return append(candidates, "/usr/lib/"+stem+".dylib")
	default:
		return candidates
	}
}
