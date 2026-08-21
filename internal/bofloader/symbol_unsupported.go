//go:build (darwin && !(amd64 || arm64)) || (linux && !(386 || amd64 || arm64)) || (windows && !(386 || amd64 || arm64)) || (!darwin && !linux && !windows)

package bofloader

import "errors"

func resolveSymbol(symbol string) (uintptr, error) {
	return 0, errors.New("BOF symbol resolution is unsupported on this platform")
}
