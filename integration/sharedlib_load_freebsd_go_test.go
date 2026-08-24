//go:build freebsd && amd64

package reflektor_test

import "testing"

func TestLoadGeneratedGoFreeBSDSOAndCallStartW(t *testing.T) {
	soPath := buildOneGoSharedLib(t, t.TempDir(), "freebsd", "amd64")
	runGoRuntimeFixtureSubprocess(t, soPath)
}
