//go:build bof && windows && (386 || amd64 || arm64)

package reflektor_test

import "github.com/ebitengine/purego"

func newBOFOptionsTestCallback() uintptr {
	return purego.NewCallback(func(_ purego.CDecl) uintptr { return 0x42 })
}
