//go:build bof && ((darwin && (amd64 || arm64)) || (linux && (386 || amd64 || arm64)))

package bofloader

import (
	"reflect"
	"testing"
)

func TestUnixSystemLibraryCandidatesPreserveLibPrefix(t *testing.T) {
	tests := []struct {
		name string
		goos string
		lib  string
		want []string
	}{
		{name: "linux short libc", goos: "linux", lib: "c", want: []string{"c", "libc.so.6", "libc.so"}},
		{name: "linux prefixed libc", goos: "linux", lib: "libc", want: []string{"libc", "libc.so.6", "libc.so"}},
		{name: "linux prefixed custom", goos: "linux", lib: "libcrypto", want: []string{"libcrypto", "libcrypto.so.6", "libcrypto.so"}},
		{name: "linux explicit soname", goos: "linux", lib: "libc.so.6", want: []string{"libc.so.6"}},
		{name: "darwin prefixed custom", goos: "darwin", lib: "libobjc", want: []string{"libobjc", "/usr/lib/libobjc.dylib"}},
		{name: "darwin system alias", goos: "darwin", lib: "libSystem", want: []string{"libSystem", "/usr/lib/libSystem.B.dylib"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := unixSystemLibraryCandidates(test.goos, test.lib); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("unixSystemLibraryCandidates(%q, %q) = %q, want %q", test.goos, test.lib, got, test.want)
			}
		})
	}
}

func TestUnixQualifiedLibraryCandidatesPreserveExactThenMachOSpelling(t *testing.T) {
	tests := []struct {
		name string
		want []string
	}{
		{name: "libc", want: []string{"libc"}},
		{name: "_libc", want: []string{"_libc", "libc"}},
	}
	for _, test := range tests {
		if got := unixQualifiedLibraryCandidates(test.name); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("unixQualifiedLibraryCandidates(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}
