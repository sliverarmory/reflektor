//go:build darwin && !ios && (amd64 || arm64)

package bofloader

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func currentExecutionThreadID() (uint64, error) {
	id, _, errno := unix.RawSyscall(unix.SYS_THREAD_SELFID, 0, 0, 0)
	if errno != 0 {
		return 0, fmt.Errorf("bofloader: get Darwin thread ID: %w", errno)
	}
	if id == 0 {
		return 0, errors.New("bofloader: invalid Darwin thread ID")
	}
	return uint64(id), nil
}
