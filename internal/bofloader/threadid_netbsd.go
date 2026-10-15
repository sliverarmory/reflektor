//go:build netbsd && amd64

package bofloader

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func currentExecutionThreadID() (uint64, error) {
	id, _, errno := unix.Syscall(unix.SYS__LWP_SELF, 0, 0, 0)
	if errno != 0 {
		return 0, fmt.Errorf("bofloader: get NetBSD LWP ID: %w", errno)
	}
	if id == 0 {
		return 0, errors.New("bofloader: invalid NetBSD LWP ID")
	}
	return uint64(id), nil
}
