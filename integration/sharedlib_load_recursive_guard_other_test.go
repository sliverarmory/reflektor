//go:build (linux && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)) || (windows && (386 || amd64 || arm64))

package reflektor_test

import "testing"

func requireRecursiveLoaderPlatform(t *testing.T) {
	t.Helper()
}
