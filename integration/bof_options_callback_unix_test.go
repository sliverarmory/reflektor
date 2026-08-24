//go:build (darwin && !ios && (amd64 || arm64)) || (freebsd && (amd64 || arm64)) || (linux && !android && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64))

package reflektor_test

import "github.com/ebitengine/purego"

func newBOFOptionsTestCallback() uintptr {
	return purego.NewCallback(func() uintptr { return 0x42 })
}
