//go:build (linux || windows) && (386 || amd64 || arm64)

package reflektor_test

import "testing"

func requireRecursiveLoaderPlatform(t *testing.T) {
	t.Helper()
}
