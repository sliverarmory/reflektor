//go:build bof && ((darwin && (amd64 || arm64)) || (linux && (386 || amd64 || arm64)))

package reflektor_test

import "github.com/ebitengine/purego"

func newBOFOptionsTestCallback() uintptr {
	return purego.NewCallback(func() uintptr { return 0x42 })
}
