package reflektor

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/sliverarmory/reflektor/memmod"
)

var ErrLibraryClosed = errors.New("reflektor: library is closed")

type Library struct {
	mu     sync.RWMutex
	module *memmod.Module
	closed bool
}

// LoadLibrary loads a shared library image from memory.
func LoadLibrary(data []byte) (*Library, error) {
	if len(data) == 0 {
		return nil, errors.New("reflektor: empty library image")
	}

	module, err := memmod.LoadLibrary(data)
	if err != nil {
		return nil, fmt.Errorf("reflektor: load library: %w", err)
	}
	return &Library{module: module}, nil
}

// LoadLibraryFile loads a shared library image from disk into memory.
func LoadLibraryFile(path string) (*Library, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reflektor: read library file: %w", err)
	}
	return LoadLibrary(data)
}

// CallExport resolves and calls a zero-argument exported function.
func (library *Library) CallExport(name string) error {
	library.mu.RLock()
	defer library.mu.RUnlock()

	if library.closed || library.module == nil {
		return ErrLibraryClosed
	}
	if err := library.module.CallExport(name); err != nil {
		return fmt.Errorf("reflektor: call export %q: %w", name, err)
	}
	return nil
}

// Close releases library resources.
func (library *Library) Close() error {
	library.mu.Lock()
	defer library.mu.Unlock()

	if library.closed {
		return nil
	}
	library.closed = true

	if library.module != nil {
		library.module.Free()
		library.module = nil
	}
	return nil
}
