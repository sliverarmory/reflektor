//go:build bof && windows && (386 || amd64 || arm64)

package bofloader

import (
	"reflect"
	"testing"
)

func TestWindowsVsnprintfCandidatesPreserveC99Semantics(t *testing.T) {
	want := []string{"vsnprintf"}
	if got := windowsFunctionCandidates("vsnprintf"); !reflect.DeepEqual(got, want) {
		t.Fatalf("windowsFunctionCandidates(vsnprintf) = %q, want %q; _vsnprintf is not C99-compatible", got, want)
	}
}
