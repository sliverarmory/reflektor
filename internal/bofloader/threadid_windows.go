//go:build windows && (386 || amd64 || arm64)

package bofloader

import (
	"errors"

	"golang.org/x/sys/windows"
)

func currentExecutionThreadID() (uint64, error) {
	id := windows.GetCurrentThreadId()
	if id == 0 {
		return 0, errors.New("bofloader: invalid Windows thread ID")
	}
	return uint64(id), nil
}
