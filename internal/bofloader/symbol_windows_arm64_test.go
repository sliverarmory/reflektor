//go:build windows && arm64

package bofloader

import "testing"

func TestNormalizeWindowsVsnprintfResult(t *testing.T) {
	tests := []struct {
		name   string
		result uintptr
		want   uintptr
	}{
		{name: "positive", result: 23, want: 23},
		{name: "minus one", result: uintptr(^uint32(0)), want: uintptr(^uint32(0))},
		{name: "other negative", result: uintptr(uint32(0xfffffffe)), want: uintptr(^uint32(0))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizeWindowsVsnprintfResult(test.result); got != test.want {
				t.Fatalf("normalizeWindowsVsnprintfResult(%#x) = %#x, want %#x", test.result, got, test.want)
			}
		})
	}
}
