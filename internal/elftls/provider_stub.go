//go:build !linux || !amd64 || !cgo

package elftls

import "errors"

func available() bool { return false }

func register([]byte, uint64, uint64) (uint64, error) {
	return 0, errors.New("general PT_TLS requires Linux/amd64 with cgo")
}

func resolverAddress() uint64 { return 0 }

func release(uint64) {}
