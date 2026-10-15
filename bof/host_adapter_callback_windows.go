//go:build windows && (386 || amd64 || arm64)

package bof

import (
	"fmt"

	"github.com/ebitengine/purego"
)

func registerHostCallback(adapter *HostAdapter, name string) (uintptr, error) {
	switch name {
	case "BeaconUseToken":
		return purego.NewCallback(func(_ purego.CDecl, token uintptr) uintptr {
			return adapter.useToken(token)
		}), nil
	case "BeaconRevertToken":
		return purego.NewCallback(func(_ purego.CDecl) uintptr {
			return adapter.revertToken()
		}), nil
	case "BeaconIsAdmin":
		return purego.NewCallback(func(_ purego.CDecl) uintptr {
			return adapter.isAdmin()
		}), nil
	case "BeaconGetSpawnTo":
		return purego.NewCallback(func(_ purego.CDecl, x86, buffer, length uintptr) uintptr {
			return adapter.getSpawnTo(x86, buffer, length)
		}), nil
	default:
		return 0, fmt.Errorf("unsupported host callback %q", name)
	}
}
