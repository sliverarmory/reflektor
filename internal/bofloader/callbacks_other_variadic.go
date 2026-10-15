//go:build !darwin || ios || !arm64

package bofloader

import "errors"

func resolveMachOARM64VariadicCallback(string) (uintptr, error) {
	return 0, errors.New("Darwin arm64 variadic callbacks are unavailable on this host")
}
