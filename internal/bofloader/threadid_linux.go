//go:build linux && !android && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)

package bofloader

import (
	"errors"

	"golang.org/x/sys/unix"
)

func currentExecutionThreadID() (uint64, error) {
	id := unix.Gettid()
	if id <= 0 {
		return 0, errors.New("bofloader: invalid Linux thread ID")
	}
	return uint64(id), nil
}
