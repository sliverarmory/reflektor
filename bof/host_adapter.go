package bof

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/sliverarmory/reflektor/internal/bofloader"
)

// TokenHandle is an opaque, host-owned handle supplied by a BOF. The adapter
// never opens, duplicates, or closes it.
type TokenHandle uintptr

// HostServices supplies selected Beacon APIs through Go functions. Every nil
// field leaves its corresponding import unavailable. A service runs
// synchronously on the thread executing the BOF entry point and must return
// promptly. It must not execute another BOF on that thread or close this BOF.
// A service error (or panic) becomes an Execute error after the entry returns.
// These APIs use the Beacon C ABI; their effects and handle ownership belong
// entirely to the host. Separate loaded objects can invoke services
// concurrently, so hosts that share state must synchronize it.
type HostServices struct {
	// UseToken is accepted only when RevertToken is also supplied. The
	// adapter schedules a revert before calling UseToken so errors and panics
	// cannot leave a partially applied token behind. RevertToken must be
	// idempotent and safe after a failed UseToken call.
	UseToken    func(TokenHandle) (bool, error)
	RevertToken func() error
	IsAdmin     func() (bool, error)

	// GetSpawnTo returns the host-selected path as bytes in the encoding
	// expected by the BOF. The adapter writes a NUL-terminated copy into
	// BeaconGetSpawnTo's buffer only when it fits in the supplied length.
	GetSpawnTo func(x86 bool) ([]byte, error)
}

// HostAdapter binds an immutable snapshot of host services to BOF load
// options. Create one adapter per stable service set and reuse it for loaded
// objects. Native callback registrations remain valid for the process lifetime
// because purego does not offer a callback-unregister API.
type HostAdapter struct {
	services  HostServices
	mu        sync.Mutex
	callbacks map[string]uintptr
}

// NewHostAdapter snapshots the service functions. Merely constructing an
// adapter does not register native callbacks.
func NewHostAdapter(services HostServices) *HostAdapter {
	return &HostAdapter{services: services}
}

// Apply returns load options that require every host-only Beacon import to
// have an explicitly registered service. Existing validation and resolution
// callbacks remain in effect for ordinary custom imports. The adapter's
// native callbacks are registered lazily after import validation.
func (adapter *HostAdapter) Apply(base LoadOptions) LoadOptions {
	validate := base.ValidateImports
	resolve := base.ResolveSymbol
	base.ValidateImports = func(imports []Import) error {
		for _, imported := range imports {
			if imported.RequiresHost && (adapter == nil || !adapter.provides(imported.Name)) {
				return fmt.Errorf("reflektor: host adapter has no service for %q", imported.Name)
			}
		}
		if validate != nil {
			return validate(imports)
		}
		return nil
	}
	base.ResolveSymbol = func(imported Import) (uintptr, bool, error) {
		if imported.RequiresHost {
			if adapter == nil || !adapter.provides(imported.Name) {
				return 0, false, nil
			}
			address, err := adapter.resolve(imported.Name)
			return address, true, err
		}
		if resolve != nil {
			return resolve(imported)
		}
		return 0, false, nil
	}
	return base
}

func canonicalHostService(name string) string {
	// Stdcall suffixes change the ABI and cannot be validated by the loader's
	// generic name normalization. Never bind one to a cdecl callback.
	if strings.ContainsRune(name, '@') {
		return ""
	}
	return bofloader.CanonicalBeaconImport(name)
}

func (adapter *HostAdapter) provides(name string) bool {
	switch canonicalHostService(name) {
	case "BeaconUseToken":
		return adapter.services.UseToken != nil && adapter.services.RevertToken != nil
	case "BeaconRevertToken":
		return adapter.services.RevertToken != nil
	case "BeaconIsAdmin":
		return adapter.services.IsAdmin != nil
	case "BeaconGetSpawnTo":
		return adapter.services.GetSpawnTo != nil
	default:
		return false
	}
}

func (adapter *HostAdapter) resolve(name string) (address uintptr, err error) {
	canonical := canonicalHostService(name)
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	defer func() {
		if recovered := recover(); recovered != nil {
			address = 0
			err = fmt.Errorf("register %s host callback: %v", canonical, recovered)
		}
	}()
	if existing := adapter.callbacks[canonical]; existing != 0 {
		return existing, nil
	}
	address, err = registerHostCallback(adapter, canonical)
	if err != nil {
		return 0, err
	}
	if address == 0 {
		return 0, errors.New("host callback registration returned a zero address")
	}
	if adapter.callbacks == nil {
		adapter.callbacks = make(map[string]uintptr)
	}
	adapter.callbacks[canonical] = address
	return address, nil
}

func (adapter *HostAdapter) useToken(handle uintptr) uintptr {
	return bofloader.InvokeEntryHostCallback("BeaconUseToken", func() (uintptr, error) {
		if err := bofloader.RegisterHostCleanup(adapter, "BeaconRevertToken", adapter.services.RevertToken); err != nil {
			return 0, err
		}
		ok, err := adapter.services.UseToken(TokenHandle(handle))
		if ok {
			return 1, err
		}
		return 0, err
	})
}

func (adapter *HostAdapter) revertToken() uintptr {
	return bofloader.InvokeEntryHostCallback("BeaconRevertToken", func() (uintptr, error) {
		err := adapter.services.RevertToken()
		if err == nil {
			bofloader.ClearHostCleanup(adapter)
		}
		return 0, err
	})
}

func (adapter *HostAdapter) isAdmin() uintptr {
	return bofloader.InvokeHostCallback("BeaconIsAdmin", func() (uintptr, error) {
		ok, err := adapter.services.IsAdmin()
		if ok {
			return 1, err
		}
		return 0, err
	})
}

//go:nocheckptr
func (adapter *HostAdapter) getSpawnTo(x86, buffer, length uintptr) uintptr {
	return bofloader.InvokeHostCallback("BeaconGetSpawnTo", func() (uintptr, error) {
		size := int32(length)
		if buffer == 0 || size <= 0 || size > 64<<10 {
			return 0, fmt.Errorf("invalid destination buffer or length %d", size)
		}
		path, err := adapter.services.GetSpawnTo(x86 != 0)
		if err != nil {
			return 0, err
		}
		if len(path) == 0 || len(path) >= int(size) || bytes.IndexByte(path, 0) >= 0 {
			return 0, fmt.Errorf("spawn path is empty, contains NUL, or exceeds %d-byte destination", size)
		}
		destination := unsafe.Slice((*byte)(unsafe.Pointer(buffer)), int(size))
		copy(destination, path)
		destination[len(path)] = 0
		return 0, nil
	})
}
