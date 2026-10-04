//go:build freebsd && (amd64 || arm64)

package bofloader

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

func currentExecutionThreadID() (uint64, error) {
	var id int64
	_, _, errno := unix.Syscall(unix.SYS_THR_SELF, uintptr(unsafe.Pointer(&id)), 0, 0)
	runtime.KeepAlive(&id)
	if errno != 0 {
		return 0, fmt.Errorf("bofloader: get FreeBSD thread ID: %w", errno)
	}
	if id <= 0 {
		return 0, errors.New("bofloader: invalid FreeBSD thread ID")
	}
	return uint64(id), nil
}
