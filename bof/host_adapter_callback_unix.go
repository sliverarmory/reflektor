//go:build (darwin && !ios && (amd64 || arm64)) || (freebsd && (amd64 || arm64)) || (linux && !android && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64))

package bof

import (
	"fmt"

	"github.com/ebitengine/purego"
)

func registerHostCallback(adapter *HostAdapter, name string) (uintptr, error) {
	switch name {
	case "BeaconUseToken":
		return purego.NewCallback(adapter.useToken), nil
	case "BeaconRevertToken":
		return purego.NewCallback(adapter.revertToken), nil
	case "BeaconIsAdmin":
		return purego.NewCallback(adapter.isAdmin), nil
	case "BeaconGetSpawnTo":
		return purego.NewCallback(adapter.getSpawnTo), nil
	default:
		return 0, fmt.Errorf("unsupported host callback %q", name)
	}
}
